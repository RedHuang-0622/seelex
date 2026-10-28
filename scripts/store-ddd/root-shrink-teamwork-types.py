#!/usr/bin/env python3
"""Move backend/teamwork production code to domain and DTO identities."""

from __future__ import annotations

import argparse
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE = ROOT / "backend/multiagent/teamwork"

TEAMWORK_TYPES = {
    "Binding": "Binding",
    "Event": "Event",
    "Member": "Member",
    "Milestone": "Milestone",
    "Plan": "Plan",
    "State": "State",
    "WorkItem": "WorkItem",
}

TEAMWORK_CONSTS = {
    "EventPlan": "EventPlan",
    "EventDispatch": "EventDispatch",
    "EventJoin": "EventJoin",
    "EventMilestone": "EventMilestone",
    "EventClose": "EventClose",
    "EventItem": "EventItem",
    "EventAccept": "EventAccept",
    "EventFail": "EventFail",
    "EventSettle": "EventSettle",
    "EventRecover": "EventRecover",
    "EventMessage": "EventMessage",
    "MilestonePending": "MilestonePending",
    "MilestoneActive": "MilestoneActive",
    "MilestoneDone": "MilestoneDone",
    "ItemPending": "ItemPending",
    "ItemRunning": "ItemRunning",
    "ItemReview": "ItemReview",
    "ItemDone": "ItemDone",
    "ItemFailed": "ItemFailed",
    "StateClosed": "StateClosed",
}


def transform(text: str) -> str:
    for old, new in TEAMWORK_TYPES.items():
        text = text.replace(f"store.Teamwork{old}", f"storeteamwork.{new}")
    for old, new in TEAMWORK_CONSTS.items():
        text = text.replace(f"store.Teamwork{old}", f"storeteamwork.{new}")
    text = text.replace("store.TeamworkPlanIsEmpty", "storeteamwork.PlanIsEmpty")
    text = text.replace("store.TeamworkWorkItemStatusOrPending", "storeteamwork.WorkItemStatusOrPending")
    text = text.replace("store.TeamworkWorkItemTerminal", "storeteamwork.WorkItemTerminal")
    text = text.replace("store.TeamworkBindings", "storeteamwork.Bindings")

    text = text.replace("store.NodeSessionRecordWriter", "storeworkunit.Writer")
    text = text.replace("store.NodeSessionRecordLink", "storeworkunit.Link")
    text = text.replace("store.NodeSessionRecord", "storeworkunit.Record")
    text = text.replace("store.NodeWorktreeRecord", "storeworkunit.WorktreeRecord")
    text = text.replace("store.NodeUnitRecord", "storeworkunit.UnitRecord")
    text = text.replace("store.NodeSessionSchemaVersion", "storeworkunit.SchemaVersion")
    text = text.replace("store.Key", "dto.SessionKey")

    if re.search(r"\bstore\.", text):
        return text
    text = re.sub(r'^\s*"github\.com/RedHuang-0622/seelex/store"\s*\n', "", text, flags=re.MULTILINE)
    return text


def ensure_imports(text: str) -> str:
    imports = []
    if "dto.SessionKey" in text:
        imports.append('"github.com/RedHuang-0622/seelex/contract/dto"')
    if "storeteamwork." in text:
        imports.append('storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"')
    if "storeworkunit." in text:
        imports.append('storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"')
    for imported in imports:
        if imported in text:
            continue
        text = text.replace(")\n", f"\t{imported}\n)\n", 1)
    return text


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--path",
        action="append",
        default=[],
        help="repo-relative package directory (default: backend/multiagent/teamwork)",
    )
    parser.add_argument("--include-tests", action="store_true")
    args = parser.parse_args()
    changed = []
    bases = [ROOT / path for path in args.path] if args.path else [BASE]
    for base in bases:
        for path in sorted(base.glob("*.go")):
            if path.name.endswith("_test.go") and not args.include_tests:
                continue
            before = path.read_text(encoding="utf-8")
            after = ensure_imports(transform(before))
            if after == before:
                continue
            changed.append(path.relative_to(ROOT).as_posix())
            if not args.dry_run:
                path.write_text(after, encoding="utf-8", newline="\n")
    print("\n".join(changed))


if __name__ == "__main__":
    main()
