# 2026-09-11 对话导航轮轴（一问一答刻度）与长会话恢复顺序

> 日期: 2026-09-11 | 范围: `gui/frontend/dist`（轮轴 + 样式 + 测试）、
> `sessionstore`（conversation 派生）、`application/core/session_runtime`
> （恢复历史重建）、`internal/adapters`（消息适配），另同步 `sessionstore/README.md`、
> `gui/frontend/README.md`、`application/core/session_runtime/README.md`。

## 背景

用户提出两件事：

1. 右侧拨轮轴要改成 DeepSeek 网页版那种效果——**一条刻度对应一问一答**，
   点下去能跳到那个问题，回答就在下面；
2. 长会话恢复要**按 message 顺序**铺开，而不是「工具挤成一坨、LLM 正文另成一坨」。

## 现场与根因

### 恢复顺序：v8 派生 conversation 丢字段

GUI 冷加载在 v8 布局下走派生通道（`LoadRecordRaw` → `DerivedRecordWorkspace`
→ `derivedRecordPayload` → `derivedConversationMessages`）。抽查本机真实长会话
（`dist/seelex-gui-dev/.seelex/sessions-json/.../message/*.jsonl`，541 行）：

| 行 | 数量 |
|---|---|
| `user/user_input` | 1 |
| `assistant/tool_call`（正文为空，思考在 `reasoning_content`） | 248 |
| `tool/tool_output` | 291 |
| `assistant/llm` | 1（seq 541，末尾） |

`derivedConversationMessages` 有三处失真：

1. 一行里带多个 `tool_call` 时**只取第一个**——其余调用从可见会话里消失，
   而它们的结果仍在，于是「工具对不上号」；
2. `role=tool` 的输出行被拆成「调用 + 结果」两条同 ID 消息——同一次调用
   重复计入窗口，前端按 ID 建 DOM key 会互相覆盖；
3. `reasoning_content` 整个丢掉——248 个助手步骤全变成空 assistant 消息，
   前端按「无正文不渲染」跳过，于是只剩一坨工具和末尾一条正文。

### 轮轴：按条目画线不解决导航

旧轮轴（2026-09-11 首版）每条可见条目画一条线、线高按内容比例。它与内容位置
对得上，但不是「导航到某轮问答」：工具过程与思考各占线，用户找不到下一个问题；
并且长会话尾部窗口里往往一条 user 轮都没有。

## 实现

### 后端：派生与恢复历史按 message 顺序

- `sessionstore/conversation.go`：`ConversationMessage` 增加
  `reasoning_content`。
- `sessionstore/json_layout.go`：`derivedConversationMessages` 改为逐行保序、
  与运行期可见投影同形——有正文/思考的行成一条消息、行内**每个** `tool_call`
  各成一条 `role=tool` 调用消息（保留 `arguments`）、`role=tool` 输出行只成
  一条 `role=tool_result`；同行的多条派生消息用 `#tool-N` 后缀保证 ID 唯一
  （`derivedToolCallMessageID`）。
- `internal/adapters/session_workspace_ports.go`：`adaptStoredConversationMessage`
  带上 `ReasoningContent`（record 通道走 JSON 同名字段自动带出）。
- `application/core/session_runtime/archive.go`：`RecordConversationTranscript`
  跳过「只有推理、没有正文也没有工具」的助手步骤——它不是 provider 该看到的话，
  放进重建历史会让每次恢复都多出一轮空 assistant（装配层再补成恢复说明）。

### 前端：一问一答刻度

- `conversation-wheel.js` 重写：`wheelAnchors` 选挂点（优先 user 轮，窗口内没有
  user 轮时退回助手步骤）、`buildWheelRounds` 按问题节点的**真实高度比例**定位
  刻度并保证最小间距、`activeRoundIndex` 用视口 40% 参考线判当前问答、
  `roundAtOffset` 命中/就近取刻度；删掉旧 `buildWheelLines`/`wheelThumb`/
  `scrollTopForThumbTop`/`scrollToKey` 之外的眼球——轨道不再画视口滑柄。
- `styles.css`：`.wheel-line`/`.wheel-viewport` 换成 `.wheel-dash`（3px 短线 +
  1px 时间基线，当前问答用 `--tick-hot` 加长，悬停加宽）+ 伪元素把命中区撑到
  ~9px。

## 验证

```text
go test ./sessionstore/ -count=1                     # OK（含新增派生用例）
go test ./application/core/... ./internal/adapters/... -count=1   # OK
go build ./...                                       # OK
node --test gui/frontend/dist/*.test.mjs             # 230 tests / 230 pass / 0 fail
```

红灯→绿灯证据：

- `sessionstore/conversation_derivation_test.go`（新增）：
  `TestDerivedConversationKeepsMessageOrder` 修复前因 `ReasoningContent` 缺失
  编译失败，修复后断言顺序 `user → assistant(思考) → tool ×2 → tool_result ×2
  → assistant`、行内两个调用参数都在、ID 唯一、每条输出只派生一条结果。
- `application/core/session_archive_test.go`：
  `TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents` 在
  去掉 transcript 守卫时会因「多出一轮恢复说明」转红，恢复守卫后绿。
- `gui/frontend/dist/conversation-wheel.test.mjs`（重写，11 例）：问答挂点、
  无 user 轮的回退、比例定位、最小间距、拥挤压缩、当前问答跟随滚动、命中/就近、
  空态、指纹。
- `gui/frontend/dist/components.test.mjs`（新增一例）：恢复形状渲染顺序 =
  问题 → 助手步骤 → 该步工具过程 → 正文。

## 未做 / 风险

- 未做真机 GUI 目视验证（本轮未启动 Wails WebView）：刻度间距、当前高亮与
  DeepSeek 的手感差异建议在真机过一遍。
- `deriveSessionRecord`（事件流兜底构造 record，非 v8 派生通道）仍不带工具
  引用；本轮只改 GUI 冷加载实际走的派生通道。
- 窗口仍是 `limits.history_window` 有界窗口：翻到中段时轮轴退化为「助手步骤」
  分段（不是问题），这是有界窗口的代价。
