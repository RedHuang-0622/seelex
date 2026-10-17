#!/usr/bin/env python3
"""核对 GUI 前端调用的 RPC / 事件名与 Wails Bridge 的导出面对齐。

检查三件事：
1. 前端 `invoke("Literal")` 的方法名必须能在 `gui/*.go` 的 `*Bridge` 方法里找到；
2. 前端 `PROCESS_KINDS` 里的进程级事件名必须能在 `middle/protocol/event` 的
   EventKind 常量里找到；
3. 前端 `runtime.EventsOn("...")` 的通道名必须与 Bridge emit 的通道名一致。

动态方法名（例如 `invoke(plan.rpc, ...)`）无法静态解析，会单独计数并从
"缺失"里排除；它们由协议测试/运行时测试覆盖。

用法：
    python scripts/check_gui_ports.py
    python scripts/check_gui_ports.py --verbose

退出码：0 = 对齐；1 = 有缺失或未知端口。
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GUI_DIR = ROOT / "gui"
FRONTEND_DIR = GUI_DIR / "frontend" / "dist"
EVENT_HUB = ROOT / "middle" / "protocol" / "event" / "hub.go"

INVOKE_LITERAL = re.compile(r'\binvoke\(\s*["\']([A-Za-z_][A-Za-z0-9_]*)["\']')
INVOKE_ANY = re.compile(r"(?<!function )\binvoke\(")
INVOKE_PLAN_RPC = re.compile(r"\binvoke\(\s*plan\.rpc\b")
PLAN_RPC_LITERAL = re.compile(r'return\s*\{\s*rpc:\s*"([^"]+)"')
BRIDGE_METHOD = re.compile(
    r"func\s*\(\s*[A-Za-z_][A-Za-z0-9_]*\s+\*Bridge\s*\)\s*"
    r"([A-Z][A-Za-z0-9_]*)\s*\("
)
EVENT_KIND = re.compile(r'Event[A-Za-z0-9_]*\s+EventKind\s*=\s*"([^"]+)"')
PROCESS_KINDS = re.compile(
    r"PROCESS_KINDS\s*=\s*new\s+Set\(\s*\[(.*?)\]\s*\)", re.DOTALL
)
EVENTS_ON = re.compile(r'EventsOn\(\s*["\']([^"\']+)["\']')
STRING_CONST = re.compile(r'const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]+)"')


def frontend_files() -> list[Path]:
    files: list[Path] = []
    for path in sorted(FRONTEND_DIR.rglob("*.js")):
        if ".test." in path.name:
            continue
        files.append(path)
    return files


def collect_frontend() -> tuple[set[str], int, int, int, int, set[str], set[str]]:
    methods: set[str] = set()
    invoke_total = 0
    literal_calls = 0
    plan_rpc_calls = 0
    plan_rpc_methods: set[str] = set()
    events_on: set[str] = set()
    process_kinds: set[str] = set()

    for path in frontend_files():
        # 历史前端资产里混有非 UTF-8 字节；正则只认 ASCII 端口名，替换坏字节即可。
        text = path.read_text(encoding="utf-8", errors="replace")
        invoke_total += len(INVOKE_ANY.findall(text))
        literal = INVOKE_LITERAL.findall(text)
        literal_calls += len(literal)
        methods.update(literal)
        plan_rpc_calls += len(INVOKE_PLAN_RPC.findall(text))
        plan_rpc_methods.update(PLAN_RPC_LITERAL.findall(text))
        events_on.update(EVENTS_ON.findall(text))
        match = PROCESS_KINDS.search(text)
        if match:
            process_kinds.update(re.findall(r'["\']([^"\']+)["\']', match.group(1)))

    # composerSubmitPlan 的两个动态 rpc 是字面量，静态补齐后仍算已解析。
    methods.update(plan_rpc_methods)
    unresolved_dynamic = max(0, invoke_total - literal_calls - plan_rpc_calls)
    return (
        methods,
        invoke_total,
        literal_calls,
        plan_rpc_calls,
        unresolved_dynamic,
        events_on,
        process_kinds,
    )


def collect_bridge() -> tuple[set[str], set[str]]:
    methods: set[str] = set()
    emitted: set[str] = set()
    for path in sorted(GUI_DIR.glob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        methods.update(BRIDGE_METHOD.findall(text))
        # 事件通道名：常量 + 字面量 emit。
        for name, value in STRING_CONST.findall(text):
            if name in {"eventName", "terminalEventName"}:
                emitted.add(value)
        emitted.update(re.findall(r'emit\([^,]+,\s*"([^"]+)"', text))
    return methods, emitted


def collect_event_kinds() -> set[str]:
    text = EVENT_HUB.read_text(encoding="utf-8")
    return set(EVENT_KIND.findall(text))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args()

    (
        frontend_methods,
        invoke_total,
        literal_calls,
        plan_rpc_calls,
        unresolved_dynamic,
        events_on,
        process_kinds,
    ) = collect_frontend()
    bridge_methods, emitted = collect_bridge()
    event_kinds = collect_event_kinds()

    missing_methods = sorted(frontend_methods - bridge_methods)
    unused_methods = sorted(bridge_methods - frontend_methods)
    unknown_kinds = sorted(process_kinds - event_kinds)
    unknown_events = sorted(events_on - emitted)
    print("GUI 端口核对")
    print(
        f"  前端 invoke 调用点：{invoke_total}"
        f"（字面 {literal_calls} · composerSubmitPlan {plan_rpc_calls}"
        f" · 未解析变量 {unresolved_dynamic}）"
    )
    print(f"  前端字面 RPC：{len(frontend_methods)} · Bridge 导出方法：{len(bridge_methods)}")
    print(f"  前端进程事件：{len(process_kinds)} · 后端 EventKind：{len(event_kinds)}")
    print(f"  前端 EventsOn 通道：{sorted(events_on)}")
    print(f"  Bridge emit 通道：{sorted(emitted)}")

    if args.verbose:
        print("\nBridge 方法（未被前端字面调用，仅提示）：")
        for name in unused_methods:
            print(f"  - {name}")

    failed = False
    if missing_methods:
        failed = True
        print("\n缺失的 Bridge RPC：")
        for name in missing_methods:
            print(f"  - {name}")

    if unknown_kinds:
        failed = True
        print("\n前端 PROCESS_KINDS 未在后端 EventKind 中登记：")
        for name in unknown_kinds:
            print(f"  - {name}")

    if unknown_events:
        failed = True
        print("\n前端 EventsOn 通道未在 Bridge emit 中登记：")
        for name in unknown_events:
            print(f"  - {name}")

    if failed:
        print("\n结论：GUI 前后端端口未对齐。")
        return 1

    print("\n结论：GUI 前后端端口对齐。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
