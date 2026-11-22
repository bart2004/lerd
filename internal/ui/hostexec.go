package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/hostbin"
	"github.com/geodro/lerd/internal/hostexec"
	"github.com/geodro/lerd/internal/serviceops"
)

var hostExecServer atomic.Pointer[hostexec.Server]

// startHostExecServer binds the host-exec bridge so allowlisted host binaries
// (lerd php:host-bin) can be invoked from inside PHP containers.
func startHostExecServer() {
	network := config.HostExecListenNetwork()
	addr := config.HostExecListenAddr()
	if network == "unix" {
		if err := os.MkdirAll(filepath.Dir(addr), 0o755); err != nil {
			fmt.Printf("[WARN] creating run dir for host-exec socket: %v\n", err)
			return
		}
	}

	var token string
	if network == "tcp" {
		t, err := ensureHostExecToken(config.HostExecTokenFile())
		if err != nil {
			fmt.Printf("[WARN] creating host-exec token: %v\n", err)
			return
		}
		token = t
	}

	srv, err := hostexec.Listen(context.Background(), hostexec.ServerConfig{
		ListenNetwork: network,
		ListenAddr:    addr,
		Token:         token,
		Allowlist: func() []string {
			cfg, err := config.LoadGlobal()
			if err != nil || cfg == nil {
				return nil
			}
			return cfg.PHP.HostBins
		},
		ResolveBin: func(name string) (string, error) {
			if p, ok := hostbin.Look(name); ok {
				return p, nil
			}
			return "", fmt.Errorf("host binary %q not found", name)
		},
	})
	if err != nil {
		fmt.Printf("[WARN] host-exec bridge: %v\n", err)
		return
	}
	hostExecServer.Store(srv)
}

func ensureHostExecToken(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf[:])
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// syncLoopbackOnStart refreshes loopback-ports.json so lerd-loopback inside
// PHP containers immediately picks up current service port mappings.
func syncLoopbackOnStart() {
	if err := serviceops.SyncLoopbackPortMap(); err != nil {
		fmt.Printf("[WARN] syncing loopback port map: %v\n", err)
	}
}
