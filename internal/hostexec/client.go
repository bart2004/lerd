package hostexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ClientOptions configures how Run connects to the host-exec server and stages
// container-local /tmp arguments.
type ClientOptions struct {
	SocketPath string
	Addr       string
	Token      string
	StagingDir string
	// TmpPrefix is the container-local temp prefix to stage (defaults to "/tmp").
	TmpPrefix string
	Env       []string
}

type stagedFile struct {
	origPath   string
	stagedPath string
	existed    bool
	beforeSize int64
	beforeMod  time.Time
}

// Run executes bin on the host through the host-exec server, streaming stdio
// and transparently staging any container-local /tmp file arguments.
func Run(
	ctx context.Context,
	opts ClientOptions,
	bin string,
	args []string,
	cwd string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) (int, error) {
	stagedArgs, callDir, stagedFiles, err := stageTmpArgs(args, opts.StagingDir, opts.TmpPrefix)
	if err != nil {
		return 1, err
	}
	if callDir != "" {
		defer os.RemoveAll(callDir)
	}

	conn, err := dialServer(ctx, opts)
	if err != nil {
		return 1, err
	}
	defer conn.Close()

	if ctx != nil && ctx.Done() != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-done:
			}
		}()
	}

	var writeMu sync.Mutex
	req := Request{
		Token: opts.Token,
		Bin:   bin,
		Args:  stagedArgs,
		Cwd:   cwd,
		Env:   opts.Env,
	}
	if err := writeJSONLine(conn, &writeMu, req); err != nil {
		return 1, fmt.Errorf("hostexec: send request: %w", err)
	}

	go func() {
		if stdin != nil {
			buf := make([]byte, chunkSize)
			for {
				n, rErr := stdin.Read(buf)
				if n > 0 {
					if wErr := writeJSONLine(conn, &writeMu, Frame{
						Type: FrameStdin,
						Data: buf[:n],
					}); wErr != nil {
						return
					}
				}
				if rErr != nil {
					break
				}
			}
		}
		_ = writeJSONLine(conn, &writeMu, Frame{Type: FrameStdinEOF})
	}()

	scanner := newLineScanner(conn)
	exitCode := 1
	gotExit := false
	var remoteErr string

	for scanner.Scan() {
		var f Frame
		if err := json.Unmarshal(scanner.Bytes(), &f); err != nil {
			return 1, fmt.Errorf("hostexec: decode frame: %w", err)
		}
		switch f.Type {
		case FrameStdout:
			if stdout != nil && len(f.Data) > 0 {
				if _, err := stdout.Write(f.Data); err != nil {
					return 1, err
				}
			}
		case FrameStderr:
			if stderr != nil && len(f.Data) > 0 {
				if _, err := stderr.Write(f.Data); err != nil {
					return 1, err
				}
			}
		case FrameExit:
			exitCode = f.Code
			remoteErr = f.Error
			gotExit = true
		}
		if gotExit {
			break
		}
	}

	if !gotExit {
		if err := scanner.Err(); err != nil {
			return 1, fmt.Errorf("hostexec: read response: %w", err)
		}
		if ctx != nil && ctx.Err() != nil {
			return 1, ctx.Err()
		}
		return 1, errors.New("hostexec: connection closed before exit frame")
	}

	if err := restoreStagedOutputs(stagedFiles); err != nil {
		return 1, err
	}

	if remoteErr != "" {
		return exitCode, errors.New(remoteErr)
	}
	return exitCode, nil
}

func dialServer(ctx context.Context, opts ClientOptions) (net.Conn, error) {
	var d net.Dialer
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.SocketPath != "" {
		return d.DialContext(ctx, "unix", opts.SocketPath)
	}
	if opts.Addr != "" {
		return d.DialContext(ctx, "tcp", opts.Addr)
	}
	return nil, errors.New("hostexec: neither socket path nor tcp address configured")
}

func stageTmpArgs(args []string, stagingRoot, tmpPrefix string) ([]string, string, []stagedFile, error) {
	if stagingRoot == "" || len(args) == 0 {
		return args, "", nil, nil
	}
	if tmpPrefix == "" {
		tmpPrefix = "/tmp"
	}
	tmpPrefix = strings.TrimRight(tmpPrefix, "/")
	matchPrefix := tmpPrefix + "/"

	outArgs := make([]string, len(args))
	copy(outArgs, args)

	var callDir string
	byOrig := map[string]string{}
	var staged []stagedFile

	ensureCallDir := func() error {
		if callDir != "" {
			return nil
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		callDir = filepath.Join(stagingRoot, id)
		return os.MkdirAll(callDir, 0770)
	}

	for i, arg := range args {
		flagPrefix, origPath, ok := splitTmpArg(arg, matchPrefix)
		if !ok {
			continue
		}
		if existing, seen := byOrig[origPath]; seen {
			outArgs[i] = flagPrefix + existing
			continue
		}

		info, err := os.Stat(origPath)
		if err != nil && !os.IsNotExist(err) {
			continue
		}
		if err == nil && info.IsDir() {
			continue
		}

		if err := ensureCallDir(); err != nil {
			return nil, "", nil, fmt.Errorf("hostexec: create staging dir: %w", err)
		}

		base := filepath.Base(origPath)
		if err == nil {
			stagedPath := filepath.Join(callDir, fmt.Sprintf("in_%d_%s", i, base))
			if err := copyFile(origPath, stagedPath, info.Mode().Perm()); err != nil {
				_ = os.RemoveAll(callDir)
				return nil, "", nil, fmt.Errorf("hostexec: stage input %s: %w", origPath, err)
			}
			afterCopy, _ := os.Stat(stagedPath)
			sf := stagedFile{
				origPath:   origPath,
				stagedPath: stagedPath,
				existed:    true,
			}
			if afterCopy != nil {
				sf.beforeSize = afterCopy.Size()
				sf.beforeMod = afterCopy.ModTime()
			}
			staged = append(staged, sf)
			byOrig[origPath] = stagedPath
			outArgs[i] = flagPrefix + stagedPath
		} else {
			stagedPath := filepath.Join(callDir, fmt.Sprintf("out_%d_%s", i, base))
			staged = append(staged, stagedFile{
				origPath:   origPath,
				stagedPath: stagedPath,
				existed:    false,
			})
			byOrig[origPath] = stagedPath
			outArgs[i] = flagPrefix + stagedPath
		}
	}

	return outArgs, callDir, staged, nil
}

func splitTmpArg(arg, matchPrefix string) (flagPrefix, path string, ok bool) {
	if strings.HasPrefix(arg, matchPrefix) {
		return "", arg, true
	}
	if eq := strings.IndexByte(arg, '='); eq > 0 && strings.HasPrefix(arg[eq+1:], matchPrefix) {
		return arg[:eq+1], arg[eq+1:], true
	}
	return "", "", false
}

// restoreStagedOutputs copies newly created or modified staged files back to
// their original container-local paths (covering both non-existent output paths
// and pre-created tempnam() files overwritten in place).
func restoreStagedOutputs(files []stagedFile) error {
	for _, sf := range files {
		info, err := os.Stat(sf.stagedPath)
		if err != nil || info.IsDir() {
			continue
		}
		if sf.existed && info.Size() == sf.beforeSize && !info.ModTime().After(sf.beforeMod) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(sf.origPath), 0755); err != nil {
			return fmt.Errorf("hostexec: create output dir for %s: %w", sf.origPath, err)
		}
		if err := copyFile(sf.stagedPath, sf.origPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("hostexec: copy output to %s: %w", sf.origPath, err)
		}
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if perm == 0 {
		perm = 0644
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func randomID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
