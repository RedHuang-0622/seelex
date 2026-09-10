# 会话存储下一阶段基建（2026-09-10 工作包）

本目录是一次性工作包，收纳下一阶段的**需求提示词**与后续设计/实现记录。

- 权威设计（当前）：[`docs/2026-09-08-session-storage-architecture/my_design.md`](../2026-09-08-session-storage-architecture/my_design.md)（v8.3）
- 当前符合度台账：[`conformance-checklist.md`](../2026-09-08-session-storage-architecture/conformance-checklist.md)
- 本轮提示词：[`prompt.md`](./prompt.md)
- 性能/IO 审查结论：[`review.md`](./review.md)
- A2A「全员 draft→message」评估：[`a2a-draft-eval.md`](./a2a-draft-eval.md)
- 在线文档协作模型调研：[`../../research/2026-09-10-collaborative-doc-session-model.md`](../../research/2026-09-10-collaborative-doc-session-model.md)
- 应用层业务接线提示词（R1→R4 顺序）：[`app-layer-wiring-prompt.md`](./app-layer-wiring-prompt.md)

范围：R1 多后端实现收口到接口、R2 goal techleader 会话独立存储、
R3 会话中间缓存层（cache hit + storage append-only）。本工作包只做**基建与接口**，
应用层接线留到后续阶段。

状态：提示词与权威稿已按 2026-09-10 用户裁决更新（R4 群聊/角色 draft、
同步即删、compact 以 main 为准、TL 会话仅备份冷恢复、桌游顺序、定时插话、
join_seq/compact_ref 恢复、每角色独立 draft 锁/actor、message head 的 floor
发言权记录、装配流程 cold/hot/runtime 全流程）；`my_design.md` 的 §2.0 准入门、§3 ER 图、
§3.1/§3.2、§4 术语与 I16–I19、§5.1/§5.3、§8.3、§11 已同步。
§4 不变量已扩展到 I22–I25、§5.7 为装配流程权威；提示词 §8 同步执行摘要与
T-ASM-01…06。
`prompt.md` §6.1 已定项直接执行，§6.2 仍需用户点（R1 后端枚举 A/B、
R3 缓存粒度/淘汰/先后、前端交互细节），§6.3 为 R4 默认口径。

实现状态：R1 后端退役已落；R2/R3/R4 存储侧基建已落（角色会话、role draft、
sequencer sync/幂等、floor、compact_ref/join_seq、角色 wire、MaterialCache
接口、schedule.* EVENT）；应用链路已同步 `model.TranscriptEvent` 角色字段、
`internal/adapters` 往返、`task_context` user/main 生产者角色归属与 round/unit
排序键；`gui/headless` 已暴露 `role.*`/`schedule.*` 接口，并新增真实 API
pprof/race 冒烟 `gui/role_live_probe_test.go`。验证记录见
`conformance-checklist.md` §6 S25/S26。
