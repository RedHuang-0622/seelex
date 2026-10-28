#!/usr/bin/env python3
"""Generate and verify the Store root-shrink data-operation coverage report."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path


BASELINE = "66f20702"
TYPES = ("Router", "jsonRepository", "SessionGranularStore")
REPORT = Path("docs/2026-10-28-store-root-shrink/read-write-coverage.md")


def git_show(path: str) -> str:
    return subprocess.check_output(
        ["git", "show", f"{BASELINE}:{path}"], text=True, encoding="utf-8"
    )


def exported(methods: list[str]) -> list[str]:
    return sorted(name for name in methods if name[:1].isupper())


def destination(receiver: str, name: str) -> tuple[str, str]:
    if receiver == "Router":
        if name.startswith("Stack") or name == "ForkStacks":
            return "migrated", "store.StackStore + runtime_api.go"
        if name.startswith("AppendProjectBoard") or name.startswith("ReadProjectBoard") or name in {
            "ProjectBoardWatermark",
            "ReclaimProjectBoard",
            "SetProjectBoardRootFor",
        }:
            return "migrated", "internal/host/boardrouter + store/board"
        if name in {"ReadProjectKnown", "WriteProjectKnown"}:
            return "migrated", "store/project"
        if name in {"WriteMediaWorkspace", "ReadMediaWorkspace"}:
            return "migrated", "store/artifact.SessionStore"
        if name == "AssembleWireWorkspace":
            return "migrated", "store.WireStore"
        if name in {
            "DerivedRecordWorkspace",
            "DerivedResumeRecordWorkspace",
            "LoadConversationRangeWorkspace",
        }:
            return "migrated", "kernel/session projection + Host raw reads"
        if name in {
            "AppendRoleDraftWorkspace",
            "AppendRoleSessionRowsWorkspace",
            "AssembleRoleWireWorkspace",
            "CreateRoleSessionWorkspace",
            "EnsureRoleSessionWorkspace",
            "ListRoleSessions",
            "ListRoleSessionsWorkspace",
            "ReadLifecycleOrderWorkspace",
            "ReadMessageFloorWorkspace",
            "ReadRoleDraftWorkspace",
            "ReadRoleSessionRowsWorkspace",
            "RoleSnapshotWorkspace",
            "SetLifecycleOrderWorkspace",
            "SetRoleLifecycleWorkspace",
            "SyncRoleDraftWorkspace",
            "ReadTeamRegistryWorkspace",
            "WriteTeamRegistryWorkspace",
            "RemoveTeamRegistryWorkspace",
        }:
            return "migrated", "store/role + store.RoleSessionAdapter"
    if receiver == "jsonRepository" and name in {
        "CollectMedia",
        "ListMedia",
        "ReadMedia",
        "WriteMedia",
    }:
        return "migrated", "store/artifact"
    if receiver == "jsonRepository" and name == "ReadConversationRange":
        return "migrated", "kernel/session projection + Host raw reads"
    if receiver == "SessionGranularStore" and name in {"ConversationRange", "Transcript"}:
        return "migrated", "Host raw event/range reads"
    raise SystemExit(f"unclassified removed operation: {receiver}.{name}")


def package_destination(name: str) -> tuple[str, str]:
    mappings = {
        "ApplyLimits": "store/project.ApplyLimits",
        "FoldProjectBoardRows": "store/board.Fold",
        "MediaRefHash": "store/artifact.MediaRefHash",
        "NewNodeSessionRecordWriter": "store/workunit.NewWriter",
        "NewNodeSessionStore": "store/workunit.NewSessionStore",
        "NewProjectKnowledgeBuilder": "store/project.NewProjectKnowledgeBuilder",
        "NewRolePermissionStore": "store/role.NewPermissionStore",
        "ProjectBoardDir": "store/board.ProjectBoardDir",
        "ProjectBoardKinds": "contract/dto project-board kinds",
        "ProjectBoardMarkdown": "store/board.Markdown",
        "ProjectBoardSessionDir": "store/board.SessionDir",
        "ProjectKnownMarkdown": "store/project.ProjectKnownMarkdown",
        "ReferencedMediaHashes": "artifact GC caller-owned reference set",
        "RefreshProjectKnowledge": "store/project.RefreshProjectKnowledge",
        "TeamworkBindings": "store/teamwork.Bindings",
        "TeamworkPlanIsEmpty": "contract/dto.TeamworkPlanIsEmpty",
        "TeamworkWorkItemStatusOrPending": "contract/dto.TeamworkWorkItemStatusOrPending",
        "TeamworkWorkItemTerminal": "contract/dto.TeamworkWorkItemTerminal",
        "ValidateGoalBoardMeta": "store/board.ValidateGoalBoardMeta",
        "ValidateTeamBoardMeta": "store/board.ValidateTeamBoardMeta",
        "ValidateTeamworkPlan": "store/teamwork.ValidatePlan",
    }
    if name in mappings:
        return "migrated", mappings[name]
    raise SystemExit(f"unclassified removed package function: {name}")


def baseline_package_functions() -> list[str]:
    files = subprocess.check_output(
        ["git", "ls-tree", "-r", "--name-only", BASELINE, "--", "store"],
        text=True,
        encoding="utf-8",
    ).splitlines()
    names = set()
    pattern = re.compile(r"^func ([A-Z][A-Za-z0-9_]*)\(")
    for path in files:
        if not re.fullmatch(r"store/[^/]+\.go", path) or path.endswith("_test.go"):
            continue
        for line in git_show(path).splitlines():
            match = pattern.match(line)
            if match:
                names.add(match.group(1))
    return sorted(names)


def current_package_functions() -> list[str]:
    names = set()
    pattern = re.compile(r"^func ([A-Z][A-Za-z0-9_]*)\(")
    for path in Path("store").glob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        for line in path.read_text("utf-8").splitlines():
            match = pattern.match(line)
            if match:
                names.add(match.group(1))
    return sorted(names)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()

    baseline = json.loads(git_show("store/testdata/god_type_methods.json"))
    current = json.loads(Path("store/testdata/god_type_methods.json").read_text("utf-8"))
    lines = [
        "# Store 旧数据读写操作覆盖清单",
        "",
        f"基线：`{BASELINE}`。清单范围是 God 类型上的公开方法；内部锁内方法不在产品调用面内。",
        "",
        "## 汇总",
        "",
        "| Receiver | 基线公开操作 | 当前保留 | 已迁移/删除 |",
        "|---|---:|---:|---:|",
    ]
    detail: list[str] = []
    for receiver in TYPES:
        old = exported(baseline[receiver])
        now = set(exported(current[receiver]))
        removed = [name for name in old if name not in now]
        lines.append(f"| `{receiver}` | {len(old)} | {len(old) - len(removed)} | {len(removed)} |")
        detail.extend(["", f"## {receiver}", "", "| 旧操作 | 状态 | 新 owner / 唯一入口 |", "|---|---|---|"])
        for name in old:
            status, target = ("retained", "root Store persistence API") if name in now else destination(receiver, name)
            detail.append(f"| `{name}` | {status} | {target} |")

    lines.extend(
        [
            "",
            "## 判据",
            "",
            "- 旧公开操作仍在当前 receiver 上：保留为 Store 持久化机制。",
            "- 旧公开操作消失：必须在逐项表中映射到新 owner，否则本脚本失败。",
            "- 调用方迁移按逐项表的目标 owner 对照，不以测试变绿替代结构覆盖证明。",
            "",
        ]
    )
    rendered = "\n".join(lines + detail) + "\n"
    old_functions = baseline_package_functions()
    current_functions = set(current_package_functions())
    missing_functions = [name for name in old_functions if name not in current_functions]
    package_rows = [
        "",
        "## 根包公开函数",
        "",
        f"基线 {len(old_functions)} 个，当前保留 {len(old_functions) - len(missing_functions)} 个。",
        "",
        "| 旧函数 | 状态 | 新 owner / 唯一入口 |",
        "|---|---|---|",
    ]
    for name in old_functions:
        status, target = (
            ("retained", "root Store persistence API")
            if name in current_functions
            else package_destination(name)
        )
        package_rows.append(f"| `{name}` | {status} | {target} |")
    rendered = "\n".join(lines + detail + package_rows) + "\n"
    if args.check:
        if not REPORT.exists() or REPORT.read_text("utf-8") != rendered:
            raise SystemExit(f"coverage report is stale: {REPORT}")
        print(f"coverage current: {REPORT}")
        return 0
    REPORT.write_text(rendered, encoding="utf-8")
    print(f"wrote {REPORT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
