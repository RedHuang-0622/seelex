# application/session/fork

## 生态位

`application/session/fork` 拥有 durable Session fork 的 Application 编排：父会话
准入、子会话 ID/引擎占位、快照与上下文深拷贝、项目绑定和空引擎卸载。

## 职责

- 检查 closed/draining/running 与 per-session execution；
- 调 Engine Session `PrepareFork` 与 workspace snapshot/context 写面；
- 深拷贝 record/events/tool results 并对齐 transcript seq；
- 绑定 workspace、切换写作用域、刷新目录后返回 child ID。

## 非职责

- 不拥有 Engine Session/Store 的事务实现；
- 不复制 fork 可达集合、段落边界或深拷贝算法；
- 不执行子代理 fork/WorkItem 编排；
- 不 import `application/assembly`。

## 文件结构

- `runtime.go`：Lifecycle/session 回调与 public fork entry；
- `fork_runtime.go`：fork admission、copy、persist 和 bind 顺序；
- `runtime_test.go`：生命周期回调 parity。

## 数据流

ForkApplication → transition lock → `ForkSessionLocked` → 父会话 gate → child ID →
PrepareFork/DeepCopy → snapshot/context write → binding/catalog → child ID。

## 依赖方向

允许依赖 Session runtime/state、Task context、protocol 与 kernel state；禁止依赖
`application/assembly`、具体 Store、GUI/TUI。

## 并发与错误

调用方持有父会话 transition lock；磁盘写发生在共享 ViewMu 之外。运行中、关闭和
draining 使用原错误身份；写失败不回滚已创建的空引擎，由调用方删除 child。

## 扩展与 Review

新增 fork 分支必须来自 Engine Session 的现有端口。Review 重点：父会话 gate、
深拷贝别名、seq 基线、workspace binding 和失败回退。

## 测试与验证

```text
go test ./application/session/fork -count=1
go test ./application/assembly -run Fork -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### fork_runtime.go

- `func (service *Runtime) forkSessionLocked(parentID string, request model.ForkRequest) (string, error)` — ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *Runtime` — NewRuntime constructs the Session fork runtime.
- `func (runtime *Runtime) closed() bool`
- `func (runtime *Runtime) draining() bool`
- `func (runtime *Runtime) perSessionExecution() bool`
- `func (runtime *Runtime) newGeneratedSessionID(prefix string) string`
- `func (runtime *Runtime) setWorkspaceWriteScope(workspaceID string)`
- `func (runtime *Runtime) ForkSessionLocked(parentID string, request protocol.ForkRequest) (string, error)` — ForkSessionLocked performs one durable Session fork while the caller holds

### runtime_test.go

- `func TestRuntimeLifecycleCallbacks(t *testing.T)`
