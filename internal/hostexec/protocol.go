package hostexec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

const (
	EnvSocket     = "LERD_HOST_EXEC_SOCK"
	EnvAddr       = "LERD_HOST_EXEC_ADDR"
	EnvToken      = "LERD_HOST_EXEC_TOKEN"
	EnvStagingDir = "LERD_HOST_EXEC_TMP"

	DefaultSocketName     = "lerd-host-exec.sock"
	DefaultStagingDirName = "host-exec-tmp"
	DefaultTCPPort        = "9914"

	FrameStdin    = "stdin"
	FrameStdinEOF = "stdin_eof"
	FrameStdout   = "stdout"
	FrameStderr   = "stderr"
	FrameExit     = "exit"

	chunkSize      = 32 << 10 // 32 KiB
	initialLineBuf = 64 << 10 // 64 KiB
	maxLineBytes   = 4 << 20  // 4 MiB
)

// Request is the first NDJSON line sent by the container client.
type Request struct {
	Token string   `json:"token,omitempty"`
	Bin   string   `json:"bin"`
	Args  []string `json:"args,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Env   []string `json:"env,omitempty"`
}

// Frame carries streaming stdio chunks or the final exit status.
type Frame struct {
	Type  string `json:"type"`
	Data  []byte `json:"data,omitempty"`
	Code  int    `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// ValidateBinName ensures name is a plain executable basename without path
// separators, traversal segments, or control characters.
func ValidateBinName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid binary name %q", name)
	}
	if strings.ContainsAny(name, "/\\\x00 \t\r\n") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid binary name %q: must be a plain basename", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid binary name %q: must be a plain basename", name)
	}
	return nil
}

func writeJSONLine(w io.Writer, mu *sync.Mutex, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_, err = w.Write(b)
	return err
}

func newLineScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, initialLineBuf), maxLineBytes)
	s.Split(scanTerminatedLines)
	return s
}

func scanTerminatedLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, bytes.TrimSuffix(data[:i], []byte{'\r'}), nil
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimSuffix(data, []byte{'\r'}), nil
	}
	return 0, nil, nil
}
