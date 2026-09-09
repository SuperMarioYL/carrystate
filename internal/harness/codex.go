// Package harness — Codex adapter (Import-only in v0.1).
//
// Codex's native layout as written by this adapter (the bundle format is the
// stable contract; this layout is the adapter's best-effort match to OpenAI's
// Codex CLI conventions, documented in SPEC.md so a future Codex revision that
// changes the on-disk shape only needs a translator update, not a bundle
// revision):
//   - sessions: <state-dir>/sessions/<project>/<session-id>.jsonl
//   - memory:   <state-dir>/AGENTS.md (global, merged from bundle memories) +
//     per-project <state-dir>/sessions/<project>/AGENTS.md
//   - MCP:      <state-dir>/mcp.json with a top-level `mcpServers` object
//
// Import is deterministic and lossless: session JSONL lines are written
// verbatim, memory markdown is concatenated in bundle order, MCP servers are
// re-serialized canonically. No LLM, no cloud.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
	"github.com/SuperMarioYL/carrystate/internal/translate"
)

// Codex is the Import adapter for OpenAI's Codex CLI.
type Codex struct{}

// Name returns the stable harness id used in --harness and the bundle manifest.
func (c *Codex) Name() string { return "codex" }

// Export is unsupported for Codex in v0.1 (Claude Code → Codex is
// one-directional). A v0.2 reverse adapter would implement this.
func (c *Codex) Export(_ ExportOptions) (*bundle.Bundle, error) {
	return nil, ErrUnsupportedDirection
}

// Import writes a bundle into a Codex-native state dir layout.
func (c *Codex) Import(b *bundle.Bundle, stateDir string) (*ImportReport, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("codex: --state-dir is required (typically ~/.codex)")
	}
	if b == nil {
		return nil, fmt.Errorf("codex: nil bundle")
	}
	if b.Manifest.SourceHarness == "" {
		return nil, fmt.Errorf("codex: bundle has no source harness in manifest")
	}
	// refuse bundles whose schema ids this adapter was not built for (a v0.2
	// bundle needs a translator upgrade first — see SPEC.md §versioning)
	if err := b.SchemaCompatibleForImport(bundle.SessionSchemaID, bundle.MemorySchemaID, bundle.McpSchemaID); err != nil {
		return nil, fmt.Errorf("codex: %w", err)
	}

	r := &ImportReport{Harness: c.Name(), StateDir: stateDir}

	// ensure the state dir + sessions root exist (mkdir -p semantics)
	for _, d := range []string{stateDir, filepath.Join(stateDir, "sessions")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return r, fmt.Errorf("codex: mkdir %q: %w", d, err)
		}
	}

	// 1. sessions
	for _, s := range b.Sessions {
		proj := safeProjectDir(s.Project)
		projDir := filepath.Join(stateDir, "sessions", proj)
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			r.AddSkipped("session:"+s.ID, fmt.Sprintf("mkdir: %v", err))
			continue
		}
		out := filepath.Join(projDir, s.ID+".jsonl")
		// write the raw JSONL content verbatim — lossless, byte-identical to
		// the source harness's file (modulo trailing-newline normalization)
		if err := os.WriteFile(out, []byte(s.Content), 0o644); err != nil {
			r.AddSkipped("session:"+s.ID, fmt.Sprintf("write: %v", err))
			continue
		}
		r.SessionsWritten++
	}

	// 2. per-project memories: every project with scoped memories gets its
	// AGENTS.md next to its sessions — including projects the bundle carries no
	// sessions for (a memory-only project still lands on the target host)
	for _, project := range memoryProjects(b) {
		md := translate.MergeMemories(b.Memories, project)
		if md == "" {
			continue
		}
		projDir := filepath.Join(stateDir, "sessions", safeProjectDir(project))
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			r.AddWarning(fmt.Sprintf("mkdir %s: %v", projDir, err))
			continue
		}
		if err := os.WriteFile(filepath.Join(projDir, "AGENTS.md"), []byte(md), 0o644); err != nil {
			r.AddWarning(fmt.Sprintf("write %s AGENTS.md: %v", projDir, err))
			continue
		}
		r.MemoriesWritten++
	}

	// 3. global memory: <state-dir>/AGENTS.md (merged from global bundle memories)
	if g := translate.MergeMemories(b.Memories, ""); g != "" {
		if err := os.WriteFile(filepath.Join(stateDir, "AGENTS.md"), []byte(g), 0o644); err != nil {
			r.AddWarning(fmt.Sprintf("write global AGENTS.md: %v", err))
		} else {
			r.MemoriesWritten++
		}
	}

	// 4. MCP config: <state-dir>/mcp.json
	if len(b.MCP.Servers) > 0 {
		if err := writeCodexMcpConfig(stateDir, b.MCP.Servers, r); err != nil {
			r.AddWarning(fmt.Sprintf("write mcp.json: %v", err))
		}
	}

	return r, nil
}

// memoryProjects returns the sorted, de-duplicated list of non-empty memory
// project scopes in a bundle. Iterating it (not the sessions) keeps unpack
// faithful to the bundle: a project's memories land even with zero sessions.
func memoryProjects(b *bundle.Bundle) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(b.Memories))
	for _, m := range b.Memories {
		if m.Project == "" || seen[m.Project] {
			continue
		}
		seen[m.Project] = true
		out = append(out, m.Project)
	}
	sort.Strings(out)
	return out
}

// writeCodexMcpConfig writes the MCP servers to <state-dir>/mcp.json in the
// same {mcpServers: {...}} shape Claude Code uses, so the two harnesses can
// share tool-call config across an airgap. Servers whose prefix-stripped names
// collide are NOT silently merged: the user-scope server wins, the loser is
// recorded as a report warning, and the report's count reflects the file.
func writeCodexMcpConfig(stateDir string, servers []bundle.McpServer, r *ImportReport) error {
	type stdioCfg struct {
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
	}
	type urlCfg struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	winning := map[string]bundle.McpServer{}
	for _, s := range servers {
		// strip the "project/" prefix Claude Code added on export so the
		// server name round-trips to the original identifier
		name := stripProjectPrefix(s.Name)
		prev, taken := winning[name]
		switch {
		case !taken:
			winning[name] = s
		case prev.Scope != "user" && s.Scope == "user":
			// the user-scope server takes the name; the project-scope server
			// written first is the one dropped
			r.AddWarning(fmt.Sprintf(
				"mcp: skipped %q (user-scope server takes the name %q)",
				prev.Name, name))
			winning[name] = s
		default:
			r.AddWarning(fmt.Sprintf(
				"mcp: skipped %q (name %q already taken by the %s-scope server)",
				s.Name, name, prev.Scope))
		}
	}

	wire := map[string]any{}
	for name, srv := range winning {
		if srv.Transport == "sse" || srv.Transport == "http" {
			wire[name] = urlCfg{Type: srv.Transport, URL: srv.URL}
		} else {
			wire[name] = stdioCfg{Command: srv.Command, Args: srv.Args, Env: srv.Env}
		}
	}
	r.McpServersWritten = len(wire)

	doc := map[string]any{"mcpServers": wire}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(stateDir, "mcp.json"), data, 0o644)
}

// safeProjectDir sanitizes a Claude Code project dir name (which may contain
// "-" from the encoded path) into a filesystem-safe subdir name for Codex.
func safeProjectDir(project string) string {
	if project == "" {
		return "default"
	}
	// keep it stable across pack/unpack: only replace path separators, keep "-"
	return strings.NewReplacer(string(os.PathSeparator), "-").Replace(project)
}

// stripProjectPrefix removes a leading "<project>/" from an MCP server name if
// the export added one (project-scope servers are prefixed on export so they
// don't collide with user-scope servers of the same name).
func stripProjectPrefix(name string) string {
	if i := strings.Index(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
