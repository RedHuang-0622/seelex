# application/conversation/edit

## 生态位

`application/conversation/edit` 承载“编辑用户消息并 fork 出 child 会话”的组件层。
`application/assembly` 只在组合根构造 Registry、Validator、TargetResolver、
Rollback 与 Process，并把最终 `EditApplication` 作为
`Components.Conversation.EditFork` 暴露给前端适配器。组件负责 admission、
target 解析、child 原子编排、rollback 和消息可编辑性。

## 职责

- operation TTL / 同 ID 幂等；
- current/running/queue/revision/window/compression 准入；
- target user event 与完整段落 fork cut；
- child fork → resume → submit → rollback 顺序；
- 可见用户消息 EventSeq 与单条禁用原因。

## 非职责

- 不拥有 Session/Fork 存储、Engine 或 View；
- 不修改 wire 字段；
- 不导入 `application/assembly`。

## 文件结构

- `registry.go`：operation 幂等账与 TTL。
- `application.go`：顶层输入校验、operation waiter 与 process 委托。
- `validator.go`：父会话准入。
- `resolver.go`：目标事件与 fork cut。
- `process.go`：child 原子编排。
- `rollback.go`：child 清理与 parent 恢复错误合成。
- `annotation.go`：消息可编辑性投影。

## 测试与验证

```text
go test ./application/conversation/edit -count=1
go test ./application/assembly -run 'EditUserMessageFork' -count=1
go test ./application/... -count=1
```

`owner_behavior_external_test.go` 使用 `package edit_test`，从外部验证 Registry、
Validator、Resolver、Process、Rollback 和 Annotation；assembly 只保留跨组件编辑并
fork 的集成用例。
