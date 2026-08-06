// Package translate holds the cross-harness schema-translation helpers shared
// by the adapters. v0.1 is lossless and deterministic — no LLM, no semantic
// merge — so these helpers are pure byte-shaping functions: merge markdown
// memories with provenance headers, canonicalize MCP server shapes. A v0.2
// LLM-reconciliation layer would sit on top of these same signatures.
package translate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

// MergeMemories concatenates memory markdown entries with a provenance header
// (the original source path) so an auditor reading the merged file can trace
// each section back to its origin harness file. Used by adapters that write a
// single merged memory file (e.g. Codex AGENTS.md).
//
// If project is "", only global (Project=="") memories are merged.
// If project is non-empty, only that project's scoped memories are merged.
// Returns "" when no memories match (caller can skip writing the file).
func MergeMemories(mems []bundle.MemoryEntry, project string) string {
	var sb strings.Builder
	for _, m := range mems {
		if (project == "" && m.Project != "") || (project != "" && m.Project != project) {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(fmt.Sprintf("<!-- from %s -->\n", m.Source))
		sb.WriteString(m.Content)
	}
	return sb.String()
}

// CanonicalMcpServerJSON returns a stable, sorted-key JSON encoding of an MCP
// server's transport-level fields (name/transport/command/args/env/url/scope).
// Two adapters packing the same logical server produce the same bytes, so the
// per-entry sha256 matches across harnesses — the cross-harness integrity
// invariant. (The bundle package uses an equivalent canonicalization internally
// for checksumming; this is the public surface for adapter authors adding new
// harnesses.)
func CanonicalMcpServerJSON(s bundle.McpServer) string {
	type canon struct {
		Name      string            `json:"name"`
		Transport string            `json:"transport"`
		Command   string            `json:"command,omitempty"`
		Args      []string          `json:"args,omitempty"`
		Env       map[string]string `json:"env,omitempty"`
		URL       string            `json:"url,omitempty"`
		Scope     string            `json:"scope"`
	}
	b, _ := json.Marshal(canon{
		Name: s.Name, Transport: s.Transport, Command: s.Command,
		Args: s.Args, Env: s.Env, URL: s.URL, Scope: s.Scope,
	})
	return string(b)
}
