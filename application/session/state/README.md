# application/session/state（Go 包名 `sessionstate`）

## 生态位

`application/session/state/` 是 Seelex 的**会话聚合状态**：会话资源（身份、可见投影、
聊天运行态、队列、生命周期状态）的唯一所有者。执行内核（`application/core`）经本包
暴露的 `Domain` / `SessionUnit` 读写会话；会话之间零共享，继承只走深拷贝。

Go 包名是 `sessionstate`。原 `middle/switch/sessionusecase` 已物理迁出并删除；
主要调用方是 `application/core`、`application/conversation`、`application/session`
的用例组件与 Wiring adapter。

**本包不 import store / Seele / application**：存储形状已下沉 `contract/dto`
（`SessionRecord` / `SessionBinding` / `SessionContextStack` / `SessionInfo`），
存储句柄由 `wiring/adapters.SessionManager` 持有。

## 职责与非职责

- 做：会话单元注册表（`Domain` actor）、`SessionUnit` 状态与队列、运行时投影槽、
  深拷贝。
- 不做：不落盘、不调引擎、不 switch 视图、不决定 LRU（分别属于 store /
  engine / middle/switch 其它文件 / residency）。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `domain.go` / `domain_actor.go` | `Domain`：单 goroutine 独占注册表与 active 指针 |
| `ports.go` | `SessionUnit` / `InputQueue` / 可选会话端口（meta/setting） |
| `runtime_slot.go` | 会话级运行时投影槽 |
| `deepcopy.go` | 会话上下文四栈与记录前缀的深拷贝 |

## 核心实现

- `Domain` 是 actor：`Register` / `Remove` / `SetActive` 等写命令等回包后才返回；
  读命令直接取一致快照。
- `SessionUnit` 持有 `Chat`（running/queued/requestID）、`approvals`、
  `resident`、`View`、`Queue`、`Context`、`Binding`、`Runtime`、`Revision`。
- `InputQueue` 的序号与重排只在本包内实现；展示下标与操作下标同一套空间。
- `DeepCopyRecord` / `DeepCopyContextStack` 是 fork/继承的唯一拷贝口径。

## 数据流

`application/core` 的 `setSessionChatLockedFor` / `sessionUnitLocked` →
`SessionUnit.UpdateChat`（单元状态）→ active 视图由 `kernel/state` 镜像；
后台会话变化由 `application/core` 发进程级 `sessions.changed` 通知显示面。

## 依赖方向

`sessionstate → contract/dto + contract/protocol`（+ stdlib）。
禁止 import `backend/*`、`middle/*`、`store`、`Seele`。

## 并发与存储

- 注册表与 active 指针由 actor 串行化；`SessionUnit` 内部用互斥量保护可变状态。
- 本包不持有存储句柄；持久化由装配层注入的端口完成。

## 扩展方式

新增会话级状态：先加到 `SessionUnit` 并给出读写方法，再由
`application/session` / `application/conversation` 的状态转换点更新；
不要在本包外直接改单元字段。

## Review 指南

- 是否有调用方绕过 `Domain` 直接改注册表/active；
- 是否把 store/engine 依赖重新带进本包（编译期检查 `go list -deps`）；
- 单元字段的拷贝是否仍走 deepcopy（fork/继承）。

## 测试与验证

```text
go test ./application/session/state/ -count=1
go test ./application/core/ -run Session -count=1
```
