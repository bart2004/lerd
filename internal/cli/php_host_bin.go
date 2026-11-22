package cli

import (
	"fmt"
	"slices"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/hostbin"
	"github.com/geodro/lerd/internal/hostexec"
	"github.com/spf13/cobra"
)

// Seams for tests.
var (
	lookHostBin      = hostbin.Look
	loadHostBins     = defaultLoadHostBins
	saveHostBins     = defaultSaveHostBins
	hostBinsDir      = config.HostBinsDir
	hostExecClientAt = config.HostExecClientBinPath
)

// NewPHPHostBinCmd returns the php:host-bin parent command for managing host
// binaries callable from PHP containers via the host-exec bridge.
func NewPHPHostBinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php:host-bin",
		Short: "Manage host binaries callable from PHP containers",
	}
	cmd.AddCommand(newPHPHostBinAddCmd())
	cmd.AddCommand(newPHPHostBinRemoveCmd())
	cmd.AddCommand(newPHPHostBinListCmd())
	return cmd
}

func newPHPHostBinAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name>",
		Short: "Allow a host binary to be invoked from PHP containers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := hostexec.ValidateBinName(name); err != nil {
				return err
			}
			hostPath, ok := lookHostBin(name)
			if !ok {
				return fmt.Errorf("host binary %q not found on PATH or standard install directories", name)
			}

			bins, err := loadHostBins()
			if err != nil {
				return fmt.Errorf("loading host binaries: %w", err)
			}
			if !slices.Contains(bins, name) {
				bins = append(bins, name)
				slices.Sort(bins)
				if err := saveHostBins(bins); err != nil {
					return fmt.Errorf("saving host binaries: %w", err)
				}
			}

			if err := hostexec.SyncShims(hostBinsDir(), hostExecClientAt(), bins); err != nil {
				return fmt.Errorf("syncing host-bin shims: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added host binary %s (%s)\n", name, hostPath)
			return nil
		},
	}
}

func newPHPHostBinRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a host binary from the PHP container allowlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := hostexec.ValidateBinName(name); err != nil {
				return err
			}

			bins, err := loadHostBins()
			if err != nil {
				return fmt.Errorf("loading host binaries: %w", err)
			}
			idx := slices.Index(bins, name)
			if idx < 0 {
				return fmt.Errorf("host binary %q is not allowlisted", name)
			}
			bins = slices.Delete(bins, idx, idx+1)
			if err := saveHostBins(bins); err != nil {
				return fmt.Errorf("saving host binaries: %w", err)
			}

			if err := hostexec.SyncShims(hostBinsDir(), hostExecClientAt(), bins); err != nil {
				return fmt.Errorf("syncing host-bin shims: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed host binary %s\n", name)
			return nil
		},
	}
}

func newPHPHostBinListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List host binaries allowlisted for PHP containers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bins, err := loadHostBins()
			if err != nil {
				return fmt.Errorf("loading host binaries: %w", err)
			}
			if len(bins) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No host binaries allowlisted.")
				return nil
			}
			for _, name := range bins {
				if p, ok := lookHostBin(name); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, p)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t(not found on host)\n", name)
				}
			}
			return nil
		},
	}
}

func defaultLoadHostBins() ([]string, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}
	return append([]string(nil), cfg.PHP.HostBins...), nil
}

func defaultSaveHostBins(bins []string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	cfg.PHP.HostBins = append([]string(nil), bins...)
	return config.SaveGlobal(cfg)
}
