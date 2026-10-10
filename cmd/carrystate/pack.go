// Package main — the `carrystate pack` subcommand.
//
// pack reads a source harness state dir, produces a .csb bundle, and writes
// it to --out. It prints the bundle's merkle root + entry counts so an airgap
// ops engineer can record the fingerprint alongside the bundle (e.g. in a
// 合规审计 log) before the bundle crosses to the target host via USB.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/carrystate/internal/harness"
)

func newPackCmd() *cobra.Command {
	var (
		harnessName string
		stateDir    string
		out         string
		project     string
		mcpConfig   string
		verbose     bool
	)
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Pack a harness state dir into a .csb bundle",
		Long: `Pack reads sessions, memories and MCP server config from a source harness
state dir and emits a content-addressed .csb (CarryState Bundle) with per-entry
sha256 and a merkle root.

Example:
  carrystate pack --harness claudecode --state-dir ~/.claude --out project.csb
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requiredFlags(cmd, map[string]string{
				"harness":   harnessName,
				"state-dir": stateDir,
				"out":       out,
			}); err != nil {
				return err
			}
			out = expandHome(out)
			stateDir = expandHome(stateDir)
			if mcpConfig != "" {
				mcpConfig = expandHome(mcpConfig)
			}

			adapter, err := harness.Lookup(harnessName)
			if err != nil {
				return err
			}

			b, err := adapter.Export(harness.ExportOptions{
				StateDir:      stateDir,
				Project:       project,
				McpConfigPath: mcpConfig,
				Verbose:       verbose,
				ToolVersion:   "carrystate-" + Version,
				WarnTo:        cmd.ErrOrStderr(),
			})
			if err != nil {
				return fmt.Errorf("pack: %w", err)
			}

			if err := b.Write(out); err != nil {
				return fmt.Errorf("write bundle: %w", err)
			}

			// emit a human-readable manifest sidecar (YAML) alongside the .csb
			// for 合规审计 logs — airgap ops teams log this with the merkle root.
			// A missing audit sidecar must be loud, not silent.
			sidecar := out + ".manifest.yaml"
			yml, yerr := b.ManifestYAML()
			if yerr == nil {
				yerr = os.WriteFile(sidecar, []byte(yml), 0o644)
			}
			if yerr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not write manifest sidecar %s: %v\n", sidecar, yerr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "packed %s → %s\n", out, b.Manifest.SourceHarness)
			fmt.Fprintf(cmd.OutOrStdout(), "  sessions:    %d\n", b.Manifest.EntryCount.Sessions)
			fmt.Fprintf(cmd.OutOrStdout(), "  memories:    %d\n", b.Manifest.EntryCount.Memories)
			fmt.Fprintf(cmd.OutOrStdout(), "  mcp servers: %d\n", b.Manifest.EntryCount.McpServers)
			fmt.Fprintf(cmd.OutOrStdout(), "  merkle root: %s\n", b.MerkleRoot())
			fmt.Fprintf(cmd.OutOrStdout(), "  fingerprint: %s\n", b.Short())
			return nil
		},
	}
	cmd.Flags().StringVar(&harnessName, "harness", "", "source harness id (e.g. claudecode)")
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "source harness state dir (e.g. ~/.claude)")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output .csb bundle path")
	cmd.Flags().StringVar(&project, "project", "", "restrict to one project: the encoded projects/<dir> name or the real path (optional)")
	cmd.Flags().StringVar(&mcpConfig, "mcp-config", "", "override user-scope MCP config file (optional)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose progress")

	mustMark(cmd, "harness", "state-dir", "out")
	return cmd
}
