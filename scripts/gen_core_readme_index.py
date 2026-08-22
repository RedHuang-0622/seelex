#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 application/core 各包 README 的「文件与函数索引」段。

根包按文件前缀分卷：每个前缀组生成一个 README-<组>.md（组概述 + 文件/函数
索引）；子包保持单 README。主 README 的索引段会被移除（改由分卷 README 承载，
导航链接见主 README「分卷 README」）。

索引从源码 doc 注释提取每个函数的首行摘要（中文优先，代码标识符保留原文），
与实现保持同步；改动源码注释后重跑本脚本即可刷新。

用法：python scripts/gen_core_readme_index.py
"""

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parent.parent / "application" / "core"
SECTION_HEADER = "## 文件与函数索引"
CORRUPTED_HEADER = re.compile(r"^## \?{7,}$")

# 根包分卷：(前缀, 卷名, 组概述)；卷名用于 README-<卷名>.md 与标题
ROOT_GROUPS = [
    ("service", "service", "Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）"),
    ("session", "session", "会话草稿/恢复/存储用例与集成测试"),
    ("chat", "chat", "聊天主循环与可见输出集成"),
    ("command", "command", "内置命令注册与执行"),
    ("error", "error", "错误码与面向用户的错误呈现"),
    ("history", "history", "历史检索与 provider 失败恢复"),
    ("input", "input", "输入分派与路由兼容测试"),
    ("plan", "plan", "Plan 打点/分支事件/重规划"),
    ("reference", "reference", "read_tool_result / read_plan 引用工具"),
    ("skill", "skill", "Skill 指令信封编解码"),
    ("tool", "tool", "工具事件钩子与诊断"),
    ("work_table", "work-table", "工作表格投影与测试"),
    ("context", "context", "上下文控制相关集成测试"),
    ("task", "task", "任务执行集成测试"),
    ("subagent", "subagent", "子代理投影集成测试"),
    ("misc", "misc", "基础与杂项（aliases/limits/completion/compressed/diagnostics/runtime/workspace/race）"),
]


def clean_sig(sig: str) -> str:
    """截取签名（去掉函数体与尾部 '{'）。"""
    depth = 0
    for idx, ch in enumerate(sig):
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        elif ch == "{" and depth == 0:
            sig = sig[:idx]
            break
    return " ".join(sig.split()).rstrip()


def extract_functions(text: str):
    """返回 [(name, sig, doc_first_line)]。"""
    lines = text.splitlines()
    items = []
    i, n = 0, len(lines)
    while i < n:
        line = lines[i]
        if line.startswith("func "):
            doc = []
            j = i - 1
            while j >= 0 and lines[j].lstrip().startswith("//"):
                doc.insert(0, lines[j].lstrip()[2:].strip())
                j -= 1
            sig = line
            depth = line.count("(") - line.count(")")
            k = i + 1
            while depth > 0 and k < n:
                sig += " " + lines[k].strip()
                depth += lines[k].count("(") - lines[k].count(")")
                k += 1
            match = re.search(r"func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(", sig)
            name = match.group(1) if match else sig
            docline = next((d for d in doc if d), "")
            items.append((name, clean_sig(sig), docline))
            i = k
        else:
            i += 1
    return items


def build_index(go_files, base: pathlib.Path) -> str:
    out = []
    for go_file in sorted(go_files):
        rel = go_file.relative_to(base).as_posix()
        items = extract_functions(go_file.read_text(encoding="utf-8"))
        if not items:
            continue
        out.append(f"### {rel}")
        out.append("")
        for _, sig, doc in items:
            doc = doc.replace("|", "\\|")
            if len(doc) > 140:
                doc = doc[:139] + "…"
            out.append(f"- `{sig}` — {doc}" if doc else f"- `{sig}`")
        out.append("")
    return "\n".join(out)


def section(go_files, base: pathlib.Path) -> str:
    return (
        SECTION_HEADER
        + "\n\n"
        + "> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。\n"
        + "> 刷新方式：`python scripts/gen_core_readme_index.py`。\n\n"
        + build_index(go_files, base)
    )


def group_section(group_name: str, overview: str, go_files, base: pathlib.Path) -> str:
    out = [
        f"# core/{group_name}",
        "",
        "## 生态位",
        "",
        overview,
        "",
        "## 文件与函数索引",
        "",
        "> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。",
        "> 刷新方式：`python scripts/gen_core_readme_index.py`。",
        "",
    ]
    out.append(build_index(go_files, base))
    return "\n".join(out).rstrip() + "\n"


def strip_old_section(text: str) -> str:
    lines = text.splitlines()
    keep = []
    for line in lines:
        if line == SECTION_HEADER or CORRUPTED_HEADER.match(line):
            break
        keep.append(line)
    return "\n".join(keep).rstrip() + "\n"


def write_if_changed(path: pathlib.Path, content: str) -> None:
    if path.exists() and path.read_text(encoding="utf-8") == content:
        return
    path.write_text(content, encoding="utf-8")
    print("wrote", path.relative_to(ROOT))


def main() -> None:
    # 子包：单 README，含生态位概述 + 索引
    for pkg in sorted(p for p in ROOT.iterdir() if p.is_dir() and p.name != "internal"):
        readme = pkg / "README.md"
        text = strip_old_section(readme.read_text(encoding="utf-8")) if readme.exists() else ""
        text = text.rstrip() + "\n\n" + section(sorted(pkg.rglob("*.go")), pkg) + "\n"
        write_if_changed(readme, text)
    for leaf in ("internal/limits", "internal/state"):
        directory = ROOT / leaf
        readme = directory / "README.md"
        text = strip_old_section(readme.read_text(encoding="utf-8")) if readme.exists() else ""
        text = text.rstrip() + "\n\n" + section(sorted(directory.glob("*.go")), directory) + "\n"
        write_if_changed(readme, text)

    # 根包：移除主 README 的索引段（由分卷 README 承载）
    main_readme = ROOT / "README.md"
    text = strip_old_section(main_readme.read_text(encoding="utf-8"))
    write_if_changed(main_readme, text)

    # 根包分卷 README
    all_files = sorted(ROOT.glob("*.go"))
    for prefix, group_name, overview in ROOT_GROUPS:
        if prefix == "misc":
            names = {
                "aliases.go", "completion.go", "compressed_turn.go", "compressed_turn_test.go",
                "diagnostics.go", "limits.go", "runtime_projection.go", "workspace_usecase.go",
                "race_test.go",
            }
            group_files = [f for f in all_files if f.name in names]
        else:
            group_files = [f for f in all_files if f.name.startswith(prefix)]
        if not group_files:
            continue
        write_if_changed(
            ROOT / f"README-{group_name}.md",
            group_section(group_name, overview, group_files, ROOT),
        )
    print("README 分卷已刷新。")


if __name__ == "__main__":
    main()
