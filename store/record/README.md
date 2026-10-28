# store/record

## 生态位

会话级工具记录快照的持久化 owner。消费方是 wiring/bootstrap 的 tool-record adapter；
快照形状仍由 `application.ToolRecordSnapshot` 定义，本包只保存调用方给的不透明 payload。

## 职责与非职责

- 职责：`<sessionRoot>/tool-record.jsonl` 批次追加、最后一条完整快照读取、崩溃残尾与坏行跳过。
- 非职责：工具计数、成功/失败分类、配额、UI 投影或跨会话汇总。

## 依赖方向

只依赖 `contract/dto`、`store/internal/layout`、`store/root` 与标准库；不得 import 根
`store`、Application、Middle、Backend 或其它 store 领域实现。

## 数据兼容

文件名、JSONL 形态、追加与读取语义保持原样；没有数据迁移或清理。

## 测试

```text
go test ./store/record ./store -run ToolRecord -count=1
```
