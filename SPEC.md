# CarryState Bundle Specification — `.csb` v0.1

> Status: **stable** (v0.1). The `.csb` format is the core primitive CarryState
> ships; a future v0.2 may add entry kinds (e.g. skills, tools) or a
> translator registry, but v0.1 bundles will remain readable by v0.2 verifiers.
>
> This spec is the single source of truth for the on-disk shape of a CarryState
> Bundle. The Go types in `internal/bundle/bundle.go` are the canonical
> implementation; this document is the human-readable contract for adapter
> authors who want to produce or consume `.csb` files from another language.

## 1. Design goals

1. **Harness-agnostic.** A bundle carries normalized agent state — sessions,
   memories, MCP server config — that any coding-agent harness can produce or
   consume via a thin adapter, without the two harnesses knowing about each
   other.
2. **Content-addressed + merkle-rooted.** Every entry is sha256-addressed by
   its canonical content; the bundle root is a merkle hash over the sorted
   per-entry digests. A single 32-byte value attests to the whole bundle, so an
   airgapped ops engineer can prove integrity with `carrystate verify` and no
   network.
3. **Lossless + deterministic.** v0.1 does pure byte-shaping translation — no
   LLM, no semantic merge, no dedup. A `pack → unpack → verify` round-trip is
   byte-identical for session transcripts and content-stable for memories/MCP.
4. **Airgap-friendly.** A bundle is a single self-contained JSON document. No
   external resources, no network, no daemons. Diffable with `jq`, verifiable
   with `sha256sum`, portable via USB.

## 2. Why JSON (not tar/zip)

A `.csb` file is a single UTF-8 JSON document (pretty-printed, trailing
newline). JSON is the deliberate choice over a tar/zip archive:

- **Self-describing + diffable.** `jq '.' project.csb` shows exactly what's
  inside; a tar archive is opaque without extraction.
- **Trivially content-addressable.** The whole file is one sha256; per-entry
  digests are computed over canonical content slices, not embedded blobs.
- **Stable across tools.** `sha256sum` verifies a bundle without any CarryState
  tool installed — useful in a strict airgap where only the verifier binary is
  allow-listed.

The trade-off (no streaming for very large sessions) is acceptable for v0.1:
agent session transcripts are KB-to-MB, and the bundle is designed for
per-project portability, not whole-enterprise archival.

## 3. Top-level structure

```jsonc
{
  "manifest":   { ... },   // metadata: source/target harness, schema map, counts
  "sessions":   [ ... ],   // normalized session transcripts (JSONL content)
  "memories":   [ ... ],   // normalized memory / instruction files (markdown)
  "mcp":        { ... },    // normalized MCP server config
  "checksums":  { ... }     // per-entry sha256 + bundle merkle root
}
```

All four top-level keys are present in every v0.1 bundle (a slice/object may
be empty, but the key exists). A verifier rejects a bundle missing any key.

## 4. `manifest`

```jsonc
"manifest": {
  "spec_version":   "0.1",          // .csb schema version (this spec)
  "source_harness": "claudecode",  // harness id that produced the bundle
  "target_harness": "",            // optional: intended target harness id
  "created_at":     "2026-08-06T10:29:45Z",  // UTC, second precision
  "schema_map": {
    "sessions": "csb-session-jsonl-v1",
    "memories": "csb-memory-markdown-v1",
    "mcp":      "csb-mcp-servers-v1"
  },
  "entry_count": { "sessions": 1, "memories": 1, "mcp_servers": 1 },
  "tool_version": "carrystate-0.1.0"  // the binary that packed it
}
```

- `spec_version` — `"0.1"` for this spec. A verifier rejects unknown versions
  rather than guessing.
- `source_harness` / `target_harness` — lowercase harness ids (e.g.
  `claudecode`, `codex`). `target_harness` is optional and informational; a
  bundle is not bound to one target (the format is symmetric).
- `schema_map` — the per-kind schema id. v0.1 ships three ids (above); a v0.2
  bundle with new ids fails `SchemaCompatibleForImport` until a translator is
  registered. This is how the format evolves without breaking old verifiers.
- `entry_count` — a summary so `unpack --dry-run` and `verify` can surface
  "what's in this bundle" without scanning every entry.

## 5. `sessions`

Each entry is one agent session transcript:

```jsonc
"sessions": [
  {
    "id":         "abc123",                 // session UUID (filename stem)
    "project":    "-Users-test-myproject",  // source project dir name
    "source":     "/home/u/.claude/projects/-Users-test-myproject/abc123.jsonl",
    "format":     "jsonl",
    "content":    "{\"type\":\"user\",\"message\":\"hello\"}\n...",  // raw file bytes
    "line_count": 2,
    "sha256":     "9e9f2272..."            // sha256(content)
  }
]
```

- `content` carries the **raw** session file bytes as a UTF-8 string. The bytes
  are stored verbatim — not parsed into a `[]json.RawMessage` — because
  `encoding/json`'s `MarshalIndent` re-indents parsed JSON, which would change
  the bytes and break the per-entry sha256 round-trip. A v0.2 LLM-merge layer
  that wants parsed lines can re-parse `content` itself.
- `sha256` = `sha256(content)` (lowercase hex). The `sha256` field itself and
  structural metadata (`source`, `project`, `format`) are excluded from the
  digest so it is stable across repacks.
- `format` is `"jsonl"` for v0.1 (the only session format Claude Code and Codex
  both speak). A future `format: "xml"` entry would carry its own schema id in
  `schema_map.sessions`.

## 6. `memories`

Each entry is one memory / instruction file (markdown):

```jsonc
"memories": [
  {
    "id":      "global:CLAUDE.md",          // stable id (kind:scope:name)
    "project": "",                          // "" = global, else project dir name
    "source":  "/home/u/.claude/CLAUDE.md",
    "format":  "markdown",
    "content": "# Global instructions\nAlways use Go.\n",
    "sha256":  "a1b2c3..."
  }
]
```

- `id` is `global:<name>` for global memory or `project:<project>:<name>` for
  project-scoped. Adapters MUST NOT reuse an id within one bundle.
- `sha256` = `sha256(content)`.

## 7. `mcp`

```jsonc
"mcp": {
  "servers": [
    {
      "name":      "filesystem",
      "transport": "stdio",            // "stdio" | "sse" | "http"
      "command":   "npx",
      "args":      ["-y", "@modelcontextprotocol/server-filesystem", "~/"],
      "env":       {},                  // optional
      "scope":     "user",             // "user" | "project"
      "sha256":    "d4e5f6..."
    }
  ],
  "roots":    [],                        // optional, reserved for v0.2
  "sampling": null                       // optional, raw JSON, reserved for v0.2
}
```

- `sha256` = `sha256(canonical_json(name, transport, command, args, env, url, scope))`
  where `canonical_json` is a stable, sorted-key JSON encoding (the Go
  `encoding/json` marshal of a struct with the field order above). The `sha256`
  field is excluded from its own digest (no circularity).
- Project-scope servers are prefixed `"<project>/<name>"` on export so they
  don't collide with user-scope servers of the same name; the Codex adapter
  strips the prefix on import to recover the original name.
- `roots` and `sampling` are reserved for v0.2 (MCP roots + sampling config);
  v0.1 adapters ignore them.

## 8. `checksums`

```jsonc
"checksums": {
  "per_entry_sha256": {
    "session:abc123":         "9e9f2272...",
    "memory:global:CLAUDE.md": "a1b2c3...",
    "mcp:filesystem":          "d4e5f6..."
  },
  "bundle_merkle_root": "9542d49c31b3..."
}
```

- `per_entry_sha256` keys are `<kind>:<id>` (`session:`, `memory:`, `mcp:`).
  Values are the per-entry sha256 from §5–7.
- `bundle_merkle_root` — a Bitcoin-style merkle root over the sorted per-entry
  digests:
  1. Collect all per-entry sha256 hex strings; sort lexicographically.
  2. Decode each to 32 bytes (fall back to hashing the raw string if a digest
     is malformed — defensive; the Go packer always produces valid hex).
  3. Pairwise: `parent = sha256(left || right)`. On an odd tail, duplicate the
     last node so the tree stays balanced.
  4. Repeat until one node remains. Its hex is the merkle root.
  5. Empty bundle → `sha256("")` (a well-defined, non-zero value).

## 9. Verification algorithm (`carrystate verify`)

1. Read + parse the JSON. Reject if `manifest.spec_version` is not `"0.1"`.
2. For each entry (sessions, memories, mcp servers): re-derive `sha256` from the
   entry's canonical content (§5–7) and compare to the stored `sha256` field.
   Record any mismatch.
3. Re-derive the merkle root (§8) from the per-entry digests and compare to
   `checksums.bundle_merkle_root`. Record a mismatch on `bundle:merkle_root`.
4. **Pass** iff zero mismatches. Print matched/mismatched counts and exit 0/1.

This is the deterministic, no-LLM behavior-equivalence gate. v0.1 does not
re-run the agent on a memory prompt; it proves the round-trip is **lossless at
the byte level**, which is the v0.1 definition of behavior-equivalence.

## 10. Versioning

- `spec_version` is bumped when the **on-disk shape** changes in a way a v0.1
  verifier cannot ignore (new required key, removed field, changed checksum
  algorithm). v0.1 verifiers reject unknown `spec_version`s.
- `schema_map.<kind>` is bumped when a **single entry kind's** schema changes
  (e.g. sessions gain an `xml` format). A v0.1 verifier reads the entry but a
  v0.1 adapter refuses to import it (`SchemaCompatibleForImport`) until a
  translator is registered — this is the extension point for v0.2 LLM-merge and
  new harness adapters.

## 11. Out of scope for v0.1

(See `mvp_plan.md` §6 for the full list.) The bundle format itself is
symmetric; the v0.1 *adapters* are directional (Claude Code → Codex). A
reverse Codex → Claude Code adapter is a v0.2 adapter against the same
`.csb` format — no spec change required, just a new `Adapter` implementation.
