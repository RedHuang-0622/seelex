# Assembly 测试迁移 Goal

日期：2026-10-28
状态：已完成

## 目标

最终让 `application/assembly/` 根目录只保留生产代码；所有测试按归属迁到：

1. 领域 owner 包（`application/*`、`kernel/*`）；
2. 公开 API 包 `application/assemblytest`；
3. AST/结构门禁包 `application/assembly/architecture`；
4. 无法用公开 API 表达的跨 owner 集成测试，进入独立的 assembly integration
   test package，并通过窄 test harness 访问宿主。

不得为了迁移测试而导出 `Service.components`、内部锁或仅供测试使用的生产 facade。

## 基线

| 指标 | 迁移前 |
|---|---:|
| assembly 全部测试文件 | 192 |
| assembly 全部测试行 | 37,358 |
| assembly 默认测试文件 | 186 |
| assembly 默认测试行 | 35,189 |

## 批次

### M1 已完成：可直接归 owner 的测试

- `public_api_test.go` → `application/assemblytest`；
- 3 个 AST 门禁 → `application/assembly/architecture`；
- 5 个 Session 组件测试 → `application/session`；
- worktable status/fuzz/A/B → `kernel/worktable`；
- session switch A/B → `application/session/runtime`。

验证：相关目标包全绿；assembly 预算下降。

### M1b 已完成：Integration harness

- 建立 `application/assembly/integration`，将 fake ports 与 Service 构造 helper
  放在测试包内；
- 测试只通过 `ComponentsFor(service)` 获取 owner，不访问 `service.components`；
- 已迁 account/account-window/history-search/notice/toolhook/任务 Skill/会话快照
  共 7 个测试文件；
- 续迁消息派号作用域集成测试，测试包自持 fake Engine，通过
  `ComponentsFor` 访问 Submission、Snapshot、DraftEntry 与 Lifecycle；
- 续迁角色工具活动事件与可见角色归属测试，测试通过 `ComponentsFor` 访问
  RoleActivity、View 与 Submission owner；
- 续迁 Plugin 来源投影测试；真实 loader/catalog 夹具随 integration 测试包自持；
- 续迁回合状态单词一致性测试；`TurnStatusOfRecord` 直接依赖 `kernel/task` owner；
- 当前 assembly：默认测试 149 files / 31,393 lines；全部测试 153 files /
  33,169 lines。

### M2：Conversation / Context owner

- 将 request、turn、queue、history、context compaction 的判据测试迁 owner；
- 保留真实引擎流、恢复门、恢复失败回滚等集成用例，后续进入 integration package；
- 先搬纯 owner 用例，再处理共享 `prefixProbeHarness`。

### M3：Execution / Tool owner

- tool hooks 诊断与投影、tool records、subagent/tree、project board、worktable
  剩余纯判据测试迁 owner；
- 保留真实装配锁序与跨 owner 事件顺序测试。

### M4：Session / Workspace / Policy owner

- archive/delete/release、draft、permission、effort、workspace file/tree/git
  的纯应用用例迁 owner；
- 需要真实 Service 的边界测试按测试语义拆分，不整体搬迁。

### M5：Integration package

- 建立 assembly integration test package；
- 用 `assembly.ComponentsFor`、公开 Dependencies 和窄 fake ports 组成测试 harness；
- 仅对无法公开表达的锁内顺序，提供测试专用适配，不导出生产状态；
- 迁完后删除 assembly 根目录所有 `_test.go`。

## 每批纪律

1. 先列出旧文件、目标 owner、是否依赖 `Service.components`；
2. 行为用例先红或保留迁移前失败证据；结构迁移使用 package/budget 门禁；
3. 每批至少迁移 20 个测试文件，按领域模块分组，停止在可独立回滚的边界；
4. 运行目标 owner 测试 + assembly 受影响测试；
5. 更新 deletion-list、owner README、默认/all-tests ratchet；
6. 不 commit/push，不动其他并行会话的 store 改动。

## 里程碑

### B1：首批 80 文件并行迁移

- Root：20 个跨模块 smoke/fault/agent-team 用例已完成，旧位置结构门禁转绿；
- Session 子批：20 个 session history/catalog/draft/scope/status 用例；
- Context 子批：20 个 context/compaction/prefix 用例；
- Execution 子批：20 个 execution/tool/subagent/chat/board 用例。

四个文件集互斥；共享预算、删除台账和 README 由根代理在冻结后统一更新。

Root 子批与 Context/Compaction 25 文件子批已验收；后者把
`legacyTestService` 作为 integration 测试包私有适配器，仅映射公开 owner 指针。
Service/Session 22 文件子批已验收，补齐 archive、session pagination 等共享
fixture，并由 integration 包内 `service_batch_helpers_test.go` 自持异步、
LRU、blocking engine 等测试依赖。
当前 assembly：默认测试 82 files / 19,124 lines；全部测试 85 files /
20,201 lines。其余子批因当前会话的子代理调度未收到任务，改由根代理按相同的
20 文件边界继续执行。

剩余 85 个根测试已按 Session 45 / Execution-Residual 40 拆成两个互斥
worktree 任务；可直接下发的提示词、文件集和最终归零命令见
[`qoder-handoff.md`](qoder-handoff.md)。

### 最终归零批：已完成

- 剩余 19 个根测试文件全部迁出；
- 17 个行为测试进入 `application/assembly/integration`；
- `session_decoupling_test.go` 进入 `application/assembly/architecture`；
- 根 `service_fakes_test.go`、`service_helpers_test.go` 与 integration 包内副本
  去重后删除；
- `application/assembly/*_test.go`：0 files / 0 lines；
- 全部递归测试仍保留在 owner、architecture、integration 与 assemblytest；
- 预算收紧为 production 49 files / 6467 lines、根测试 0 files / 0 lines。

最终验证：

```text
go test ./application/assembly/integration -count=1 -timeout=180s
go test ./application/assembly/architecture -count=1 -timeout=120s
go test ./application -run '^TestApplicationArchitectureBudget$' -count=1 -v
```

### B2-B4：剩余文件

- 每个后续批次仍至少 20 个文件；
- 优先按 owner 包迁移，Integration 只接收跨 owner 用例；
- 依赖 helper/fixture 时，把 helper 作为本批文件一起迁入测试包，或改写成测试包私有
  fake；不得导出生产内部；
- 最后一批必须是根目录 `*_test.go` 数量归零的归零批次。

## 完成标准

- `application/assembly/*_test.go` 数量为 0；
- 所有测试仍有实际执行路径；
- 默认测试和 all-tests 预算只下降；
- 无生产内部导出、无反射/unsafe 写生产字段；
- README 索引和迁移台账可追踪每批来源与目标。
