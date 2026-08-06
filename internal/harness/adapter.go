// Package harness hosts the per-harness adapters that know how to read a
// specific coding-agent harness's native state dir (Export → bundle.Bundle)
// and write a bundle back out into a target harness's native layout (Import).
//
// v0.1 ships two directional adapters:
//   - ClaudeCode: Export-only (reads ~/.claude → bundle)
//   - Codex:      Import-only (bundle → writes ~/.codex)
//
// The Adapter interface is symmetric on purpose: the bundle format is
// reversible, so a v0.2 reverse-direction adapter is just a new impl of the
// same two methods, not a new contract. The v0.1 adapters return
// ErrUnsupportedDirection for the direction they don't implement yet, so a
// `carrystate pack --harness codex` call fails with a clear message rather than
// a nil-pointer.
package harness

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

// ErrUnsupportedDirection is returned by a v0.1 adapter when asked for the
// direction it does not yet implement (e.g. ClaudeCode.Import, Codex.Export).
// v0.2 reverse-direction adapters will remove this sentinel for their pair.
var ErrUnsupportedDirection = errors.New("carrystate: harness adapter does not support this direction in v0.1 (reverse direction is a v0.2 adapter)")

// ExportOptions configures an Export (pack) call.
type ExportOptions struct {
	// StateDir is the harness's state directory (e.g. ~/.claude for Claude Code).
	StateDir string
	// Project, if non-empty, restricts the export to one project subdir.
	Project string
	// McpConfigPath optionally overrides the user-scope MCP config file
	// (defaults are resolved per-adapter, e.g. ~/.claude.json for Claude Code).
	McpConfigPath string
	// Verbose turns on per-file progress logging to the returned bundle's
	// manifest (useful for airgap audit trails).
	Verbose bool
}

// ImportReport summarizes an Import (unpack) call.
type ImportReport struct {
	Harness          string
	StateDir         string
	SessionsWritten  int
	MemoriesWritten  int
	McpServersWritten int
	Skipped          []string // "id: reason" entries that were intentionally not written
	Warnings         []string // non-fatal issues (e.g. target dir created, env collision)
}

// AddSkipped appends a skipped-entry note.
func (r *ImportReport) AddSkipped(id, reason string) {
	r.Skipped = append(r.Skipped, fmt.Sprintf("%s: %s", id, reason))
}

// AddWarning appends a non-fatal warning.
func (r *ImportReport) AddWarning(w string) {
	r.Warnings = append(r.Warnings, w)
}

// Adapter is the contract every harness implements. v0.1 adapters implement
// only one of Export/Import; the other returns ErrUnsupportedDirection.
type Adapter interface {
	// Name is the stable harness id used in --harness and the bundle manifest
	// (e.g. "claudecode", "codex"). Lowercase, no spaces.
	Name() string
	// Export reads a source harness state dir and produces a normalized bundle.
	Export(opts ExportOptions) (*bundle.Bundle, error)
	// Import writes a bundle back out into a target harness state dir.
	Import(b *bundle.Bundle, stateDir string) (*ImportReport, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Adapter{}
)

// Register adds an adapter to the global registry. Duplicate names overwrite
// (last-wins) so a v0.2 adapter can shadow a v0.1 one for the same harness id.
func Register(a Adapter) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[a.Name()] = a
}

// Lookup returns the adapter for a harness id, or an error naming the known
// adapters if the id is unknown.
func Lookup(name string) (Adapter, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if a, ok := registry[name]; ok {
		return a, nil
	}
	known := make([]string, 0, len(registry))
	for k := range registry {
		known = append(known, k)
	}
	sort.Strings(known)
	return nil, fmt.Errorf("unknown harness %q (known: %v)", name, known)
}

// Names returns the registered harness ids in sorted order.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func init() {
	// v0.1 ships ClaudeCode (export) and Codex (import). v0.2 adapters
	// (OpenCode, Grok Build, Hermes Agent, reverse direction) Register
	// themselves the same way.
	Register(&ClaudeCode{})
	Register(&Codex{})
}
