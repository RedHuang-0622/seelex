#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Mermaid 代码块结构校验。

只做**结构冒烟**，不替代真正的渲染器：它检查 fence 是否闭合、图类型是否
可识别、每行的括号与引号是否配平、flowchart 的 subgraph/end 是否成对。
这些正是手写图最容易出的错，而真渲染问题（例如 Mermaid 版本差异）仍需
在 GitHub 或本地渲染器上确认。

用法：
  python scripts/check_mermaid.py            # 报告全部问题
  python scripts/check_mermaid.py --strict    # 有问题时退出码 1（可用于 CI）
  python scripts/check_mermaid.py README.md application/   # 只查指定路径
"""

import pathlib
import re
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent

SKIP_DIRS = {".git", "dist", "_tmp", "tmp", "node_modules", ".seelex", "local", ".gocache", ".venv"}

DIAGRAM_TYPES = (
    "flowchart",
    "graph",
    "sequenceDiagram",
    "stateDiagram-v2",
    "stateDiagram",
    "classDiagram",
    "erDiagram",
    "journey",
    "gantt",
    "pie",
    "mindmap",
    "timeline",
    "quadrantChart",
)

FENCE = "```mermaid"


def iter_markdown(targets):
    for target in targets:
        path = (REPO / target) if not pathlib.Path(target).is_absolute() else pathlib.Path(target)
        if path.is_file():
            if path.suffix == ".md":
                yield path
            continue
        for candidate in sorted(path.rglob("*.md")):
            if any(part in SKIP_DIRS for part in candidate.parts):
                continue
            yield candidate


def check_block(path, start_line, body):
    """返回结构问题列表（不含行号，调用方补）。"""
    problems = []
    lines = [line for line in body.splitlines() if line.strip()]
    if not lines:
        return ["代码块为空"]

    header = lines[0].strip()
    if not header.startswith(DIAGRAM_TYPES):
        problems.append("首行不是已知图类型：%r" % header)

    is_flowchart = header.startswith("flowchart") or header.startswith("graph")
    # erDiagram / classDiagram 用 { } 表示实体与成员体，||--o{ 表示基数，
    # 括号天然不成对；它们只做引号与标签检查。
    check_brackets = is_flowchart or header.startswith(("stateDiagram", "sequenceDiagram"))
    subgraphs = 0

    for line in lines:
        stripped = line.strip()
        if stripped.startswith("%%"):
            continue
        if is_flowchart:
            if re.match(r"^subgraph\b", stripped):
                subgraphs += 1
            elif stripped == "end":
                subgraphs -= 1
                if subgraphs < 0:
                    problems.append("多余的 end：%r" % stripped)
                    subgraphs = 0
        if stripped.count('"') % 2 != 0:
            problems.append("引号未配平：%r" % stripped)
        if check_brackets:
            for opener, closer in (("[", "]"), ("(", ")"), ("{", "}")):
                if stripped.count(opener) != stripped.count(closer):
                    problems.append("括号未配平（%s%s）：%r" % (opener, closer, stripped))
        if "<code>" in stripped or "</code>" in stripped:
            problems.append("图内出现 HTML 标签，渲染器会当字面量：%r" % stripped)

    if subgraphs != 0:
        problems.append("subgraph 与 end 不成对（差 %d）" % subgraphs)
    return problems


def scan(path):
    text = path.read_text(encoding="utf-8", errors="replace")
    lines = text.splitlines()
    findings = []
    index = 0
    while index < len(lines):
        if lines[index].strip() != FENCE:
            index += 1
            continue
        start = index + 1
        cursor = start
        while cursor < len(lines) and lines[cursor].strip() != "```":
            cursor += 1
        if cursor >= len(lines):
            findings.append((start, "mermaid fence 未闭合"))
            break
        body = "\n".join(lines[start:cursor])
        for problem in check_block(path, start, body):
            findings.append((start, problem))
        index = cursor + 1
    return findings


def main(argv):
    strict = "--strict" in argv
    targets = [arg for arg in argv if not arg.startswith("--")] or ["."]
    total_blocks = 0
    bad_files = 0
    for path in iter_markdown(targets):
        text = path.read_text(encoding="utf-8", errors="replace")
        count = text.count(FENCE)
        if count == 0:
            continue
        total_blocks += count
        findings = scan(path)
        if findings:
            bad_files += 1
            rel = path.relative_to(REPO) if path.is_relative_to(REPO) else path
            for line, problem in findings:
                print("%s:%d: %s" % (rel, line, problem))
    print("mermaid 代码块 %d 个；问题文件 %d 个" % (total_blocks, bad_files))
    if strict and bad_files:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
