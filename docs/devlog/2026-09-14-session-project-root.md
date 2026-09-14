# 2026-09-14 工具项目根按会话路由（工作区污染修复）

## 症状（真实运行）

在别的项目（会话绑定 `G:\一些暴论\-`）里工作时代理发现自己"项目作用域指到了
`G:\Program\go\seelex`"：文件被读写到另一个项目，临时脚本/`.bak` 落在错误目录。

## 复现（红灯）

`application/core/session_project_root_test.go::TestBackgroundSessionKeepsOwnProjectRoot`：

```text
会话 A 的工具根 = "...\TestBackgroundSessionKeepsOwnProjectRoot...\002",
want "...\001"：后台会话解析到了视图会话的项目根（工作区污染）
```

场景 = 会话 A 属项目 A → 视图切到项目 B 的会话（B 跑过一轮）→ `SubmitToSession(A)`
后台续跑。A 的工具根此时是 B 的项目目录。

## 根因

`seelebridge/security.ProjectScope` 是**进程级单根**（`root`/`realRoot` 两个字段），
`tools.Router` 解析路径时只有 worktree 节点作用域与"全局根"两条分支；
core 也只在会话切换/恢复/绑定工作区时按**视图会话**绑根
（`bindProjectRootIfSafe` 在运行期刻意跳过重绑）。于是：

- 视图会话的项目根 = 进程根；
- 后台/并行会话（`SubmitToSession`、冷恢复续跑、多项目并行）没有自己的根；
- `Runtime.PerSessionExecution()` 长期返回 false 正是因为这个前置条件未满足。

## 变更

| 层 | 变更 |
|---|---|
| `seelebridge/security/project_scope.go` | 根按**会话键**分格：`BindFor`/`UnbindFor`/`RootFor`/`Resolve{Read,Write,Workdir}For`/`RelativeFor`；空键 `DefaultScopeKey` = 进程默认根；未绑定会话键回退默认根（旧会话语义） |
| `seelebridge/tools/router.go` | `Deps.SessionKey(ctx)`（生产 = telemetry 会话 ID）；`resolveNodePath` 解析顺序 = worktree 节点 → **执行会话自己的根** → 默认根；grep/glob 的显示路径同键解析 |
| `seelebridge/runtime.go` / `runtime_tools.go` | 新增 `BindProjectRootFor`/`UnbindProjectRootFor`，并把 `SessionKey` 接到工具 Deps；`PerSessionExecution` 注释更新为"只剩 worktree/PathGuard 未按会话" |
| `application/contract/ports.go` + `internal/adapters` | RuntimePort 增加两个逐会话绑定方法（含 e2e/gui 测试桩） |
| `application/core` | `runChat` 起点调用 `bindSessionProjectRoot(sessionID)`（按会话绑自己的项目根）；工作区解绑/无工作区恢复时 `UnbindProjectRootFor` 清根 |

## 验证

```text
go test ./application/core -run TestBackgroundSessionKeepsOwnProjectRoot -count=1   → PASS（红灯转绿）
go test ./seelebridge/security -run ProjectScope -count=1                            → PASS（新增按会话分格隔离用例）
go test ./seelebridge/tools -run ScopedTools -count=1                                → PASS（write_file 落会话根，不污染视图根）
go test ./seelebridge/... ./application/... ./sessionstore ./internal/adapters -count=1 → 全 ok
go test ./gui ./e2e -count=1                                                         → ok
go vet ./... ; go build ./...                                                        → 干净
```

回归用例：`application/core/session_project_root_test.go`、
`seelebridge/security/project_scope_session_test.go`、
`seelebridge/tools/router_session_root_test.go`。

## 未覆盖（已知缺口）

- **worktree / PathGuard 仍读进程默认根**：子代理 worktree 由
  `worktree_manager` 的 `Root func() string` 闭包创建，创建/收尾路径没有会话键；
  跨项目并行时子代理 worktree 仍会建到视图会话的项目仓库。要收口需把
  `BeginNodeWorktree`/`WorktreeManager.Begin|Finish` 换成按 ctx 会话取根，这也是
  `Runtime.PerSessionExecution()` 仍返回 false 的原因。
- 会话存储侧不受影响：persist 走显式 `location.WorkspaceID`（`SaveRecordRaw`/
  `LoadSessionRecordWorkspace`），没有借用全局写作用域。
