#!/usr/bin/env python3
"""Move root Project tests directly onto store/project."""

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
FILES = (
    "store/concurrency_race_test.go",
    "store/project_known_test.go",
    "store/project_record_test.go",
    "store/project_refresh_key_test.go",
)
NAMES = {
    "ProjectRecord": "storeproject.ProjectRecord",
    "ModuleSemantics": "storeproject.ModuleSemantics",
    "ProjectKnowledgeSources": "storeproject.ProjectKnowledgeSources",
    "ProjectKnowledgeBuilder": "storeproject.ProjectKnowledgeBuilder",
    "ProjectRefreshResult": "storeproject.ProjectRefreshResult",
    "NewProjectKnowledgeBuilder": "storeproject.NewProjectKnowledgeBuilder",
    "ApplyLimits": "storeproject.ApplyLimits",
    "ProjectKnownMarkdown": "storeproject.ProjectKnownMarkdown",
    "RefreshProjectKnowledge": "storeproject.RefreshProjectKnowledge",
    "projectKnownPath": "storeproject.KnownPath",
    "isProjectRecordNotFound": "storeproject.IsRecordNotFound",
    "ProjectBoardKnownFile": "storeproject.KnownFile",
}

for relative in FILES:
    path = ROOT / relative
    text = path.read_text(encoding="utf-8")
    for old, new in NAMES.items():
        text = re.sub(rf"(?<![A-Za-z0-9_]){old}(?![A-Za-z0-9_])", new, text)
    text = re.sub(r"(\w+)\.WriteProjectKnown\(", r"storeproject.WriteKnown(\1, ", text)
    text = re.sub(r"(\w+)\.ReadProjectKnown\(", r"storeproject.ReadKnown(\1, ", text)
    if "storeproject." in text and '"github.com/RedHuang-0622/seelex/store/project"' not in text:
        match = re.search(r"import \((.*?)\)", text, flags=re.DOTALL)
        imported = 'storeproject "github.com/RedHuang-0622/seelex/store/project"'
        if match:
            text = text[: match.end() - 1] + f"\t{imported}\n" + text[match.end() - 1 :]
        else:
            package_end = text.index("\n", text.index("package ")) + 1
            text = text[:package_end] + f"\nimport {imported}\n" + text[package_end:]
    path.write_text(text, encoding="utf-8", newline="\n")
