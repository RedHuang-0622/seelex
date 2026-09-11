"""Dump full content of subagent.result events (ASCII output only)."""
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
data = json.loads(path.read_text(encoding="utf-8"))
for item in data:
    payload = item.get("payload", {})
    if payload.get("source") != "seelex.subagent.result":
        continue
    content = payload.get("content") or ""
    print("=" * 70)
    print("occurred_at:", payload.get("occurred_at"))
    print("scope:", json.dumps(payload.get("scope"), ensure_ascii=False))
    print("content:")
    try:
        parsed = json.loads(content)
        for key, value in parsed.items():
            if isinstance(value, str) and len(value) > 1200:
                value = value[:1200] + "...<truncated>"
            print(f"  {key} = {value}")
    except Exception:
        print(" ", str(content)[:2000])
