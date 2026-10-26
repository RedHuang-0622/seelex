#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""全仓 README「文件与函数索引」生成器（P13：按包生成 + `--check` / `--only`）。

判据的事实源：`docs/arch/readme-spec.md` §3/§4 与
`docs/2026-10-15-architecture-reshape/r10-r11-p13-contract.md` §3。

本脚本只写**已定案树**（搬迁中/收口中的树由各自切片负责，见契约 §3.1）：

* 单 README 包（`## 生态位` + 索引）：`SETTLED_TREES` 下按目录自动发现；
* 分卷包（根/大包，文件数 > 15，按文件前缀分卷）：`application/core`、`test/`、`store/`；
* 树外已登记包：`kernel/state`、`middle/protocol/{model,event}`、`middle/view{,/board,/boardtest}`。

脚本名保持不变：仓库既有 README 的「刷新方式」行都指向
`python scripts/gen_core_readme_index.py`（该格式已冻结，改名会让全仓 README 漂移）。

用法：

    python scripts/gen_core_readme_index.py                        # 全仓刷新（只写已定案树）
    python scripts/gen_core_readme_index.py --check                # 只报漂移，不写文件；有漂移 exit 1
    python scripts/gen_core_readme_index.py --only <import-path>…  # 只处理给定包（仓库相对路径前缀匹配）

安全（「索引静默漏文档」的报警器）：

1. 分卷覆盖自检：每个 `.go` 必须**恰好**归入一卷，漏归属/重复归属直接失败；
2. 索引段是生成器的地盘：段内只允许生成器写的形状（`### 文件` / `- 条目` / `> 说明`）。
   一旦发现手写表格、缩进列表或别的分节，**拒绝覆盖整批**并逐条列出，
   避免刷新索引时静默丢掉手写文档——先人工归一到冻结格式，再跑本脚本。
"""

from __future__ import annotations

import pathlib
import re
import sys
from dataclasses import dataclass

REPO = pathlib.Path(__file__).resolve().parent.parent
SECTION_HEADER = "## 文件与函数索引"
CORRUPTED_HEADER = re.compile(r"^## \?{7,}$")
NAVIGATION_HEADER = "## 分卷 README"
MODULE_PREFIX = "github.com/RedHuang-0622/seelex/"

# 索引段的说明两行是**冻结格式**（契约 §3.1）：改这里等于让全仓索引漂移。
SECTION_NOTE = (
    "> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。",
    "> 刷新方式：`python scripts/gen_core_readme_index.py`。",
)

# 单 README 包：按树自动发现（搬迁中/收口中的树刻意不在列）。
SETTLED_TREES = (
    "contract",
    "console",
    "e2e",
    "headless",
    "wiring",
    "store",
    "internal",
    "backend/workunit",
    "backend/limit",
    "backend/multiagent",
)

# 树外已登记包（历史遗留的基座包；索引一直由本脚本负责）。
EXTRA_PACKAGES = (
    "kernel/state",
    "middle/protocol/model",
    "middle/protocol/event",
    "middle/view",
    "middle/view/board",
    "middle/view/board/boardtest",
)

# 「搬迁中/收口中」的延后树在 2026-10-17 已全部清空：M5 的搬迁与收口片入库后工作树静默，
# 原先登记的 52 个延后包（`application/core`、`backend/{engine,tool}`、`middle`）由本脚本
# 一次性刷新完毕（见 `docs/2026-10-15-architecture-reshape/m5-record.md` §29 的收尾记录）。
# 判据与实现仍是同一份：全仓包一律按 `--check` 逐字比对，没有再需要跳过的树。
DEFERRED_TREES = ()

# 例外表随之清空（它只为"延后树里已登记的包"服务）。
REGISTERED_IN_DEFERRED = ()


def is_deferred(rel: str) -> bool:
    """该包是否属于「搬迁中/收口中」的延后树。"""
    if rel in REGISTERED_IN_DEFERRED:
        return False
    return any(rel == tree or rel.startswith(tree + "/") for tree in DEFERRED_TREES)


def deferred_packages() -> dict:
    """延后清单：包路径 → 目录内 .go 文件数（供登记与回报用）。"""
    out = {}
    for tree in DEFERRED_TREES:
        base = REPO / tree
        if not base.is_dir():
            continue
        for path in sorted({go.parent for go in base.rglob("*.go")}):
            rel = path.relative_to(REPO).as_posix()
            if is_deferred(rel):
                out[rel] = len(list(path.glob("*.go")))
    return out


@dataclass(frozen=True)
class VolumeSpec:
    """一卷：卷名、一句话概述、覆盖用的文件前缀与显式名单。"""

    name: str
    overview: str
    prefixes: tuple[str, ...] = ()
    names: tuple[str, ...] = ()
    fallback: bool = False


@dataclass(frozen=True)
class VolumePackage:
    """根/大包：主 README 之外的 `README-<卷名>.md` 由本脚本生成。"""

    rel: str
    title: str
    volumes: tuple[VolumeSpec, ...]
    main_mode: str
    keep_prose: bool = False
    fallback_volume: str | None = None
    table: str = ""  # 本包分卷登记表的常量名（写在卷 README 的「覆盖：」行里，便于维护者定位）


# 根包分卷：(卷名, 组概述, 覆盖前缀, 显式文件名)；卷名用于 README-<卷名>.md。
# 覆盖规则 = 命中任一前缀 **或** 在显式名单中；每个根包 .go 必须恰好命中一卷。
ROOT_GROUPS = (
    VolumeSpec("service", "Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）", ("service",), ("component_getter_boundary_test.go", "employee_permission_assembly_test.go", "fixture_concurrency_test.go", "frontend_migration_test.go", "frontend_ports.go", "frontend_shell_legacy_test.go", "scheduler_test_aliases_test.go", "teamwork_service.go", "teamwork_board_projection_test.go", "role_tool_activity_test.go", "teamwork_board_session_switch_test.go", "teamwork_completion_trigger_test.go")),
    VolumeSpec("session", "会话草稿/恢复/存储用例与集成测试；运行中切到未驻留会话走异步冷加载（restoring 空壳 + 后台装载 + epoch 判定发布基线）", ("conversation_session", "edit_fork", "session"), ("approval_session_ownership_test.go", "message_seq_scope_test.go", "view_singleton_test.go", "view_switch_isolation_test.go", "subscribe_session_test.go", "subscription_legacy_test.go", "hot_attach_running_test.go", "interrupted_continue_test.go", "fork_gate_test.go", "shutdown_concurrent_test.go", "content_lru.go", "content_lru_test.go", "content_lru_snapshot_test.go")),
    VolumeSpec("chat", "聊天主循环与可见输出集成（含可见会话的群聊角色归属载荷）", ("chat",), ("visible_output_test.go", "reasoning_visible_test.go", "visible_role_attribution_test.go")),
    VolumeSpec("command", "内置命令注册与执行", ("command",), ("permission_command_test.go",)),
    VolumeSpec("error", "错误码与面向用户的错误呈现", ("error",)),
    VolumeSpec("history", "历史检索与 provider 失败恢复", ("history",)),
    VolumeSpec("input", "输入分派与路由兼容测试", ("input",), ("durable_queue_wire_test.go",)),
    VolumeSpec("plan", "Plan 打点/分支事件/重规划", ("plan",)),
    VolumeSpec("reference", "read_tool_result / read_plan 引用工具", ("reference",)),
    VolumeSpec("skill", "Skill 指令信封编解码", ("skill",)),
    VolumeSpec("tool", "工具事件钩子与诊断", ("tool",)),
    VolumeSpec("work-table", "后台作业面与工作表格投影：执行登记表的只读投影、请求尾部打点块（在途行 + 待取回的完成行）、作业终态为**空闲**会话触发对话、项目看板的派生投影（按会话落盘 + 按需读取面）", ("work_table", "async", "project_board")),
    VolumeSpec("context", "上下文控制相关集成测试", ("context",), ("environment_context.go", "environment_context_test.go")),
    VolumeSpec("task", "任务执行集成测试", ("task",)),
    VolumeSpec("subagent", "子代理投影集成测试", ("subagent",)),
    VolumeSpec("goal", "goal 域协调器/门面用例与「goal 上线不覆盖会话团队」接线回归", ("goal",)),
    VolumeSpec("agentteam", "AgentTeam 装配适配与群聊角色会话透传（端口形状与 A2A 元数据口径）", ("agentteam", "role_session"), ("role_turn.go", "role_turn_test.go")),
    VolumeSpec("permission", "角色默认/会话覆盖的权限向量、账号选择、继承与 SSH 路径白名单", ("role_permissions",), ("policy_test_aliases_test.go",)),
    VolumeSpec("composer", "输入草稿合成器的持久化与工作区绑定（重启后恢复）", ("composer",)),
    VolumeSpec("archive", "会话归档与收尾语义（归档隐藏/拒绝忙碌会话、推理与工具叙述随记录持久化、运行判定与取消排空）", ("archive",), ("close_semantics_test.go",)),
    # P8/R10b（2026-10-17）：`aliases.go` 已删，拆成 `vocabulary.go`（词表与错误值）与
    # `assembly_helpers.go`（装配与转发面），两者随本卷。
    VolumeSpec("misc", "基础与杂项（vocabulary/assembly_helpers/limits/completion/compressed/diagnostics/fault-guard/perf/snapshot-budget/runtime/workspace/race）", (), ("vocabulary.go", "assembly_helpers.go", "completion.go", "completion_test.go", "compressed_turn.go", "compressed_turn_test.go", "diagnostics.go", "fault_guard.go", "fault_guard_test.go", "limits.go", "perf_stats.go", "race_test.go", "review_files.go", "review_files_test.go", "resident_lru.go", "resident_lru_test.go", "runtime_projection.go", "snapshot_budget.go", "snapshot_budget_test.go", "workspace_host.go", "workspace_usecase.go", "workspace_file_usecase_test.go", "workspace_tree_usecase_test.go", "workspace_environments.go", "workspace_gitrepos.go", "workspace_gitrepos_usecase_test.go", "plugin_source_projection_test.go", "served_route_test.go", "turn_status_single_word_test.go", "window.go")),
)

# test/：仓库级集成用例包（文件数 > 15，按 readme-spec §4 分卷）。
TEST_VOLUMES = (
    VolumeSpec("harness", "用例基座：包说明、TestMain 的 CWD 口径与 launchFlags 摆现场句柄。", (), ("doc.go", "main_test.go")),
    VolumeSpec("live", "真 API / 手工冒烟族：由 build tag 与环境变量双开关，缺凭证即 skip（默认集合不红）。", ("async_", "compact_live", "computer_use", "context_long", "job_backfill", "job_live", "manual_smoke", "prefix_chain", "real_api_", "scheduled_task", "smoke_")),
    VolumeSpec("repro", "会话/工具链回归复现：用户现象 → 会红的断言（含多会话并发与热切场景）。", ("repro_",), ("prefix_invariant_fullchain_test.go",)),
    VolumeSpec("session", "会话生命周期与切换成本：链路基准与切换剖面。", ("session_",)),
    VolumeSpec("plugin", "插件与精选目录契约：目录登记、根解析、自省工具、技能激活、工具声明分封。", ("plugin_", "dev_plugin_", "curated_", "skill_activate_tool", "tool_meta_declaration", "tool_face_prompt")),
    VolumeSpec("project-board", "项目看板：读/写/刷新工具与装配接线。", ("project_board_",)),
    VolumeSpec("teamwork", "teamwork / 子代理装配：headless 冒烟、插件装配与 worktree 生命周期。", ("teamwork_",)),
    VolumeSpec("tool", "工具面全链：装配基座与上下文读工具、工具记录写路径。", ("tool_full_chain",)),
    VolumeSpec("permission", "权限门与审批桥：配置叠加、员工面板、真 API 权限冒烟。", ("permission_",)),
    VolumeSpec("release", "发布与交付契约：版本号、默认档位、构建布局、图标资源、快照校验与启动种子。", ("release_", "build_layout", "icon_resource", "boot_")),
    VolumeSpec("probe", "一次性探针：headless 恢复前缀顺序与作业迁移残留（现场取证用，保持可复跑）。", ("headless_restore_prefix_probe", "job_migration_residue")),
)

# store/：根包文件多（> 100），同款按文件前缀分卷
# （原先的第二份生成器 scripts/gen_store_readme_volumes.py 已并入本脚本，见 P13 删除清单）。
STORE_VOLUMES = (
    VolumeSpec("session", "会话句柄与上下文状态：Router/SessionGranularStore、历史读面与运行期 API", ("sessionstore", "session_granular", "session_context", "session_meta", "session_ssh", "durable_history", "runtime_api", "conversation", "history_resume_readers")),
    VolumeSpec("message", "message/event 事件行、段落投影、wire 装配与大工具结果归档", ("message_rows", "structural_events", "event_store", "event_range", "paragraphs", "wire_assembler", "big_tool_result", "provider_wire")),
    VolumeSpec("compact", "压缩帧与压缩记录、检查点、运行期材料/尝试缓存", ("compact_frames", "compaction_records", "checkpoint_store", "context_state", "material_cache", "attempt_cache")),
    VolumeSpec("stack", "上下文四栈的通道与日志（Plan/Task/Skill/Compact）", ("stack_channel", "stack_journal")),
    VolumeSpec("retention", "草稿尾与未提交批次、队列消费与 LRU 回收", ("retention", "pending_tail", "lifecycle", "queue_consume")),
    VolumeSpec("board", "项目看板投影：行文件、会话分目录、项目知识记录与渲染件", ("board", "project_board", "project_known", "project_record")),
    VolumeSpec("team", "团队计划/工作项、角色会话与节点会话记账", ("teamwork", "team_registry", "role_session", "node_session_store", "unit_record_writer"), ("role_permission.go", "role_permission_test.go")),
    VolumeSpec("media", "媒体分区、检索索引与派生通道（工具记录、定时任务事件）", ("media", "search_index", "tool_record", "schedule_events")),
    # P8 §5.2/§5.3（2026-10-17）：`root_aliases`（保形别名）与 `legacy_storage`（僵尸 shard 存储）
    # 两个前缀已随兼容面/僵尸面清理删除，本卷覆盖随之收缩。
    VolumeSpec("storage", "存储引擎底座：JSON v8 布局、模块 head 发布、fork 子树与历史迁移", ("json_layout", "module_heads", "fork_store", "router_storage")),
    VolumeSpec("misc", "跨卷用例与基础回归（会话状态/锁、head 发布与自愈、并发与读放大探针）", (), (), fallback=True),
)

VOLUME_PACKAGES = (
    VolumePackage("application/core", "# core/{name}（根包分卷）", ROOT_GROUPS, "strip-index", keep_prose=True, table="ROOT_GROUPS"),
    VolumePackage("test", "# test/{name}（集成用例分卷）", TEST_VOLUMES, "navigation", table="TEST_VOLUMES"),
    VolumePackage("store", "# store/{name}（根包分卷）", STORE_VOLUMES, "untouched", fallback_volume="misc", table="STORE_VOLUMES"),
)


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
        + "\n".join(SECTION_NOTE)
        + "\n\n"
        + build_index(go_files, base)
    )


def strip_old_section(text: str) -> str:
    """去掉上一轮的索引段（本次重写）。"""
    keep = []
    for line in text.splitlines():
        if line == SECTION_HEADER or CORRUPTED_HEADER.match(line):
            break
        keep.append(line)
    return "\n".join(keep).rstrip() + "\n"


def index_tail_offenders(text: str) -> list:
    """索引段里**非生成器形状**的行（手写表格/缩进列表/别的分节）。

    生成器只认三种行：`### <文件>`、`- <条目>`、`> <说明>`（空行随意）。
    发现别的内容就拒绝覆盖——否则刷新索引会把维护者手写的内容静默删掉。
    """
    lines = text.splitlines()
    start = None
    for i, line in enumerate(lines):
        if line == SECTION_HEADER or CORRUPTED_HEADER.match(line):
            start = i
            break
    if start is None:
        return []
    return [
        line
        for line in lines[start + 1 :]
        if line.strip() and not line.startswith(("### ", "- ", "> "))
    ]


def extra_prose(text: str) -> str:
    """返回卷 README 中「生态位」概述之外的既有散文段。

    卷 README = 组概述 + 索引，但维护者会在概述后补手写契约（如历史分页
    契约、实现要点）。这类段落不属于生成器管辖，重新生成时必须原样保留，
    否则刷新索引会静默丢文档。

    两个起始点：生成器写的「覆盖：」行之后（手写散文紧跟概述的常见写法），
    或任意非「生态位」的 `## ` 子标题（手写分节）。索引段之后的内容由
    strip_old_section 负责丢弃，不算散文。
    """
    lines = strip_old_section(text).splitlines()
    kept = []
    started = False
    for line in lines:
        if not started and line.startswith("覆盖："):
            started = True
            continue
        if line.startswith("## ") and line.strip() != "## 生态位":
            started = True
        if started:
            kept.append(line)
    return "\n".join(kept).strip()


def coverage_hint(vol: VolumeSpec, table: str) -> str:
    """卷内「覆盖」说明行：前缀 + 是否有显式名单（兜底卷单独说清）。"""
    if vol.fallback:
        return "测试文件兜底卷（前缀无归属时落这里）"
    parts = "、".join(f"`{prefix}*.go`" for prefix in vol.prefixes) if vol.prefixes else "显式名单"
    if vol.prefixes and vol.names:
        parts += f" + 显式名单（见生成器 `{table}`）"
    elif not vol.prefixes:
        parts = f"显式名单 {len(vol.names)} 个文件（见生成器 `{table}`）"
    return parts


def group_section(title: str, vol: VolumeSpec, go_files, base: pathlib.Path, prose: str = "", table: str = "") -> str:
    out = [
        title,
        "",
        "## 生态位",
        "",
        vol.overview,
        "",
        f"覆盖：{coverage_hint(vol, table)}；未归属文件由覆盖自检拦下。",
        "",
    ]
    if prose:
        out.append(prose)
        out.append("")
    out.extend([SECTION_HEADER, "", *SECTION_NOTE, ""])
    out.append(build_index(go_files, base))
    return "\n".join(out).rstrip() + "\n"


def strip_navigation(text: str) -> str:
    """去掉上一轮的「分卷 README」导航段（本次重写）。"""
    index = text.find(NAVIGATION_HEADER)
    if index < 0:
        return text
    return text[:index].rstrip() + "\n"


def assign_volumes(files, spec: VolumePackage) -> dict:
    """把包内 .go 文件按前缀/名单分派到分卷；漏归属或重复归属直接失败。"""
    hits = {}
    for vol in spec.volumes:
        for path in files:
            if any(path.name.startswith(prefix) for prefix in vol.prefixes) or path.name in vol.names:
                hits.setdefault(path.name, []).append(vol.name)
    orphans = sorted(name for name in (p.name for p in files) if name not in hits)
    overlaps = {name: groups for name, groups in hits.items() if len(groups) > 1}
    owner = {name: groups[0] for name, groups in hits.items()}
    problems = []
    if overlaps:
        problems.append(
            "同时命中多个分卷：" + "、".join(f"{n}→{'/'.join(g)}" for n, g in sorted(overlaps.items()))
        )
    if orphans:
        if spec.fallback_volume:
            stray = [name for name in orphans if not name.endswith("_test.go")]
            if stray:
                problems.append("未归属任何分卷（非测试文件不落兜底卷）：" + "、".join(stray))
            for name in orphans:
                if name.endswith("_test.go"):
                    owner[name] = spec.fallback_volume
        else:
            problems.append("未归属任何分卷：" + "、".join(orphans))
    if problems:
        raise SystemExit(
            f"{spec.rel} 分卷覆盖自检失败（请把新文件归入某个分卷）：\n  " + "\n  ".join(problems)
        )
    return owner


def navigation_table(spec: VolumePackage, owner: dict) -> str:
    out = [
        NAVIGATION_HEADER,
        "",
        "本包文件数超过 15，按 readme-spec §4 分卷承载「文件与函数索引」（每卷一个 "
        "`README-<卷名>.md`）：",
        "",
        "| 卷 | 覆盖 | 内容 |",
        "|---|---|---|",
    ]
    for vol in spec.volumes:
        covered = sorted(name for name, group in owner.items() if group == vol.name)
        if not covered:
            continue
        out.append(
            "| [README-%s.md](README-%s.md) | %s | %s |"
            % (vol.name, vol.name, "、".join("`%s`" % n for n in covered), vol.overview)
        )
    return "\n".join(out).rstrip() + "\n"


def plan_single(rel: str) -> dict:
    """单 README 包：既有文本 + 索引段。"""
    package = REPO / rel
    readme = package / "README.md"
    if not readme.exists():
        print(f"警告：{rel} 无 README.md（本脚本不代写；README 义务见 e2e/layout_test.go）")
        return {}
    text = strip_old_section(readme.read_text(encoding="utf-8"))
    return {readme: text.rstrip() + "\n\n" + section(sorted(package.glob("*.go")), package) + "\n"}


def plan_volumes(spec: VolumePackage) -> dict:
    """分卷包：每卷一个 README-<卷名>.md（+ 主 README 的索引/导航处理）。"""
    package = REPO / spec.rel
    files = sorted(package.glob("*.go"))
    if not files:
        return {}
    owner = assign_volumes(files, spec)
    planned = {}
    for vol in spec.volumes:
        group_files = [f for f in files if owner[f.name] == vol.name]
        if not group_files:
            continue
        target = package / f"README-{vol.name}.md"
        prose = ""
        if spec.keep_prose and target.exists():
            prose = extra_prose(target.read_text(encoding="utf-8"))
        planned[target] = group_section(
            spec.title.format(name=vol.name), vol, group_files, package, prose, spec.table
        )
    main = package / "README.md"
    if main.exists():
        text = main.read_text(encoding="utf-8")
        if spec.main_mode == "strip-index":
            planned[main] = strip_old_section(text)
        elif spec.main_mode == "navigation":
            base = strip_navigation(strip_old_section(text).rstrip())
            planned[main] = base + "\n\n" + navigation_table(spec, owner) + "\n"
    return planned


def selected(rel: str, only: tuple) -> bool:
    if not only:
        return True
    return any(rel == want or rel.startswith(want + "/") for want in only)


def normalize_only(values) -> tuple:
    return tuple(
        v.strip().replace("\\", "/").removeprefix(MODULE_PREFIX).strip("/")
        for v in values
        if v.strip()
    )


def build_plan(only: tuple) -> tuple:
    """返回 (path -> 目标内容, 拒绝覆盖清单)。"""
    singles = set()
    for tree in SETTLED_TREES:
        base = REPO / tree
        if not base.is_dir():
            continue
        for path in sorted({go.parent for go in base.rglob("*.go")}):
            singles.add(path.relative_to(REPO).as_posix())
    singles.update(EXTRA_PACKAGES)
    for spec in VOLUME_PACKAGES:
        singles.discard(spec.rel)

    planned = {}
    for rel in sorted(singles):
        if selected(rel, only) and (only or not is_deferred(rel)):
            planned.update(plan_single(rel))
    for spec in VOLUME_PACKAGES:
        if selected(spec.rel, only) and (only or not is_deferred(spec.rel)):
            planned.update(plan_volumes(spec))

    refused = []
    for path in sorted(planned):
        if not path.exists():
            continue
        offenders = index_tail_offenders(path.read_text(encoding="utf-8"))
        if offenders:
            refused.append((path, offenders))
    return planned, refused


def write_if_changed(path: pathlib.Path, content: str) -> None:
    if path.exists() and path.read_text(encoding="utf-8") == content:
        return
    # 显式 LF：Windows 上 `write_text` 的默认换行翻译会把整份 README 写成 CRLF，
    # 而那与仓库的 LF 约定、`.githooks/pre-commit` 的 CRLF 拦截直接冲突
    # （同款事故见 docs/2026-10-15-architecture-reshape/m2-record.md 的 D-M2-13）。
    path.write_text(content, encoding="utf-8", newline="\n")
    print("wrote", path.relative_to(REPO).as_posix())


def report_refused(refused) -> None:
    print(f"拒绝覆盖 {len(refused)} 个 README：索引段里有非生成器内容（手写表格/缩进列表/别的分节）。")
    print("请先人工归一到冻结格式（每文件一节 `### <文件>`、每条目一行 `- <签名> — <摘要>`），再重跑本脚本：")
    for path, offenders in refused:
        print(f"  {path.relative_to(REPO).as_posix()}（{len(offenders)} 行）")
        for line in offenders[:3]:
            print(f"      {line[:100]}")


def main() -> int:
    argv = sys.argv[1:]
    check_only = "--check" in argv
    only = ()
    if "--only" in argv:
        idx = argv.index("--only")
        only = normalize_only([a for a in argv[idx + 1 :] if not a.startswith("--")])
        if not only:
            print("--only 需要一个或多个 import path")
            return 2
    unknown = [a for a in argv if a.startswith("--") and a not in ("--check", "--only")]
    if unknown:
        print("未知参数：" + " ".join(unknown))
        return 2

    if not only and DEFERRED_TREES:
        print(
            f"跳过 {len(deferred_packages())} 个延后包（搬迁中/收口中，README 由各自切片负责；"
            "登记见 tmp/reshape/p13-deferred.tsv）。"
        )
    planned, refused = build_plan(only)
    if refused:
        report_refused(refused)
        return 1

    if check_only:
        drift = []
        for path in sorted(planned):
            rel = path.relative_to(REPO).as_posix()
            if not path.exists():
                drift.append((rel, "索引段缺失"))
            elif path.read_text(encoding="utf-8") != planned[path]:
                drift.append((rel, "内容漂移"))
        if drift:
            print(f"--check：检查 {len(planned)} 个 README，{len(drift)} 个需要刷新。")
            for rel, why in drift:
                print(f"  漂移 {rel}（{why}）")
            return 1
        print(f"--check：检查 {len(planned)} 个 README，无漂移。")
        return 0

    for path in sorted(planned):
        write_if_changed(path, planned[path])
    print(f"索引已刷新（{len(planned)} 个 README）。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
