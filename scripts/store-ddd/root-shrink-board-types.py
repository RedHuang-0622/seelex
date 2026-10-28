#!/usr/bin/env python3
"""Move Board consumers to contract/dto and store/board identities."""

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
QUALIFIED = {
    "store.ProjectBoardSchema": "dto.ProjectBoardSchema",
    "store.ProjectBoardMaxRows": "dto.ProjectBoardMaxRows",
    "store.ProjectBoardReadMaxSessions": "dto.ProjectBoardReadMaxSessions",
    "store.ProjectBoardWorkFile": "dto.ProjectBoardWorkFile",
    "store.ProjectBoardTeamworkFile": "dto.ProjectBoardTeamworkFile",
    "store.ProjectBoardMarkdownFile": "dto.ProjectBoardMarkdownFile",
    "store.ProjectBoardKindWork": "dto.ProjectBoardKindWork",
    "store.ProjectBoardKindMilestone": "dto.ProjectBoardKindMilestone",
    "store.ProjectBoardKindMilestoneItem": "dto.ProjectBoardKindMilestoneItem",
    "store.ProjectBoardScope": "dto.ProjectBoardScope",
    "store.ProjectBoardRow": "dto.ProjectBoardRow",
    "store.ProjectBoardReadOptions": "dto.ProjectBoardReadOptions",
    "store.ProjectBoardDir": "storeboard.Dir",
    "store.ProjectBoardSessionDir": "storeboard.SessionDir",
    "store.ProjectBoardMarkdown": "storeboard.Markdown",
    "store.ProjectBoardKinds": "storeboard.Kinds",
    "store.FoldProjectBoardRows": "storeboard.Fold",
    "store.BoardKindGoal": "dto.BoardKindGoal",
    "store.BoardKindTeam": "dto.BoardKindTeam",
    "store.BoardStateActive": "dto.BoardStateActive",
    "store.BoardStateClosed": "dto.BoardStateClosed",
    "store.BoardCloseGoalFinish": "dto.BoardCloseGoalFinish",
    "store.BoardCloseGoalAbort": "dto.BoardCloseGoalAbort",
    "store.BoardCloseTeamReplan": "dto.BoardCloseTeamReplan",
    "store.BoardCloseTeamClose": "dto.BoardCloseTeamClose",
    "store.BoardLifecycle": "dto.BoardLifecycle",
    "store.GoalBoardActive": "dto.GoalBoardActive",
    "store.GoalBoardHistory": "dto.GoalBoardHistory",
    "store.GoalBoardMeta": "dto.GoalBoardMeta",
    "store.TeamBoardMeta": "dto.TeamBoardMeta",
    "store.SessionBoards": "storeboard.SessionBoardStore",
    "store.ValidateGoalBoardMeta": "storeboard.ValidateGoalBoardMeta",
    "store.ValidateTeamBoardMeta": "storeboard.ValidateTeamBoardMeta",
}

ROOT_FILES = (
    "store/board_test.go",
    "store/m10_test_compat_test.go",
)


def add_import(text: str, imported: str) -> str:
    if imported in text:
        return text
    match = re.search(r"import \((.*?)\)", text, flags=re.DOTALL)
    if match:
        return text[: match.end() - 1] + f"\t{imported}\n" + text[match.end() - 1 :]
    package_end = text.index("\n", text.index("package ")) + 1
    return text[:package_end] + f'\nimport {imported}\n' + text[package_end:]


def qualify_unqualified(text: str) -> str:
    names = {
        "GoalBoardMeta": "dto.GoalBoardMeta",
        "TeamBoardMeta": "dto.TeamBoardMeta",
        "GoalBoardActive": "dto.GoalBoardActive",
        "GoalBoardHistory": "dto.GoalBoardHistory",
        "BoardLifecycle": "dto.BoardLifecycle",
        "BoardKindGoal": "dto.BoardKindGoal",
        "BoardKindTeam": "dto.BoardKindTeam",
        "BoardStateActive": "dto.BoardStateActive",
        "BoardStateClosed": "dto.BoardStateClosed",
        "ValidateGoalBoardMeta": "storeboard.ValidateGoalBoardMeta",
        "ValidateTeamBoardMeta": "storeboard.ValidateTeamBoardMeta",
    }
    for old, new in names.items():
        text = re.sub(rf"(?<![A-Za-z0-9_]){old}(?![A-Za-z0-9_])", new, text)
    return text


paths = set(ROOT.glob("*/*.go")) | set(ROOT.glob("*/*/*.go")) | set(ROOT.glob("*/*/*/*.go"))
paths |= {ROOT / name for name in ROOT_FILES}
for path in sorted(paths):
    relative = path.relative_to(ROOT).as_posix()
    if relative.startswith("store/board/") or relative.startswith("store/boardrouter/") or relative == "store/board.go":
        continue
    text = path.read_text(encoding="utf-8")
    before = text
    for old, new in QUALIFIED.items():
        text = text.replace(old, new)
    if relative in ROOT_FILES:
        text = qualify_unqualified(text)
    if text == before:
        continue
    if "dto." in text:
        text = add_import(text, '"github.com/RedHuang-0622/seelex/contract/dto"')
    if "storeboard." in text:
        text = add_import(text, 'storeboard "github.com/RedHuang-0622/seelex/store/board"')
    path.write_text(text, encoding="utf-8", newline="\n")
