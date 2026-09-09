// Package bundle defines the CarryState Bundle (.csb) — a harness-agnostic,
// content-addressed state-interchange format for coding-agent state.
//
// A bundle carries three normalized entry kinds — sessions, memories, and MCP
// server config — plus a manifest and merkle-checksummed integrity block. Every
// entry is sha256-addressed by its canonical content; the bundle root is a
// merkle hash over the sorted per-entry digests, so an airgapped ops engineer
// can verify a bundle end-to-end with `carrystate verify` and no network.
package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/SuperMarioYL/carrystate/internal/checksum"
	"gopkg.in/yaml.v3"
)

// SpecVersion is the .csb schema version this package reads and writes.
const SpecVersion = "0.1"

// Extension is the canonical CarryState Bundle filename suffix.
const Extension = ".csb"

// Schema ids carried in the manifest schema_map. An adapter that wants to
// import a bundle without a translator upgrade must declare support for these
// exact ids (see SchemaCompatibleForImport) — consts, not literals, so the
// import-side check can never drift from what New() writes.
const (
	SessionSchemaID = "csb-session-jsonl-v1"
	MemorySchemaID  = "csb-memory-markdown-v1"
	McpSchemaID     = "csb-mcp-servers-v1"
)

// Manifest describes the bundle: which harness it came from, where it is going,
// when it was packed, and which schema versions each entry kind uses.
type Manifest struct {
	SpecVersion   string     `json:"spec_version" yaml:"spec_version"`
	SourceHarness string     `json:"source_harness" yaml:"source_harness"`
	TargetHarness string     `json:"target_harness,omitempty" yaml:"target_harness,omitempty"`
	CreatedAt     time.Time  `json:"created_at" yaml:"created_at"`
	SchemaMap     SchemaMap  `json:"schema_map" yaml:"schema_map"`
	EntryCount    EntryCount `json:"entry_count" yaml:"entry_count"`
	ToolVersion   string     `json:"tool_version,omitempty" yaml:"tool_version,omitempty"`
}

// SchemaMap records the per-kind schema id used by this bundle, so a target
// adapter knows which translator revision to apply.
type SchemaMap struct {
	Sessions string `json:"sessions" yaml:"sessions"`
	Memories string `json:"memories" yaml:"memories"`
	MCP      string `json:"mcp" yaml:"mcp"`
}

// EntryCount is a small summary used by `verify` and `unpack --dry-run` to
// surface "what's in this bundle" without scanning every entry.
type EntryCount struct {
	Sessions   int `json:"sessions" yaml:"sessions"`
	Memories   int `json:"memories" yaml:"memories"`
	McpServers int `json:"mcp_servers" yaml:"mcp_servers"`
}

// SessionEntry is one normalized agent session transcript. Claude Code stores
// sessions as one JSONL file per session UUID under
// ~/.claude/projects/<project>/*.jsonl; the bundle carries the raw file
// content verbatim as a string (Content) plus a line count for quick sizing.
// Storing raw bytes — not parsed []json.RawMessage — is deliberate: a parsed
// slice gets re-indented by encoding/json's MarshalIndent, which would change
// the bytes and break the per-entry sha256 round-trip. Raw bytes are the only
// way to keep `verify` byte-identical to `pack`.
type SessionEntry struct {
	ID        string `json:"id"`         // session UUID (filename stem)
	Project   string `json:"project"`    // project dir name under projects/
	Source    string `json:"source"`     // original file path
	Format    string `json:"format"`     // "jsonl"
	Content   string `json:"content"`    // raw session file content (JSONL)
	LineCount int    `json:"line_count"` // number of JSONL lines
	SHA256    string `json:"sha256"`     // content checksum
}

// MemoryEntry is one normalized agent memory / instruction file. For Claude
// Code this is a project CLAUDE.md (or ~/.claude/CLAUDE.md global memory).
type MemoryEntry struct {
	ID      string `json:"id"`      // stable id, e.g. "project:CLAUDE.md"
	Project string `json:"project"` // project name or "" for global
	Source  string `json:"source"`  // original file path
	Format  string `json:"format"`  // "markdown"
	Content string `json:"content"` // raw file content
	SHA256  string `json:"sha256"`  // content checksum
}

// McpServer is one normalized MCP server entry. Claude Code keeps these in
// ~/.claude.json (user-scope mcpServers) and project .mcp.json (project-scope);
// the bundle carries both, tagged by Scope, so the target harness can decide
// where to write each.
type McpServer struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "sse" | "http"
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Scope     string            `json:"scope"` // "user" | "project"
	SHA256    string            `json:"sha256"`
}

// McpConfig is the normalized MCP slice of a bundle.
type McpConfig struct {
	Servers  []McpServer     `json:"servers"`
	Roots    []string        `json:"roots,omitempty"`
	Sampling json.RawMessage `json:"sampling,omitempty"`
}

// Checksums holds the integrity block: per-entry sha256 (keyed by stable entry
// id) and the merkle root over the sorted per-entry digests.
type Checksums struct {
	PerEntry         map[string]string `json:"per_entry_sha256"`
	BundleMerkleRoot string            `json:"bundle_merkle_root"`
}

// Bundle is the top-level .csb document.
type Bundle struct {
	Manifest  Manifest       `json:"manifest"`
	Sessions  []SessionEntry `json:"sessions"`
	Memories  []MemoryEntry  `json:"memories"`
	MCP       McpConfig      `json:"mcp"`
	Checksums Checksums      `json:"checksums"`
}

// New returns an empty bundle with a manifest pre-populated for the given
// source/target harness pair and the current tool version.
func New(source, target, toolVersion string) *Bundle {
	return &Bundle{
		Manifest: Manifest{
			SpecVersion:   SpecVersion,
			SourceHarness: source,
			TargetHarness: target,
			CreatedAt:     time.Now().UTC().Truncate(time.Second),
			SchemaMap: SchemaMap{
				Sessions: SessionSchemaID,
				Memories: MemorySchemaID,
				MCP:      McpSchemaID,
			},
			ToolVersion: toolVersion,
		},
		Checksums: Checksums{PerEntry: map[string]string{}},
	}
}

// AppendSession appends a session entry; the SHA256 is recomputed.
func (b *Bundle) AppendSession(e SessionEntry) {
	e.SHA256 = SessionContentSHA(e)
	b.Sessions = append(b.Sessions, e)
	b.Checksums.PerEntry["session:"+e.ID] = e.SHA256
}

// AppendMemory appends a memory entry; the SHA256 is recomputed.
func (b *Bundle) AppendMemory(e MemoryEntry) {
	e.SHA256 = MemoryContentSHA(e)
	b.Memories = append(b.Memories, e)
	b.Checksums.PerEntry["memory:"+e.ID] = e.SHA256
}

// AppendMcpServer appends an MCP server; the SHA256 is recomputed.
func (b *Bundle) AppendMcpServer(s McpServer) {
	s.SHA256 = McpServerContentSHA(s)
	b.MCP.Servers = append(b.MCP.Servers, s)
	b.Checksums.PerEntry["mcp:"+s.Name] = s.SHA256
}

// Finalize recomputes the manifest entry counts and the merkle root. Call once
// after all entries are appended and before Write.
func (b *Bundle) Finalize() {
	b.Manifest.EntryCount = EntryCount{
		Sessions:   len(b.Sessions),
		Memories:   len(b.Memories),
		McpServers: len(b.MCP.Servers),
	}
	b.Checksums.PerEntry = map[string]string{}
	for _, e := range b.Sessions {
		b.Checksums.PerEntry["session:"+e.ID] = e.SHA256
	}
	for _, e := range b.Memories {
		b.Checksums.PerEntry["memory:"+e.ID] = e.SHA256
	}
	for _, s := range b.MCP.Servers {
		b.Checksums.PerEntry["mcp:"+s.Name] = s.SHA256
	}
	b.Checksums.BundleMerkleRoot = b.merkleRoot()
}

// merkleRoot sorts the per-entry digests lexicographically and folds them into
// a single sha256 merkle root (pairwise hashing; odd tail is duplicated so the
// tree is balanced regardless of entry count).
func (b *Bundle) merkleRoot() string {
	digests := make([]string, 0, len(b.Checksums.PerEntry))
	for _, h := range b.Checksums.PerEntry {
		digests = append(digests, h)
	}
	sort.Strings(digests)
	return checksum.MerkleRootHex(digests)
}

// Verify re-derives every per-entry checksum and the merkle root from the
// bundle's content and reports the first mismatch (nil if all match). This is
// the deterministic, no-LLM behavior-equivalence check `carrystate verify` runs.
func (b *Bundle) Verify() *VerifyReport {
	r := &VerifyReport{}

	for i := range b.Sessions {
		e := b.Sessions[i]
		want := e.SHA256
		got := SessionContentSHA(e)
		if got != want {
			r.AddMismatch("session:"+e.ID, want, got)
		} else {
			r.Matched = append(r.Matched, "session:"+e.ID)
		}
	}
	for i := range b.Memories {
		e := b.Memories[i]
		want := e.SHA256
		got := MemoryContentSHA(e)
		if got != want {
			r.AddMismatch("memory:"+e.ID, want, got)
		} else {
			r.Matched = append(r.Matched, "memory:"+e.ID)
		}
	}
	for i := range b.MCP.Servers {
		s := b.MCP.Servers[i]
		want := s.SHA256
		got := McpServerContentSHA(s)
		if got != want {
			r.AddMismatch("mcp:"+s.Name, want, got)
		} else {
			r.Matched = append(r.Matched, "mcp:"+s.Name)
		}
	}

	// re-derive merkle root and compare
	saved := b.Checksums.BundleMerkleRoot
	b2 := *b
	b2.Finalize()
	if b2.Checksums.BundleMerkleRoot != saved {
		r.AddMismatch("bundle:merkle_root", saved, b2.Checksums.BundleMerkleRoot)
	} else {
		r.Matched = append(r.Matched, "bundle:merkle_root")
	}
	return r
}

// VerifyReport is the result of Bundle.Verify — a list of matched and mismatched
// entry checksums plus a single Pass boolean.
type VerifyReport struct {
	Matched    []string
	Mismatched []Mismatch
}

// Mismatch is one entry whose re-derived checksum did not equal the stored one.
type Mismatch struct {
	Entry string
	Want  string
	Got   string
}

// AddMismatch records a checksum mismatch.
func (r *VerifyReport) AddMismatch(entry, want, got string) {
	r.Mismatched = append(r.Mismatched, Mismatch{Entry: entry, Want: want, Got: got})
}

// Pass is true iff no mismatches were recorded.
func (r *VerifyReport) Pass() bool { return len(r.Mismatched) == 0 }

// Summary returns a one-line human-readable summary.
func (r *VerifyReport) Summary() string {
	return fmt.Sprintf("%d matched, %d mismatched", len(r.Matched), len(r.Mismatched))
}

// --- content canonicalization for per-entry sha256 ----------------------------

// SessionContentSHA returns the sha256 of a session entry's raw JSONL content.
// The SHA256 field itself and structural metadata (Source/Project/Format) are
// excluded so the digest is stable across repacks.
func SessionContentSHA(e SessionEntry) string {
	return checksum.SHA256Hex([]byte(e.Content))
}

// MemoryContentSHA returns the sha256 of a memory entry's raw content bytes.
func MemoryContentSHA(e MemoryEntry) string {
	return checksum.SHA256Hex([]byte(e.Content))
}

// McpServerContentSHA returns the sha256 of an MCP server's canonical JSON
// (name/transport/command/args/env/url/scope — excluding the SHA256 field).
func McpServerContentSHA(s McpServer) string {
	canon := mcpServerCanonical(s)
	return checksum.SHA256Hex([]byte(canon))
}

// mcpServerCanonical emits a stable, sorted-key JSON body for an MCP server so
// two adapters packing the same logical server produce the same digest.
func mcpServerCanonical(s McpServer) string {
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

// --- file I/O -----------------------------------------------------------------

// Write serializes the bundle to a .csb file as pretty JSON. JSON (not tar/zip)
// is deliberate: a single document is trivially content-addressable, diffable
// with `jq`, and verifiable by `sha256sum` — the airgap-friendly choice.
func (b *Bundle) Write(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	// trailing newline so the file is POSIX-text-friendly and stable across tools
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// Read loads a .csb file into a Bundle.
func Read(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse .csb (is this a CarryState Bundle?): %w", err)
	}
	if b.Manifest.SpecVersion == "" {
		return nil, fmt.Errorf("missing spec_version — not a .csb file")
	}
	return &b, nil
}

// EntryIDs returns the stable entry ids (kind-prefixed) in deterministic order.
func (b *Bundle) EntryIDs() []string {
	ids := make([]string, 0, len(b.Sessions)+len(b.Memories)+len(b.MCP.Servers))
	for _, e := range b.Sessions {
		ids = append(ids, "session:"+e.ID)
	}
	for _, e := range b.Memories {
		ids = append(ids, "memory:"+e.ID)
	}
	for _, s := range b.MCP.Servers {
		ids = append(ids, "mcp:"+s.Name)
	}
	sort.Strings(ids)
	return ids
}

// MerkleRoot returns the bundle's stored merkle root (does not recompute).
func (b *Bundle) MerkleRoot() string {
	return b.Checksums.BundleMerkleRoot
}

// ManifestYAML returns the manifest serialized as YAML — a human + auditor
// friendly sidecar to the JSON .csb. An airgapped ops team can log this YAML
// alongside the bundle's merkle root in a 合规审计 trail without parsing the
// full JSON document.
func (b *Bundle) ManifestYAML() (string, error) {
	out, err := yaml.Marshal(b.Manifest)
	if err != nil {
		return "", fmt.Errorf("marshal manifest as yaml: %w", err)
	}
	return string(out), nil
}

// Short returns the first 12 hex chars of the merkle root, or "(empty)". It's
// a human-facing bundle fingerprint, not a security-truncation.
func (b *Bundle) Short() string {
	r := b.Checksums.BundleMerkleRoot
	if len(r) >= 12 {
		return r[:12]
	}
	return "(empty)"
}
