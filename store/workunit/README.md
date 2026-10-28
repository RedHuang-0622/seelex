# store/workunit

## 生态位

node/subagent/teammate 会话记录与主会话索引的持久化 owner。主要调用方是根
`store.NodeSessionStore` 的迁移期单层转发、`backend/multiagent/subagent`、
`backend/workunit` 与装配层恢复链。

## 职责与非职责

- 职责：`NodeSessionRecord` 稳定 wire、`subagents/` 文件布局、原子写、枚举、
  删除、缺失即 `ErrNotFound`，以及记录写侧责任链。
- 非职责：子代理执行、作业调度、主会话消息提交、worktree merge 或跨域收口。

## 依赖方向

依赖标准库、`store/root` 与 `store/internal/layout`；禁止 import 根 `store`、
Application、Middle、Backend 或其它 store 领域实现。

## 数据兼容

目录保持 `<sessionRoot>/subagents/`，文件名保持
`<mainSessionID>-<subSessionID>.json`；JSON tag、schema version 与排序保持不变。
本包不迁移或删除既有记录。

## 文件与函数

- `record.go`：`Record`、`UnitRecord`、`WorktreeRecord`、`SchemaVersion`、`ErrNotFound`。
- `store.go`：`Store.Write/Read/List/Delete/MarkTerminal` 与写侧 `Writer` 责任链。
- `store_test.go`：路径/默认值、缺失/list/delete/terminal identity、writer chain。

## 测试

```text
go test ./store/workunit -count=1
go test ./store -run 'NodeSession|Subagent|UnitRecord' -count=1
```
