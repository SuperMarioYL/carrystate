// Package main — the `carrystate unpack` subcommand.
//
// unpack reads a .csb bundle and writes its normalized state into a target
// harness's native layout via that harness's adapter Import. It prints an
// import report (sessions/memories/MCP counts + any skipped entries or
// warnings) so the ops engineer can eyeball that the round-trip landed.
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
	"github.com/SuperMarioYL/carrystate/internal/harness"
)

func newUnpackCmd() *cobra.Command {
	var (
		harnessName string
		in          string
		stateDir    string
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Unpack a .csb bundle into a target harness state dir",
		Long: `Unpack reads a .csb (CarryState Bundle) and writes its sessions, memories
and MCP config into a target harness's native state dir layout via that
harness's adapter.

Example:
  carrystate unpack --harness codex --in project.csb --state-dir ~/.codex
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requiredFlags(cmd, map[string]string{
				"harness":   harnessName,
				"in":        in,
				"state-dir": stateDir,
			}); err != nil {
				return err
			}
			in = expandHome(in)
			stateDir = expandHome(stateDir)

			b, err := bundle.Read(in)
			if err != nil {
				return fmt.Errorf("read bundle: %w", err)
			}

			// structural gate (unknown spec version, missing manifest fields)
			// before the integrity check — never write a malformed bundle
			if err := b.Validate(); err != nil {
				return fmt.Errorf("bundle failed structural validation: %w", err)
			}

			// always verify the bundle is intact before unpacking — never write
			// a tampered bundle into a target harness
			report := b.Verify()
			if !report.Pass() {
				return fmt.Errorf("bundle failed integrity check: %s (refusing to unpack)", report.Summary())
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: bundle %s (source=%s, %s)\n",
					in, b.Manifest.SourceHarness, report.Summary())
				fmt.Fprintf(cmd.OutOrStdout(), "  sessions:    %d\n", b.Manifest.EntryCount.Sessions)
				fmt.Fprintf(cmd.OutOrStdout(), "  memories:    %d\n", b.Manifest.EntryCount.Memories)
				fmt.Fprintf(cmd.OutOrStdout(), "  mcp servers: %d\n", b.Manifest.EntryCount.McpServers)
				fmt.Fprintf(cmd.OutOrStdout(), "  merkle root: %s\n", b.MerkleRoot())
				return nil
			}

			adapter, err := harness.Lookup(harnessName)
			if err != nil {
				return err
			}

			r, err := adapter.Import(b, stateDir)
			if err != nil {
				return fmt.Errorf("unpack: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "unpacked %s → %s\n", in, r.Harness)
			fmt.Fprintf(cmd.OutOrStdout(), "  state dir:      %s\n", r.StateDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  sessions written:  %d\n", r.SessionsWritten)
			fmt.Fprintf(cmd.OutOrStdout(), "  memories written:  %d\n", r.MemoriesWritten)
			fmt.Fprintf(cmd.OutOrStdout(), "  mcp servers:       %d\n", r.McpServersWritten)
			if len(r.Skipped) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  skipped (%d):\n", len(r.Skipped))
				for _, s := range r.Skipped {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", s)
				}
			}
			if len(r.Warnings) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  warnings (%d):\n", len(r.Warnings))
				for _, w := range r.Warnings {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&harnessName, "harness", "", "target harness id (e.g. codex)")
	cmd.Flags().StringVarP(&in, "in", "i", "", "input .csb bundle path")
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "target harness state dir (e.g. ~/.codex)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "verify + print bundle contents without writing")

	mustMark(cmd, "harness", "in", "state-dir")
	return cmd
}
