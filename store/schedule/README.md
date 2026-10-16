# store/schedule

## 生态位

`store/schedule` 承载**定时任务定义**的全局落盘：一个路径、一条 append-only JSONL、
一行一条变更。它是 `contract.ScheduledTaskPersistence` 端口的唯一实现，
主要调用方是调度器（`seelebridge/scheduler`，M4 之后落 `backend/engine/schedule/`），
由组合根在装配处注入。

物理位置：2026-10-16 按
[plan.md](../../docs/2026-10-15-architecture-reshape/plan.md) §2.12.1 从
`seelebridge/scheduler/persistence.go`（该文件已移除）归位到这里——同一份实现，
没有第二份。

## 职责与非职责

- **做**：把"一条任务定义的一次变更"以整行 JSON 追加落盘；读回时重放（后写覆盖先写、
  墓碑行删除该 ID、残尾视为未提交）。
- **不做**：不判断任务能不能跑（白名单 / 周期合法性 / 过期判定都在调度器侧）；
  不落运行期结果（`LastResult` / `LogTail` / `RunCount` 只在内存里——命令输出可能含
  敏感内容，不因为"重启后还想看上次结果"就落到盘上）；不碰会话正文。

## 文件结构

| 文件 | 职责 |
|---|---|
| `file_store.go` | `FileStore` / `NewFileStore` / `Append` / `Load` / `Path`，以及端口断言 `var _ contract.ScheduledTaskPersistence = (*FileStore)(nil)` |
| `file_store_test.go` | 纯落盘通道的用例（残尾丢弃、缺文件 = 空集、未配置路径 = 空操作） |

## 核心实现

粒度选择是这条通道的全部要点：**任务定义是全局资产**，刻意不按项目分区、不按会话分片
（"每天九点巡检"不该因为切了项目就换一套）。形态是 append-only：登记写一行、
取消写一行墓碑（`deleted`），读取时按 ID 后写覆盖先写——因此没有整文件重写，
也不需要把索引和正文凑成一次原子提交。崩溃只留下**残尾**（未换行收尾的半行），
读侧按"未收尾即未提交"跳过，与会话事件行的恢复语义同一口径。

## 并发与错误语义

`FileStore` 自持一把 `sync.Mutex`：`Append` 与 `Load` 互斥，保证"一整行 + 换行"的
一次写出不被同进程读打断。空路径（`""`）= 未配置持久化，`Append`/`Load` 都是空操作
（返回 nil），不报错。`Append` 的失败要如实返回——调用方据此决定"这次登记/取消算不算成立"。

## 依赖方向

只依赖标准库与 `contract` / `contract/dto`（契约层是最底层，向下依赖它不算反向）。
不得 import `seelebridge`、`application`、`backend` 等上层包；`store` 边界门禁
（`e2e/store_boundary_test.go`）为本包登记了这两条允许项。

## 测试与验证

```text
go test ./store/schedule -count=1
```

经 `State` 的集成面用例（定义/取消/编辑/重启恢复）留在调度器包里，见
`seelebridge/scheduler/persistence_test.go`。

## Review 指南

- 残尾语义有没有被"顺手修复"掉？未收尾的一行就是未提交，不许猜。
- `Append` 是否仍是"一次写出整行 + 换行"？拆成两次写会制造假的残尾。
- 新增字段时是否同步了 `dto.ScheduledTaskRecord` 与本文表格。
