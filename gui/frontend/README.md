# GUI Frontend

## 模块定位

本目录包含被 Go `embed.FS` 打包进 Wails 的前端源码。当前没有 bundler；`dist/` 就是可维护源文件和生产资产，而不是可随意删除的生成目录。

## 结构

| 文件 | 职责 |
|---|---|
| `dist/app.js` | DOM 绑定、Bridge 调用、工作区/session/runtime/settings 编排。 |
| `dist/client-state.js` | Snapshot/Event reducer、seq gap 和 resync。 |
| `dist/runtime-events.js` | Wails `EventsOn` 就绪探测、幂等绑定与 ready/event 转发。 |
| `dist/conversation-view.js` / `chat-view.js` | 变高 keyed conversation、顶部 history sentinel 与 chat activity 渲染。 |
| `dist/components.js` | message/tool/queue 等纯渲染组件。 |
| `dist/plan-dsl.js` | Plan JSON DSL 归一化和卡片渲染。 |
| `dist/read-sources.js` | 从会话工具事件中收集成功完成的 `read_file` 路径，供右侧栏显示。 |
| `dist/markdown.js` | 安全 Markdown、think block 和 URL 过滤。 |
| `dist/effort-control.js` | Effort selector 状态与 rollback。 |
| `dist/protocol.js` | protocol version 校验、conversation window 和递归 Plan 增量 reducer。 |
| `dist/*.test.mjs` | Node 内置 test runner 契约测试。 |

## 状态流

1. 初始化先等待并幂等绑定 Wails `EventsOn`，再通过 Bridge `Snapshot` 获取权威状态；runtime 尚未就绪时整个初始化按既有重试机制继续，不能静默进入无事件模式。
2. `client-state` 应用连续 `seelex:event` 增量。
3. seq gap、协议不兼容或未知状态触发完整 Snapshot resync。
4. render functions 根据 state 投影 DOM；所有 mutation 通过 Bridge 返回 Application。

Plan DSL 常驻右侧项目栏；没有 Plan 时隐藏整个 section，加载、运行和完成状态都由 `runtime.plan` 驱动。Runtime 弹窗只保留运行时诊断信息。

`Snapshot.Conversation` 是后端提供的有界窗口；增量 reducer 继续按 `conversation_window` 截断。消息 DOM 使用真实内容高度的 keyed reconciliation，顶部 sentinel 接近视口时调用 `LoadMoreHistory` 并用 anchor 恢复滚动位置，不使用 `virtual-list.js` 的固定行高模型。

子代理增量递归更新 `runtime.plan.nodes`：`subagent.changed` 替换完整节点，工具 started/completed 按 ID upsert `node.tool_events`。Plan 支持 `worktree_creating`、`rebasing`、`merging`，详情弹窗显示会话、节点时间线和工具输入/结果/错误。所有更新都先深拷贝 Plan 树，避免修改旧 Snapshot。

点击新建会话只调用 `BeginNewSession` 进入编辑草稿：允许选择项目和编辑输入框，但左侧列表不新增任何条目，也不生成临时 ID。第一次提交真实对话后，Application 返回真实 ID，左侧才新增正式 Session，并以首个问题作为列表标题。

## 安全和身份规则

- 所有模型/工具/用户文本在进入 HTML 前 escape 或经过受控 Markdown renderer。
- 禁止执行 raw HTML、危险 URL 或任意脚本。
- session/project 名称只显示；按钮 `data-session`、`data-ws` 必须保存 ID。
- draft session 没有 ID、没有左侧列表行，不允许触发 resume/delete/binding；物化后列表行为仍只使用真实 ID。
- DSN、API key 等秘密不能进入 renderer state。
- system prompt、其装配结果和层摘要不能进入 renderer state；Runtime 面板只显示模型、Provider、Plugin、Effort、工具和 Plan 等可公开诊断信息。

## Review 指南

- Event delta 是否可能重复应用或越过 revision floor。
- DOM key 是否稳定，streaming 时是否无意义重建大列表。
- `innerHTML` 数据是否全部 escape。
- Plan 点状态更新是否来自权威 JSON，而非仅靠 CSS 本地猜测。
- 新 Bridge 调用是否处理 rejection 并恢复 optimistic UI。

## 测试

```text
Get-ChildItem gui/frontend/dist -Filter *.js | ForEach-Object { node --check $_.FullName }
node --test gui/frontend/dist/*.test.mjs
go test ./gui -count=1
```

`runtime-events.test.mjs` 验证 Wails runtime 延迟就绪时不会漏绑或重复绑定。`event-chain.test.mjs` mock Wails `seelex:event` 并验证主代理/子代理工具完成状态通过 `createGUIClient`/`protocol.js` 后可见，且连续事件不会退化为 Snapshot reload；主代理工具卡明确断言完成后不再显示 `Waiting for output…`。

## Context compression summary

The project overview renders `task.context_compactions` as a small timeline of successful context compressions. The frontend receives only public metadata (version, reason, counts, and time); it does not receive private checkpoint content, prompt text, tool payloads, or raw conversation history.

The “Agent read files” panel also merges the live successful `read_file` calls with the persisted `read_files` cache. It therefore remains useful after context compression or session restoration without retaining file content in renderer state.
