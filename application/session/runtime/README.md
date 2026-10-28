# application/session/runtime

## 生态位

`application/session/runtime` 是 Engine Session 的 Application-facing identity facade。
它服务 core 的 session catalog/record/binding/cold/fork/history 路径；真实 durable
session 状态与恢复策略仍在 [`kernel/session`](../../../kernel/session/README.md)。

## 职责与非职责

- 做：expose Coordinator/Deps/Location、SessionContext/Fork/Transcript/Range ports，
  并转发 title/workspace/resume/fork helper。
- 不做：不复制 session record、catalog、project binding、恢复或存储策略。

## 文件结构

- `facade.go`：类型/var alias、NewCoordinator 与 helper forwarders。
- `facade_test.go`：Coordinator / Location 类型身份。

## 核心实现

所有类型均为 identity alias；`SessionArchiveResumePrefix` 指向 Backend 同一变量。
Application 只改变 import 路径，不改变 durable layout 或恢复语义。

## 数据流

core use case → Application session runtime facade → Engine Session Coordinator →
session records/binding/transcript facts。

## 依赖方向

允许依赖 `kernel/session`、`contract` 与 `contract/protocol`。禁止依赖
`application/assembly`、GUI/TUI 或具体 store。

## 并发与错误语义

锁、catalog refresh、project binding、fork/release 与恢复顺序全部由 Engine Session
持有；本包无状态。

## 扩展方式

只有 core 真实调用且 Engine Session 已有唯一实现时才新增 alias/forwarder；禁止在本包
复制 session state。

## Review 指南

- 类型/变量是否仍为 identity alias？
- 是否复制 session catalog、binding 或 cold recovery 判据？
- 是否改变 durable record、seq 或 fork 语义？

## 测试与验证

```text
go test ./application/session/runtime -count=1
go test ./kernel/session -count=1
go test ./application/assembly -run 'Session|Cold|Fork|History|Reference|Chat' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### facade.go

- `func NewCoordinator(deps Deps) *Coordinator`
- `func SessionTitle(input string) string`
- `func SessionTitleFromHistory(history []contract.EngineMessage, displayUserInput func(string) string) string`
- `func WorkspaceID(workspace *model.WorkspaceInfo) string`
- `func RecordResumeHistory(record model.SessionRecord) []contract.EngineMessage`
- `func CloneSessionPlanStack(stack []model.SessionPlanFrame) []model.SessionPlanFrame`

### facade_test.go

- `func TestSessionRuntimeTypesAreBackendIdentity(t *testing.T)`

### session_switch_ab_test.go

- `func newLockRegistry(interval time.Duration) *lockRegistry`
- `func (registry *lockRegistry) busyWork(stop <-chan struct{})`
- `func (registry *lockRegistry) switchTo(sessionID string) time.Duration`
- `func (registry *lockRegistry) applied() int`
- `func (registry *lockRegistry) validate() bool`
- `func newActorRegistry(interval time.Duration) *actorRegistry`
- `func (registry *actorRegistry) loop()`
- `func (registry *actorRegistry) busyWork(stop <-chan struct{})`
- `func (registry *actorRegistry) switchTo(sessionID string) time.Duration`
- `func (registry *actorRegistry) applied() int`
- `func (registry *actorRegistry) validate() bool`
- `func (registry *actorRegistry) close()`
- `func appendMirrorWork(log *[]int, value int)` — appendMirrorWork 追加一条“镜像工作”日志并裁剪（模拟忙写/切换的固定临界
- `func mirrorMonotonic(log []int) bool`
- `func runABRound(t *testing.T, registry abSwitchRegistry, name string) (time.Duration, time.Duration, int)` — runABRound 在 rate-limited 忙负载下并发发起 switches，返回一次轮次的
- `func median(values []time.Duration) time.Duration`
- `func runABCompare(t *testing.T, registry abSwitchRegistry, name string)` — runABCompare 跑多次轮次并汇总两个维度：
- `func TestSessionSwitchABLockVsActor(t *testing.T)` — TestSessionSwitchABLockVsActor 对比锁串行（A）与 actor 模型（B）在忙会话
