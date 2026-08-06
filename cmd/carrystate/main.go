// Package main is the CarryState CLI entrypoint.
//
// CarryState is a 信创/airgap harness-state packer: it bundles agent state
// (sessions, memories, MCP config) from one coding-agent harness into a
// verifiable .csb (CarryState Bundle) and restores it into another harness,
// entirely inside an intranet — no cloud round-trip.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is the CarryState binary version. It is the single source of truth
// surfaced by `carrystate --version`; release tags use the same value (read
// from the repo-root VERSION file by publish_repo.sh).
const Version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "carrystate",
		Short: "信创 airgap harness-state packer — pack/unpack/verify .csb bundles",
		Long: `CarryState ports agent state across coding-agent harnesses without leaving the intranet.

It reads sessions, memories and MCP server config from a source harness (e.g.
Claude Code), normalizes them into a content-addressed .csb (CarryState Bundle)
with per-entry sha256 and a merkle root, then writes the bundle back out into a
target harness's native layout (e.g. Codex). A ` + "`verify`" + ` subcommand re-derives the
checksums and runs a behavior-equivalence check so an airgapped ops engineer can
prove the round-trip is lossless before shipping.

数据不出境 — no network, no cloud, no daemons.
`,
		Version: Version,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cmd.AddCommand(newPackCmd())
	cmd.AddCommand(newUnpackCmd())
	cmd.AddCommand(newVerifyCmd())
	return cmd
}
