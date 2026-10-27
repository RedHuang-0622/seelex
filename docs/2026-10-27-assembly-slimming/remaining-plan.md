# Assembly 剩余治理与 Store 衔接方案

日期：2026-10-27
状态：assembly 当前批次继续实施；store 拆分尚未编码

## 1. 当前事实

| 指标 | 当前值 |
|---|---:|
| `serviceComponents` 字段 | 65 |
| assembly 生产文件 | 51 |
| assembly 生产代码 | 6,891 行 |
| assembly 测试文件 | 192 |
| assembly 测试代码 | 37,382 行 |
| 旧 Application/Assembly 宽接口与前端端口集合 | 0 |
| 旧兼容包 / test alias / `frontendOfTestService` | 0 |

已经完成的是：旧宽接口和兼容层退场、turn/cold/edit-fork 的 builder-local
中间件清理、若干外部端口的调用点生命周期归位，以及 owner 测试迁包。

尚未完成的是：assembly 仍承载多段跨组件用例顺序与锁序；store 仍以
`Router` / `jsonRepository` / `storeEngine` 共享 receiver 为主。

## 2. Assembly 未完成清单

### A5.1 Conversation submission / turn

现状：

- `chat.go`、`service_input.go` 仍包含
  restoring / running / compaction 门、queue 合并、turn 启停、budget
  finalization 和 plan/task 镜像顺序。
- `Service` 目前只保留 `conversationTurnProcess` 与
  `conversationTurnRunner`，但两阶段的 admission 与 commit 仍未完全归 owner。

已完成：

- queue enqueue / revision / durable mirror 已归 `conversation.QueueApplication`；
- durable queue 回填与 recovered queue 合并已归同一 owner；
- `assembly/session_queue_recover.go` 已删除。

目标：

- 归 `application/conversation` 的显式 submission 与 turn use case。
- 锁、视图、session unit、引擎持久化和发布面由 consumption-owned narrow
  ports 注入。
- 保持 restoring 延后、compaction 延后、queue FIFO、revision 发布和
  shutdown 顺序不变。

退出条件：

- `chat.go` / `service_input.go` 不再包含跨组件判据，只保留 host adapters。
- 同一提交路径不再同时存在 assembly 与 owner 两份编排。

### A5.2 Session transition / scope

现状：

- `session_scope.go`、`session_lifecycle.go`、`session_pending_tail.go` 仍负责
  view transition、project root、engine 切换、恢复等待、resident/live scope
  和发布顺序。
- 这些规则共享 `ViewMu`、session unit 和 engine bundle，不能按文件机械搬迁。

目标：

- 在 `application/session` 下形成明确的 session transition use case 与
  Unit-of-Work。
- 将锁、视图写面、catalog refresh、engine attach/detach 定义为窄 Host/
  Store ports。
- `hostAttach`、cold resume、draft materialization、workspace switch 共用同一
  个当前会话切换不变式。

退出条件：

- project/session scope、revision/seq、关闭顺序由 transition owner 明确持有。
- assembly 只装配 transition，不判断何时切换或恢复。

### A5.3 Draft / permission / interaction

现状：

- Draft Entry/Materialization 已归 owner 应用对象，但 `session_draft.go`、
  `session_permission_tier.go`、`service_interaction.go` 仍保留 host 规则。
- 权限档位的持久化、session unit、approval 和 runtime 写入顺序仍在 assembly。

目标：

- draft、permission tier 和 approval interaction 各自使用消费方窄端口。
- 继续保持“写持久化成功后才改 unit/runtime/approval”的现状失败语义。

### A5.4 Execution / projection / scheduler

现状：

- `tool_hooks.go`、`reference_tools.go`、`snapshot_budget.go`、
  `plan_tools.go`、`project_board_read.go` 仍包含执行投影组合。
- `service_scheduler.go` 只保留 workspace lookup 与 Engine/knobs host 适配。

已完成：

- scheduled workspace / plugin catalog 判据归 `application/admin/scheduler`；
- scheduled run 参数默认与写入顺序归 scheduler RunApplication。

目标：

- tool hook、reference 和 snapshot projection 归
  `application/execution/toolhooks`、`application/reference`、
  `application/session/viewstate` 等现有 owner。
- scheduler 的工作区/插件校验归 `application/admin/scheduler`。
- assembly 只提供锁内快照、发布端口和真实依赖，不复制 owner 判据。

### A6 Composition root 收口

剩余 `service_assembler*.go` 中仍有一些 callback 同时完成“取值 + 业务判断 +
副作用”。目标是只保留三类内容：

1. 具体依赖适配；
2. owner 构造；
3. 生命周期和注入。

以下内容不得继续留在 composition root：

- `if` 形态的业务状态判定；
- optional type assertion 后的回退策略；
- 跨多个 owner 的提交顺序；
- 为测试或调用方便新增的转发 getter/facade。

### A7 测试与预算

已完成：

- turn、edit-fork、cold-stage、draft、fork-lineage 的 owner 测试迁包；
- policy owner 行为测试迁 `application/policy` 的 `policy_test`；
- 22 个分散的 `...IsAssembled` 反射断言合并为单一 owner graph 门禁；
- application 根重复 boundary 测试合并；
- AB/benchmark 进入 `extended` build tag；
- assembly 生产文件/行、组件字段、测试文件/行预算写入
  `application/testdata/application_budget.json`。

剩余：

- 将 scheduler、policy、review、completion 等 owner 算法测试迁 owner 包。
- 将 `context_cache_divergence_probe_test.go` 的共享 harness 与两个纯诊断用例
  分离，使纯 probe 可由 build tag 排除，同时保留默认 prefix invariant 用例。
- 每次测试减少后只下调预算，不允许净零换名。

## 3. 依赖与接口规则

- 接口由消费方定义，单个接口不超过 20 个方法。
- `Components` 只保存 owner 指针；禁止闭包、业务判据和锁。
- owner 包不得 import `application/assembly`。
- 跨聚合事务放在 application use case / Unit-of-Work，不放进 store。
- assembly 的共享协调器（view/sessions/tasks/residency/workTable/history runtime）
  只有在其锁、缓存和后台任务已有 owner 后才允许迁移。
- 先写红测和结构预算，再迁移实现；每批给出旧位置 → 新唯一位置的删除清单。

## 4. Store 拆分方案

事实源不再复制，直接执行
[`docs/2026-10-27-store-ddd-split/plan.md`](../2026-10-27-store-ddd-split/plan.md)、
[`milestones.md`](../2026-10-27-store-ddd-split/milestones.md) 与
[`external-change-protocol.md`](../2026-10-27-store-ddd-split/external-change-protocol.md)。

当前状态：

- 方案已确认，M0–M11 **尚未编码**。
- 当前 `store` 约 50 个生产文件、17,646 行。
- 方案基线记录 `storeEngine` 183 个生产方法、`Router` 130 个、
  `jsonRepository` 90 个；这三个类型必须只收缩。

### 4.1 目标边界

```text
frontend adapter
  → application route / use case
  → owner operation
  → consumer-owned narrow port
  → store/<bounded-context>
  → store/internal/layout
  → store/root
```

`store/root` 只保留数据根、锁、共享读和原子写机制。
`store/internal/layout` 保留会话路径、head 信封和模块锁。

领域包：

- `store/session`
- `store/context`
- `store/artifact`
- `store/project`
- `store/board`
- `store/teamwork`
- `store/role`
- `store/workunit`

### 4.2 固定实施顺序

| 里程碑 | 目标 | 关键验收 |
|---|---|---|
| M0 | 基线门禁 | 物理布局 golden、Commit 写序/半失败、锁身份、方法 ratchet、import 边界 |
| M1 | Board | 根 Board 实现变薄转发；物理文件、水位、折叠、压实逐字兼容 |
| M2 | Layout 内核 | 路径/head/模块锁下沉；新领域不再改中央锁枚举 |
| M3 | Artifact | blob/media/search 独立；引用与 GC 语义不变 |
| M4 | Commit / Turn UoW | 拆 Session/Artifact/Compaction 提交，application 保持原写序 |
| M5 | Session 核心 | message/event/head 与 transcript 读面迁 owner |
| M6 | Session 状态机 | draft/queue/retention/compact/fork 归 Session 子模块 |
| M7 | Context | prompt、四栈、checkpoint、wire 装配归 Context/engine |
| M8 | Teamwork | plan/event/binding/registry 迁 owner |
| M9 | Role / WorkUnit | 消除 `roleStore` 与同型子引擎递归 |
| M10 | Facade 清零 | 删除能力发现口和 Router/Repository/SessionGranularStore 泛化面 |
| M11 | 发布验收 | 全量 build/vet/test、冷读、恢复、媒体、关闭和回滚演练 |

### 4.3 Store 实施纪律

1. M0 未完成前不得进入 M1。
2. 每个里程碑独立 worktree、branch、checkpoint 和回滚演练。
3. 修改外部消费方必须使用 `external-change-protocol.md` 的清单和批处理脚本。
4. 不改变 JSON 路径、schema、head 水位、锁粒度和用户数据。
5. 根包迁移期只允许同身份 alias 和一层薄转发，并登记删除条件。
6. 领域包禁止 import 根 `store`、application、middle、backend；领域包之间不直连。
7. 跨 transcript/artifact/compaction 的写序归 application Unit-of-Work。

## 5. 推荐执行顺序

1. 完成当前 assembly 分支的 A5/A6/A7 剩余小批次，做 A/B 与全量验证并合并 main。
2. 从合并后的 main 创建专用 `codex/store-ddd-M0` worktree，只做 store 基线与
   ratchet；不改业务实现，不进入 M1。
3. 根据 assembly 与 store 的冲突面决定 M4/M7 的外部批次顺序：先完成 store
   M0–M3，再开始会触及 turn commit 和 context wire 的 application 迁移。
4. 每个批次停止在可独立回滚的边界，不用新增兼容 facade 换取“暂时可编译”。
