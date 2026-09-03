# Session Manager

## 模块定位

`session` 是 Seelex 的**会话域**：会话资源（身份、可见投影、聊天运行态、
生命周期状态机）的唯一所有者。执行内核（`application/core`）经本包暴露的
端口读写会话；会话之间零共享，继承只走深拷贝（详细设计见
`docs/2026-08-30-session-resource-refactor/session-domain-design.md`）。

`manager.go` 保留为 legacy 存储桥（Save/Load callback、active workspace
routing、显式 project-scoped read、存储设置），自会话域重构起降级为迁移辅助，
新逻辑不得依赖它。

## 核心实现

- `Store`：兼容旧 Seele store 的最小 List/Delete/Load/Range/Count 接口。
- `Manager`：持有 legacy store、可选 `NestedSessionStore` 和生产 `sessionstore.Router`。
- `InjectSaveLoad`：连接 Engine 当前会话的 Save/Resume callback。
- `SetWorkspace`/`Workspace`：只影响后续默认读写 scope。
- `ListByWorkspace`/`LoadHistoryByWorkspace`/`DeleteByWorkspace`：不改变 active scope 的显式读取。
- `SaveCommitWorkspace`/`LoadEventRangeByWorkspace`/`ListToolResultsByWorkspace`/
  `CurrentGenerationWorkspace`/`SaveContextStateWorkspace`：会话 fork 需要的
  显式项目作用域读写（深拷贝 tool-results 物理复制、段落边界解析、血缘
  generation 读取），不改变 active write scope。
- `StorageConfig`/`TestStorage`/`ConfigureStorage`：委托 Router 原子切换 backend。
- `Domain`（`domain_actor.go`）：会话域 actor —— 注册表与视图指针 V 由单条
  goroutine 独占，方法一律经 channel 命令交互，域内没有共享 mutex。
  - 写命令（`Register`/`Remove`/`SetActive`）等 actor 回包后才返回：视图指针是
    事件投递端判定会话归属的依据（见 `application/event`），异步会让紧随其后
    的 `ActiveID()`/`Unit()` 读到旧状态。
  - `Close` 投递 `domainCmdClose`，由 actor 自己关闭 `stopCh` 并退出：因此重复
    与并发 `Close` 都幂等，也不需要往 `Domain` 上加共享可变状态（共享面约束由
    `shared_face_test.go` 把守）。停机后 `call` 经 `stopCh` 返回零值，不挂起。
- `SessionUnit`（`ports.go` / `runtime_slot.go`）：每个会话一份的资源单元 ——
  身份/血缘、`Runtime` 投影槽、`Revision`、`Composer` 草稿与 `Effort`/
  `FullAccess` 会话选择（G4：未选择回退进程默认；fork 子单元不继承父的运行期
  选择，见 `deepcopy_test.go: TestS0ForkDeepCopyIsolation`）。

## 生态位

Application 关心“保存/恢复哪一个 session”，sessionstore 关心“如何原子存储”；Manager 隔离两者并保留旧调用兼容。

## 并发与 Review

- save/load callbacks 受 mutex 保护，但不能在回调内反向调用持同一锁的方法。
- 跨项目读取必须优先显式 API，不能临时切 active workspace 后忘记恢复。
- Router 可用时它是生产事实源；legacy nested store 仅兼容旧数据路径。
- 配置切换错误必须保留旧 repository 可用。

## 测试

```text
go test ./session -count=1
go test ./application/core -run 'Session|History|Workspace' -count=1
```

## Atomic recovery APIs

`SaveCommit` adapts the Application snapshot to `sessionstore.Commit`. `LoadEventTailByWorkspace` performs token-bounded complete-unit recovery without changing the active workspace, and `LoadToolResultByWorkspace` supports scoped read-only result retrieval. These APIs require the configurable Router; legacy stores retain history-only compatibility.

对话 fork 一期（深拷贝 + 血缘 meta）：`ForkSession` 由 application 层编排
（`application/core`），Manager 提供显式项目作用域的枚举/提交/上下文读写，
存储层保证 tool-results 通道随子会话提交物理复制。
