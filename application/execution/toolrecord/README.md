# application/execution/toolrecord

## 生态位

`application/execution/toolrecord` 拥有工具记录的应用侧台账：成功/失败累计、失败原因、
执行单元暂存、会话批次落盘与看板投影。主要调用方是 `application/core` 的
`Service` 兼容入口；具体工具执行与持久化仍分别归工具层和 Session Store 的
`tool-record.jsonl` 端口。

## 职责

- 维护每会话一份本包 `Accumulator`；
- 维护 `(sessionID, unitID)` 的本包 `Stage`、`lastSeq` 幂等序号和 dropped 诊断；
- 唯一拥有工具成败与失败原因的判据；
- 先持久化开关设置、再修改内存；
- 在提交点 drain 叶子暂存并批量落盘整份会话快照；
- 每个会话最多懒加载一次持久快照；
- 把累计读数投影成 `dto.ToolRecordBoard`。

## 非职责

- 不执行工具、不判断工具权限、不生成工具消息；
- 不执行工具；
- 不直接读写存储文件；持久化只通过 `contract.ToolRecordSnapshotStore`；
- 不决定会话是否已物化；该宿主判据由 `Deps.WritableSession` 注入；
- 不持有完整 `Service`，也不 import `application/core`。

## 核心契约

```go
type Deps struct {
    SaveEnabled     func(bool) error
    WritableSession func(string) bool
    ViewSessionID   func() string
}
```

`SaveEnabled` 失败时开关保持旧值。`WritableSession` 只在 `Flush` 的落盘门前调用；
`ViewSessionID` 只用于 `Board` 选择当前会话。

## 数据流与生命周期

1. 主代理/命令结果进入 `RecordOutcome`，直接累计到会话；
2. 子代理/teammate 结果先进入 `StageUnitOutcome` 的执行单元暂存；
3. 单元收口 `CloseUnit` 或 flush 前 drain 通过 `CommitUnit` 合并到会话累计；
4. `Flush` 先 drain、再检查可写会话、最后在 dirty 时写整份快照；
5. `Board` lazily load 一次持久快照，再读取当前会话累计与最近 flush 错误；
6. 会话释放通过 `ReleaseSession` 回收累计、暂存、加载标记和幂等序号。

## 并发与错误语义

- 顶层 mutex 只保护开关、端口、表索引和错误槽；
- `Accumulator` / `Stage` 使用各自的锁，热路径不在顶层锁下累加；
- 持久化 I/O 不持顶层锁；
- 同一 `(sessionID, unitID)` 的 `seq <= lastSeq` 幂等忽略，`seq == 0` 保留旧兼容语义；
- flush 失败不回抛，错误进入看板 `LedgerError`；
- 无 owner/unit 的读数丢弃并增加诊断计数，不回落到当前视图。

## 扩展与 Review

- 新增读面优先扩展现有 `SessionView` / `Diagnostics`，避免创建无人使用的未来接口；
- 原因词表只能改 `Reason` 一处，并与 `dto.ToolReason*` 同步；
- Review 时检查 `Flush` 是否先 drain、是否在锁外 I/O、是否错误地回落视图会话，
  以及 `ReleaseSession` 是否清理 `lastSeq`。

## 测试与验证

```text
go test ./application/execution/toolrecord -count=1
go test ./application/core -run 'ToolRecord|ToolCommand|RoleTool|SubagentTool' -count=1
```
