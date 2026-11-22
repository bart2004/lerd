package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestPHPHostBinCmd_AddListRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	origLook := lookHostBin
	t.Cleanup(func() { lookHostBin = origLook })
	lookHostBin = func(name string) (string, bool) {
		if name == "wkhtmltopdf" {
			return "/usr/bin/wkhtmltopdf", true
		}
		return "", false
	}

	// Reject binary not found on host.
	cmd := NewPHPHostBinCmd()
	cmd.SetArgs([]string{"add", "missing-tool"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error adding binary not found on host")
	}

	// Reject invalid binary name.
	cmd = NewPHPHostBinCmd()
	cmd.SetArgs([]string{"add", "../sh"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for path traversal name")
	}

	// Add valid host binary.
	var addOut bytes.Buffer
	cmd = NewPHPHostBinCmd()
	cmd.SetArgs([]string{"add", "wkhtmltopdf"})
	cmd.SetOut(&addOut)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add wkhtmltopdf: %v", err)
	}
	if !strings.Contains(addOut.String(), "/usr/bin/wkhtmltopdf") {
		t.Errorf("add output = %q, want resolved path", addOut.String())
	}

	shimPath := filepath.Join(config.HostBinsDir(), "wkhtmltopdf")
	target, err := os.Readlink(shimPath)
	if err != nil {
		t.Fatalf("readlink shim: %v", err)
	}
	if target != config.HostExecClientBinPath() {
		t.Errorf("shim target = %q, want %q", target, config.HostExecClientBinPath())
	}

	// List shows allowlisted binary and its host path.
	var listOut bytes.Buffer
	cmd = NewPHPHostBinCmd()
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(&listOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut.String(), "wkhtmltopdf\t/usr/bin/wkhtmltopdf") {
		t.Errorf("list output = %q, want wkhtmltopdf with host path", listOut.String())
	}

	// Remove deletes from allowlist and prunes shim.
	var rmOut bytes.Buffer
	cmd = NewPHPHostBinCmd()
	cmd.SetArgs([]string{"remove", "wkhtmltopdf"})
	cmd.SetOut(&rmOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Lstat(shimPath); !os.IsNotExist(err) {
		t.Errorf("expected shim %s to be removed", shimPath)
	}

	// List after remove reports empty.
	listOut.Reset()
	cmd = NewPHPHostBinCmd()
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(&listOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if !strings.Contains(listOut.String(), "No host binaries allowlisted") {
		t.Errorf("empty list output = %q", listOut.String())
	}
}
