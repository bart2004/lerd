package hostexec

import (
	"fmt"
	"os"
	"path/filepath"
)

// SyncShims creates hostBinsDir, removes stale symlinks not in allowed, and
// creates symlinks pointing each allowed binary name to clientBinPath.
func SyncShims(hostBinsDir, clientBinPath string, allowed []string) error {
	want := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		if err := ValidateBinName(name); err != nil {
			return err
		}
		want[name] = struct{}{}
	}

	if err := os.MkdirAll(hostBinsDir, 0755); err != nil {
		return fmt.Errorf("hostexec: create shims dir: %w", err)
	}

	entries, err := os.ReadDir(hostBinsDir)
	if err != nil {
		return fmt.Errorf("hostexec: read shims dir: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := want[name]; ok {
			continue
		}
		p := filepath.Join(hostBinsDir, name)
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("hostexec: remove stale shim %s: %w", name, err)
		}
	}

	for _, name := range allowed {
		linkPath := filepath.Join(hostBinsDir, name)
		if target, err := os.Readlink(linkPath); err == nil && target == clientBinPath {
			continue
		}
		if info, err := os.Lstat(linkPath); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("hostexec: refusing to overwrite non-symlink %s", linkPath)
			}
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("hostexec: replace shim %s: %w", name, err)
			}
		}
		if err := os.Symlink(clientBinPath, linkPath); err != nil {
			return fmt.Errorf("hostexec: symlink %s: %w", name, err)
		}
	}

	return nil
}
