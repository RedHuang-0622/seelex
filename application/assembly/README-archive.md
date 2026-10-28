# assembly/archive（根包分卷）

## 生态位

会话归档与收尾语义（归档隐藏/拒绝忙碌会话、推理与工具叙述随记录持久化、运行判定与取消排空）

覆盖：`archive*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## Session archive 宿主适配（AP-011 首个切片）

归档的宿主闭包已移到 `Session archive application`：
`middle/residency.Archive` 仍是准入、persist、归档写入和目录刷新的唯一判据；
`Service.ArchiveSession` 只做一行组件转发。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。
