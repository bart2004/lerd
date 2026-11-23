package hostexec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortSocketPath creates a Unix socket path under /tmp so macOS stays well
// below its 104-byte sun_path limit (t.TempDir under /var/folders exceeds it).
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lerd-he-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func TestServerAndClient_UnixEndToEnd(t *testing.T) {
	sock := shortSocketPath(t)
	srv, err := Listen(context.Background(), ServerConfig{
		ListenNetwork: "unix",
		ListenAddr:    sock,
		Allowlist:     func() []string { return []string{"sh"} },
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0660 {
		t.Errorf("socket perm = %o, want 0660", perm)
	}

	var stdout, stderr bytes.Buffer
	code, err := Run(
		context.Background(),
		ClientOptions{SocketPath: sock},
		"sh",
		[]string{"-c", "cat; echo err-line >&2; exit 42"},
		t.TempDir(),
		strings.NewReader("hello from stdin\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
	if stdout.String() != "hello from stdin\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello from stdin\n")
	}
	if stderr.String() != "err-line\n" {
		t.Errorf("stderr = %q, want %q", stderr.String(), "err-line\n")
	}
}

func TestServer_RejectsDisallowedAndTraversal(t *testing.T) {
	sock := shortSocketPath(t)
	srv, err := Listen(context.Background(), ServerConfig{
		ListenNetwork: "unix",
		ListenAddr:    sock,
		Allowlist:     func() []string { return []string{"echo"} },
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	cases := []struct {
		name    string
		bin     string
		wantSub string
	}{
		{"disallowed", "sh", "not allowlisted"},
		{"parent traversal", "../sh", "invalid binary name"},
		{"absolute path", "/bin/sh", "invalid binary name"},
		{"subpath", "dir/echo", "invalid binary name"},
		{"dotdot", "..", "invalid binary name"},
		{"empty", "", "invalid binary name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := Run(context.Background(), ClientOptions{SocketPath: sock}, tc.bin, nil, "", nil, nil, nil)
			if err == nil {
				t.Fatalf("expected error for %q, got nil (code %d)", tc.bin, code)
			}
			if code == 0 {
				t.Errorf("expected non-zero exit code for %q", tc.bin)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestServer_TCPTokenAuth(t *testing.T) {
	if _, err := Listen(context.Background(), ServerConfig{
		ListenNetwork: "tcp",
		ListenAddr:    "127.0.0.1:0",
		Allowlist:     func() []string { return []string{"sh"} },
	}); err == nil {
		t.Fatal("expected error starting TCP listener without token")
	}

	srv, err := Listen(context.Background(), ServerConfig{
		ListenNetwork: "tcp",
		ListenAddr:    "127.0.0.1:0",
		Token:         "secret-token-123",
		Allowlist:     func() []string { return []string{"sh"} },
	})
	if err != nil {
		t.Fatalf("Listen TCP: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	for _, badToken := range []string{"", "wrong-token"} {
		code, err := Run(context.Background(), ClientOptions{
			Addr:  srv.Addr(),
			Token: badToken,
		}, "sh", []string{"-c", "echo ok"}, "", nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Errorf("token %q: got code=%d err=%v, want unauthorized", badToken, code, err)
		}
	}

	var stdout bytes.Buffer
	code, err := Run(context.Background(), ClientOptions{
		Addr:  srv.Addr(),
		Token: "secret-token-123",
	}, "sh", []string{"-c", "printf ok"}, "", nil, &stdout, nil)
	if err != nil || code != 0 || stdout.String() != "ok" {
		t.Fatalf("valid token: code=%d err=%v stdout=%q", code, err, stdout.String())
	}
}

func TestClient_TmpStaging(t *testing.T) {
	sock := shortSocketPath(t)
	srv, err := Listen(context.Background(), ServerConfig{
		ListenNetwork: "unix",
		ListenAddr:    sock,
		Allowlist:     func() []string { return []string{"sh"} },
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	fakeTmp := t.TempDir()
	stagingDir := t.TempDir()

	inFile := filepath.Join(fakeTmp, "input.html")
	if err := os.WriteFile(inFile, []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	preCreatedOut := filepath.Join(fakeTmp, "precreated.pdf")
	if err := os.WriteFile(preCreatedOut, nil, 0644); err != nil {
		t.Fatalf("write precreated: %v", err)
	}
	newOut := filepath.Join(fakeTmp, "out.pdf")
	flagOut := filepath.Join(fakeTmp, "flag.pdf")

	// Verify the host receives paths under stagingDir (not fakeTmp), reads the
	// staged input, and writes to all three output paths.
	script := `
set -e
in="$1"
out="$2"
pre="$3"
flag="${4#--out=}"
case "$in" in "$5"/*) ;; *) echo "in not staged: $in" >&2; exit 2 ;; esac
case "$out" in "$5"/*) ;; *) echo "out not staged: $out" >&2; exit 3 ;; esac
case "$pre" in "$5"/*) ;; *) echo "pre not staged: $pre" >&2; exit 4 ;; esac
case "$flag" in "$5"/*) ;; *) echo "flag not staged: $flag" >&2; exit 5 ;; esac
content=$(cat "$in")
printf "PDF:%s" "$content" > "$out"
printf "PRE:%s" "$content" > "$pre"
printf "FLAG:%s" "$content" > "$flag"
`
	var stderr bytes.Buffer
	code, err := Run(
		context.Background(),
		ClientOptions{
			SocketPath: sock,
			StagingDir: stagingDir,
			TmpPrefix:  fakeTmp,
		},
		"sh",
		[]string{"-c", script, "sh", inFile, newOut, preCreatedOut, "--out=" + flagOut, stagingDir},
		"",
		nil,
		nil,
		&stderr,
	)
	if err != nil || code != 0 {
		t.Fatalf("Run failed: code=%d err=%v stderr=%s", code, err, stderr.String())
	}

	checks := map[string]string{
		newOut:        "PDF:<h1>hello</h1>",
		preCreatedOut: "PRE:<h1>hello</h1>",
		flagOut:       "FLAG:<h1>hello</h1>",
	}
	for path, want := range checks {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", filepath.Base(path), string(got), want)
		}
	}

	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		t.Fatalf("read stagingDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("staging dir not cleaned up, remaining entries: %d", len(entries))
	}
}

func TestSyncShims(t *testing.T) {
	dir := t.TempDir()
	clientBin := "/usr/local/bin/lerd-host-exec"

	if err := SyncShims(dir, clientBin, []string{"wkhtmltopdf", "pdftotext"}); err != nil {
		t.Fatalf("SyncShims initial: %v", err)
	}

	for _, name := range []string{"wkhtmltopdf", "pdftotext"} {
		target, err := os.Readlink(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("readlink %s: %v", name, err)
		}
		if target != clientBin {
			t.Errorf("%s target = %q, want %q", name, target, clientBin)
		}
	}

	if err := SyncShims(dir, clientBin, []string{"wkhtmltopdf", "gs"}); err != nil {
		t.Fatalf("SyncShims update: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(dir, "pdftotext")); !os.IsNotExist(err) {
		t.Errorf("expected stale symlink pdftotext to be removed")
	}
	for _, name := range []string{"wkhtmltopdf", "gs"} {
		target, err := os.Readlink(filepath.Join(dir, name))
		if err != nil || target != clientBin {
			t.Errorf("%s target = %q (err=%v), want %q", name, target, err, clientBin)
		}
	}

	if err := SyncShims(dir, clientBin, []string{"../evil"}); err == nil {
		t.Fatal("expected error for invalid binary name in SyncShims")
	}
}
