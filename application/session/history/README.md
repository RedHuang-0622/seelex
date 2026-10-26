# application/session/history

## 生态位

`application/session/history` 拥有 Session resume、冷恢复顶层流程、可见历史窗口
安装和恢复后页面接管。主要调用方是 `application/core` 的 Session assembly 与
`application/history` 查询适配。

## 职责

- hot/restoring/cold 恢复分支与 restoring shell；
- 冷恢复的 read/history/restore/workspace/activation/task/view/commit/publish 编排；
- durable conversation 分页、可见窗口安装与正文 LRU 触发；
- provider history 到可见消息的适配与可见性判据。

## 非职责

- 不拥有 SessionUnit、View、Engine 或 Store；
- 不复制 Middle history window 算法；
- 不执行压缩、对话主循环或持久化事务；
- 不 import `application/core`。

## 文件结构

- `runtime.go`：依赖图、Session unit/event 回调、message adapter 与 public entry；
- `history_runtime.go`：resume/cold restore/history 编排；
- `runtime_test.go`：可见性与消息投影 parity。

## 数据流

Resume 请求 → 过渡锁与视图意图 → hot/restoring/cold 分支 → cold 组件链 →
锁内安装 View/激活 Session → 锁外发布 Snapshot/Runtime/Catalog → 内容 LRU touch。

## 依赖方向

允许依赖 `application/session`、`session/{cold,residency,runtime,state,viewstate}`、
`conversation`、`workspace`、`history`、`contract`、kernel state 与 Middle 纯策略。
禁止依赖 `application/core`、GUI/TUI 或具体 Store。

## 并发与错误

过渡锁只覆盖视图意图和提交，磁盘读/Engine 恢复在锁外；发布在锁外完成。
恢复失败保持 notice/close/fallback 语义；查询窗口不使用共享写锁。

## 扩展与 Review

新增恢复分支必须先定义状态所有权和失效条件。Review 重点：锁是否跨 I/O、
epoch 是否仍按会话复判、可见/未过滤行数是否混用、恢复失败是否留下半装状态。

## 测试与验证

```text
go test ./application/session/history -count=1
go test ./application/core -run 'Session|History|Cold|Resume|Restore|Fork' -count=1
go test ./application/... -count=1
```
