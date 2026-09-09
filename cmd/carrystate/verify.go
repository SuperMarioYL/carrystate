// Package main — the `carrystate verify` subcommand.
//
// verify re-derives every per-entry sha256 and the bundle merkle root from a
// .csb bundle's content and compares against the stored checksums. This is the
// deterministic, no-LLM behavior-equivalence gate: green check = the bundle is
// intact and the round-trip is lossless, so an airgapped ops engineer can ship
// it across the intranet with confidence.
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

func newVerifyCmd() *cobra.Command {
	var bundlePath string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a .csb bundle's integrity (per-entry sha256 + merkle root)",
		Long: `Verify re-derives every per-entry sha256 and the bundle merkle root from a
.csb bundle's content and compares against the stored checksums. A green check
is the "yes, ship it" gate for an airgapped ops engineer.

Example:
  carrystate verify --bundle project.csb
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requiredFlags(cmd, map[string]string{"bundle": bundlePath}); err != nil {
				return err
			}
			bundlePath = expandHome(bundlePath)

			b, err := bundle.Read(bundlePath)
			if err != nil {
				return fmt.Errorf("read bundle: %w", err)
			}

			// structural gate before the checksum comparison: unknown spec
			// version, missing manifest fields, entries without checksums
			if err := b.Validate(); err != nil {
				return fmt.Errorf("verify FAILED: %w", err)
			}

			report := b.Verify()

			fmt.Fprintf(cmd.OutOrStdout(), "bundle:   %s\n", bundlePath)
			fmt.Fprintf(cmd.OutOrStdout(), "source:   %s\n", b.Manifest.SourceHarness)
			if b.Manifest.TargetHarness != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "target:   %s\n", b.Manifest.TargetHarness)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "spec:     %s\n", b.Manifest.SpecVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "packed:   %s\n", b.Manifest.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
			fmt.Fprintf(cmd.OutOrStdout(), "entries:  %d sessions, %d memories, %d mcp servers\n",
				b.Manifest.EntryCount.Sessions, b.Manifest.EntryCount.Memories, b.Manifest.EntryCount.McpServers)
			fmt.Fprintf(cmd.OutOrStdout(), "merkle:   %s\n", b.MerkleRoot())
			fmt.Fprintf(cmd.OutOrStdout(), "result:   %s\n", report.Summary())

			if !report.Pass() {
				fmt.Fprintln(cmd.OutOrStdout(), "  MISMATCHES:")
				for _, m := range report.Mismatched {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n      want %s\n      got  %s\n", m.Entry, m.Want, m.Got)
				}
				return fmt.Errorf("verify FAILED: %d checksum mismatches", len(report.Mismatched))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓ all entries match — bundle is intact, round-trip is lossless\n")
			return nil
		},
	}
	cmd.Flags().StringVarP(&bundlePath, "bundle", "b", "", ".csb bundle path")
	mustMark(cmd, "bundle")
	return cmd
}
