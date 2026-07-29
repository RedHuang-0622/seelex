# GUI Frontend

## 模块定位

本目录包含被 Go `embed.FS` 打包进 Wails 的前端源码。当前没有 bundler；`dist/` 就是可维护源文件和生产资产，而不是可随意删除的生成目录。

## 结构

| 文件 | 职责 |
|---|---|
| `dist/app.js` | DOM 绑定、Bridge 调用、工作区/session/runtime/settings 编排。 |
| `dist/client-state.js` | Snapshot/Event reducer、seq gap 和 resync。 |
| `dist/conversation-view.js` / `chat-view.js` | keyed conversation 与 chat activity 渲染。 |
| `dist/components.js` | message/tool/queue 等纯渲染组件。 |
| `dist/plan-dsl.js` | Plan JSON DSL 归一化和卡片渲染。 |
| `dist/read-sources.js` | 从会话工具事件中收集成功完成的 `read_file` 路径，供右侧栏显示。 |
| `dist/markdown.js` | 安全 Markdown、think block 和 URL 过滤。 |
| `dist/effort-control.js` | Effort selector 状态与 rollback。 |
| `dist/protocol.js` | protocol version 校验。 |
| `dist/*.test.mjs` | Node 内置 test runner 契约测试。 |

## 状态流

1. 初始化通过 Bridge `Snapshot` 获取权威状态。
2. `client-state` 应用连续 `seelex:event` 增量。
3. seq gap、协议不兼容或未知状态触发完整 Snapshot resync。
4. render functions 根据 state 投影 DOM；所有 mutation 通过 Bridge 返回 Application。

Plan DSL 常驻右侧项目栏；没有 Plan 时隐藏整个 section，加载、运行和完成状态都由 `runtime.plan` 驱动。Runtime 弹窗只保留运行时诊断信息。

点击新建会话只调用 `BeginNewSession` 进入编辑草稿：允许选择项目和编辑输入框，但左侧列表不新增任何条目，也不生成临时 ID。第一次提交真实对话后，Application 返回真实 ID，左侧才新增正式 Session，并以首个问题作为列表标题。

## 安全和身份规则

- 所有模型/工具/用户文本在进入 HTML 前 escape 或经过受控 Markdown renderer。
- 禁止执行 raw HTML、危险 URL 或任意脚本。
- session/project 名称只显示；按钮 `data-session`、`data-ws` 必须保存 ID。
- draft session 没有 ID、没有左侧列表行，不允许触发 resume/delete/binding；物化后列表行为仍只使用真实 ID。
- DSN、API key 等秘密不能进入 renderer state。

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
