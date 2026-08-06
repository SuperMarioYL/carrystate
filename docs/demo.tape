Output assets/demo.gif
Set FontSize 16
Set Width 1100
Set Height 560
Set Padding 20
Set Theme Dracula
Set TypingSpeed 12ms

# CarryState — 90-second round-trip demo (信创内网，全程不出内网)
# Renders a real pack → verify → unpack of a Claude Code state dir into Codex's
# native layout. The binary is built fresh from source; the fixture is a tiny
# ~/.claude tree so the demo is reproducible on any machine.

Type "# build the binary + set up a tiny fake ~/.claude"
Enter
Sleep 500ms

Type "go build -o /tmp/carrystate ./cmd/carrystate"
Enter
Sleep 1200ms

Type "rm -rf /tmp/csdemo && mkdir -p /tmp/csdemo/claude/.claude/projects/-tmp-csdemo-app && mkdir -p /tmp/csdemo/codex"
Enter
Type 'printf "%s\n" "{\"type\":\"user\",\"message\":\"hello\"}" "{\"type\":\"assistant\",\"message\":\"hi back\"}" > /tmp/csdemo/claude/.claude/projects/-tmp-csdemo-app/sess-001.jsonl'
Enter
Type 'printf "# Global memory\nAlways use Go.\n" > /tmp/csdemo/claude/.claude/CLAUDE.md'
Enter
Type 'printf "{\"mcpServers\":{\"filesystem\":{\"command\":\"npx\",\"args\":[\"-y\",\"@modelcontextprotocol/server-filesystem\"]}}}" > /tmp/csdemo/claude/.claude.json'
Enter
Sleep 500ms

Type "# 1. pack: Claude Code state → project.csb (merkle-rooted)"
Enter
Sleep 500ms
Type "/tmp/carrystate pack --harness claudecode --state-dir /tmp/csdemo/claude/.claude --out /tmp/csdemo/project.csb"
Enter
Sleep 1500ms

Type "# 2. verify: re-derive every sha256 + the merkle root (green = ship)"
Enter
Sleep 500ms
Type "/tmp/carrystate verify --bundle /tmp/csdemo/project.csb"
Enter
Sleep 1500ms

Type "# 3. unpack: project.csb → Codex native layout (~/.codex)"
Enter
Sleep 500ms
Type "/tmp/carrystate unpack --harness codex --in /tmp/csdemo/project.csb --state-dir /tmp/csdemo/codex"
Enter
Sleep 1500ms

Type "# the round-trip landed — session is byte-identical to the source"
Enter
Type "diff /tmp/csdemo/claude/.claude/projects/-tmp-csdemo-app/sess-001.jsonl /tmp/csdemo/codex/sessions/-tmp-csdemo-app/sess-001.jsonl && echo '✓ byte-identical round-trip'"
Enter
Sleep 1500ms

Type "# 数据不出境 — no network, no cloud, one binary"
Enter
Sleep 1000ms
