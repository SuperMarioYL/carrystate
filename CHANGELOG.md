# Changelog

All notable changes to CarryState are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
