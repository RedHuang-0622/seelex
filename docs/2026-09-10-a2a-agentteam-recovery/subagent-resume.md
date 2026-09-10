# 工作流 A：subagent 中断后恢复续跑 + 通用恢复模板

## 0. 边界

- subagent = 劳务派遣式能力调用，等同一次 tool call；**不是 AgentTeam 成员**，
  不进成员表/`order_roles`/floor/role draft/sequencer。
- 本流只负责 subagent 相关链路与"终止前未完成工作"的通用恢复模板；
  AgentTeam 工厂/角色注册表归工作流 B，不要越界改 `role.*`/`team.*`。
- 权威设计：[`docs/arch/a2a-agent-team-factory.md`](../../arch/a2a-agent-team-factory.md) §5、§5.1、§9。
- 开工前必读：根 `MEMORY.md`（危险操作铁律 + 新功能归属决策）、`AGENTS.md`、
  `seelebridge/README.md`、`sessionstore/README.md`、`docs/2026-09-08-interrupted-round-truncation/README.md`。

## 1. 现状（已实现，作为起点）

| 事实 | 位置 |
|---|---|
| 中断工具链补占位（provider-only tool result） | `application/core/context_runtime/history.go` `RepairInterruptedToolChains` |
| 子代理会话记录（`Status: queued/running/done/failed`、`History`、`ContextJSON`、`StagesJSON`、worktree） | `sessionstore/node_session_store.go` `NodeSessionRecord` |
| 运行期落盘 + 结论事件回传 main（`seelex.subagent.result`），结束后删记录 | `seelebridge/runtime_subagent_recovery.go` |
| 冷恢复锚点重建（残留记录 → 详情面/树/worktree；结论事件 → 树节点） | `Runtime.RestoreSubagentAnchors` |
| 子代理 registry（Register/Unregister/Restore/persist） | `seelebridge/session/subagent_sessions.go` |
| 冷恢复调用点 | `application/core/session_lifecycle.go`、`application/core/session_history.go` |

**缺口**：冷恢复目前只"重建锚点/树"，不重建现场、不注入之前的上下文、不重新派发；
`Status=running/queued` 的残留记录只是被恢复成节点，没有续跑语义；通用恢复模板
尚未抽成独立能力。

## 2. 目标

1. 把设计稿 §5.1 的 7 步模板落成**可复用的恢复能力**（定位 → 判定 → 补历史 →
   重建现场 → `system` 注入说明 → 同键重跑 → 收敛），subagent 是第一个使用者。
2. subagent 冷恢复续跑：发现 `active`（`queued`/`running`）且无结论事件的 subagent
   → 重建它的输入上下文与执行现场 → 以 `system` 注入恢复说明（这是恢复、之前做到
   哪、已完成什么、接下来继续什么）→ 用**同一幂等键**重新派发续跑。
3. 运行中的 active 只活跃在表格（task/worktable/子代理表格）；运行期不往 main
   历史写 active/恢复说明；只有用户输入是 `user`，恢复说明与状态材料是 `system`。
4. 同一派发 ID/节点 ID 不得产生两条结论；续跑失败必须显式报错并可重试，不伪造结果。
5. headless 暴露可观测/可操作接口，并用真实 API 冒烟 + pprof + race 验证。

## 3. 实施要求

### 3.1 通用恢复能力

- 新建能力时按 `MEMORY.md`「新功能归属决策」判断落点：优先复用
  `application/core/context_runtime`（若只是历史修复），需要独立生命周期/端口时
  新开小包（例：`application/core/resume`）并配 `README.md`；禁止塞进上帝文件。
- 模板必须是**声明式步骤 + 可插拔端口**，不得把 subagent 专有逻辑写死进通用层；
  subagent 只提供"现场重建 / 上下文导出 / 重新派发"的适配器。
- 补历史与重跑是两件事：能补历史就补历史（tool 占位），能续跑才续跑；
  既不补也不续的情形必须显式失败，不许静默吞掉。

### 3.2 subagent 冷恢复续跑

- 触发点：冷恢复（`RestoreSubagentAnchors` 之后的应用层接线；以及会话打开/切换
  路径），只处理目标会话范围内的记录，禁止跨会话/跨项目误恢复。
- 判定用持久化事实（`NodeSessionRecord.Status` + 是否已有结论事件），不用内存
  active 标记；`done`/`failed` 只重建历史，不重启。
- 注入的恢复说明必须是 `system`，并且只进**被重启的 subagent 自己的上下文**；
  main 侧只出现 provider-only tool 占位与最终结果。
- 续跑要能终止（会话关闭/进程退出/shutdown 路径不泄漏 goroutine），并遵守
  `MEMORY.md` 的并发与错误语义要求。

### 3.3 表格与 wire 口径

- subagent 运行态 active：只在表格/工作台可见；不得作为 `assistant` 或 `system`
  发言写进 main 的 provider 历史。
- 若发现 subagent 相关注入仍以 `assistant`/`user` 出现，按设计 §2.1 表收口为
  `system`，并补回归测试。

### 3.4 可观测与接口

- headless RPC：至少能列出 subagent（含 `status`/`node_id`/`session_id`/是否待恢复）、
  触发一次恢复、读取最近一次恢复结果/错误（RPC 名自定但需在 `gui/README.md` 记录）。
- `gui/headless.go` 只改 subagent 相关段，不动 `role.*`/`team.*`。

## 4. 验收

必须落成测试（红→绿），不接受口头结论：

- T-A-01：残留 `Status=running` 记录 + 冷恢复 → subagent 被重启续跑，且注入的恢复
  说明是 `system`、只在该 subagent 上下文内。
- T-A-02：`Status=done/failed` → 只重建历史/树，不重启。
- T-A-03：中断链补占位幂等（同一派发 ID 冷却恢复两次结果不重复）。
- T-A-04：`main` 历史中不出现 subagent 的 active/恢复说明（无 `assistant`/`user`
  伪装）。
- T-A-05：会话关闭/Shutdown 后无残留 goroutine（可用 `goleak` 风格断言或等价）。

验证命令（最低要求）：

```text
gofmt -l .
go build ./...
go vet ./seelebridge/... ./application/... ./sessionstore ./gui
go test ./seelebridge/... ./application/... ./sessionstore ./gui -count=1
go test -race ./seelebridge ./application/core/... -run 'Subagent|Resume|Interrupt' -count=1
```

真实 API 冒烟（需用户环境凭证时按既有 `SEELEX_LIVE_SMOKE=1` 约定跑）：

- 跑一条真实链路：派发 subagent → 中途终止（模拟）→ 冷恢复 → 续跑 → 结论单一。
- 同时抓 pprof（mutex/block）与 `-race`，观察是否死锁/数据竞争，报告落
  `tmp/` 或 `docs/test/`，不得写入 `config/`。

## 5. 交付物

- 通用恢复能力（代码 + `README.md` + 测试）。
- subagent 冷恢复续跑接线（代码 + 测试 + `seelebridge/README.md` 同步）。
- headless RPC + `gui/README.md` 同步。
- 验证记录：追加到
  `docs/2026-09-08-session-storage-architecture/conformance-checklist.md`（新编号）
  并回填本目录 `README.md` 的"实现状态"。
- 不 commit、不 push；不改 `config/accounts.yaml` 与 `*.local.yaml`。

## 6. 与其他流的接口

- 若需要 AgentTeam 侧配合（角色注册、成员表字段），发消息给 team 流 agent，
  不要自己实现 `role.*`/`team.*`。
- 共享文件冲突按本目录 `README.md` 的"并行冲突纪律"处理。
