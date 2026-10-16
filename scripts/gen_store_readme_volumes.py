"""生成 store/ 根包的「分卷 README」（AGENTS.md §3 + docs/arch/readme-spec.md §4）。

判据与 application/core 同款：根包文件多（>15），README 按**文件前缀**分卷，
每卷一个 `README-<卷名>.md`（生态位 + 文件与函数索引），主 README 保留生态位与
「分卷 README」导航表。

摘要的事实源是源码 doc 注释首行，提取逻辑**复用** scripts/gen_core_readme_index.py，
不再写第二份提取器（AGENTS.md §8.1）。

用法：

    python scripts/gen_store_readme_volumes.py            # 写入 store/README-<卷>.md
    python scripts/gen_store_readme_volumes.py --check    # 只做覆盖自检，不写文件

覆盖自检：store/ 下每个 .go（含测试）必须恰好落进一卷；漏掉或重复即失败——
避免新增文件时文档静默漏写。
"""

from __future__ import annotations

import importlib.util
import pathlib
import sys

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
STORE = REPO_ROOT / "store"

# 卷名 -> (覆盖文件前缀, 一句话生态位)
VOLUMES = [
    ("session", ("sessionstore", "session_granular", "session_context", "session_meta",
                 "durable_history", "runtime_api", "conversation", "history_resume_readers"),
     "会话句柄与上下文状态：Router/SessionGranularStore、历史读面与运行期 API"),
    ("message", ("message_rows", "structural_events", "event_store", "event_range",
                 "paragraphs", "wire_assembler", "big_tool_result", "provider_wire"),
     "message/event 事件行、段落投影、wire 装配与大工具结果归档"),
    ("compact", ("compact_frames", "compaction_records", "checkpoint_store",
                 "context_state", "material_cache", "attempt_cache"),
     "压缩帧与压缩记录、检查点、运行期材料/尝试缓存"),
    ("stack", ("stack_channel", "stack_journal"),
     "上下文四栈的通道与日志（Plan/Task/Skill/Compact）"),
    ("retention", ("retention", "pending_tail", "lifecycle", "queue_consume"),
     "草稿尾与未提交批次、队列消费与 LRU 回收"),
    ("board", ("board", "project_board", "project_known", "project_record"),
     "项目看板投影：行文件、会话分目录、项目知识记录与渲染件"),
    ("team", ("teamwork", "team_registry", "role_session", "node_session_store",
              "unit_record_writer"),
     "团队计划/工作项、角色会话与节点会话记账"),
    ("media", ("media", "search_index", "tool_record", "schedule_events"),
     "媒体分区、检索索引与派生通道（工具记录、定时任务事件）"),
    ("storage", ("json_layout", "module_heads", "fork_store", "legacy_storage",
                 "router_storage", "root_aliases"),
     "存储引擎底座：JSON v8 布局、模块 head 发布、fork 子树、历史迁移与保形别名"),
    ("misc", (),
     "跨卷用例与基础回归（会话状态/锁、head 发布与自愈、并发与读放大探针）"),
]

# misc 是测试文件的兜底卷：生产文件必须落进显式前缀，测试文件允许进 misc。
FALLBACK = "misc"

HEADER = "## 文件与函数索引"


def load_generator():
    path = REPO_ROOT / "scripts" / "gen_core_readme_index.py"
    spec = importlib.util.spec_from_file_location("gen_core_readme_index", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def assign(name: str) -> str | None:
    for volume, prefixes, _ in VOLUMES:
        for prefix in prefixes:
            if name.startswith(prefix):
                return volume
    if name.endswith("_test.go"):
        return FALLBACK
    return None


def coverage_line(prefixes, names) -> str:
    if prefixes:
        return f"覆盖 {len(names)} 个文件（前缀 `{'`/`'.join(prefixes)}`）。"
    return f"覆盖 {len(names)} 个文件（兜底卷：测试文件按前缀无归属时落这里）。"


def main() -> int:
    check_only = "--check" in sys.argv
    generator = load_generator()
    files = sorted(path.name for path in STORE.glob("*.go"))
    groups: dict[str, list[str]] = {volume: [] for volume, _, _ in VOLUMES}
    unassigned = []
    for name in files:
        volume = assign(name)
        if volume is None:
            unassigned.append(name)
            continue
        groups[volume].append(name)
    if unassigned:
        print("未归属文件（请补进 VOLUMES 前缀）:")
        for name in unassigned:
            print(f"  {name}")
        return 1

    total = 0
    for volume, prefixes, niche in VOLUMES:
        names = groups[volume]
        if not names:
            print(f"卷 {volume} 没有文件")
            return 1
        body = [f"# store/{volume}（根包分卷）", "", "## 生态位", "", niche, "",
        coverage_line(prefixes, names), "",
                HEADER, "",
                "> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。",
                "> 刷新方式：`python scripts/gen_store_readme_volumes.py`。", "",
                generator.build_index([STORE / name for name in names], STORE)]
        text = "\n".join(body).rstrip() + "\n"
        target = STORE / f"README-{volume}.md"
        if not check_only:
            with open(target, "wb") as handle:
                handle.write(text.encode("utf-8"))
        total += len(names)
        print(f"  {target.name:26} {len(names):3} files  {len(text):7} bytes")
    print(f"covered {total}/{len(files)} files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
