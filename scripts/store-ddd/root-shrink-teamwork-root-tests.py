#!/usr/bin/env python3
"""Qualify root-package Teamwork tests with the domain owner identities."""

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
FILES = (
    "store/m10_test_compat_test.go",
    "store/teamwork_accessor_test.go",
    "store/teamwork_items_test.go",
    "store/teamwork_plugins_test.go",
    "store/teamwork_test.go",
)

NAMES = {
    "TeamworkPlan": "storeteamwork.Plan",
    "TeamworkMember": "storeteamwork.Member",
    "TeamworkMilestone": "storeteamwork.Milestone",
    "TeamworkState": "storeteamwork.State",
    "TeamworkEvent": "storeteamwork.Event",
    "TeamworkWorkItem": "storeteamwork.WorkItem",
    "TeamworkBinding": "storeteamwork.Binding",
    "TeamworkPlanIsEmpty": "storeteamwork.PlanIsEmpty",
    "ValidateTeamworkPlan": "storeteamwork.ValidatePlan",
    "TeamworkBindings": "storeteamwork.Bindings",
    "TeamworkWorkItemStatusOrPending": "storeteamwork.WorkItemStatusOrPending",
    "TeamworkWorkItemTerminal": "storeteamwork.WorkItemTerminal",
}

CONSTS = {
    "TeamworkMilestonePending": "storeteamwork.MilestonePending",
    "TeamworkMilestoneActive": "storeteamwork.MilestoneActive",
    "TeamworkMilestoneDone": "storeteamwork.MilestoneDone",
    "TeamworkEventPlan": "storeteamwork.EventPlan",
    "TeamworkEventDispatch": "storeteamwork.EventDispatch",
    "TeamworkEventJoin": "storeteamwork.EventJoin",
    "TeamworkEventMilestone": "storeteamwork.EventMilestone",
    "TeamworkEventRetire": "storeteamwork.EventRetire",
    "TeamworkEventClose": "storeteamwork.EventClose",
    "TeamworkEventItem": "storeteamwork.EventItem",
    "TeamworkEventAccept": "storeteamwork.EventAccept",
    "TeamworkEventFail": "storeteamwork.EventFail",
    "TeamworkEventSettle": "storeteamwork.EventSettle",
    "TeamworkEventRecover": "storeteamwork.EventRecover",
    "TeamworkEventMessage": "storeteamwork.EventMessage",
    "TeamworkStateClosed": "storeteamwork.StateClosed",
    "TeamworkItemPending": "storeteamwork.ItemPending",
    "TeamworkItemRunning": "storeteamwork.ItemRunning",
    "TeamworkItemReview": "storeteamwork.ItemReview",
    "TeamworkItemDone": "storeteamwork.ItemDone",
    "TeamworkItemFailed": "storeteamwork.ItemFailed",
}


def replace_token(text: str, old: str, new: str) -> str:
    return re.sub(rf"(?<![A-Za-z0-9_]){old}(?![A-Za-z0-9_])", new, text)


for relative in FILES:
    path = ROOT / relative
    text = path.read_text(encoding="utf-8")
    for old, new in {**NAMES, **CONSTS}.items():
        text = replace_token(text, old, new)
    if "storeteamwork." in text and '"github.com/RedHuang-0622/seelex/store/teamwork"' not in text:
        text = text.replace(")\n", '\tstoreteamwork "github.com/RedHuang-0622/seelex/store/teamwork"\n)\n', 1)
    path.write_text(text, encoding="utf-8", newline="\n")
