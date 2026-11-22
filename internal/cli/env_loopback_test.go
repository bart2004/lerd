package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestUsesLoopbackServicesConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	site := &config.Site{Name: "demo"}

	// Without lerd-loopback binary and without explicit config, containerized
	// sites default to false (preserving upstream test expectations).
	if usesLoopbackServices(site) {
		t.Fatal("expected false without lerd-loopback binary or explicit config")
	}

	// Installing the lerd-loopback binary enables loopback mode by default.
	bin := config.LoopbackBinPath()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !usesLoopbackServices(site) {
		t.Fatal("expected true when lerd-loopback binary is present")
	}

	// Explicit false overrides even when the binary is installed.
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	cfg.PHP.LoopbackHosts = &disabled
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
	if usesLoopbackServices(site) {
		t.Fatal("expected false when loopback_hosts is explicitly false")
	}

	// Explicit true enables it regardless of binary presence.
	_ = os.Remove(bin)
	enabled := true
	cfg.PHP.LoopbackHosts = &enabled
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
	if !usesLoopbackServices(site) {
		t.Fatal("expected true when loopback_hosts is explicitly true")
	}
}
