package hostexec

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ServerConfig configures the host-side exec RPC server.
type ServerConfig struct {
	ListenNetwork string
	ListenAddr    string
	Token         string
	Allowlist     func() []string
	ResolveBin    func(name string) (string, error)
}

// Server accepts host-exec RPC connections from containers and executes
// allowlisted host binaries with streaming stdio.
type Server struct {
	network    string
	addr       string
	socketPath string
	token      string
	allowlist  func() []string
	resolveBin func(string) (string, error)

	ln     net.Listener
	wg     sync.WaitGroup
	closed chan struct{}
	once   sync.Once
	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

// Listen starts a host-exec RPC server on the configured Unix socket or TCP address.
func Listen(ctx context.Context, cfg ServerConfig) (*Server, error) {
	network := cfg.ListenNetwork
	if network == "" {
		network = "unix"
	}
	addr := cfg.ListenAddr
	if addr == "" {
		return nil, errors.New("hostexec: listen address is required")
	}
	if network == "tcp" && cfg.Token == "" {
		return nil, errors.New("hostexec: token is required for tcp listener")
	}
	if network == "unix" {
		if err := os.MkdirAll(filepath.Dir(addr), 0755); err != nil {
			return nil, fmt.Errorf("hostexec: create socket dir: %w", err)
		}
		if err := removeStaleUnixSocket(addr); err != nil {
			return nil, fmt.Errorf("hostexec: %w", err)
		}
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("hostexec: listen %s %s: %w", network, addr, err)
	}
	if network == "unix" {
		if err := os.Chmod(addr, 0660); err != nil {
			_ = ln.Close()
			_ = removeStaleUnixSocket(addr)
			return nil, fmt.Errorf("hostexec: chmod socket: %w", err)
		}
	}

	resolve := cfg.ResolveBin
	if resolve == nil {
		resolve = exec.LookPath
	}

	s := &Server{
		network:    network,
		addr:       ln.Addr().String(),
		token:      cfg.Token,
		allowlist:  cfg.Allowlist,
		resolveBin: resolve,
		ln:         ln,
		closed:     make(chan struct{}),
	}
	if network == "unix" {
		s.socketPath = addr
	}

	s.wg.Add(1)
	go s.acceptLoop()

	if ctx != nil && ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			_ = s.Close()
		}()
	}

	return s, nil
}

// Addr returns the bound listener address.
func (s *Server) Addr() string { return s.addr }

// Close stops accepting connections, aborts active calls, and removes the Unix socket.
func (s *Server) Close() error {
	s.once.Do(func() {
		s.connMu.Lock()
		close(s.closed)
		for c := range s.conns {
			_ = c.Close()
		}
		s.connMu.Unlock()
		if s.ln != nil {
			_ = s.ln.Close()
		}
	})
	s.wg.Wait()
	if s.socketPath != "" {
		_ = removeStaleUnixSocket(s.socketPath)
	}
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if !s.track(conn) {
			_ = conn.Close()
			return
		}
		s.wg.Add(1)
		go s.handle(conn)
	}
}

func (s *Server) track(conn net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	select {
	case <-s.closed:
		return false
	default:
	}
	if s.conns == nil {
		s.conns = map[net.Conn]struct{}{}
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.connMu.Lock()
	delete(s.conns, conn)
	s.connMu.Unlock()
}

func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer s.untrack(conn)
	defer conn.Close()

	var writeMu sync.Mutex
	sendExit := func(code int, errMsg string) {
		_ = writeJSONLine(conn, &writeMu, Frame{
			Type:  FrameExit,
			Code:  code,
			Error: errMsg,
		})
	}

	scanner := newLineScanner(conn)
	if !scanner.Scan() {
		return
	}
	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		sendExit(126, fmt.Sprintf("invalid request: %v", err))
		return
	}

	if s.token != "" {
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
			sendExit(126, "unauthorized: invalid host-exec token")
			return
		}
	}

	if err := ValidateBinName(req.Bin); err != nil {
		sendExit(126, err.Error())
		return
	}

	if !s.isAllowed(req.Bin) {
		sendExit(126, fmt.Sprintf("host binary %q is not allowlisted", req.Bin))
		return
	}

	binPath, err := s.resolveBin(req.Bin)
	if err != nil {
		sendExit(127, fmt.Sprintf("host binary %q not found: %v", req.Bin, err))
		return
	}

	cmdCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binPath, req.Args...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	if len(req.Env) > 0 {
		cmd.Env = mergeSafeEnv(os.Environ(), req.Env)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		sendExit(1, fmt.Sprintf("stdin pipe: %v", err))
		return
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		sendExit(1, fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		sendExit(1, fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		sendExit(127, fmt.Sprintf("start %s: %v", req.Bin, err))
		return
	}

	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		defer stdinPipe.Close()
		for scanner.Scan() {
			var f Frame
			if err := json.Unmarshal(scanner.Bytes(), &f); err != nil {
				cancel()
				return
			}
			switch f.Type {
			case FrameStdin:
				if len(f.Data) > 0 {
					if _, err := stdinPipe.Write(f.Data); err != nil {
						return
					}
				}
			case FrameStdinEOF:
				return
			}
		}
		cancel()
	}()

	var ioWG sync.WaitGroup
	streamPipe := func(r io.Reader, frameType string) {
		defer ioWG.Done()
		buf := make([]byte, chunkSize)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if wErr := writeJSONLine(conn, &writeMu, Frame{
					Type: frameType,
					Data: buf[:n],
				}); wErr != nil {
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	ioWG.Add(2)
	go streamPipe(stdoutPipe, FrameStdout)
	go streamPipe(stderrPipe, FrameStderr)
	ioWG.Wait()

	waitErr := cmd.Wait()
	code := 0
	errMsg := ""
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 1
			errMsg = waitErr.Error()
		}
	}

	sendExit(code, errMsg)
	_ = conn.Close()
	<-stdinDone
}

func (s *Server) isAllowed(bin string) bool {
	if s.allowlist == nil {
		return false
	}
	return slices.Contains(s.allowlist(), bin)
}

func mergeSafeEnv(base, extra []string) []string {
	out := slices.Clone(base)
	for _, kv := range extra {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || k == "PATH" || strings.HasPrefix(k, "LD_") || strings.HasPrefix(k, "DYLD_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func removeStaleUnixSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket file %s", path)
	}
	return os.Remove(path)
}
