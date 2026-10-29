# 上下文前缀稳定性 M2–M5 前置审查

## 需求摘要

在已冻结的 M0/M1 基线上完成 `context-prefix-stability-l1.md` 的 M2–M5，并收口交接单中与主链直接相关的续跑持久化和节点子代理尾部帧接缝。M0/M1 不重做。

## 既有事实

| 事实 | 位置 | 本轮处理 |
|---|---|---|
| 字符探针、user/plan/task 字节冻结已完成 | `kernel/context/prefixprobe/`、`kernel/task/task_wire_content.go`、`kernel/task/plan_frame.go` | 保持，不改判据 |
| M2 有 WIP 提交但不可直接合并 | `seelex/l1-wi-m2-l1` / `9353dc99` | 吸收设计意图，重新以红测驱动实现 |
| 每轮强制释放工作历史 | `application/conversation/turn_completion.go`、`contract/engine_capabilities.go` | 删除热路径调用，保留显式卸载能力 |
| 正常装配仍按尾部窗口重选 | `kernel/context/coordinator.go` `fitExecutionHistory` / `tryFitExecutionHistory` | 改为 `truth[retained_from:]`；窗口只用于压缩边界决策 |
| memory 按 query 选择 | `backend/engine/context/seelectx/assembler.go` | M4 改为压缩纪元快照 + append-only 表 |
| 工具发布顺序来自 map range | `backend/engine/assemble/runtime_context.go`、Seele tools catalog | M5 冻结 catalog 并稳定排序 |
| 节点会话继承块包含 plan/task 尾部帧 | `backend/engine/assemble/runtime.go`、`backend/tool/node/coordinator.go` | 拆稳定块与尾部块，尾部块追加到 WorkingHistory 之后 |
| 续跑运行时已有两层状态与账本 | `application/execution/continuation*.go`、`kernel/task/continuation_state.go` | 补齐会话存档映射和审计接线验证 |

## 影响文件清单

| 文件/目录 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `kernel/context/prefix_key.go` | 新增 | 装配出口前缀键 | 真六格键，禁止 query 进入 |
| `kernel/task/context_l1.go` | 新增 | L1 派生快照与命中归因 | 快路径只追加、失败显式归因 |
| `kernel/task/coordinator.go` | 修改 | `sessionTaskRuntime` | 进程内 L1、水位和 draft actor 状态 |
| `kernel/context/coordinator.go` | 修改 | `prepareExecutionContextFor`、`fitExecutionHistory` | truth 直切、L1 复用、压缩失效 |
| `backend/engine/assemble/runtime_context.go` | 修改 | `PrefixWorld`、工具/栈投影 | 提供键世界状态，稳定 catalog |
| `application/conversation/turn_completion.go` | 修改 | 删除 release 热路径 | 工作历史跨轮保留 |
| `backend/tool/node/coordinator.go` | 修改 | `ScopeAssembler` | 子代理尾部帧放到请求最末 |
| `contract/protocol/state.go` | 修改 | `SessionExecutionRecord` | draft 水位、memory/tools 纪元持久化 |
| `kernel/task/` | 新增/修改 | draft actor、水位、纪元 fold | 单写者、ack、幂等、崩溃重放 |
| `backend/engine/context/seelectx/assembler.go` | 修改 | memory/工具集注入 | 从 query top-K 改为纪元快照 |
| `application/assembly/` | 修改 | 窄端口与持久化映射 | 接线和架构预算 |
| 相关 `README.md`、主设计和 devlog | 修改 | 当前实现、风险、验收 | 代码与文档同步 |

## 依赖分析

- 上游：Application 只通过窄端口提交状态；Context 装配消费 task truth；Task 是 transcript/水位 owner。
- 下游：EnginePort 接收稳定历史；Seele request assembler 消费 tools 与 messages；Store 持久化 `SessionRecord`。
- L1 是派生数据，不能成为 Store 或 Engine 的事实源。
- memory/tools 纪元表属于会话持久事实，冻结 select 后的字节，冷加载只 fold 不重选。

## 循环依赖检查

- 不新增 `kernel/context` → `application/assembly` 反向依赖。
- 键和 draft actor 的纯类型/算法留在 `kernel`；Runtime 只通过窄可选端口提供项目/压缩世界状态。
- Store 根包不新增领域方法；新增持久字段由现有 SessionRecord 原子快照携带。

## 风险预估

| 风险 | 概率 | 严重度 | 缓解 |
|---|---:|---:|---|
| L1 键相同但 truth 内容变化导致错误复用 | 中 | 高 | 记录已消费 Seq 水位；只追加新事件，缺口即重建 |
| 压缩边界改变仍读旧 L1 | 中 | 高 | compact/memory epoch 进键，压缩后显式失效 |
| 删除每轮 ClearHistory 暴露引擎历史残留 | 低 | 高 | 以会话路由和完整回归验证；卸载路径仍显式释放 |
| draft actor 重复提交 | 中 | 高 | `(session, seq)` 幂等键、轮尾 ack、持久水位 |
| 纪元表损坏造成静默漂移 | 低 | 高 | Digest 校验，失败显式降级/拒绝 |
| 工具可用性变化让顶层 schema 抖动 | 高 | 高 | catalog 冻结、排序；可用性只追加哨兵 |
| 装配层预算超限 | 高 | 中 | 新逻辑优先下沉 owner 包，assembly 只接线 |

## 建议路径

按 M2 → M3 → M4 → M5 串行推进，再收口节点尾部帧和续跑持久化两个旁支。每个里程碑先加入可失败的字节/并发/冷加载用例，再实现；每个里程碑完成后运行目标测试和架构预算，最后统一跑仓库验证。
