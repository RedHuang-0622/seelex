# 用户消息随行图片：发出去的图必须看得见（2026-10-30）

## 问题

输入框图片附件（`e5df0db9 feat(gui): attach composer images by drag or paste`）只做通了
"字节 → 会话媒体分区 → 随下一次请求送模型"这一段：用户拖着截图发出去之后，**对话区只
显示那句话，看不到自己发了哪张图**。这不是样式问题——`protocol.Message` 里根本没有图片
字段，渲染层无图可画。

## 事实模型（这次的判据）

一条用户输入是「这句话 + 这几张图」，因此图片是**这条消息的一部分**，不是一次旁路投递：

| 问题 | 归属 | 实现点（唯一一处） |
|---|---|---|
| 模型什么时候看得到 | 会话的待随图队列（`Take` 即清空） | `imageattach.Registry` + `SessionMedia.attach`（未改动） |
| 用户发的哪句话配了哪几张图 | **user 事件行**的 `Images` | 提交那一刻由 `SessionMedia.ResolveUserImages` 定死 |

两件事刻意分开：前者答"模型看没看见"，后者答"这句话配了哪几张图"。混成一条，就会出现
排队晋升/冷恢复之后图跑到别的消息下面的情况。

## 改动（自底向上）

1. **契约**：`protocol.TranscriptEvent.Images`（落盘行）+ `protocol.Message.Images`（快照行）
   + `protocol.MessageOrigin.Images`（实时视图行的携带位），类型都是 `[]dto.ComposerImage`
   —— 与前端输入框 chip 同一份形状，不新造第二个"图片附件"结构。
   顺带把 `Message` / `MessageOrigin` / 群聊角色名从 `contract/protocol/state.go` 拆到
   `contract/protocol/message.go`：`state.go` 撞上了 frozen 行数预算（1233），预算的规则
   是"先拆再长"，不是"抬预算"。
2. **提交链**：`SubmitToSession(sessionID, text, imageRefs)`（桥面）→
   `SubmitApplication.SubmitToSessionWithImages`（准入顺序仍是唯一一份）→
   `conversation.Request.Images`（入队随 payload、晋升按批次合并去重）→
   `TurnProcess.BeginLocked` 写进 user 事件行 → 两个 append 点（起始 / 队列晋升）都只从
   事件行照抄 `MessageOrigin.Images`。
3. **投影**：`ProjectConversationEvents` 复制 `Images`——冷恢复与实时视图因此是同一份事实。
4. **媒体**：`store/artifact.MediaMeta`（只读 meta 文件，不读图本体）、
   `SessionMedia.ResolveUserImages`（ref → 展示元数据，跨会话/不存在一律整条失败）、
   `SessionMedia.ReadUserImage`（缩略图字节，只读、归属解析同一份）。
5. **前端**：`components.js` 在用户消息正文**下面**画 `figure`/`img[data-message-image]`；
   `message-images.js` 注水（一次 RPC/ref，data URL 缓存 24 条上限，读不回来打
   `missing` 占位）；`conversation-view.js` 在 reconcile 之后调注水；`app.js` 注入读图口、
   委托点击（→ `OpenComposerImage`），提交时把 ref 交给后端。

## 顺带修正的两件事

- **只有图没有字也能发**：原来 `if (!text) return` 会让"拖张截图按 Enter"什么都不发生。
  现在按"有图就算有输入"放行（正文为空时轮轴标签写「图片 ×N」而不是「（空）」）。
- **sigil 输入不清 chip**：`/` `#` `$` 走后端路由器，那条路不吃图片；原来无论走哪条路都
  会 `clearComposerAttachments()`，等于把没发出去的图悄悄扔掉。现在只在 `SubmitToSession`
  这条路上清，另一条路明确提示"图片仍挂在输入框上"。

## 已知边界

- 耐久队列（`DurableEnqueue`）只落正文：进程重启后**排队中**的那条消息没有缩略图；
  已发出的消息不受影响（图片在事件行里）。
- 消息队列的条上不显示缩略图（`chat.input_queue` 是纯文本投影）：会话运行中发出带图的
  输入时，图要等这条晋升进对话区才看得见。
- 缩略图读的是原图字节（不生成缩略图）：8 MiB 上限内可接受，前端按 ref 缓存避免重复读。
- 「送给模型」仍按**附上那一刻**入队（`AttachUserImage` 不变）：会话正在跑时附的图，
  可能被当前回合的下一次请求先取走，而展示上它属于随后那条消息。修它要动随图队列的
  绑定时机（改成提交时入队），不在本次范围；两件事的口径已分开记录（见
  `backend/tool/io/media/README.md`）。

## 验证

- `go build ./...` / `go build -tags "gui,desktop,production" ./...` / `go vet ./...`
- `go test ./... -count=1`（含 `contract` 的 BM001 行数预算）
- `node --test gui/frontend/dist/*.test.mjs`（961 通过）
- 新增用例：`backend/tool/io/media/user_image_test.go`（解析/读回/跨会话）、
  `store/artifact/artifact_test.go`（`MediaMeta`）、
  `kernel/session/projection_images_test.go`（投影带图）、
  `application/conversation/request_test.go`（批次合并去重）、
  `gui/composer_message_image_test.go`（桥面解析与失败面）、
  `gui/frontend/dist/message-images.test.mjs`（注水/缓存/失败标记）、
  `gui/frontend/dist/components.test.mjs`（图片条位置与转义）
