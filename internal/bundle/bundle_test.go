package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: build a small but representative bundle
func sampleBundle() *Bundle {
	b := New("claudecode", "", "carrystate-test")
	b.AppendSession(SessionEntry{
		ID:        "sess-1",
		Project:   "-Users-test-proj",
		Source:    "/home/u/.claude/projects/-Users-test-proj/sess-1.jsonl",
		Format:    "jsonl",
		Content:   "{\"type\":\"user\",\"message\":\"hi\"}\n{\"type\":\"assistant\",\"message\":\"hello\"}\n",
		LineCount: 2,
	})
	b.AppendMemory(MemoryEntry{
		ID:      "global:CLAUDE.md",
		Project: "",
		Source:  "/home/u/.claude/CLAUDE.md",
		Format:  "markdown",
		Content: "# Global\nUse Go.\n",
	})
	b.AppendMcpServer(McpServer{
		Name:      "filesystem",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem"},
		Scope:     "user",
	})
	b.Finalize()
	return b
}

func TestNewBundleHasEmptyChecksums(t *testing.T) {
	b := New("claudecode", "codex", "test")
	assert.Equal(t, SpecVersion, b.Manifest.SpecVersion)
	assert.Equal(t, "claudecode", b.Manifest.SourceHarness)
	assert.Equal(t, "codex", b.Manifest.TargetHarness)
	assert.Equal(t, "csb-session-jsonl-v1", b.Manifest.SchemaMap.Sessions)
	require.NotNil(t, b.Checksums.PerEntry)
	assert.Empty(t, b.Checksums.BundleMerkleRoot, "no Finalize yet → empty root")
}

func TestAppendSessionSetsSHA256(t *testing.T) {
	b := New("claudecode", "", "test")
	b.AppendSession(SessionEntry{
		ID: "s1", Content: "line\n", Format: "jsonl",
	})
	require.Len(t, b.Sessions, 1)
	assert.Equal(t, SessionContentSHA(b.Sessions[0]), b.Sessions[0].SHA256)
	assert.Equal(t, b.Sessions[0].SHA256, b.Checksums.PerEntry["session:s1"])
}

func TestFinalizeComputesMerkleAndCounts(t *testing.T) {
	b := sampleBundle()
	require.NotEmpty(t, b.Checksums.BundleMerkleRoot)
	assert.Equal(t, 1, b.Manifest.EntryCount.Sessions)
	assert.Equal(t, 1, b.Manifest.EntryCount.Memories)
	assert.Equal(t, 1, b.Manifest.EntryCount.McpServers)
	// merkle root is over 3 entries (session, memory, mcp)
	assert.Len(t, b.Checksums.PerEntry, 3)
}

func TestWriteReadRoundTrip(t *testing.T) {
	b := sampleBundle()
	dir := t.TempDir()
	path := filepath.Join(dir, "project.csb")

	require.NoError(t, b.Write(path))

	loaded, err := Read(path)
	require.NoError(t, err)
	assert.Equal(t, b.Manifest.SourceHarness, loaded.Manifest.SourceHarness)
	assert.Equal(t, b.Manifest.EntryCount, loaded.Manifest.EntryCount)
	assert.Equal(t, b.Checksums.BundleMerkleRoot, loaded.Checksums.BundleMerkleRoot)

	// the per-entry sha256 must survive the JSON round-trip unchanged
	require.Len(t, loaded.Sessions, 1)
	assert.Equal(t, b.Sessions[0].SHA256, loaded.Sessions[0].SHA256)
	assert.Equal(t, b.Sessions[0].Content, loaded.Sessions[0].Content,
		"session content must be byte-identical after JSON round-trip")
}

func TestVerifyPassesOnIntactBundle(t *testing.T) {
	b := sampleBundle()
	b.Finalize()
	r := b.Verify()
	assert.True(t, r.Pass(), r.Summary())
	assert.Empty(t, r.Mismatched)
	assert.Len(t, r.Matched, 4) // 3 entries + merkle root
}

func TestVerifyDetectsTamperedSessionContent(t *testing.T) {
	b := sampleBundle()
	b.Finalize()
	// tamper with the content but NOT the stored sha256 → per-entry mismatch
	b.Sessions[0].Content = "{\"type\":\"user\",\"message\":\"TAMPERED\"}\n"
	r := b.Verify()
	assert.False(t, r.Pass())
	require.Len(t, r.Mismatched, 1)
	assert.Equal(t, "session:sess-1", r.Mismatched[0].Entry)
}

func TestVerifyDetectsTamperedMerkleRoot(t *testing.T) {
	b := sampleBundle()
	b.Finalize()
	b.Checksums.BundleMerkleRoot = "deadbeef" + strings.Repeat("0", 56) // wrong root, intact entries
	r := b.Verify()
	assert.False(t, r.Pass())
	// the merkle root mismatch is reported (entries still match)
	found := false
	for _, m := range r.Mismatched {
		if m.Entry == "bundle:merkle_root" {
			found = true
		}
	}
	assert.True(t, found, "merkle root mismatch must be reported")
}

func TestValidateRejectsUnknownSpecVersion(t *testing.T) {
	b := sampleBundle()
	b.Manifest.SpecVersion = "9.9"
	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestValidatePassesOnGoodBundle(t *testing.T) {
	b := sampleBundle()
	b.Finalize()
	assert.NoError(t, b.Validate())
}

func TestSchemaCompatibleForImport(t *testing.T) {
	b := sampleBundle()
	// v0.1 adapters all use csb-*-v1; should be compatible
	err := b.SchemaCompatibleForImport("csb-session-jsonl-v1", "csb-memory-markdown-v1", "csb-mcp-servers-v1")
	assert.NoError(t, err)
	// mismatched schema → error
	err = b.SchemaCompatibleForImport("csb-session-xml-v2", "csb-memory-markdown-v1", "csb-mcp-servers-v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sessions")
}

func TestReadRejectsNonCSB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-bundle.csb")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o644))
	_, err := Read(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing spec_version")
}

func TestMcpServerCanonicalExcludesSHA256(t *testing.T) {
	// two servers identical except for sha256 field → same content sha
	s1 := McpServer{Name: "x", Transport: "stdio", Command: "c", Scope: "user"}
	s2 := s1
	s2.SHA256 = "different-but-irrelevant"
	assert.Equal(t, McpServerContentSHA(s1), McpServerContentSHA(s2))
}

func TestSessionContentSHAIgnoresMetadata(t *testing.T) {
	// two entries with same content but different source/project → same sha
	a := SessionEntry{ID: "1", Content: "x\n", Source: "/a", Project: "p"}
	b := SessionEntry{ID: "1", Content: "x\n", Source: "/b", Project: "q"}
	assert.Equal(t, SessionContentSHA(a), SessionContentSHA(b))
}

func TestBundleJSONIsStableShape(t *testing.T) {
	// the .csb must have all 5 top-level keys present (even if empty), so a
	// verifier from another language can rely on the shape
	b := sampleBundle()
	data, err := json.Marshal(b)
	require.NoError(t, err)
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &top))
	for _, k := range []string{"manifest", "sessions", "memories", "mcp", "checksums"} {
		_, ok := top[k]
		assert.True(t, ok, "top-level key %q must be present", k)
	}
}

func TestManifestYAMLIsAuditable(t *testing.T) {
	b := sampleBundle()
	yml, err := b.ManifestYAML()
	require.NoError(t, err)
	// YAML sidecar must surface the fields an auditor checks at a glance
	assert.Contains(t, yml, "source_harness: claudecode")
	assert.Contains(t, yml, "spec_version: \"0.1\"")
	assert.Contains(t, yml, "sessions: 1")
	assert.Contains(t, yml, "mcp_servers: 1")
}
