# 会话薄封装任务派发与打点表（Task Dispatch & Checkpoints）

> 派发依据: [module-map.md](./module-map.md)（前置/验收）、[test-cases.md](./test-cases.md)（四维度）、
>       [mbd-activities.md](./mbd-activities.md)（活动图）、[mbd-models.md](./mbd-models.md)（不变量）
> 打点状态: ⬜待派发 → 🟨进行中 → 🟩完成 | 🟥受阻（写明原因）

## 0. 派发流程（给子代理/Agent 的通用要求）

1. 先读本任务「前置阅读」列出的文件，再读对应模块的 module-map 前置文件。
2. 严格遵守文件白名单：只改列出的文件；不自动 commit；保留仓库已有改动。
3. 测试先行：新增不变量先写红，实现后转绿即验收。
4. 中文注释、UTF-8；危险操作（clean/删除/移动）先读 `MEMORY.md` 并预警。
5. 完成跑「验收」列命令，把结果回填到打点表（状态 + 完成日期）。

## 1. 任务打点表

| 任务 | 阶段 | 模块 | 提示词 | 依赖 | 状态 | 验收 | 完成 |
|---|---|---|---|---|---|---|---|
| TSK-9.1.1 | 9.1 | session/ + application/core | §2.1 | — | 🟩 | T2.1/T2.3/T2.4、B1/B2、R2、全量绿 | 2026-09-01 接口契约 + 骨架；go test ./... -p 1 全绿；go vet ./... 绿 |
| TSK-9.1.2 | 9.1 | application/core（装配） | §2.2 | 9.1.1 | 🟩 | 编译断言 + `go vet ./...` + 全量绿 | 2026-09-01 T2.4 编译断言（core 无容器字段）+ 事件指纹回归；go vet 绿；go test ./... -p 1 全绿 |
| TSK-9.2.1 | 9.2 | application/core + seelebridge | §2.3 | 9.1.2 | 🟩 | UC6 结构断言、事件指纹、core/seelebridge 全量 | 2026-09-01 UC6 委托断言（单次提交）+ runChat 委托边界/hooks 说明；core/seelebridge 全绿 + node 165 绿 |
| TSK-9.2.2 | 9.2 | seelebridge/internal/telemetry | §2.4 | 9.2.1 | 🟩 | T1.1/1.2/1.4、T5.4、`-race` | 2026-09-01 SessionTagHook + SessionTracer（INV-T1/T2）；T1.1-1.4/T5.4 测试；seelebridge 全量 + -race 绿 |
| TSK-9.2.3 | 9.2 | gui/ 前端 | §2.5 | 9.2.1 | 🟩 | T3.7/T3.8、node 全量、GUI build | 2026-09-01 trace-view.js（T_i 会话投影）+ consistency 测试（T3.7/T3.8/T3.9）；node 173 全绿 + gui 测试 + GUI tag 构建绿 |
| TSK-9.3.1 | 9.3 | sessionstore + session | §2.6 | 9.1.1 | 🟩 | T2.6/B6、store/session 全量 | 2026-09-01 SessionGranularStore 五片 + 项目索引 + StorePort 适配器；T2.6/B6 双后端绿；全量绿 |
| TSK-9.3.2 | 9.3 | session_runtime/archive | §2.7 | 9.3.1 | 🟩 | 旧口废弃、迁移测试 | 2026-09-01 ScopedSessionPort 标记 Deprecated + SessionGranularPort 迁移目标 + 迁移测试（旧数据同键可读）；全量绿 |
| TSK-9.4.1 | 9.4 | seelebridge/plan + core | §2.8 | 9.2.1 | 🟩 | UC7、fork 既有测试全绿 | 2026-09-01 NewSubagentSessionWithID（独立子代理会话）+ registry Session/Count 查询 + UC7/UC8 测试；seelebridge + core 全绿 |
| TSK-9.4.2 | 9.4 | application/core + session | §2.9 | 9.3.1 | 🟩 | T2.7/B4、UC5 | 2026-09-01 session/deepcopy 工具 + fork 深拷贝接线 + T2.7/B4/UC5 测试；全量绿 |
| TSK-9.5.1 | 9.5 | 全仓 | §2.10 | 全部 | 🟩 | T5.5、清理断言 | 2026-09-01 删除自造状态机、ScopedSessionPort、Manager 存储桥、workspace 粒度旧口、平行 Unit/ChatRuntime（聊天运行态收进 SessionUnit，core 直接接入会话域）；V 镜像一致性修复；session/core -race 绿 |
| TSK-9.5.2 | 9.5 | application/core + seelebridge | §2.11 | 9.5.1 | 🟩 | R5、P4 | 2026-09-01 并发 Shutdown R5/P4 测试（core + seelebridge）；-race 全绿 |
| TSK-9.5.3 | 9.5 | 文档 + 冒烟 | §2.12 | 9.5.2 | 🟨 | 冒烟 + 文档同步 | 2026-09-01 Stage/Smoke 通过（tmp/smoke/smoke-seelex-gui-20260901-183751.log）；文档状态已同步；Deploy 涉及 dist 基线二进制，按 MEMORY.md 铁律待用户确认 |

## 2. 提示词清单（可直接复制派发）

### 2.1 TSK-9.1.1 session/ 端口契约与 SessionUnit 骨架

```text
你负责 Seelex「会话薄封装」9.1 契约层。
目标：在 session/ 定义 EnginePort/StorePort 端口与 SessionUnit 骨架，
      生命周期 status 由 HasSession 驱动（不重造 COLD/PREPARED/LIVE）。
前置阅读：
  docs/2026-09-01-session-thin-wrapper/thin-wrapper-session-design.md
  docs/2026-09-01-session-thin-wrapper/mbd-models.md §2（M2）
  session/domain.go、session/manager.go
  seelebridge/runtime.go（bundleFor/NewMainSession/ChatStreamFor/UnloadSession）
文件白名单：session/ 下新增/修改（ports.go、domain.go、新测试文件）；
          不得改 seelebridge 与 application/core 的实现文件。
实现要求：
  1) EnginePort：HasSession / NewMainSessionWithID / NewSubagentSessionWithID /
     ChatStreamFor / UnloadSession / PrepareMainSessionHistory（接口先行）。
  2) StorePort：SaveSession/LoadSession/History/Transcript/ToolResults/Context/
     SessionsOf/Bind（会话粒度键）。
  3) SessionUnit：S_i=(id,K,parent,E,V,Q,C,B,status)；薄状态机由 HasSession 驱动。
  4) 测试先行写红：T2.1 元组契约、T2.3 非法迁移、T2.4 编译断言（core 无容器字段）、
     B1 空/nil、B2 非法生命周期、R2 锁序。
验收：go test ./... -p 1 && go vet ./...
交付：列出新增接口签名、状态机规则、红→绿测试结果。
```

### 2.2 TSK-9.1.2 依赖方切换（装配接线）

```text
你负责把 application/core 对 session 的依赖切到 9.1 端口。
前置阅读：module-map.md §1/§3；service_assembler.go；session_scope/history/lifecycle.go。
文件白名单：application/core 的装配与 session 访问点（assembler、session_*）、
          application/contract/ports.go（如需要新端口别名）。
实现要求：core 不直接持有会话容器（编译断言）；会话写/读经 session 端口；
          行为不变（事件指纹一致）。
验收：go vet ./...；go test ./application/core ./session -count=1；事件指纹回归。
交付：列出每个旧调用点 → 新端口方法的映射表。
```

### 2.3 TSK-9.2.1 runChat 委托 Seele loop

```text
你负责把 application/core 的自编 ReAct 编排委托给 Seele loop（hooks 投影）。
前置阅读：mbd-activities.md AD-1；chat.go（runChat/appendDelta）；
          seelebridge 的 LoopHooks 装配；module-map.md §3。
文件白名单：application/core/chat.go、service_input.go、service_snapshot.go、
          见 module-map §3 前置列表；seelebridge 仅当确认需要时经主代理同意。
实现要求：
  1) 提交 → session.Engine.ChatStreamFor；seelex 只经 LoopHooks 投影 ΔV/事件。
  2) 删除平行循环/预算/transcript 编排；UC6 结构断言（core 无自编循环）。
  3) 事件指纹回归：相同输入序列 → 相同事件序列。
  4) 后台流式增量仍不取全局写锁（R4 保持绿）。
验收：go test ./application/core ./seelebridge -count=1；node --test 全量。
交付：说明 loop 委托边界、hooks 清单、事件指纹方法。
```

### 2.4 TSK-9.2.2 trace 会话隔离与 hook 链

```text
你负责 trace 按会话隔离与 hook 链序验证（M1）。
前置阅读：mbd-models.md §1；seelebridge/internal/telemetry/*；
          seelebridge/runtime.go（Telemetry 装配）。
文件白名单：seelebridge/internal/telemetry、seelebridge/runtime.go（trace 相关）、
           seelebridge 测试。
实现要求：
  1) T_i 按会话隔离（INV-T1）；查询带会话过滤。
  2) 链序断言：H=h_n∘…∘h_1 依序透传（T1.2）；Stream 结果返回模型（T1.3）。
  3) 测试先行写红：T1.1/T1.2/T1.4/T5.4。
验收：go test ./seelebridge -count=1；go test -race ./seelebridge。
交付：trace 数据结构、过滤实现、链序测试结果。
```

### 2.5 TSK-9.2.3 前端双投影一致性与 trace 视图

```text
你负责对话/轨迹双投影一致性（INV-M3-3）与按会话的 trace 视图。
前置阅读：mbd-models.md §3；components.js/trajectory.js/protocol.js/client-state.js/app.js；
          bridge.go。
文件白名单：gui/frontend/dist/*（前端）、gui/bridge.go。
实现要求：
  1) 同一 tool 在对话 chip 与轨迹行 status/duration/size 一致；chip↔轨迹同 key 跳转命中。
  2) 空态/超限（T3.8）：空会话、超长消息、超多工具不溢出。
  3) trace 视图按会话投影（数据源 T_i，非 Π_traj）。
  4) 测试先行写红：T3.7/T3.8。
验收：node --test gui/frontend/dist/*.test.mjs；go test ./gui -count=1；
      go build -tags "gui,desktop,production" ./...
交付：双投影一致性规则、trace 视图数据流、测试结果。
```

### 2.6 TSK-9.3.1 StorePort 会话粒度实现

```text
你负责把持久化实现为会话粒度（原子单位 = session）。
前置阅读：mbd-models.md §2.3；module-map.md §4；
          sessionstore/router.go、durable_history.go、sessionstore.go；workspace/。
文件白名单：sessionstore/、session/（StorePort 实现）、session_runtime/archive.go。
实现要求：
  1) session:<id> 五片（record/history/transcript/toolresults/context）+ 项目索引。
  2) 读写幂等（B6）；subagent 与 main 同构落盘。
  3) 测试先行写红：T2.6/B6。
验收：go test ./sessionstore ./session -count=1。
交付：键设计、迁移影响、幂等测试结果。
```

### 2.7 TSK-9.3.2 旧 workspace 粒度口废弃

```text
你负责废弃 workspace 粒度旧口（LoadHistoryByWorkspace 等），迁移到会话粒度。
前置阅读：module-map.md §4；session_runtime/archive.go、coordinator.go。
文件白名单：application/core/session_runtime/、调用旧口的测试。
实现要求：旧口标记废弃并迁移调用方；迁移测试覆盖旧数据读取。
验收：go test ./application/core ./sessionstore -count=1。
交付：废弃清单 + 迁移映射。
```

### 2.8 TSK-9.4.1 subagent 独立会话

```text
你负责 plan 节点 = 独立 SubagentSession（own loop/视图/历史）。
前置阅读：mbd-activities.md AD-4；module-map.md §8；
          seelebridge/plan/executor.go、runtime_bundle.go、subagent sessions。
文件白名单：seelebridge/plan、seelebridge（bundle 相关）、application/core（subagent 适配）。
实现要求：UC7（节点创建独立 SessionUnit）；merge-back 经会话端口（UC8）；
          既有 fork/subagent 测试全绿。
验收：go test ./seelebridge ./application/core -count=1。
交付：子代理会话创建/回收/merge-back 说明 + 测试结果。
```

### 2.9 TSK-9.4.2 fork 会话粒度深拷贝

```text
你负责 fork = 会话粒度深拷贝（record 前缀 + 上下文栈 → 新会话）。
前置阅读：mbd-activities.md AD-9；module-map.md §8；session_fork.go。
文件白名单：application/core/session_fork.go、session/（深拷贝工具）、相关测试。
实现要求：T2.7/B4 引用不相交；UC5；运行中拒绝 fork。
验收：go test ./application/core ./session -count=1。
交付：拷贝面定义、隔离测试结果。
```

### 2.10 TSK-9.5.1 清理平行世界

```text
你负责删除自造状态机/平行 ChatRuntime/workspace 粒度口。
前置阅读：module-map.md 全部；thin-wrapper-session-design.md §6。
文件白名单：按 module-map 各模块（仅清理类改动）。
实现要求：T5.5（G 之外无共享可变状态）静态断言；删除死代码；全量绿。
验收：go test ./... -p 1；go vet ./...。
交付：删除清单（文件/符号）、静态断言结果。
```

### 2.11 TSK-9.5.2 并发 Shutdown 与资源有界

```text
你负责运行中多会话并发 Shutdown 可终止、无泄漏。
前置阅读：mbd-activities.md AD-8；module-map.md §3/§2。
文件白名单：application/core、seelebridge（关闭路径）。
实现要求：R5（并发 Shutdown 无死锁/泄漏/panic）；P4（goroutine/堆有界）。
验收：go test -race ./application/core ./seelebridge -count=1。
交付：关闭顺序、资源统计、测试结果。
```

### 2.12 TSK-9.5.3 文档同步与冒烟

```text
你负责收尾：文档同步（thin-wrapper/module-map/test-cases 状态）与产物冒烟。
前置阅读：scripts/seelex-flow.ps1、Makefile。
文件白名单：docs/2026-09-01-session-thin-wrapper/、scripts/（如需）。
实现要求：冒烟 stage→smoke→deploy→smoke；GUI tag 构建；前端 node 全量。
验收：seelex-flow 冒烟报告 + go build -tags "gui,desktop,production" ./...
交付：冒烟报告路径 + 文档同步 diff 摘要。
```

## 3. 派发纪律

- 一个任务一个子代理/一次会话；串行依赖按打点表「依赖」列排队。
- 任务交付时回填打点表：状态 + 完成日期 + 验收命令输出摘要。
- 受阻（红测试无法转绿/外部依赖）→ 打 🟥 并写明原因，不静默跳过。
- 每个阶段完成后跑 module-map.md 汇总表中的阶段验收命令，绿才进入下一阶段。
