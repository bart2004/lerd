package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/geodro/lerd/internal/hostexec"
)

func main() {
	invoked := filepath.Base(os.Args[0])
	var bin string
	var args []string
	if invoked == "lerd-host-exec" {
		if len(os.Args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: lerd-host-exec <binary> [args...]")
			os.Exit(2)
		}
		bin = os.Args[1]
		args = os.Args[2:]
	} else {
		bin = invoked
		args = os.Args[1:]
	}

	runDir := defaultRunDir()
	opts := hostexec.ClientOptions{
		SocketPath: os.Getenv(hostexec.EnvSocket),
		Addr:       os.Getenv(hostexec.EnvAddr),
		Token:      os.Getenv(hostexec.EnvToken),
		StagingDir: os.Getenv(hostexec.EnvStagingDir),
	}
	if opts.SocketPath == "" && opts.Addr == "" && runDir != "" {
		opts.SocketPath = filepath.Join(runDir, hostexec.DefaultSocketName)
	}
	if opts.StagingDir == "" && runDir != "" {
		opts.StagingDir = filepath.Join(runDir, hostexec.DefaultStagingDirName)
	}

	cwd, _ := os.Getwd()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	code, err := hostexec.Run(ctx, opts, bin, args, cwd, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lerd-host-exec: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func defaultRunDir() string {
	// Inside PHP-FPM containers HOME is /root while lerd-host-exec lives under
	// the host user's <DataDir>/bin/lerd-host-exec (mounted via %h:%h). Resolve
	// symlinks from the executable path first so the host's run dir is found.
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			candidate := filepath.Join(filepath.Dir(filepath.Dir(resolved)), "run")
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				return candidate
			}
		}
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "lerd", "run")
}
