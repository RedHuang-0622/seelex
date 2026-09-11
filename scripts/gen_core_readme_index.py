#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 application/core 各包 README 的「文件与函数索引」段。

根包按文件前缀分卷：每个前缀组生成一个 README-<组>.md（组概述 + 文件/函数
索引）；子包保持单 README。主 README 的索引段会被移除（改由分卷 README 承载，
导航链接见主 README「分卷 README」）。根包每个 .go 必须归入恰好一个分卷，
否则脚本报错退出（覆盖自检，见 verify_coverage）。

索引从源码 doc 注释提取每个函数的首行摘要（中文优先，代码标识符保留原文），
与实现保持同步；改动源码注释后重跑本脚本即可刷新。

用法：python scripts/gen_core_readme_index.py
"""

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parent.parent / "application" / "core"
SECTION_HEADER = "## 文件与函数索引"
CORRUPTED_HEADER = re.compile(r"^## \?{7,}$")

# 会话/视图/审批等跨域回归测试：文件名前缀不表达归属，用显式名单挂到 session 卷
SESSION_EXTRA = [
    "approval_session_ownership_test.go",
    "message_seq_scope_test.go",
    "view_singleton_test.go",
    "view_switch_isolation_test.go",
    "subscribe_session_test.go",
    "hot_attach_running_test.go",
    "interrupted_continue_test.go",
    "fork_gate_test.go",
    "shutdown_concurrent_test.go",
]

# 基础与杂项：前缀各异（或就是单文件域），用显式名单成卷
MISC_FILES = [
    "aliases.go",
    "completion.go",
    "compressed_turn.go",
    "compressed_turn_test.go",
    "diagnostics.go",
    "fault_guard.go",
    "fault_guard_test.go",
    "limits.go",
    "perf_stats.go",
    "race_test.go",
    "resident_lru.go",
    "resident_lru_test.go",
    "runtime_projection.go",
    "snapshot_budget.go",
    "snapshot_budget_test.go",
    "workspace_usecase.go",
    "workspace_file_usecase_test.go",
    "workspace_tree_usecase_test.go",
]

# 根包分卷：(卷名, 组概述, 前缀列表, 显式文件名列表)；卷名用于 README-<卷名>.md。
# 覆盖规则 = 命中任一前缀 **或** 在显式名单中；每个根包 .go 必须恰好命中一卷，
# 由 verify_coverage 兜底（新增文件不归卷即失败，不静默漏文档）。
ROOT_GROUPS = [
    ("service", "Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）", ["service"], []),
    ("session", "会话草稿/恢复/存储用例与集成测试；运行中切到未驻留会话走异步冷加载（restoring 空壳 + 后台装载 + epoch 判定发布基线）", ["session"], SESSION_EXTRA),
    ("chat", "聊天主循环与可见输出集成", ["chat"], ["visible_output_test.go", "reasoning_visible_test.go"]),
    ("command", "内置命令注册与执行", ["command"], []),
    ("error", "错误码与面向用户的错误呈现", ["error"], []),
    ("history", "历史检索与 provider 失败恢复", ["history"], []),
    ("input", "输入分派与路由兼容测试", ["input"], []),
    ("plan", "Plan 打点/分支事件/重规划", ["plan"], []),
    ("reference", "read_tool_result / read_plan 引用工具", ["reference"], []),
    ("skill", "Skill 指令信封编解码", ["skill"], []),
    ("tool", "工具事件钩子与诊断", ["tool"], []),
    ("work-table", "工作表格投影与测试", ["work_table"], []),
    ("context", "上下文控制相关集成测试", ["context"], []),
    ("task", "任务执行集成测试", ["task"], []),
    ("subagent", "子代理投影集成测试", ["subagent"], []),
    ("goal", "goal 域协调器/门面用例与「goal 上线即装配 TL 团队」接线回归", ["goal"], []),
    ("agentteam", "AgentTeam 装配适配与群聊角色会话透传（端口形状与 A2A 元数据口径）", ["agentteam", "role_session"], []),
    ("composer", "输入草稿合成器的持久化与工作区绑定（重启后恢复）", ["composer"], []),
    ("archive", "会话归档与收尾语义（归档隐藏/拒绝忙碌会话、推理与工具叙述随记录持久化、运行判定与取消排空）", ["archive"], ["close_semantics_test.go"]),
    ("misc", "基础与杂项（aliases/limits/completion/compressed/diagnostics/fault-guard/perf/snapshot-budget/runtime/workspace/race）", [], MISC_FILES),
]


def verify_coverage(all_files):
    """把根包 .go 文件按前缀/名单分派到分卷；漏归属或重复归属直接失败。

    这是「索引静默漏文档」的报警器：新增根包文件后必须显式归卷（前缀打头
    或加进某个显式名单），否则本脚本拒绝刷新。
    """
    hits = {}
    for group_name, _, prefixes, names in ROOT_GROUPS:
        for path in all_files:
            if any(path.name.startswith(prefix) for prefix in prefixes) or path.name in names:
                hits.setdefault(path.name, []).append(group_name)
    orphans = sorted(name for name in (p.name for p in all_files) if name not in hits)
    overlaps = {name: groups for name, groups in hits.items() if len(groups) > 1}
    problems = []
    if orphans:
        problems.append("未归属任何分卷：" + "、".join(orphans))
    if overlaps:
        problems.append(
            "同时命中多个分卷：" + "、".join(f"{n}→{'/'.join(g)}" for n, g in sorted(overlaps.items()))
        )
    if problems:
        raise SystemExit("根包分卷覆盖自检失败（请把新文件归入某个分卷）：\n  " + "\n  ".join(problems))
    return {name: groups[0] for name, groups in hits.items()}


def coverage_hint(prefixes, names) -> str:
    """卷内「覆盖」说明行：前缀 + 是否有显式名单。"""
    parts = "、".join(f"`{prefix}*.go`" for prefix in prefixes) if prefixes else "显式名单"
    if prefixes and names:
        parts += " + 显式名单（见生成器 `ROOT_GROUPS`）"
    elif not prefixes:
        parts = f"显式名单 {len(names)} 个文件（见生成器 `ROOT_GROUPS`）"
    return parts


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


def extra_prose(text: str) -> str:
    """返回卷 README 中「生态位」概述之外的既有散文段。

    卷 README = 组概述 + 索引，但维护者会在概述后补手写契约（如历史分页
    契约、实现要点）。这类段落不属于生成器管辖，重新生成时必须原样保留，
    否则刷新索引会静默丢文档。
    """
    lines = strip_old_section(text).splitlines()
    kept = []
    started = False
    for line in lines:
        if line.startswith("## ") and line.strip() != "## 生态位":
            started = True
        if started:
            kept.append(line)
    return "\n".join(kept).rstrip()


def group_section(
    group_name: str,
    overview: str,
    go_files,
    base: pathlib.Path,
    prefixes,
    names,
    prose: str = "",
) -> str:
    out = [
        f"# core/{group_name}（根包分卷）",
        "",
        "## 生态位",
        "",
        overview,
        "",
        f"覆盖：{coverage_hint(prefixes, names)}；未归属文件由覆盖自检拦下。",
        "",
    ]
    if prose:
        out.append(prose)
        out.append("")
    out.extend(
        [
            "## 文件与函数索引",
            "",
            "> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。",
            "> 刷新方式：`python scripts/gen_core_readme_index.py`。",
            "",
        ]
    )
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
    owner = verify_coverage(all_files)
    for group_name, overview, prefixes, names in ROOT_GROUPS:
        group_files = [f for f in all_files if owner[f.name] == group_name]
        if not group_files:
            continue
        volume = ROOT / f"README-{group_name}.md"
        prose = extra_prose(volume.read_text(encoding="utf-8")) if volume.exists() else ""
        write_if_changed(
            volume,
            group_section(group_name, overview, group_files, ROOT, prefixes, names, prose),
        )
    print("README 分卷已刷新。")


if __name__ == "__main__":
    main()
