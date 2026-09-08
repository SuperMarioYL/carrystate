"""Pack, verify and unpack synthetic state into a temporary directory."""
import json
from pathlib import Path
import subprocess
import tempfile
binary = (Path(__file__).resolve().parents[1] / "bin/carrystate")
with tempfile.TemporaryDirectory(prefix="carrystate-demo-") as tmp:
    root = Path(tmp)
    session = '{"type":"user","message":{"role":"user","content":"Explain the cache"}}\n'
    source = root / "source/projects/demo"
    source.mkdir(parents=True)
    (source / "session.jsonl").write_text(session)
    (root / "source/CLAUDE.md").write_text("Use small reproducible examples.\n")
    (root / "source/.claude.json").write_text(json.dumps({"mcpServers":{"local-demo":{"command":"echo","args":["demo"]}}}))
    for args in [
        ["pack", "--harness", "claudecode", "--state-dir", "source", "--out", "example.csb"],
        ["verify", "--bundle", "example.csb"],
        ["unpack", "--harness", "codex", "--in", "example.csb", "--state-dir", "target"],
    ]:
        result = subprocess.run([str(binary), *args], cwd=root, text=True, capture_output=True, check=True)
        print(result.stdout, end="")
    restored = (root / "target/sessions/demo/session.jsonl").read_text()
    assert restored == session
    print("Session bytes preserved:", restored == session)
    print("Written files:", ", ".join(sorted(str(p.relative_to(root / "target")) for p in (root / "target").rglob("*") if p.is_file())))
    print("Scope: file migration only; no Codex process or MCP server started.")
