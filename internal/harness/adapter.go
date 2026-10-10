// Package harness hosts the per-harness adapters that know how to read a
// specific coding-agent harness's native state dir (Export → bundle.Bundle)
// and write a bundle back out into a target harness's native layout (Import).
//
// v0.3.0 ships one bidirectional pair:
//   - ClaudeCode: Export (reads ~/.claude → bundle) + Import (bundle → ~/.claude)
//   - Codex:      Export (reads ~/.codex → bundle) + Import (bundle → ~/.codex)
//
// The Adapter interface is symmetric on purpose: the bundle format is
// reversible, so each direction is just an impl of the same two methods, not
// a new contract. An adapter that has not implemented a direction yet returns
// ErrUnsupportedDirection so `carrystate pack --harness <future>` fails with a
// clear message rather than a nil-pointer.
package harness

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

// ErrUnsupportedDirection is returned by an adapter when asked for the
// direction it does not implement yet. The v0.3.0 pair (ClaudeCode, Codex) is
// fully bidirectional; future harness adapters (OpenCode, Grok Build, Hermes
// Agent) use this sentinel for their unimplemented side.
var ErrUnsupportedDirection = errors.New("carrystate: harness adapter does not support this direction")

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
	// ToolVersion identifies the packing binary in the bundle manifest
	// (manifest.tool_version). Pack sets it from the CLI's Version const so
	// the manifest stays in lockstep with the release tag; the empty string
	// falls back to a per-adapter default.
	ToolVersion string
	// WarnTo receives one-line warnings for state the adapter had to skip
	// during Export (a malformed mcpServers entry, an unreadable memory file).
	// Adapters must skip-and-warn, never silently drop; nil falls back to
	// os.Stderr so a direct library caller still sees the warnings.
	WarnTo io.Writer
}

// warnf writes one warning line to WarnTo (os.Stderr when unset). Used by
// adapters to surface export-side skips — the pack-side analogue of the
// ImportReport warnings unpack prints.
func (o ExportOptions) warnf(format string, args ...any) {
	w := o.WarnTo
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "warning: "+format+"\n", args...)
}

// ImportReport summarizes an Import (unpack) call.
type ImportReport struct {
	Harness           string
	StateDir          string
	SessionsWritten   int
	MemoriesWritten   int
	McpServersWritten int
	Skipped           []string // "id: reason" entries that were intentionally not written
	Warnings          []string // non-fatal issues (e.g. target dir created, env collision)
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
	// the bidirectional ClaudeCode <-> Codex pair. Future harness adapters
	// (OpenCode, Grok Build, Hermes Agent) Register themselves the same way.
	Register(&ClaudeCode{})
	Register(&Codex{})
}
