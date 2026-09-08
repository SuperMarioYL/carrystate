[简体中文](./README.md) · [Website](https://carrystate.lei6393.com) · [GitHub](https://github.com/SuperMarioYL/carrystate)

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg">
  <img src="./assets/presentation/hero-light.svg" width="960" alt="Hero diagram">
</picture>

# CarryState

**Carry local agent state between workspaces**

CarryState packs Claude Code session files, Markdown memories and MCP configuration into a .csb bundle, verifies its checksums, and writes a Codex-oriented directory layout.

## Why use it

Moving a local agent setup involves several kinds of files with different locations and formats. A bundle keeps the transcript, instructions and tool configuration together, with an inspectable manifest and a check before unpacking.

- **One transferable bundle** — Move .csb files through an internal file share or removable media.
- **Check before writing** — Unpack recomputes checksums and refuses a mismatching bundle.
- **Keep transcript bytes** — The exporter stores raw JSONL and the importer writes that content back.

## Architecture

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg">
  <img src="./assets/presentation/architecture-light.svg" width="960" alt="Architecture diagram">
</picture>

The Claude Code adapter reads projects/<project>/*.jsonl, global and discoverable project CLAUDE.md files, and MCP server entries. The bundle module addresses entries by SHA-256 and computes a Merkle root. The Codex adapter writes sessions, merges Markdown with provenance headers and serializes mcp.json.

| Component | Responsibility |
| --- | --- |
| `Claude Code files` | sessions / CLAUDE.md / MCP |
| `Bundle` | manifest and entry digests |
| `Verification` | recomputed Merkle root |
| `Codex adapter` | sessions / AGENTS.md / mcp.json |

## Install and quickstart

Go 1.24+; Python 3 runs the self-contained example. No agent CLI is required.

```bash
git clone https://github.com/SuperMarioYL/carrystate.git
cd carrystate
go build -o bin/carrystate ./cmd/carrystate
```

The example creates synthetic source files in a temporary directory and runs the actual pack, verify and unpack commands. It never reads your ~/.claude or writes your ~/.codex.

```bash
python3 examples/presentation_demo.py
```

## Recorded demo

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg">
  <img src="./assets/presentation/process-light.svg" width="960" alt="Process diagram">
</picture>

One synthetic session survives pack → verify → unpack byte-for-byte.

```text
packed example.csb → claudecode
  sessions:    1
  memories:   1
  mcp servers: 1
  merkle root: 65c311e365e4501004838cfe803ab49c5184881fef25648e85386d41efa77f5a
  fingerprint: 65c311e365e4
bundle:   example.csb
source:   claudecode
spec:     0.1
packed:   2026-09-08 02:10:03 UTC
entries:  1 sessions, 1 memories, 1 mcp servers
merkle:   65c311e365e4501004838cfe803ab49c5184881fef25648e85386d41efa77f5a
result:   4 matched, 0 mismatched
  ✓ all entries match — bundle is intact, round-trip is lossless
unpacked example.csb → codex
  state dir:      target
  sessions written:  1
  memories written: 1
  mcp servers:       1
Session bytes preserved: True
Written files: AGENTS.md, mcp.json, sessions/demo/session.jsonl
Scope: file migration only; no Codex process or MCP server started.
```

The complete command and output are recorded in [docs/demo-results.json](./docs/demo-results.json). Inputs and reproduction code are included in the repository.

![Existing terminal recording](./assets/demo.gif)

The existing recording is retained for context; the text example above documents the reproducible scenario.

## Usage

Replace ./source-state with a source directory you intend to export. Use --project to restrict one encoded project subdirectory, or --mcp-config to select an explicit MCP JSON file. Import into a new directory first: the importer writes target files and is not a conflict-aware merge tool. See SPEC.md and examples/happy-path.md for the file contract.

```bash
./bin/carrystate pack --harness claudecode --state-dir ./source-state --out project.csb
./bin/carrystate verify --bundle project.csb
./bin/carrystate unpack --harness codex --in project.csb --state-dir ./imported-state --dry-run
./bin/carrystate unpack --harness codex --in project.csb --state-dir ./imported-state
```

## Configuration

All paths are explicit CLI arguments. The Claude Code project-directory decoding is best effort because hyphens can be ambiguous. Global Markdown is concatenated with source comments; MCP configuration is serialized as mcp.json. Bundles may contain sensitive session content and MCP environment values, so choose their destination as carefully as the original state files.

## Integrations and responsibilities

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg">
  <img src="./assets/presentation/integrations-light.svg" width="960" alt="Integrations diagram">
</picture>

CarryState handles file packaging and deterministic translation. The destination runtime owns whether it recognizes and resumes those files. The supported direction is Claude Code export → Codex-oriented import; the reverse methods return unsupported-direction errors.

| Route | Implemented role |
| --- | --- |
| Claude Code | export local file state |
| CSB archive | portable checksummed bundle |
| Codex layout | write adapter-defined files |
| YAML sidecar | human-readable manifest |

## Limits and next steps

- Checksum agreement establishes bundle consistency, not signer identity, runtime compatibility or behavior equivalence.
- The Codex layout is an adapter convention. This demo does not launch Codex or verify that a current Codex release resumes the imported session.
- Reverse migration and semantic memory reconciliation are not implemented.

Implemented: Claude Code export, CSB checksums, verification, dry-run and target-file import. Next directions are current-runtime compatibility checks, reverse migration and additional harness adapters. No hosted service or enterprise license system is implemented here.

## License and contributions

See [LICENSE](./LICENSE). When reporting an issue, include a minimal input, the command, and the observed output.
