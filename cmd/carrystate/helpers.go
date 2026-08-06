// Package main — shared CLI helpers used by pack/unpack/verify.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// requiredFlags returns an error naming the first flag in needs that is empty.
// cobra's MarkFlagRequired panics on duplicates at init-time; this helper does a
// runtime check so a missing --harness produces a clean usage error, not a
// generic "required flag(s) \"harness\" not set" that buries the hint.
func requiredFlags(cmd *cobra.Command, needs map[string]string) error {
	for name, val := range needs {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("--%s is required (see `%s --help`)", name, cmd.CommandPath())
		}
	}
	return nil
}

// mustMark marks flags as required, panicking only on a programmer error
// (e.g. a flag that doesn't exist), never on user input.
func mustMark(cmd *cobra.Command, flags ...string) {
	for _, f := range flags {
		if err := cmd.MarkFlagRequired(f); err != nil {
			panic(fmt.Errorf("mark %s required: %w", f, err))
		}
	}
}

// expandHome replaces a leading ~ with the user's home dir. Flags accept ~ in
// the user's voice (e.g. --state-dir ~/.claude); Go's os.Open does not.
func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~"+string(os.PathSeparator)) || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
