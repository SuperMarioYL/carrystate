# CarryState — what using it looks like

This is the 3-line happy path: pack Claude Code state into a `.csb`, verify
it, and unpack it into Codex's native layout — entirely inside an intranet.

```bash
# 1. pack: ~/.claude → project.csb (sessions + memories + MCP, merkle-rooted)
carrystate pack --harness claudecode --state-dir ~/.claude --out project.csb

# 2. verify: re-derive every sha256 + the merkle root (green check = ship it)
carrystate verify --bundle project.csb

# 3. unpack: project.csb → ~/.codex (Codex-native layout, lossless)
carrystate unpack --harness codex --in project.csb --state-dir ~/.codex
```

Sample output from `pack`:

```
packed project.csb → claudecode
  sessions:    1
  memories:   1
  mcp servers: 1
  merkle root: 9542d49c31b322ed5eaa54e713f61a6dab6e8bc50270e96720baac75b612a95d
  fingerprint: 9542d49c31b3
```

No network, no cloud, no daemons — the binary reads one state dir and writes
another. `数据不出境` by construction.
