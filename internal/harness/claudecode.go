// Package harness — Claude Code adapter.
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
// Since v0.3.0 the pair is bidirectional: Export reads this layout into a
// bundle, Import writes a bundle back into it. Import never clobbers
// unrelated user state — the user-scope MCP merge preserves every other
// top-level key in ~/.claude.json (see mergeClaudeMcpConfig).
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
	"github.com/SuperMarioYL/carrystate/internal/translate"
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

	// manifest.tool_version comes from the packing binary (pack passes the
	// CLI Version const) so it can never drift from the release tag
	toolVersion := opts.ToolVersion
	if toolVersion == "" {
		toolVersion = "dev"
	}
	b := bundle.New(c.Name(), "", toolVersion)

	// 1. sessions: <state-dir>/projects/<proj>/*.jsonl
	projectsRoot := filepath.Join(opts.StateDir, "projects")
	if projectDirs, err := os.ReadDir(projectsRoot); err == nil {
		// --project accepts either the encoded dir name (as stored under
		// projects/) or the real path it encodes ("/" → "-"); anything else
		// must fail fast — a silently-empty bundle is worse than an error.
		want := map[string]bool{opts.Project: true}
		if enc := encodeProjectPath(opts.Project); enc != opts.Project {
			want[enc] = true
		}
		matched := false
		var available []string
		for _, d := range projectDirs {
			if !d.IsDir() {
				continue
			}
			available = append(available, d.Name())
			if opts.Project != "" && !want[d.Name()] {
				continue
			}
			matched = true
			projDir := filepath.Join(projectsRoot, d.Name())
			if serr := c.exportSessions(b, projDir, d.Name()); serr != nil {
				return nil, serr
			}
			// best-effort project-scope memory + MCP from the decoded real path
			if realPath := decodeClaudeProject(d.Name()); realPath != "" && dirExists(realPath) {
				c.exportProjectMemory(b, realPath, d.Name(), opts)
				c.exportProjectMcp(b, realPath, d.Name(), opts)
			}
		}
		if opts.Project != "" && !matched {
			return nil, fmt.Errorf("claudecode: --project %q matches no dir under %s (available: %s)",
				opts.Project, projectsRoot, strings.Join(available, ", "))
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
		if err := c.exportUserMcp(b, mcpPath, opts); err != nil {
			return nil, fmt.Errorf("claudecode: user mcp config %q: %w", mcpPath, err)
		}
	}

	b.Finalize()
	return b, nil
}

// Import writes a bundle into a Claude Code-native state dir layout (the
// reverse of this adapter's Export; the Codex Export side produces exactly
// this shape). Sessions land under projects/<project>/<id>.jsonl, memories
// merge into per-project + global CLAUDE.md with provenance headers, and MCP
// servers merge into the user-scope config — replacing only the mcpServers
// key and preserving every other top-level key of an existing ~/.claude.json.
func (c *ClaudeCode) Import(b *bundle.Bundle, stateDir string) (*ImportReport, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("claudecode: --state-dir is required (typically ~/.claude)")
	}
	if b == nil {
		return nil, fmt.Errorf("claudecode: nil bundle")
	}
	if b.Manifest.SourceHarness == "" {
		return nil, fmt.Errorf("claudecode: bundle has no source harness in manifest")
	}
	// refuse bundles whose schema ids this adapter was not built for (a future
	// bundle needs a translator upgrade first — see SPEC.md §versioning)
	if err := b.SchemaCompatibleForImport(bundle.SessionSchemaID, bundle.MemorySchemaID, bundle.McpSchemaID); err != nil {
		return nil, fmt.Errorf("claudecode: %w", err)
	}

	r := &ImportReport{Harness: c.Name(), StateDir: stateDir}

	// ensure the state dir + projects root exist (mkdir -p semantics)
	for _, d := range []string{stateDir, filepath.Join(stateDir, "projects")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return r, fmt.Errorf("claudecode: mkdir %q: %w", d, err)
		}
	}

	// 1. sessions: projects/<project>/<id>.jsonl
	for _, s := range b.Sessions {
		proj := safeProjectDir(s.Project)
		projDir := filepath.Join(stateDir, "projects", proj)
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
	// CLAUDE.md next to its sessions — including projects the bundle carries
	// no sessions for
	for _, project := range memoryProjects(b) {
		md := translate.MergeMemories(b.Memories, project)
		if md == "" {
			continue
		}
		projDir := filepath.Join(stateDir, "projects", safeProjectDir(project))
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			r.AddWarning(fmt.Sprintf("mkdir %s: %v", projDir, err))
			continue
		}
		if err := os.WriteFile(filepath.Join(projDir, "CLAUDE.md"), []byte(md), 0o644); err != nil {
			r.AddWarning(fmt.Sprintf("write %s CLAUDE.md: %v", projDir, err))
			continue
		}
		r.MemoriesWritten++
	}

	// 3. global memory: <state-dir>/CLAUDE.md (merged from global bundle memories)
	if g := translate.MergeMemories(b.Memories, ""); g != "" {
		if err := os.WriteFile(filepath.Join(stateDir, "CLAUDE.md"), []byte(g), 0o644); err != nil {
			r.AddWarning(fmt.Sprintf("write global CLAUDE.md: %v", err))
		} else {
			r.MemoriesWritten++
		}
	}

	// 4. MCP config: merge into the user-scope config (~/.claude.json),
	// preserving every other top-level key — never clobber unrelated state
	if len(b.MCP.Servers) > 0 {
		if err := mergeClaudeMcpConfig(stateDir, b.MCP.Servers, r); err != nil {
			r.AddWarning(fmt.Sprintf("merge mcp config: %v", err))
		}
	}

	return r, nil
}

// mergeClaudeMcpConfig merges bundle servers into the Claude Code user-scope
// MCP config. The file (~/.claude.json) holds unrelated Claude Code state
// (session history, project state), so the contract is surgical: parse the
// existing document, replace ONLY the mcpServers key, preserve everything
// else byte-value-identical; with no file present, write a minimal one.
// Bundle-side name collisions follow resolveMcpNameCollisions (user-scope
// wins); a bundle server replacing an existing config entry is warned about.
func mergeClaudeMcpConfig(stateDir string, servers []bundle.McpServer, r *ImportReport) error {
	path := resolveUserMcpConfig(stateDir)
	if path == "" {
		// no existing config anywhere: use the same location resolution the
		// export side reads (parent of ~/.claude, else <state-dir> fallback)
		if filepath.Base(stateDir) == ".claude" {
			path = filepath.Join(filepath.Dir(stateDir), ".claude.json")
		} else {
			path = filepath.Join(stateDir, ".claude.json")
		}
	}

	// existing document, all top-level keys preserved as raw JSON
	doc := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse existing mcp config %s: %w", path, err)
		}
	}

	wire := map[string]json.RawMessage{}
	if old, ok := doc["mcpServers"]; len(old) > 0 && ok {
		if err := json.Unmarshal(old, &wire); err != nil {
			return fmt.Errorf("parse existing mcpServers in %s: %w", path, err)
		}
	}

	winning := resolveMcpNameCollisions(servers, r)
	written := 0
	for name, srv := range winning {
		entry, err := wireEntry(srv)
		if err != nil {
			r.AddWarning(fmt.Sprintf("mcp: skipped %q (serialize: %v)", name, err))
			continue
		}
		if _, exists := wire[name]; exists {
			r.AddWarning(fmt.Sprintf("mcp: replaced existing server %q in %s", name, filepath.Base(path)))
		}
		wire[name] = entry
		written++
	}
	// the report counts servers from the bundle actually present in the merged
	// config — additions and same-name replacements alike
	r.McpServersWritten = written

	merged, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("marshal merged mcpServers: %w", err)
	}
	doc["mcpServers"] = merged

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged mcp config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
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
func (c *ClaudeCode) exportProjectMemory(b *bundle.Bundle, realPath, project string, opts ExportOptions) {
	mf := filepath.Join(realPath, "CLAUDE.md")
	if !fileExists(mf) {
		return
	}
	mem, err := readMemoryFile(mf, project, mf)
	if err != nil {
		// a memory that exists but cannot be read must be loud, not silently lost
		opts.warnf("claudecode: skipping project memory %s: %v", mf, err)
		return
	}
	// avoid duplicating a global memory already added with Project=""
	mem.ID = "project:" + project + ":CLAUDE.md"
	b.AppendMemory(mem)
}

// exportProjectMcp reads <real-path>/.mcp.json as project-scope MCP servers.
func (c *ClaudeCode) exportProjectMcp(b *bundle.Bundle, realPath, project string, opts ExportOptions) {
	cfg := filepath.Join(realPath, ".mcp.json")
	if !fileExists(cfg) {
		return
	}
	servers, skipped, err := readMcpServersFile(cfg)
	if err != nil {
		opts.warnf("claudecode: skipping project mcp config %s: %v", cfg, err)
		return
	}
	for _, name := range skipped {
		opts.warnf("claudecode: skipping malformed mcpServers entry %q in %s", name, cfg)
	}
	for _, s := range servers {
		s.Scope = "project"
		s.Name = project + "/" + s.Name
		b.AppendMcpServer(s)
	}
}

// exportUserMcp reads the user-scope ~/.claude.json mcpServers map.
func (c *ClaudeCode) exportUserMcp(b *bundle.Bundle, path string, opts ExportOptions) error {
	servers, skipped, err := readMcpServersFile(path)
	if err != nil {
		return err
	}
	for _, name := range skipped {
		opts.warnf("claudecode: skipping malformed mcpServers entry %q in %s", name, path)
	}
	for _, s := range servers {
		s.Scope = "user"
		b.AppendMcpServer(s)
	}
	return nil
}

// readMcpServersFile reads a JSON file with a top-level `mcpServers` object and
// returns normalized McpServer entries (without SHA256; AppendMcpServer fills
// it) plus the names of malformed entries that were skipped — the caller owns
// surfacing those skips so nothing is dropped silently.
func readMcpServersFile(path string) ([]bundle.McpServer, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse mcpServers: %w", err)
	}
	out := make([]bundle.McpServer, 0, len(doc.McpServers))
	var skipped []string
	for name, raw := range doc.McpServers {
		s, err := parseMcpServer(name, raw)
		if err != nil {
			// record the skip; never fail the whole export over one bad entry
			skipped = append(skipped, name)
			continue
		}
		out = append(out, s)
	}
	return out, skipped, nil
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

// encodeProjectPath is the forward direction of decodeClaudeProject: it maps a
// real project path to the dir name Claude Code stores under projects/ ("/" →
// "-"), so `pack --project /Users/me/app` finds the "-Users-me-app" dir. Lossy
// in the same way (paths containing "-" are ambiguous); the exact dir name
// always wins because both forms are matched.
func encodeProjectPath(realPath string) string {
	return strings.ReplaceAll(realPath, string(os.PathSeparator), "-")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
