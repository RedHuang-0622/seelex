# 会话渲染与内容安全模块详细设计

## 1. 职责与边界

该模块把已归并的 Conversation/Chat DTO 转换为可维护的视图节点，负责：

- 消息、工具调用和运行/队列状态的 presentation model；
- keyed DOM 协调；
- 滚动跟随和历史锚点；
- think/工具 OUT 展开状态；
- Markdown 安全渲染；
- 工具完整 payload 的复制和按需展开。

它不调用 Bridge，不决定 Chat 业务状态，也不修改客户端 Snapshot。

## 2. Presentation model

实现位置：`gui/frontend/dist/components.js:45-128`。

`renderConversationModel` 输出：

```text
{
  items: [{ key, html }],
  payloads: Map<payloadKey, fullText>
}
```

稳定 key：

| 实体 | key |
|------|-----|
| 普通消息 | `message:<message.id>` |
| 工具调用 | `tool:<tool.id>` |
| 运行/队列尾部 | `chat:activity` |

`buildConversationItems` 先记录 tool start，再用 tool ID 把 tool_result 附加到同一卡片。仅在历史数据缺 ID 时回退 message index；Core 当前加载路径会补稳定 ID。

## 3. 工具卡片

实现位置：`gui/frontend/dist/components.js:152-201`。

### 结构

- Header：工具图标、短名称、耗时、RUN/OK/ERR；
- IN panel：格式化后的 arguments；
- OUT panel：result 或 error；
- copy：复制完整 payload；
- expand：把完整 payload 按需写入当前 `<pre>`。

### 输出限制

| 面板 | 最大字符 | 最大行 |
|------|---------:|-------:|
| IN | 1400 | 28 |
| OUT | 2400 | 40 |

限制只影响默认 DOM preview，不截断 payload Map 中的完整值。JSON 字符串优先 pretty print，解析失败保留原文。

## 4. Keyed DOM 协调

实现位置：`gui/frontend/dist/conversation-view.js:3-89`。

算法：

1. 从 container children 建立 `key → node` Map；
2. 依次遍历目标 items；
3. key 不存在则创建；HTML 未变化则复用原节点；
4. HTML 变化时捕获 details/open 和 OUT expanded 状态后替换；
5. 用 `insertBefore` 调整顺序；
6. 删除目标集合外的尾部/旧节点；
7. 清理 HTML cache。

HTML 只进入离屏 `<template>` 创建单个 renderer 已生成节点，不接受未经 Markdown/escape 路径处理的业务字符串。

## 5. 滚动策略

实现位置：`gui/frontend/dist/conversation-view.js:97-117`。

- `auto`：更新前用户距底部不超过 72px 才跟随；
- `bottom`：新建/恢复/主动提交后强制到底部；
- `preserve`：保持当前 scrollTop；
- `anchor`：prepend 历史后加上 scrollHeight 增量，视觉锚点不跳。

scroll listener 只更新 `followsTail` 本地状态，不写 Core。

## 6. Chat 控件与活动尾部

实现位置：

- `gui/frontend/dist/chat-view.js:3-31`；
- `gui/frontend/dist/components.js:68-84`。

ChatView 负责空状态、会话 model 和 composer controls。运行中 send 不禁用，而是改为“加入队列”；stop 只在 running 时显示。活动尾部展示 WORKING 动效和真实 `input_queue` 卡片。

## 7. Markdown 与 think

实现位置：`gui/frontend/dist/markdown.js:6-279`。

支持：标题、段落、引用、列表/任务列表、表格、分隔线、围栏代码、行内代码、强调/删除线、链接和图片。

安全顺序：

1. 原始 NUL 替换，避免 token 占位碰撞；
2. 代码/链接转为内部 token；
3. 普通文本统一 HTML escape；
4. 只恢复 renderer 自己生成的 token；
5. URL 仅允许 http、https、受限 mailto 和相对路径；拒绝 javascript/data。

`<think>` 只在 block 起始识别：

- 已闭合：生成默认折叠 `<details>`；
- 未闭合：生成 open + LIVE 的流式块；
- fenced code 中的 think 标签保持纯代码文本。

## 8. 局部 UI 状态

替换 keyed node 前捕获：

- 节点内所有 details 的 open 序列；
- `.io-panel.expanded` 的 payload key 集合。

替换后按相同 details 顺序恢复，并从最新 payload Map 恢复完整 OUT。滚动、展开、复制都属于客户端交互态，不进入 Snapshot。

## 9. 自动化证据

- `components.test.mjs:9-51`：activity、队列安全 Markdown、稳定 message/tool keys。
- `markdown.test.mjs:9-58`：block/inline、表格、XSS、code、closed/live think。
- `protocol/client-state` tests 间接保证 message/tool model 输入稳定。
- 真实 DOM reconciliation 尚无 jsdom/WebView 自动测试，是当前非阻塞缺口。

## 10. 审查清单

- 新 renderer 字段是否 escape 或经过安全 Markdown？
- 稳定 key 是否来自业务实体 ID？
- 大输出是否只默认进入受限 preview？
- 替换节点是否保存必要局部状态和滚动？
- Markdown 新语法是否覆盖危险 URL、原始 HTML和代码边界？
