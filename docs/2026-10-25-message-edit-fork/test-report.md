# 测试报告

## 概览

| 范围 | 结果 | 关键指标 |
|---|---|---|
| `go test ./application/core/... ./backend/engine/session/...` | ✅ 通过 | 最后一次完整相关包运行通过 |
| `go test ./store/...` | ✅ 通过 | 最后一次完整 store 运行通过 |
| `go test ./middle/protocol/model/... ./middle/switch/sessionusecase/... ./middle/view/state/...` | ✅ 通过 | 协议与 session/view 适配通过 |
| `go test ./gui/...` | ⚠️ 曾被本任务改动版本通过；最终复跑被并行工作区阻断 | `gui/team_conversation.go` 与 `gui/bridge.go` 出现同名 `ConversationChat/RecoverConversation` 方法 |
| `go build ./...` | ⚠️ 曾被本任务改动版本通过；最终复跑被同一并行改动阻断 | 重复方法不在本任务范围，未修改 teammate 会话实现 |
| `node --test gui/frontend/dist/*.test.mjs` | ✅ 通过 | 874 个 Node 用例通过 |
| `git diff --check` | ✅ 通过 | 无空白错误 |
| `gofmt -l <本次 Go 文件>` | ✅ 通过 | 无输出 |

## 功能覆盖

| 场景 | 结果 | 测试落点 |
|---|---|---|
| 用户消息复制动作与原始内容 | ✅ | `components.test.mjs` |
| 不可编辑消息仅复制/显示禁用原因 | ✅ | `components.test.mjs` |
| 编辑器复用 copyText、稳定 operationId、单一原子调用 | ✅ | `conversation-view.test.mjs` |
| 最近一条用户消息编辑 | ✅ | `TestEditUserMessageAndForkCanReplaceLatestUserMessage` |
| 保留窗口内较早用户消息编辑 | ✅ | `TestEditUserMessageAndForkUsesPreviousParagraphAndSubmitsReplacement` |
| 压缩前缀拒绝 | ✅ | `TestEditUserMessageAndForkRejectsCompressedAndRunningParents` |
| running parent 拒绝 | ✅ | 同上 |
| stale conversation revision 拒绝 | ✅ | `TestEditUserMessageAndForkRejectsStaleParentRevision` |
| 同一 operation 双击幂等 | ✅ | `TestEditUserMessageAndForkSameOperationIsIdempotent` |
| 同一点不同 operation 创建两个 child | ✅ | `TestEditUserMessageAndForkSamePointWithNewOperationCreatesNewChild` |
| 父会话后续回合不污染 child | ✅ | `TestEditUserMessageAndForkKeepsChildIsolatedFromParentLaterTurns` |
| 失败回滚 child 并恢复 parent 视图 | ✅ | `TestEditForkRollbackDeletesChildAndRestoresParentView` |
| 血缘持久化与 UI 来源 | ✅ | `TestPrepareForkTruncatesToRequestBoundary`、`branch-origin.test.mjs`、`tree-fork.test.mjs` |
| Bridge 原子转发/未装配错误 | ✅ | `bridge_message_edit_test.go` |

## 边界与失败语义

- 目标消息缺 EventSeq：不可编辑，后端返回明确原因。
- 目标不在完整段落、非 user、内部注入、恢复说明：拒绝。
- 父事件流在读取与 fork 落盘之间推进：`ExpectedEventSeq` CAS 拒绝。
- 首轮启动失败：删除本次新建 child 并恢复 parent；错误保留。
- 前端提交失败：编辑器不关闭，textarea 内容保留，按钮恢复。
- 剪辑板失败：走既有 `notify(error)` 提示。

## 未执行项

- 未执行 `-race`：当前环境为 Windows 本地，不能据此声称 race 已运行。
- 未执行真实 API live probe（依赖账号与外部服务，相关用例按仓库约定 env 门控）。
- 未创建 commit/push（用户要求）。

## 并行工作区说明

最终门禁期间，另一会话把 `gui/team_conversation.go` 与团队/WorkItem 相关改动写入
同一工作区，造成 GUI 包重复方法编译错误。本任务未修改该文件或对应 teammate
能力；本任务自身的 Go/Node 用例在冲突出现前通过，冲突出现后的核心/会话包与 Node
全量用例仍通过。
