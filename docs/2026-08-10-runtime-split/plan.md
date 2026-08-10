# Runtime 拆分实施计划（Step 1）

> 分支：`refactor/runtime-split`（不污染 main）。
> 目标：Step 1 拆出 `subagentSessions` actor 与 `worktreeManager`，保持
> `application/contract/ports.go` 无破坏性改动，测试全绿 + 真实 API 冒烟
> 正常后 build dev。

## 1. 改动范围

### 1.1 新增 `seelebridge/subagent_sessions.go`

从 `agent_node.go`/`node_tool_result.go` 迁出子代理会话注册表：

- 字段：`nodeSessions`/`nodeSnapshots`/`nodeGoals`/`nodeContextSnapshots`/
  `nodeToolArchivers` + 原 `nodeSessionsMu`；
- 方法：`RegisterNodeSession` / `UnregisterNodeSession` /
  `NodeSessionConversation` / `NodeContextSnapshot` / `NodeToolResult` /
  `NodeToolResultArchiverFor`（含 `node:<nodeID>:` 前缀归档器）；
- 并发模型：与 `subagentContext` 同构——channel 命令 + 单 goroutine 串行写，
  atomic 快照做无锁读取面；`Close()` 幂等。

### 1.2 新增 `seelebridge/worktree_manager.go`

从 `worktree.go` 迁出 worktree 生命周期：

- 字段：`wt` + `worktreeState`；
- 方法：`BeginNodeWorktree` / `FinishNodeWorktree` / `ReleaseNodeWorktree` /
  `NodeWorktreeInfoFor` / `ApproveMerge` / `gitRunner` 族；
- 并发模型：git 子进程天然串行，组件内单锁即可；保留失败现场语义
  （`releaseNodeWorktree` 仅在成功路径调用）。

### 1.3 Runtime 门面收窄

- `runtime.go`：删除迁出的字段，方法改为委托组件；`Shutdown` 增加两个
  actor 的 `Close()`；
- `agent_node.go`/`node_tool_result.go`/`worktree.go`：只留调用 Runtime
  委托方法的入口，字段访问全部改组件；
- `application_adapters.go`/`contract/ports.go`：**接口不变**（Runtime 仍
  实现 `RuntimePort`/`ChatEngine`），适配器内部转发。

## 2. import 迁移

- `subagent_sessions.go`/`worktree_manager.go` 引入 `seelexctx`、
  `seelexctx/snapshot`、`Seele/session`、`Seele/types` 等（从原文件复制
  import 集合）；
- 原文件删除迁出方法后，若 import 不再使用必须移除（`goimports` 校验）；
- `agent_node.go` 保留 `SeelexAgentNode` 节点执行包装，方法调用改为
  `n.runtime.subagentSessions.Register(...)` 形式。

## 3. 测试迁移

### 3.1 直接构造 Runtime 的测试

搜索 `Runtime{` 字面量构造（如 `&Runtime{subagentMailbox: ...}`），改为
`newTestRuntime(t)` 或组件级构造：

```text
rg -n "Runtime\{" seelebridge/*_test.go
```

### 3.2 子代理会话/worktree 相关测试

- `node_scope_test.go` / `subagent_tree_test.go` / `worktree_test.go`：
  `runtime.nodeSessions` 等字段访问改组件访问器；
- `worktree_test.go` 中环境受限用例（`resolve root links: Access is denied`）
  维持 `-skip` 白名单，不在本分支新增/删除；
- 新增组件级单测：`subagent_sessions_test.go`（注册/读回/并发 -race）、
  `worktree_manager_test.go`（生命周期/失败现场）。

### 3.3 fake 端口同步

`service_test.go` 的 `fakeRuntime` / `fakeEngine` 只依赖 `RuntimePort`/
`ChatEngine` 接口，字段未变则无需改动；跑 `go test ./application/...`
 验证。

## 4. 验收命令

```text
go build ./seelebridge/... ./application/...
go vet ./seelebridge/ ./application/...
go test -race ./seelebridge/ -skip '^Test(Worktree|ResolveNodePath|GlobSkipsHeavyDirs|ProjectScopeResolvesOnlyInsideBoundRoot|RuntimeProjectScopedToolsUseBoundProject|ScopedBashPublishesDiagnosticStages|BashDiagnosticObserverPanicDoesNotBreakTool)' -count=1
go test ./application/... -count=1
```

## 5. 真实 API 冒烟

```text
$env:SEELEX_LIVE_SMOKE=1
$env:SEELEX_ACCOUNTS_PATH='G:\Program\go\seelex\config\accounts.yaml'
go test ./seelebridge -run TestForkSubagentsLiveSmoke -v
```

冒烟通过条件：fork 完成、子代理真实产出、merge-back 注入主会话、
worktree 成功清理（或非 git 项目正确降级）。

## 6. 完成标志

- 上述验收命令全绿；
- 真实 API 冒烟正常；
- 无未提交改动（除 `.claude/`、`opt/` 本地文件）；
- build dev：`make rebuild-gui VERSION=dev LOCAL_CONFIG=config/accounts.yaml`
  （Git bash 执行）。
