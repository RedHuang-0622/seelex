# Store 领域驱动拆分 · 进度台账

本文件是 M0–M11 的唯一进度台账，每完成一个里程碑追加一节并刷新顶部状态表。
方案事实源仍是 [`plan.md`](plan.md) 与 [`milestones.md`](milestones.md)；
本文件只记录**已跑出来的证据**，不重复设计论证。

快照时间：2026-10-27（M0 完成）。

## 1. 状态总览

| 里程碑 | 能力面 | 状态 | 门禁证据 |
|---|---|---|---|
| M0 | 基线门禁 | 已完成 | 见 §2 |
| M1 | Board 试点 | 未开始 | — |
| M2 | layout 内核 | 未开始 | — |
| M3 | Artifact | 未开始 | — |
| M4 | Commit/Turn UoW | 未开始 | — |
| M5 | Session 核心 | 未开始 | — |
| M6 | Session 状态机 | 未开始 | — |
| M7 | Context | 未开始 | — |
| M8 | Teamwork | 未开始 | — |
| M9 | Role/WorkUnit | 未开始 | — |
| M10 | Facade 清零 | 未开始 | — |
| M11 | 全仓验收 | 未开始 | — |

## 2. M0：基线门禁（已完成）

### 2.1 逐项交付

`milestones.md` §4 的七项交付，逐项落到可执行断言：

| # | 交付 | 落地文件 | 断言 | 结果 |
|---|---|---|---|---|
| 1 | message/event/compact/retention/lifecycle/tool-result/media 物理布局 golden | `store/layout_golden_test.go` | `TestSessionLayoutGolden` | PASS |
| 2 | `Commit` 写序：artifact → refs → message → compaction | `store/commit_write_order_test.go` | `TestCommitWritesArtifactBeforeMessage` | PASS |
| 3 | `Commit` 半失败语义 | 同上 | `TestCommitPublishesMessageBeforeCompactionFailure`、`TestCommitRetryWithExplicitSeqIsIdempotent` | PASS |
| 4 | 锁身份：同模块同锁、不同模块不同锁、未知模块显式失败 | `store/module_lock_identity_test.go`（M0 前已存在，本里程碑只登记不改写） | `TestModuleLocksAreDistinct`、`TestUnmappedModuleLockPanicsInsteadOfAliasing`、`TestMediaReadNotBlockedByHeldMessageLock` | PASS |
| 5 | `Router`/`jsonRepository`/`storeEngine` 方法面 ratchet | `store/god_type_ratchet_test.go` + `store/testdata/god_type_methods.json` | `TestGodTypeMethodSurfaceOnlyShrinks`、`TestGodTypeMethodScannerSeesAdditionsAndSkipsTests` | PASS |
| 6 | `./store/...` 全量 I6 import 门禁 | `e2e/store_boundary_test.go`（本次由人肉清单改为枚举） | `TestStoreDependencyClosureHasNoSeelexUpwardDependency`、`TestStoreTestFilesImportNothingUpward`、`TestStoreBoundaryEnumeratesEveryStorePackage` | PASS |
| 7 | 代表性会话 fixture，可从当前版本冷读 | `store/cold_read_fixture_test.go` + `store/testdata/coldread/` | `TestColdReadFixture`（9 个子测试）、`TestColdReadFixtureRegenerate` | PASS |

冷读 fixture 覆盖 9 个读路径：消息行、会话范围读（`ReadRange` 与
`ReadConversationRange` 两种语义）、工具结果、压缩记录、压缩帧、草稿与队列、
媒体、retention 水位、结构事件。fixture 由 `SEELEX_COLDREAD_REGENERATE=1`
显式重建，落盘只含 `project-<hash>/`，不含数据根锁。

### 2.2 基线测量（M0 之后所有里程碑只许与它比较）

**方法面 ratchet**（`store/testdata/god_type_methods.json`，按方法名集合精确相等判定）：

| 类型 | 方法数 |
|---|---|
| `Router` | 130 |
| `jsonRepository` | 90 |
| `storeEngine` | 183 |
| `SessionGranularStore` | 46 |

**底座向上依赖**（`go list -deps` 实测，I6 登记表当前例外）：

| 包 | 允许的 Seelex 内向上依赖 | 归零条件 |
|---|---|---|
| `store` | `kernel/event` | 事件端口下沉 `contract`（M4 前后） |
| `store/root` | 无（闭包 = 纯标准库） | 已归零 |
| `store/schedule` | `kernel/event` | 同上 |
| `store/workspace` | `internal/winhide` | 机制叶子，位在 store 之下，长期保留 |

`store/workspace → internal/winhide` 不是待清理项：`winhide` 自身闭包为零 Seelex
依赖，登记它而不是放过整个 `internal/` 前缀，是为了让后续新增的 `internal/*`
依赖照样必须显式登记。

**直接 import 根 `store` 的文件**（统计口径：`grep -rl '"github.com/RedHuang-0622/seelex/store"' --include='*.go' .`，
排除 `store/` 自身与 `tmp/reshape/*` 独立模块快照）：

- 合计 **232** 个文件：生产 **86**、测试 **146**。
- 按目录统计（生产 / 测试 / 合计）：

| 目录 | 生产 | 测试 | 合计 |
|---|---|---|---|
| `backend/engine/assemble` | 22 | 35 | 57 |
| `backend/multiagent/teamwork` | 16 | 23 | 39 |
| `backend/engine/context`（含 `seelectx/{memory,search}`） | 10 | 15 | 25 |
| `wiring/adapters` | 8 | 11 | 19 |
| `test` | 0 | 11 | 11 |
| `application/assembly` | 0 | 11 | 11 |
| `backend/multiagent/subagent` | 5 | 3 | 8 |
| `kernel/session` | 4 | 3 | 7 |
| `backend/workunit` | 3 | 4 | 7 |
| `kernel/goal` | 3 | 3 | 6 |
| `kernel/context` | 2 | 3 | 5 |
| `backend/tool/worktree` | 1 | 4 | 5 |
| `application`（根包） | 0 | 5 | 5 |
| `kernel/board` | 0 | 4 | 4 |
| `backend/tool/io`（`media`/`multimodal`） | 2 | 2 | 4 |
| `backend/multiagent/team` | 2 | 2 | 4 |
| `wiring/bootstrap` | 3 | 0 | 3 |
| `backend/engine/obs` | 2 | 1 | 3 |
| `e2e` | 0 | 3 | 3 |
| `backend/multiagent/agentsession` | 1 | 1 | 2 |
| `wiring/registry` | 1 | 0 | 1 |
| `kernel/board/boardtest` | 1 | 0 | 1 |
| `application/session` | 0 | 1 | 1 |
| `application/goal` | 0 | 1 | 1 |

生产文件的完整清单见 §4。这张表是 M10「Facade 清零」的收缩基线：
每迁移一个领域，对应包的生产文件应当从清单里消失，而不是改写 import。

### 2.3 零路径 / 零格式 / 零数据迁移

M0 只新增测试与 testdata，**没有改动任何生产代码**：

- 新增：`store/layout_golden_test.go`、`store/commit_write_order_test.go`、
  `store/god_type_ratchet_test.go`、`store/cold_read_fixture_test.go`、
  `store/testdata/god_type_methods.json`、`store/testdata/coldread/`。
- 修改：`e2e/store_boundary_test.go`（I6 从人肉清单改为枚举 + 只许收缩登记表）。
- 未触碰：`.seelex` 用户数据、`config/*`、`dist/`、任何落盘路径或 JSON 字段。

### 2.4 验收

```text
go test ./store/... ./e2e/... -count=1   # 2026-10-27 PASS（store 137.1s，e2e 56.7s）
go test ./test/ -count=1                 # 2026-10-27 PASS（102.0s）
git diff --check
```

格式检查 `gofmt -l` 对本里程碑新增测试无输出；`git diff --check` 通过。

## 3. 未决风险

1. **冷读语义必须先裁决再改**。当前 `ReadRange(ctx, key, offset, limit)` 的
   `limit <= 0` 是「只取总数」而不是「取全部」；provider 消息只来自**完整事件单元**
   （孤儿 tool 行不进 provider 上下文）；`ReadConversationRange` 是派生视图
   （assistant 的每个 tool_call 与 tool 行各展开一条）。M4 把写序提升为
   Application `CommitTurn` 时，这三条属于**兼容要求**，不允许用「新实现更合理」直接改。
2. **I6 还有 3 条例外**。`store` 与 `store/schedule` 的 `kernel/event` 边要等事件端口
   下沉 `contract` 才能归零；在那之前 `max: 1` 的 ratchet 不允许加第二条。
3. **新领域包默认零允许**。`store/<domain>` 一旦出现在 `go list ./store/...`，
   就自动受 I6 约束且没有例外额度；需要例外的包必须在
   `storeBoundaryRegisteredScopes` 里登记并说明归零条件，否则视为越界。
4. **`tmp/reshape/{r5check,r6verify,r7verify,verify-wt}` 是历史快照副本**（各自带
   `go.mod`，不参与 `go list ./...`，其中 2145 个 `.go` 文件同样 import 根 `store`）。
   它们会让 grep 类统计严重失真，但属于工作区数据，未经用户确认不删除；
   统计时一律排除。
5. **并发会话正在改同一批目录**。`application/*`、`backend/engine/context/seelectx/*`、
   `store/README-session.md` 等有另一会话的未提交改动，本里程碑不触碰这些文件。

## 4. 附录：直接 import 根 `store` 的生产文件（86）

```text
backend/engine/assemble/runtime.go
backend/engine/assemble/runtime_agent_conversation.go
backend/engine/assemble/runtime_context.go
backend/engine/assemble/runtime_deps.go
backend/engine/assemble/runtime_project_board.go
backend/engine/assemble/runtime_role_plugins.go
backend/engine/assemble/runtime_session.go
backend/engine/assemble/runtime_subagent.go
backend/engine/assemble/runtime_subagent_recovery.go
backend/engine/assemble/runtime_subagent_resume.go
backend/engine/assemble/runtime_teammate_inspection.go
backend/engine/assemble/runtime_teamwork.go
backend/engine/assemble/runtime_teamwork_board.go
backend/engine/assemble/runtime_teamwork_board_archive.go
backend/engine/assemble/runtime_teamwork_board_close.go
backend/engine/assemble/runtime_teamwork_context.go
backend/engine/assemble/runtime_teamwork_curated.go
backend/engine/assemble/runtime_teamwork_scene.go
backend/engine/assemble/runtime_teamwork_scope.go
backend/engine/assemble/runtime_unit_recovery.go
backend/engine/assemble/workunit_assembly.go
backend/engine/assemble/workunit_team_records.go
backend/engine/context/seelectx/assembler.go
backend/engine/context/seelectx/controller.go
backend/engine/context/seelectx/dag.go
backend/engine/context/seelectx/frame.go
backend/engine/context/seelectx/gap.go
backend/engine/context/seelectx/memory/memory.go
backend/engine/context/seelectx/replay.go
backend/engine/context/seelectx/replay_material.go
backend/engine/context/seelectx/router_storage_conformance.go
backend/engine/context/seelectx/search/search.go
backend/engine/obs/event_correlation.go
backend/engine/obs/unified_events.go
backend/multiagent/agentsession/factory.go
backend/multiagent/subagent/conclusion.go
backend/multiagent/subagent/resume_units.go
backend/multiagent/subagent/session_factory.go
backend/multiagent/subagent/subagent_sessions.go
backend/multiagent/subagent/subagent_tree.go
backend/multiagent/team/plugin_assembly.go
backend/multiagent/team/plugin_reject.go
backend/multiagent/teamwork/board_archive.go
backend/multiagent/teamwork/board_view.go
backend/multiagent/teamwork/context_view.go
backend/multiagent/teamwork/coordinator.go
backend/multiagent/teamwork/items.go
backend/multiagent/teamwork/items_gate.go
backend/multiagent/teamwork/milestone_lifecycle.go
backend/multiagent/teamwork/output_store.go
backend/multiagent/teamwork/plan_query.go
backend/multiagent/teamwork/planlock.go
backend/multiagent/teamwork/scene.go
backend/multiagent/teamwork/store.go
backend/multiagent/teamwork/teamwork.go
backend/multiagent/teamwork/tool_requests.go
backend/multiagent/teamwork/unit_record.go
backend/multiagent/teamwork/unit_recovery.go
backend/tool/io/media/media.go
backend/tool/io/multimodal/content.go
backend/tool/worktree/worktree_manager.go
backend/workunit/progress.go
backend/workunit/recovery.go
backend/workunit/session.go
kernel/board/boardtest/store.go
kernel/context/compaction_stack.go
kernel/context/wiring.go
kernel/goal/audit.go
kernel/goal/board_archive.go
kernel/goal/sessionstore_store.go
kernel/session/archive.go
kernel/session/fork.go
kernel/session/ports.go
kernel/session/transcript_range.go
wiring/adapters/compressed_turn_store.go
wiring/adapters/goal_coordinator.go
wiring/adapters/role_permission_port.go
wiring/adapters/runtime_narrow_ports.go
wiring/adapters/session_manager.go
wiring/adapters/session_ssh_state_port.go
wiring/adapters/session_status_record.go
wiring/adapters/session_workspace_ports.go
wiring/bootstrap/application.go
wiring/bootstrap/bootstrap.go
wiring/bootstrap/runtime.go
wiring/registry/registry.go
```

重算命令（排除独立模块快照）：

```bash
grep -rl --include='*.go' '"github.com/RedHuang-0622/seelex/store"' . \
  | sed 's#^\./##' | grep -v -e '^store/' -e '^tmp/' | sort
```

## 5. 下一步

M1：Board 垂直试点，按 `milestones.md` §5 的范围搬
`store/project_board.go`、`store/project_board_markdown.go` 与其测试，
打通「`contract/dto` Board 形状 → 消费方窄端口 → `store/board` → 根包单层转发 →
删除旧实现」这条路径。M1 的第一件事是先写红灯：把
`store/testdata/god_type_methods.json` 里 Board 相关方法从 `Router` 集合中删掉
（期望 ratchet 变红），并把 `store/board` 加进 I6 的必现包清单。
