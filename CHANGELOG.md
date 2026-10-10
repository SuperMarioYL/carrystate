# Changelog

All notable changes to CarryState are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] — 2026-10-11

### Fixed — m8 (pack export drops)
- `pack` no longer silently drops state on the export side: a malformed
  `mcpServers` entry, an unreadable project `CLAUDE.md`, or an unreadable
  project `.mcp.json` are each skipped with a stderr warning instead of
  vanishing from the bundle without a trace.
- `pack --project` now also accepts the real-path form (`/Users/me/app` finds
  the encoded `-Users-me-app` dir), and a filter that matches no project dir
  fails fast listing the available dirs instead of exiting 0 with a
  near-empty bundle.

### Fixed — m9 (verify accepts empty content)
- `verify` and `unpack` no longer reject bundles whose entries have empty
  content. An empty (0-byte) `CLAUDE.md` is legitimate user state; v0.2.0
  packed it and then failed its own verifier ("content is empty"). The
  lossless contract is restored: what pack emits, verify accepts.

### Added — m10 (reverse direction Codex → Claude Code)
- The adapter pair is bidirectional: `carrystate pack --harness codex` reads
  the Codex-native layout (`sessions/<project>/*.jsonl`, per-project + global
  `AGENTS.md`, `mcp.json`) into a bundle, and `carrystate unpack --harness
  claudecode` writes the Claude Code-native layout (`projects/<project>/*.jsonl`,
  `CLAUDE.md` global + per-project).
- The Claude Code user-scope MCP merge is surgical: only the `mcpServers` key
  of `~/.claude.json` is replaced — every other top-level key (session
  history, project state) is preserved, and a same-name replacement is
  reported as a warning.

### Housekeeping — m11
- Version bumped to 0.3.0 in lockstep across all surfaces: `VERSION`,
  `carrystate --version`, `manifest.tool_version`, and `web/site.json`
  `meta.content_version`; the pack/unpack report value columns are aligned.

### Commitment update
- The v0.1.0 note "reverse direction + OpenCode/Grok Build/Hermes Agent
  adapters are v0.2" is superseded: the reverse Codex → Claude Code adapter
  shipped in v0.3.0 (m10). Remaining roadmap: OpenCode/Grok Build/Hermes
  Agent adapters and LLM-based memory reconciliation.

## [0.2.0] — 2026-09-09

### Fixed — m4 (unpack fidelity)
- `unpack` no longer drops a project's memories when the bundle carries no
  sessions for it: per-project `AGENTS.md` files are written for every project
  with scoped memories, not only for projects seen in the sessions loop.
- Per-project `AGENTS.md` write failures are now reported as import warnings
  instead of being silently discarded.
- MCP servers whose prefix-stripped names collide no longer overwrite each
  other silently in `mcp.json`: the user-scope server wins, the dropped server
  is recorded as a warning, and the report counts what actually landed.

### Fixed — m5 (structural validation gate)
- `verify` and `unpack` now run the bundle structural validation (supported
  `manifest.spec_version`, required manifest fields, entries without
  checksums) before the per-entry sha256 comparison, as the schema docs always
  promised — a tampered `spec_version` such as "9.9" previously passed verify.
- `unpack --harness codex` refuses bundles whose schema ids the adapter was
  not built for (`SchemaCompatibleForImport`) instead of writing them blind.

### Changed — m6 (pack reliability)
- `pack` warns on stderr when the `.manifest.yaml` audit sidecar cannot be
  written, instead of ignoring the failure.
- `manifest.tool_version` now comes from the binary's `Version` const (single
  source of truth) instead of a hardcoded string.

### Housekeeping — m7
- Version bumped to 0.2.0 in lockstep across all surfaces: `VERSION`,
  `carrystate --version`, `manifest.tool_version`, and `web/site.json`
  `meta.content_version`.

## [0.1.0] — 2026-08-06

### Added — m1 (pack)
- `carrystate pack --harness claudecode` reads `~/.claude` and emits a
  content-addressed `.csb` (CarryState Bundle) with sessions (JSONL), memories
  (`CLAUDE.md` global + per-project), and MCP config (`~/.claude.json`
  user-scope + `.mcp.json` project-scope).
- Per-entry `sha256` + a Bitcoin-style merkle root over the sorted digests.
- A `.manifest.yaml` sidecar is emitted next to the `.csb` for 合规审计 logs.
- `internal/bundle` — the Bundle types + JSON read/write + `Verify()`.
- `internal/checksum` — stdlib-only `sha256` + `MerkleRootHex`.
- `internal/harness/claudecode.go` — the Claude Code Export adapter.

### Added — m2 (unpack round-trip)
- `carrystate unpack --harness codex` writes a `.csb` into a Codex-native
  layout (`sessions/<project>/<id>.jsonl`, `AGENTS.md`, `mcp.json`).
- `carrystate verify --bundle` re-derives every per-entry `sha256` + the merkle
  root; green check = lossless round-trip. This is the v0.1 deterministic
  behavior-equivalence gate (no LLM).
- `internal/harness/codex.go` — the Codex Import adapter.
- `unpack` refuses to write a bundle that fails `verify` (tamper-safety).

### Added — m3 (spec + publish)
- `SPEC.md` — the human-readable `.csb` v0.1 interchange spec (single source
  of truth for adapter authors in any language).
- `internal/bundle/schema.go` — `Validate()` + `SchemaCompatibleForImport()`.
- `internal/harness/adapter.go` — the symmetric `Adapter` interface + registry;
  v0.1 adapters return `ErrUnsupportedDirection` for the unimplemented side.
- `internal/translate/translate.go` — cross-harness memory-merge + MCP
  canonicalization helpers (lossless, deterministic).
- Bilingual README (`README.md` 中文 + `README.en.md` English), animated
  hero/atlas SVG pairs (house style), `docs/demo.tape`, release + demo CI.

### Commitment
- v0.1 is one-directional (Claude Code → Codex). The bundle format is
  symmetric; reverse direction + OpenCode/Grok Build/Hermes Agent adapters
  are v0.2.
- OSS core is feature-complete and free forever; the on-prem enterprise license
  (¥50–200k/site/yr) buys 信创 SLA + 合规审计 + custom adapters, not features.

(The reverse-direction commitment above shipped in v0.3.0 — see the [0.3.0]
section's commitment update; the remaining harness adapters are still on the
roadmap.)
