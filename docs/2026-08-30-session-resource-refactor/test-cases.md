# Session 资源控制重构：测试用例

> 日期：2026-08-30
> 状态：用例规格；阶段 0/1/2 用例已实施并转绿（2026-08-31，见
> [implementation-record.md](./implementation-record.md)）；阶段 1/2 用例规划中
> 前置：[plan.md](./plan.md)（资源清单、竞争/污染源 P1–P6 / R1–R7、场景 A1–A5）、
> [design-model.md](./design-model.md)（六元组与不变量 Ⅰ–Ⅳ）
> 表达形式：每条用例给出编号、名称、Given/When/Then、断言要点、映射与实施状态

---

## 1. 测试分层与文件组织

| 层 | 文件（现有=链接，规划=代码） | 覆盖 |
|----|------------------------------|------|
| 集成复现（根包，真 harness + httptest provider） | [repro_session_disappear_test.go](../../repro_session_disappear_test.go)（增强 A1）；`repro_session_workspace_test.go`（规划，A4） | 真实链路 P1/P2/P3/P4、跨工作区 |
| 单元（application/core，fake 引擎/存储端口） | [session_scope_test.go](../../application/core/session_scope_test.go)、[session_archive_test.go](../../application/core/session_archive_test.go)（扩展）；`session_resource_isolation_test.go`（规划，不变量 Ⅰ–Ⅳ） | 单槽读源、For 变体、深拷贝 |
| 切换/拒绝契约 | [session_switch_deadlock_test.go](../../application/core/session_switch_deadlock_test.go)、[session_fork_test.go](../../application/core/session_fork_test.go)（扩展） | A3/A5 拒绝语义 |
| 竞态（-race） | [race_test.go](../../application/core/race_test.go)（扩展）；CI 上运行 | R1–R7 |
| 生命周期（阶段 2） | `session_lifecycle_test.go`（规划） | cold/hot/unload |

> 规划中的新文件：创建后补链接；未创建前一律按代码文本引用（链接目标必须存在）。

---

## 2. A1 系列：同工作区后台完成交叉写盘（P1+P2+P3 主干）

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 | 状态 |
|------|--------|---------------------|----------|------|------|
| TC-A1-01 | `TestBackgroundSessionCompletionMustNotPolluteOwner`（已有，已转绿） | Given：A 完成首轮并 fork 出 B；回 A 提交长任务（provider 第二轮阻塞）；A 运行中 `ResumeSession(B)`。When：B 完成一轮；释放 A 的 provider；切回 A。Then：A 对话含 `first A`、`long task A` 与回复。 | `containsUserText(long task A)` 为真；`containsUserText(hello B)` 为假 | P1/P2/P3；Ⅱ/Ⅲ；A1 | 已绿（3/3） |
| TC-A1-02 | `TestBackgroundCompletionPreservesARecordDomains` | Given：同 TC-A1-01。When：A 后台完成并落盘后 `LoadSessionRecordWorkspace(X, A)`。Then：record 全域只属 A。 | Title == A 标题（非 B）；PlanStack/ActivePlanID 无 B 内容；transcript 事件不含 `hello B`；Tasks 快照无 B 任务 | P2（task/plan/事件读口）、P1 身份；Ⅲ | 已绿 |
| TC-A1-03 | `TestBackgroundCompletionKeepsActiveSessionQueue` | Given：A 运行中切 B；B 排队 q1、q2。When：A 后台完成（B 仍活跃/排队）。Then：B 的 `inputQueue` 仍含 q1、q2，`Chat.Running` 不变。 | 队列内容与运行态不受 A 收尾影响 | R7；X_i 独立；A1 变体 | 已绿 |

---

## 3. A2 系列：收尾时再次切换（P4 + R3）

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 | 状态 |
|------|--------|---------------------|----------|------|------|
| TC-A2-01 | `TestBackgroundCompletionWhileSwitchingToC` | Given：A 运行中切 B；C 完成一轮后再提交（请求 #5 阻塞保持运行中）；此刻释放 A，A 收尾与 C 运行并发。Then：A 的 record 只含 A 内容；C 运行态/引擎工作历史未被清。 | A record 无 C 内容；C `Running` 不变；C 完整收尾后可见内容含 C 消息 | P4；R2/R3；A2 | 已绿 |
| TC-A2-02 | `TestBackgroundPersistConcurrentWithSwitch` | Given：同 TC-A2-01，`-race` 下并发执行。Then：无数据竞争、无串写。 | race 通过 + record 断言 | R1/R2/R3/R4 | 阶段 0 后绿（CI -race） |
| TC-A2-03 | `TestCancelChatTargetsOnlySession` | Given：A 后台运行，B 活跃。When：`CancelChat(A 的 requestID)`。Then：仅 A 取消，B 不受影响。 | A `Running=false`；B `Running` 与引擎不变 | X_i 隔离；A2 变体 | 现状可测，阶段 0 后加固 |

---

## 4. A3 系列：回看运行中会话

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 | 状态 |
|------|--------|---------------------|----------|------|------|
| TC-A3-01 | `TestResumeRunningSessionAllowsHotAttach`（决策：允许只读回看） | Given：A 运行中。When：`ResumeSession(A)`。Then：返回 nil（hot_attach）；`Snapshot.Session.ID` = A；A 运行态不变。 | 热加载回看；无死锁（有超时护栏） | A3 | 已绿 |
| TC-A3-02 | `TestHotAttachDoesNotTouchRunningSession` | Given：A 运行中，B 活跃。When：`ResumeSession(A)`（hot_attach）。Then：A 的 `Chat.Running`、引擎历史、事件流不变。 | 不变量 Ⅱ；无重放 | A3；Ⅱ | 已绿 |
| TC-A3-03 | `TestSnapshotOfRunningNonActiveUnavailable` | Given：A 运行中且非活跃。When：`SnapshotOf(A)`。Then：返回 `ErrSessionSnapshotUnavailable`。 | 现状契约 | A3 | 现状绿 |

---

## 5. A4 系列：跨工作区键漂移（R3）

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 | 状态 |
|------|--------|---------------------|----------|------|------|
| TC-A4-01 | `TestBackgroundPersistUsesSessionWorkspaceKey` | Given：A 绑定 workspace X；切到 workspace Y 的会话 B；A 后台完成。Then：A 的 record 落在 X，Y 下无 A 键。 | `LoadSessionRecordWorkspace(X, A)` 含 `long task A`；`LoadSessionRecordWorkspace(Y, A)` 不存在 | R3；P1 键漂移；A4 | 已绿 |
| TC-A4-02 | `TestBackgroundPersistDoesNotMutateWriteScope` | Given：A 后台运行，当前 `Router.Workspace()` = Y。When：A 完成落盘。Then：`Router.Workspace()` 仍 = Y。 | 持久化无副作用（不变量 Ⅲ 存储侧） | Ⅲ；R3 | 已绿 |
| TC-A4-03 | `TestNoPhantomSessionInForeignWorkspace` | Given：TC-A4-01 场景。Then：workspace Y 的 catalog 不含 A 会话条目。 | 目录刷新后无幽灵会话（含 framework DurableHistory 显式键） | A4 | 已绿 |

---

## 6. A5 系列：fork

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 | 状态 |
|------|--------|---------------------|----------|------|------|
| TC-A5-01 | `TestForkReadsParentDurableOnly` | Given：A 有已提交内容 + 在途未提交内容。When：`ForkSessionLatest(A)`。Then：子会话含父已提交前缀，不含在途内容。 | 子 record/事件/tool-results 均来自父持久化；与父内存无共享引用 | Ⅳ；A5 | 现状基本绿，阶段 0 补断言 |
| TC-A5-02 | `TestForkDoesNotShiftOtherSessionPersistKey` | Given：C 属 workspace Y 且后台运行；fork workspace X 的父会话。When：fork 后 C 完成落盘。Then：C 的 record 仍在 Y。 | fork 改全局写作用域不再影响其它会话落盘键 | R3；A5 | 已绿 |
| TC-A5-03 | `TestForkRunningParentRejected`（扩展 [session_fork_test.go](../../application/core/session_fork_test.go)） | Given：父会话运行中。When：`ForkSessionLatest(parent)`。Then：`ErrChatRunning`。 | 既有契约不回归 | A5 | 现状绿 |

---

## 7. 不变量单元断言（Ⅰ–Ⅳ，fake 端口）

| 编号 | 用例名 | Given / When / Then | 断言要点 | 映射 |
|------|--------|---------------------|----------|------|
| TC-INV-01 | `TestSessionDomainsDisjoint` | Given：构造会话 A、B 的状态（transcript/plan/engine/队列）。Then：A 与 B 的 M/X/R 对象无共享；`sessionRecordLocked(A)` 读不到 B 任何域。 | 指针不同、内容隔离；快照=B 时 A 的 record 仍只含 A | Ⅰ | 已绿 |
| TC-INV-02 | `TestViewSwitchDoesNotMutateExecution` | Given：A 后台运行。When：`ActivateSession(B)` / `SnapshotOf` / `SubscribeSession`。Then：A 的 `Chat.Running`、引擎历史、事件流逐字节不变。 | 视图操作零写入 | Ⅱ | 已绿 |
| TC-INV-03 | `TestPersistReadsOnlyOwnDomain` | Given：`Snapshot.Conversation` = B；tasks 活跃状态 = B；引擎活跃 = B。When：`PersistCurrentSession(location(A), A)`。Then：record 的 Conversation/PlanStack/Transcript/Title 均为 A。 | 污染回归的单元版（TC-A1-02 的 fast path） | Ⅲ | 已绿 |
| TC-INV-04 | `TestForkDeepCopyNoAliasing` | Given：fork 后子会话。Then：ToolResults / planStack / context 与父无共享可变引用；修改子不影响父。 | 深拷贝边界 | Ⅳ（design-model 2.1） |

---

## 8. 生命周期用例（阶段 2 实施）

| 编号 | 用例名 | 断言要点 |
|------|--------|----------|
| TC-LC-01 | `TestColdLoadAtomicVisibility` | 冷加载 PREPARED→IDLE 发布窗口内，catalog/snapshot 不暴露半成品会话 | 规划（现有 resume 冷加载路径覆盖） |
| TC-LC-02 | `TestHotAttachNoReplay` | attach 运行中会话只发基线 + 增量事件，不重放历史（对应 ACP resume / tmux attach） | 已绿 |
| TC-LC-03 | `TestUnloadReleasesScope` | `unload(i)` 后 `engines`/`sessionStates` 无 i；registry 保留 COLD 元数据；重开走 `cold_load` | 已绿 |
| TC-LC-04 | `TestPersistOwnTimelineAtomic` | 后台完成落盘与活跃会话落盘不交错；提交原子（无半写快照/事件） | 已由 TC-A1-02/A4-01 覆盖 |

---

## 9. 竞态用例（-race，CI 运行）

| 编号 | 用例名 | 覆盖槽位 |
|------|--------|----------|
| TC-R-01 | `TestWorkspaceSwitchConcurrentWithBackgroundPersist` | R3（`Router.projectID` 切换 vs 后台写）→ 断言键正确 | 已绿（-race） |
| TC-R-02 | `TestSnapshotBumpConcurrentWithRunChatTail` | R1（快照 bump / 目录刷新 vs runChat 尾部） | 已绿（-race） |
| TC-R-03 | `TestReleaseWorkingHistoryConcurrentWithChatStream` | R2（`ReleaseWorkingHistoryFor` vs `ChatStreamFor`）→ 不误清、无竞争 | 已绿（-race） |

> 说明：Windows 本地 `CGO_ENABLED=0` 时 `-race` 不可用，不得声称已执行；
> 竞态用例在 Linux CI（`-race -covermode=atomic -coverpkg=./...`）上算数。

---

## 10. 验证命令与通过标准

```text
# 阶段 0 主回归（复现 + 新场景）
go test . -run 'TestBackgroundSessionCompletionMustNotPolluteOwner|TestBackgroundCompletionWhileSwitchingToC|TestBackgroundPersistUsesSessionWorkspaceKey' -count=3

# 单元与适配层
go test ./application/core/... ./sessionstore/... ./internal/adapters/... -count=1 -timeout=120s

# 全量 + 静态检查
go test ./... -count=1 -timeout=120s
go vet ./...
gofmt -l .
git diff --check
```

通过标准：

- 复现链路 TC-A1-01/02、TC-A4-01/02、TC-A5-02 转绿；
- 不变量用例 TC-INV-01/02/03 在"快照=B"的编排下保持绿；
- `rg -n "Snapshot\.Conversation|Deps\.Engine\.History\(\)"` 仅剩阶段 1 标注的遗留点；
- 每条用例落盘前先跑一次当前基线（记录红/绿），实施后再跑同命令对比。

---

## 11. 与改动方案的映射

| 改动点（code-review-and-fix-plan.md 第 5 节） | 对应用例 |
|----------------------------------------------|----------|
| 5.1 / 5.2 For 变体接口 | TC-A1-02、TC-INV-03 |
| 5.3 显式键 + 会话读源 | TC-A1-01/02、TC-A4-01/02/03、TC-A5-02、TC-INV-01/03 |
| 5.4 标题 per-session | TC-A1-02 |
| 5.5 `ReleaseWorkingHistoryFor` + 取消活跃回退 | TC-A2-01/02/03、TC-R-03 |
| 5.6 `TaskSnapshotFor` | TC-A1-02 |
| 阶段 1 SessionScope / hot_attach | TC-A3-02、TC-LC-01/02 |
| 阶段 2 unload | TC-LC-01–04 |
