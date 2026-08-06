// Package harness — Claude Code adapter (Export-only in v0.1).
//
// Claude Code's on-disk state (per the fixed schema smoke):
//   - sessions: ~/.claude/projects/<project>/*.jsonl — one JSONL file per
//     session, named <session-uuid>.jsonl; the <project> dir is the project's
//     absolute path with "/" encoded as "-" (lossy for paths containing "-").
//   - memory: ~/.claude/CLAUDE.md (global) + <real-project-root>/CLAUDE.md
//     (per-project) — markdown instruction files the agent carries across
//     sessions.
//   - MCP config: ~/.claude.json `mcpServers` key (user-scope, primary) +
//     <real-project-root>/.mcp.json (project-scope, secondary).
//
// Export normalizes all three into a bundle.Bundle. Every entry is
// sha256-addressed by its canonical content; the bundle root is a merkle hash
// over the sorted per-entry digests.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

// ClaudeCode is the Export adapter for Anthropic's Claude Code CLI.
type ClaudeCode struct{}

// Name returns the stable harness id used in --harness and the bundle manifest.
func (c *ClaudeCode) Name() string { return "claudecode" }

// Export reads a Claude Code state dir and produces a normalized bundle.
func (c *ClaudeCode) Export(opts ExportOptions) (*bundle.Bundle, error) {
	if opts.StateDir == "" {
		return nil, fmt.Errorf("claudecode: --state-dir is required (typically ~/.claude)")
	}
	info, err := os.Stat(opts.StateDir)
	if err != nil {
		return nil, fmt.Errorf("claudecode: state dir %q: %w", opts.StateDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("claudecode: state dir %q is not a directory", opts.StateDir)
	}

	b := bundle.New(c.Name(), "", "carrystate-0.1.0")

	// 1. sessions: <state-dir>/projects/<proj>/*.jsonl
	projectsRoot := filepath.Join(opts.StateDir, "projects")
	if projectDirs, err := os.ReadDir(projectsRoot); err == nil {
		for _, d := range projectDirs {
			if !d.IsDir() {
				continue
			}
			if opts.Project != "" && d.Name() != opts.Project {
				continue
			}
			projDir := filepath.Join(projectsRoot, d.Name())
			if serr := c.exportSessions(b, projDir, d.Name()); serr != nil {
				return nil, serr
			}
			// best-effort project-scope memory + MCP from the decoded real path
			if realPath := decodeClaudeProject(d.Name()); realPath != "" && dirExists(realPath) {
				c.exportProjectMemory(b, realPath, d.Name())
				c.exportProjectMcp(b, realPath, d.Name())
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("claudecode: read projects dir: %w", err)
	}

	// 2. global memory: <state-dir>/CLAUDE.md
	if mf := filepath.Join(opts.StateDir, "CLAUDE.md"); fileExists(mf) {
		if mem, err := readMemoryFile(mf, "", mf); err == nil {
			b.AppendMemory(mem)
		}
	}

	// 3. user-scope MCP config: ~/.claude.json (default) or override
	mcpPath := opts.McpConfigPath
	if mcpPath == "" {
		mcpPath = resolveUserMcpConfig(opts.StateDir)
	}
	if mcpPath != "" && fileExists(mcpPath) {
		if err := c.exportUserMcp(b, mcpPath); err != nil {
			return nil, fmt.Errorf("claudecode: user mcp config %q: %w", mcpPath, err)
		}
	}

	b.Finalize()
	return b, nil
}

// Import is unsupported for ClaudeCode in v0.1 (Claude Code → Codex is
// one-directional). A v0.2 reverse adapter would implement this.
func (c *ClaudeCode) Import(_ *bundle.Bundle, _ string) (*ImportReport, error) {
	return nil, ErrUnsupportedDirection
}

// exportSessions walks one project dir and appends each *.jsonl session.
func (c *ClaudeCode) exportSessions(b *bundle.Bundle, projDir, project string) error {
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return fmt.Errorf("read project dir %q: %w", projDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(projDir, e.Name())
		session, err := readSessionJSONL(path, project)
		if err != nil {
			return err
		}
		b.AppendSession(session)
	}
	return nil
}

// readSessionJSONL reads one Claude Code JSONL session file and normalizes it
// to a SessionEntry. The raw file content is stored verbatim (string) so the
// per-entry sha256 is stable across the JSON round-trip — encoding/json's
// MarshalIndent re-indents parsed []json.RawMessage, which would change the
// bytes and break the lossless guarantee. A v0.2 LLM-merge layer that wants
// parsed lines can re-parse Content itself.
func readSessionJSONL(path, project string) (bundle.SessionEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bundle.SessionEntry{}, fmt.Errorf("read session %q: %w", path, err)
	}
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	return bundle.SessionEntry{
		ID:        id,
		Project:   project,
		Source:    path,
		Format:    "jsonl",
		Content:   string(data),
		LineCount: len(splitJSONL(data)),
	}, nil
}

// splitJSONL parses raw JSONL bytes into a slice of raw JSON line bodies.
// Blank lines and lines that are not valid JSON objects are skipped (Claude
// Code's JSONL is well-formed, but the bundle should not choke on a trailing
// newline or a corrupted tail).
func splitJSONL(data []byte) []json.RawMessage {
	var out []json.RawMessage
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// validate it's a JSON object/array; accept any valid JSON value
		var any json.RawMessage
		if json.Unmarshal([]byte(line), &any) == nil {
			out = append(out, json.RawMessage(line))
		}
	}
	return out
}

// exportProjectMemory reads <real-path>/CLAUDE.md as a project-scoped memory.
func (c *ClaudeCode) exportProjectMemory(b *bundle.Bundle, realPath, project string) {
	mf := filepath.Join(realPath, "CLAUDE.md")
	if !fileExists(mf) {
		return
	}
	mem, err := readMemoryFile(mf, project, mf)
	if err != nil {
		return
	}
	// avoid duplicating a global memory already added with Project=""
	mem.ID = "project:" + project + ":CLAUDE.md"
	b.AppendMemory(mem)
}

// exportProjectMcp reads <real-path>/.mcp.json as project-scope MCP servers.
func (c *ClaudeCode) exportProjectMcp(b *bundle.Bundle, realPath, project string) {
	cfg := filepath.Join(realPath, ".mcp.json")
	if !fileExists(cfg) {
		return
	}
	servers, err := readMcpServersFile(cfg)
	if err != nil {
		return
	}
	for _, s := range servers {
		s.Scope = "project"
		s.Name = project + "/" + s.Name
		b.AppendMcpServer(s)
	}
}

// exportUserMcp reads the user-scope ~/.claude.json mcpServers map.
func (c *ClaudeCode) exportUserMcp(b *bundle.Bundle, path string) error {
	servers, err := readMcpServersFile(path)
	if err != nil {
		return err
	}
	for _, s := range servers {
		s.Scope = "user"
		b.AppendMcpServer(s)
	}
	return nil
}

// readMcpServersFile reads a JSON file with a top-level `mcpServers` object and
// returns normalized McpServer entries (without SHA256; AppendMcpServer fills it).
func readMcpServersFile(path string) ([]bundle.McpServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse mcpServers: %w", err)
	}
	out := make([]bundle.McpServer, 0, len(doc.McpServers))
	for name := range doc.McpServers {
		raw := doc.McpServers[name]
		s, err := parseMcpServer(name, raw)
		if err != nil {
			continue // skip malformed entry rather than failing the whole export
		}
		out = append(out, s)
	}
	return out, nil
}

// parseMcpServer normalizes one mcpServers entry. Claude Code's format:
//   - stdio: { "command": "...", "args": [...], "env": {...} }
//   - sse/http: { "type": "sse"|"http", "url": "..." }
func parseMcpServer(name string, raw json.RawMessage) (bundle.McpServer, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return bundle.McpServer{}, err
	}
	s := bundle.McpServer{Name: name, Transport: "stdio"}
	if t, ok := generic["type"]; ok {
		var ts string
		if json.Unmarshal(t, &ts) == nil && (ts == "sse" || ts == "http") {
			s.Transport = ts
		}
	}
	if v, ok := generic["command"]; ok {
		_ = json.Unmarshal(v, &s.Command)
	}
	if v, ok := generic["args"]; ok {
		_ = json.Unmarshal(v, &s.Args)
	}
	if v, ok := generic["env"]; ok {
		_ = json.Unmarshal(v, &s.Env)
	}
	if v, ok := generic["url"]; ok {
		_ = json.Unmarshal(v, &s.URL)
	}
	return s, nil
}

// readMemoryFile reads a markdown memory file into a MemoryEntry.
func readMemoryFile(path, project, source string) (bundle.MemoryEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bundle.MemoryEntry{}, err
	}
	id := "global:CLAUDE.md"
	if project != "" {
		id = "project:" + project + ":CLAUDE.md"
	}
	return bundle.MemoryEntry{
		ID:      id,
		Project: project,
		Source:  source,
		Format:  "markdown",
		Content: string(data),
	}, nil
}

// resolveUserMcpConfig finds the user-scope MCP config for a Claude Code state
// dir. If stateDir is ~/.claude, the user config lives at ~/.claude.json (one
// level up, in $HOME). For non-default state dirs, fall back to
// <state-dir>/.claude.json.
func resolveUserMcpConfig(stateDir string) string {
	if filepath.Base(stateDir) == ".claude" {
		parent := filepath.Dir(stateDir)
		cand := filepath.Join(parent, ".claude.json")
		if fileExists(cand) {
			return cand
		}
	}
	cand := filepath.Join(stateDir, ".claude.json")
	if fileExists(cand) {
		return cand
	}
	return ""
}

// decodeClaudeProject decodes a Claude Code projects/<name> dir back to the
// original absolute path: "/" was encoded as "-", so we reverse it. This is
// lossy for paths containing "-" (best-effort: the caller verifies the
// decoded path exists on disk before using it).
func decodeClaudeProject(name string) string {
	if name == "" {
		return ""
	}
	dec := strings.ReplaceAll(name, "-", "/")
	// Claude Code project dirs start with "-" (encoding of leading "/")
	if strings.HasPrefix(name, "-") {
		return dec // already starts with "/"
	}
	return dec
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
