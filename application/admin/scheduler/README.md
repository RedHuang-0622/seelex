# application/admin/scheduler

## 生态位

`application/admin/scheduler` 承载定时任务创建/编辑/取消、目标会话运行装配和定时触发新建
会话。定义与执行仍归 Runtime scheduler。

## 职责

- 应用默认执行参数并统一校验；
- 创建/编辑共用 workspace 与插件校验顺序；
- 提交前应用 permission tier、run params 和插件 carrier；
- 新会话的创建、workspace projection、snapshot 发布和后台提交。

## 非职责

- 不实现定时定义持久化或触发调度；
- 不拥有 Session、Engine、Runtime 或 workspace；
- 不导入 `application/assembly`。

## 文件结构

- `application.go`：CRUD 与 run-param 默认/校验。
- `run.go`：目标会话运行装配。
- `session.go`：定时触发新会话顶层流程。
- `scheduler_test.go`：校验、默认参数、装配和发布顺序测试。

## 数据流

Service 兼容入口 → scheduler application → Runtime scheduler / session / workspace
port。任务定义仍是 Runtime 单一事实源。

## 依赖方向

允许依赖 `contract/dto`、`kernel/limit`、`kernel/limit/effort` 与
`application/workspace/switch.Projection`。禁止依赖 `application/assembly`、GUI/TUI。

## 并发与错误

workspace projection 在锁外采集，锁内应用并 bump；发布在解锁后。关闭、排空、空 ID、
未知 workspace/plugin 和非法执行参数均保持显式错误。

## 测试与验证

```text
go test ./application/admin/scheduler -count=1
go test ./application/assembly -run 'Scheduler|Scheduled' -count=1
go test ./application/... -count=1
```
