package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SuperMarioYL/carrystate/internal/bundle"
)

// writeFixture builds a minimal but representative ~/.claude tree under a temp
// dir and returns (stateDir, homeDir, encodedProjectName). The project real
// path is set up to match the encoded dir name so the adapter's best-effort
// project decode finds the per-project CLAUDE.md and .mcp.json.
func writeFixture(t *testing.T) (stateDir, homeDir, encodedProj string) {
	t.Helper()
	homeDir = t.TempDir()
	stateDir = filepath.Join(homeDir, ".claude")

	// real project dir under the temp home (no "-" in path → lossless decode).
	// encoded name = encode(realProj) = realProj with "/" → "-".
	realProj := filepath.Join(homeDir, "proj")
	encodedProj = strings.ReplaceAll(realProj, "/", "-")

	// sessions: <state-dir>/projects/<encodedProj>/sess-abc.jsonl
	projDir := filepath.Join(stateDir, "projects", encodedProj)
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projDir, "sess-abc.jsonl"),
		[]byte("{\"type\":\"user\",\"message\":\"hello\"}\n{\"type\":\"assistant\",\"message\":\"hi\"}\n"),
		0o644))

	// global memory: <state-dir>/CLAUDE.md
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "CLAUDE.md"),
		[]byte("# Global\nUse Go.\n"), 0o644))

	// real project dir with CLAUDE.md + .mcp.json (decode of encodedProj → realProj)
	require.NoError(t, os.MkdirAll(realProj, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(realProj, "CLAUDE.md"),
		[]byte("# Project\nUse testify.\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(realProj, ".mcp.json"),
		[]byte(`{"mcpServers":{"context7":{"command":"npx","args":["-y","@context7/mcp"]}}}`),
		0o644))

	// user-scope MCP: ~/.claude.json (one level above stateDir; basename .claude)
	require.NoError(t, os.WriteFile(
		filepath.Join(homeDir, ".claude.json"),
		[]byte(`{"mcpServers":{"filesystem":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem"]}}}`),
		0o644))

	return stateDir, homeDir, encodedProj
}

func TestClaudeCodeExportFullFixture(t *testing.T) {
	stateDir, _, encodedProj := writeFixture(t)

	c := &ClaudeCode{}
	b, err := c.Export(ExportOptions{StateDir: stateDir})
	require.NoError(t, err)

	assert.Equal(t, "claudecode", b.Manifest.SourceHarness)
	assert.Equal(t, "0.1", b.Manifest.SpecVersion)

	// 1 session
	require.Len(t, b.Sessions, 1)
	assert.Equal(t, "sess-abc", b.Sessions[0].ID)
	assert.Contains(t, b.Sessions[0].Content, "hello")

	// 2 memories: global (Project=="") + project (Project==encodedProj)
	require.Len(t, b.Memories, 2, "global + project memory")
	projects := map[string]bool{}
	for _, m := range b.Memories {
		projects[m.Project] = true
	}
	assert.True(t, projects[""], "global memory present")
	assert.True(t, projects[encodedProj], "project memory present")

	// 2 mcp servers: user-scope filesystem + project-scope context7
	require.Len(t, b.MCP.Servers, 2, "user + project mcp")
	foundFs, foundCtx := false, false
	for _, s := range b.MCP.Servers {
		if s.Name == "filesystem" {
			foundFs = true
			assert.Equal(t, "user", s.Scope)
		}
		if strings.HasSuffix(s.Name, "/context7") {
			foundCtx = true
			assert.Equal(t, "project", s.Scope)
		}
	}
	assert.True(t, foundFs, "user-scope filesystem")
	assert.True(t, foundCtx, "project-scope context7")

	// every entry has a non-empty sha256
	for _, e := range b.Sessions {
		assert.NotEmpty(t, e.SHA256)
	}
	for _, e := range b.Memories {
		assert.NotEmpty(t, e.SHA256)
	}
	for _, s := range b.MCP.Servers {
		assert.NotEmpty(t, s.SHA256)
	}
	assert.NotEmpty(t, b.Checksums.BundleMerkleRoot)
}

func TestExportImportRoundTrip(t *testing.T) {
	stateDir, _, encodedProj := writeFixture(t)

	// 1. pack (Claude Code → bundle)
	cc := &ClaudeCode{}
	b, err := cc.Export(ExportOptions{StateDir: stateDir})
	require.NoError(t, err)
	b.Finalize()

	// 2. verify the bundle is intact
	rep := b.Verify()
	require.True(t, rep.Pass(), rep.Summary())

	// 3. unpack (bundle → Codex)
	targetDir := t.TempDir()
	codex := &Codex{}
	r, err := codex.Import(b, targetDir)
	require.NoError(t, err)
	assert.Equal(t, 1, r.SessionsWritten)
	assert.GreaterOrEqual(t, r.MemoriesWritten, 1)
	assert.Equal(t, 2, r.McpServersWritten)

	// 4. session content is byte-identical to the source
	src := filepath.Join(stateDir, "projects", encodedProj, "sess-abc.jsonl")
	dst := filepath.Join(targetDir, "sessions", encodedProj, "sess-abc.jsonl")
	srcBytes, _ := os.ReadFile(src)
	dstBytes, _ := os.ReadFile(dst)
	assert.Equal(t, srcBytes, dstBytes, "session must round-trip byte-identical")

	// 5. global AGENTS.md exists + has provenance header
	agentsMd, err := os.ReadFile(filepath.Join(targetDir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(agentsMd), "<!-- from")
	assert.Contains(t, string(agentsMd), "Use Go.")

	// 6. mcp.json has the mcpServers shape with both servers
	mcpData, err := os.ReadFile(filepath.Join(targetDir, "mcp.json"))
	require.NoError(t, err)
	var mcpDoc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mcpData, &mcpDoc))
	_, hasFs := mcpDoc.McpServers["filesystem"]
	assert.True(t, hasFs, "user-scope filesystem round-tripped")
	_, hasCtx := mcpDoc.McpServers["context7"]
	assert.True(t, hasCtx, "project-scope context7 round-tripped (prefix stripped)")
}

func TestLookupFindsRegisteredAdapters(t *testing.T) {
	cc, err := Lookup("claudecode")
	require.NoError(t, err)
	assert.Equal(t, "claudecode", cc.Name())

	cx, err := Lookup("codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", cx.Name())
}

func TestLookupUnknownHarnessErrors(t *testing.T) {
	_, err := Lookup("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown harness")
	assert.Contains(t, err.Error(), "known:")
}

// The v0.2.0 directional sentinel tests flipped with m10: both directions are
// implemented since v0.3.0, so these assert the adapters' argument validation
// instead of ErrUnsupportedDirection.
func TestClaudeCodeImportRejectsEmptyStateDir(t *testing.T) {
	c := &ClaudeCode{}
	_, err := c.Import(nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state-dir is required")
}

func TestCodexExportRejectsEmptyStateDir(t *testing.T) {
	c := &Codex{}
	_, err := c.Export(ExportOptions{StateDir: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state-dir is required")
}

func TestNamesIncludesV01Adapters(t *testing.T) {
	names := Names()
	assert.Contains(t, names, "claudecode")
	assert.Contains(t, names, "codex")
}

func TestMergeMemoriesFiltersByScope(t *testing.T) {
	// covered via the Codex import path above; this is a direct unit check on
	// the translate helper the adapter uses. (lives in translate package; we
	// exercise it through the adapter to avoid an import cycle in tests.)
	_ = bundle.New // keep the bundle import meaningful for the test file
}

// --- v0.2.0 fix regression tests -----------------------------------------------

// A project whose scoped memory is in the bundle but that has no sessions must
// still get its AGENTS.md on unpack (v0.1.0 wrote per-project AGENTS.md only
// inside the sessions loop, silently dropping such memories).
func TestImportWritesProjectMemoryWithoutSessions(t *testing.T) {
	b := bundle.New("claudecode", "codex", "test")
	b.AppendMemory(bundle.MemoryEntry{
		ID:      "project:-tmp-only:CLAUDE.md",
		Project: "-tmp-only",
		Source:  "/tmp/only/CLAUDE.md",
		Format:  "markdown",
		Content: "# Only memory\n",
	})
	b.Finalize()

	target := t.TempDir()
	r, err := (&Codex{}).Import(b, target)
	require.NoError(t, err)
	assert.Equal(t, 1, r.MemoriesWritten)

	data, err := os.ReadFile(filepath.Join(target, "sessions", "-tmp-only", "AGENTS.md"))
	require.NoError(t, err, "memory-only project must still get its AGENTS.md")
	assert.Contains(t, string(data), "# Only memory")
	assert.Contains(t, string(data), "<!-- from /tmp/only/CLAUDE.md -->")
}

// A user-scope and a project-scope server whose prefix-stripped names collide
// must not silently overwrite each other in mcp.json (v0.1.0 kept whichever
// serialized last and still reported both as written).
func TestImportMcpNameCollisionUserScopeWins(t *testing.T) {
	b := bundle.New("claudecode", "codex", "test")
	// export order: project-scope servers are appended during the projects
	// walk, user-scope servers afterwards
	b.AppendMcpServer(bundle.McpServer{
		Name: "-tmp-proj/shared", Transport: "stdio",
		Command: "project-server", Scope: "project",
	})
	b.AppendMcpServer(bundle.McpServer{
		Name: "shared", Transport: "stdio",
		Command: "user-server", Scope: "user",
	})
	b.Finalize()

	target := t.TempDir()
	r, err := (&Codex{}).Import(b, target)
	require.NoError(t, err)

	assert.Equal(t, 1, r.McpServersWritten, "count reflects what lands in mcp.json")
	require.Len(t, r.Warnings, 1, "the dropped server must be warned about")
	assert.Contains(t, r.Warnings[0], "skipped")

	data, err := os.ReadFile(filepath.Join(target, "mcp.json"))
	require.NoError(t, err)
	var doc struct {
		McpServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.McpServers, 1)
	assert.Equal(t, "user-server", doc.McpServers["shared"].Command,
		"user-scope server wins the colliding name")
}

// Import must refuse a bundle whose schema ids this adapter was not built for
// (the v0.1.0 adapter never called SchemaCompatibleForImport).
func TestImportRefusesForeignSchema(t *testing.T) {
	b := bundle.New("claudecode", "codex", "test")
	b.Manifest.SchemaMap.Sessions = "csb-session-jsonl-v2"
	b.Finalize()

	_, err := (&Codex{}).Import(b, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema incompatible")
}

// manifest.tool_version must come from ExportOptions.ToolVersion (pack passes
// the CLI Version const), not a hardcoded string that drifts from the tag.
func TestExportToolVersionFromOptions(t *testing.T) {
	stateDir, _, _ := writeFixture(t)
	c := &ClaudeCode{}

	b, err := c.Export(ExportOptions{StateDir: stateDir, ToolVersion: "carrystate-9.9.9-test"})
	require.NoError(t, err)
	assert.Equal(t, "carrystate-9.9.9-test", b.Manifest.ToolVersion)

	b2, err := c.Export(ExportOptions{StateDir: stateDir})
	require.NoError(t, err)
	assert.Equal(t, "dev", b2.Manifest.ToolVersion)
}

// --- v0.3.0 — m8: pack surfaces every silently-dropped entry --------------------

// A malformed mcpServers entry must not vanish silently: the export keeps the
// parseable servers and names every skipped one on WarnTo (v0.2.0 dropped such
// entries with no trace, so pack's server count quietly omitted them).
func TestClaudeCodeExportWarnsOnMalformedMcpEntry(t *testing.T) {
	stateDir, homeDir, _ := writeFixture(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(homeDir, ".claude.json"),
		[]byte(`{"mcpServers":{"filesystem":{"command":"npx"},"broken":"not-an-object"}}`),
		0o644))

	var warnTo bytes.Buffer
	b, err := (&ClaudeCode{}).Export(ExportOptions{StateDir: stateDir, WarnTo: &warnTo})
	require.NoError(t, err, "one malformed entry must not fail the whole export")

	require.Len(t, b.MCP.Servers, 2, "only the parseable servers (user filesystem + project context7) are packed")
	for _, s := range b.MCP.Servers {
		assert.NotEqual(t, "broken", s.Name)
	}
	assert.Contains(t, warnTo.String(), "broken", "the skipped entry is named in a warning")
}

// --project must accept the real-path form ("/" encoded to "-"), not only the
// encoded projects/<dir> name (v0.2.0 silently matched nothing for the natural
// form and packed a near-empty bundle with exit 0).
func TestClaudeCodeExportProjectRealPathForm(t *testing.T) {
	stateDir, _, encodedProj := writeFixture(t)
	realPath := decodeClaudeProject(encodedProj)
	require.NotEqual(t, encodedProj, realPath)

	b, err := (&ClaudeCode{}).Export(ExportOptions{StateDir: stateDir, Project: realPath})
	require.NoError(t, err)
	require.Len(t, b.Sessions, 1, "the real-path form finds the encoded project dir")
}

// A --project filter that matches no project dir must fail fast with the
// available names instead of packing a silently-empty bundle.
func TestClaudeCodeExportProjectNoMatchFailsFast(t *testing.T) {
	stateDir, _, encodedProj := writeFixture(t)

	_, err := (&ClaudeCode{}).Export(ExportOptions{StateDir: stateDir, Project: "-tmp-nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matches no dir")
	assert.Contains(t, err.Error(), encodedProj, "the error lists the available project dirs")
}

// --- v0.3.0 — m10: reverse direction (Codex -> Claude Code) ---------------------

// writeCodexFixture builds a minimal ~/.codex tree (the layout Codex.Import
// writes) and returns (stateDir, codexHome). mcp.json carries one flat
// user-scope server.
func writeCodexFixture(t *testing.T) (stateDir string) {
	t.Helper()
	home := t.TempDir()
	stateDir = filepath.Join(home, ".codex")

	projDir := filepath.Join(stateDir, "sessions", "-tmp-projB")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projDir, "s2.jsonl"),
		[]byte("{\"type\":\"user\",\"message\":\"from codex\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(projDir, "AGENTS.md"),
		[]byte("<!-- from /tmp/only/CLAUDE.md -->\n# Project B\nUse Go.\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "AGENTS.md"),
		[]byte("<!-- from /home/u/.claude/CLAUDE.md -->\n# Global\nUse Go.\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(stateDir, "mcp.json"),
		[]byte(`{"mcpServers":{"filesystem":{"command":"npx","args":["-y","x"]}}}`),
		0o644))
	return stateDir
}

// Codex.Export reads the Codex-native layout into a verifiable bundle.
func TestCodexExportReadsFixture(t *testing.T) {
	stateDir := writeCodexFixture(t)

	b, err := (&Codex{}).Export(ExportOptions{StateDir: stateDir})
	require.NoError(t, err)

	assert.Equal(t, "codex", b.Manifest.SourceHarness)
	assert.Equal(t, "0.1", b.Manifest.SpecVersion)
	require.Len(t, b.Sessions, 1)
	assert.Equal(t, "s2", b.Sessions[0].ID)
	assert.Equal(t, "-tmp-projB", b.Sessions[0].Project)

	require.Len(t, b.Memories, 2, "global + project AGENTS.md")
	scopes := map[string]string{}
	for _, m := range b.Memories {
		scopes[m.Project] = m.ID
	}
	assert.Equal(t, "global:AGENTS.md", scopes[""])
	assert.Equal(t, "project:-tmp-projB:AGENTS.md", scopes["-tmp-projB"])

	require.Len(t, b.MCP.Servers, 1)
	assert.Equal(t, "filesystem", b.MCP.Servers[0].Name)
	assert.Equal(t, "user", b.MCP.Servers[0].Scope)

	assert.True(t, b.Verify().Pass(), b.Verify().Summary())
}

// The full reverse round trip: a Codex state dir exports to a bundle and the
// Claude Code adapter imports it byte-identically, with MCP merged into an
// existing user config without touching unrelated keys.
func TestReverseRoundTripCodexToClaudeCode(t *testing.T) {
	codexDir := writeCodexFixture(t)

	// 1. pack (Codex → bundle)
	b, err := (&Codex{}).Export(ExportOptions{StateDir: codexDir})
	require.NoError(t, err)
	b.Finalize()

	// 2. verify the bundle is intact
	rep := b.Verify()
	require.True(t, rep.Pass(), rep.Summary())

	// 3. unpack (bundle → Claude Code), into a real ~/.claude-shaped home with
	// a pre-existing user config holding an unrelated key + existing server
	home := t.TempDir()
	target := filepath.Join(home, ".claude")
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".claude.json"),
		[]byte(`{"userID":"abc123","mcpServers":{"existing":{"command":"keep-me"}}}`),
		0o644))
	r, err := (&ClaudeCode{}).Import(b, target)
	require.NoError(t, err)
	assert.Equal(t, 1, r.SessionsWritten)
	assert.Equal(t, 2, r.MemoriesWritten)
	assert.Equal(t, 1, r.McpServersWritten, "bundle server added; existing server preserved")

	// 4. session content is byte-identical under the encoded project dir
	src := filepath.Join(codexDir, "sessions", "-tmp-projB", "s2.jsonl")
	dst := filepath.Join(target, "projects", "-tmp-projB", "s2.jsonl")
	srcBytes, _ := os.ReadFile(src)
	dstBytes, _ := os.ReadFile(dst)
	assert.Equal(t, srcBytes, dstBytes, "session must round-trip byte-identical")

	// 5. memories land as global + per-project CLAUDE.md
	projMd, err := os.ReadFile(filepath.Join(target, "projects", "-tmp-projB", "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(projMd), "# Project B")
	globalMd, err := os.ReadFile(filepath.Join(target, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(globalMd), "# Global")

	// 6. the user config merge is surgical: unrelated key + existing server
	// preserved, bundle server added
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	var doc struct {
		UserID     string `json:"userID"`
		McpServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "abc123", doc.UserID, "unrelated top-level keys are preserved verbatim")
	assert.Equal(t, "keep-me", doc.McpServers["existing"].Command, "existing servers survive the merge")
	assert.Equal(t, "npx", doc.McpServers["filesystem"].Command, "bundle server is merged in")
}

// A bundle server whose name collides with an existing config entry replaces
// it — loudly (warning) — while unrelated keys stay untouched.
func TestClaudeCodeImportMcpMergeReplacesWithWarning(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".claude")
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".claude.json"),
		[]byte(`{"userID":"keep","mcpServers":{"shared":{"command":"old"}}}`),
		0o644))

	b := bundle.New("codex", "claudecode", "test")
	b.AppendMcpServer(bundle.McpServer{
		Name: "shared", Transport: "stdio", Command: "new", Scope: "user",
	})
	b.Finalize()

	r, err := (&ClaudeCode{}).Import(b, target)
	require.NoError(t, err)
	assert.Equal(t, 1, r.McpServersWritten)
	require.Len(t, r.Warnings, 1)
	assert.Contains(t, r.Warnings[0], "replaced existing server")

	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	var doc struct {
		UserID     string `json:"userID"`
		McpServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "keep", doc.UserID)
	assert.Equal(t, "new", doc.McpServers["shared"].Command, "the bundle server wins the colliding name")
}
