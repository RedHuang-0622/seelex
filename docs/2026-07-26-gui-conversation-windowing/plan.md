# GUI 对话区滑动窗口虚拟滚动方案

## 一、设计目标

1. **限制 DOM 节点数**：对话区内最多同时存在 ~70 个直接渲染项（含缓冲），避免长会话 DOM 膨胀。
2. **HTML 持久缓存**：所有消息的渲染结果（Markdown 转 HTML）在内存中永久缓存，窗口平移时不再重新解析 Markdown。
3. **视觉连续性**：窗口平移期间用户感知不到 scrollTop 突变或布局闪烁。
4. **尾部跟随优先**：流式增量（新消息到达）时窗口锁定在尾部，不触发不必要的平移。
5. **保持现有接口**：`conversationView.render(model, { scrollMode })` 接口签名不变，调用方无感知。
6. **渐进增强**：在现有 reconcile 基础上叠加窗口层，可逐项测试。

## 二、设计模式选择

| 模式 | 应用位置 | 理由 |
|------|---------|------|
| Virtual Scroller (Fixed Window) | `conversation-view.js` | 限制 DOM 节点数；聊天场景符合 append-heavy 模式 |
| Presentation Model (扩展) | `components.js` | 保留并扩展现有 render model，增加高度元数据 |
| HTML Cache Store | `components.js` | 所有项目的 HTML 字符串持久缓存，避免重复 Markdown 解析 |
| Spacer-based Scroll Compensation | `conversation-view.js` | 用空 div 补偿滚动高度，避免 ResizeObserver 复杂度 |
| Tail Locking | `conversation-view.js` | 尾部追加时禁止窗口平移，保持 isAtBottom 语义 |

## 三、架构设计

### 3.1 容器结构

```text
.conversation (overflow-y: auto, scroll container)
 ├── <div data-window-spacer="top">       ← spacer: height = 已溢出上方所有不可见项的总高度
 ├── <div data-window-viewport>            ← 视口：仅含当前窗口内的项
 │    ├── [item -overhang]                 ← 缓冲项（窗口上方，pre-rendered）
 │    ├── [item windowStart]
 │    ├── [item ...]
 │    ├── [item windowStart + windowSize]
 │    └── [item +overhang]                 ← 缓冲项（窗口下方，pre-rendered）
 ├── <div data-window-spacer="bottom">    ← spacer: height = 已溢出下方所有不可见项的总高度
 └── <div data-activity-tail>             ← WORKING/Queue 指示器（永远渲染）
```

### 3.2 核心数据结构（`ConversationWindow` 类）

```js
class ConversationWindow {
  // === 配置 ===
  windowSize = 50;        // 渲染窗口大小（项数）
  overhang = 10;          // 上下各多渲染的缓冲项数
  minWindow = 15;         // 消息数少于该值时始终全渲染

  // === 全量数据引用 ===
  fullModel = null;       // 最新的完整 render model（包含所有 items + payloads）
  
  // === 缓存 ===
  htmlCache = new Map();  // key → { html, estimatedHeight, measuredHeight, renderedAt }
  payloadCache = null;    // 当前 payloads Map 引用

  // === 窗口状态 ===
  windowFirst = 0;        // fullItems 中窗口起始索引
  isAtBottom = true;      // 是否跟随尾部
  scrollMode = "auto";    // 当前滚动模式

  // === DOM 引用 ===
  container = null;       // .conversation 滚动容器
  topSpacer = null;       // data-window-spacer="top"
  viewport = null;        // data-window-viewport
  bottomSpacer = null;    // data-window-spacer="bottom"
  tailNode = null;        // data-activity-tail (由外面管理)
}
```

### 3.3 数据流

```text
旧链路（当前）：
Snapshot → renderConversationModel(all messages) → renderMessage/renderToolCall (Markdown) → reconcile(DOM)

新链路（窗口化）：
Snapshot → renderConversationModel(all messages) → 1. 缓存 HTML（如命中直接复用）
                                                     2. 计算窗口位置
                                                     3. 仅渲染窗口内的项 → reconcile(DOM 视口)
                                                     4. 更新 spacers
```

## 四、关键行为设计

### 4.1 全量刷新 `render(fullModel, options)`

```
输入：renderConversationModel(messages, chat) 的输出
     { items: [{key, html, kind}], payloads: Map }
     
步骤：
1. fullModel = latestModel
2. htmlCache 增量合并：
   - 对每个 item：key 已存在且 html 一致 → 跳过
   - key 不存在或 html 变化 → 存储 { html, estimatedHeight, measuredHeight: null }
3. 计算 windowFirst：
   - 如果 isAtBottom → windowFirst = max(0, items.length - windowSize - overhang)
   - 否则保持当前 windowFirst
4. 如果 items.length <= windowSize + 2*overhang → 全量渲染（跳过窗口逻辑）
5. 计算 spacers：
   - topHeight = sum(fullModel.items[0..windowFirst-overhang-1].height)
   - bottomHeight = sum(fullModel.items[windowFirst+windowSize+overhang..].height)
6. 截取窗口项 = fullModel.items.slice(windowFirst - overhang, windowFirst + windowSize + overhang)
7. 窗口项输入 reconcile（复用现有 keyed DOM 协调）
8. 更新 topSpacer.style.height / bottomSpacer.style.height
9. 恢复滚动位置（见 4.5）
```

### 4.2 HTML 缓存层

```js
// 位于 components.js，或 conversation-view.js 内部

class HtmlCache {
  constructor() {
    this.map = new Map();  // key → CacheEntry
  }

  getOrRender(key, renderFn) {
    const cached = this.map.get(key);
    if (cached && cached.html === renderFn.cachedKey) return cached.html;
    const html = renderFn();
    this.map.set(key, { html, measuredHeight: null, estimate: 80 });
    return html;
  }

  // 首次渲染后从 DOM 测量实际高度
  measure(key, height) {
    const entry = this.map.get(key);
    if (entry) entry.measuredHeight = height;
  }
}
```

高度估算规则：

| ite
m.kind | 默认估算高度 | 说明 |
|-----------|-------------|------|
| `message` | content.length / 80 * 22 + 48 | 每行 ~80 字符，行高 22px + 头部 48px |
| `tool` | 180 | 固定：头部 38 + IO 面板各 56 + 边框 |
| `tool (expanded)` | 480 | 展开后 IO 面板更大 |
| `chat:activity` | 40 | 活动尾部 |

> 估算值只在首次渲染前使用；`measure()` 会在 DOM 更新后替换为真实高度。

### 4.3 窗口平移

#### 触发条件

- 用户向上滚动，`topSpacer` 剩余高度不足 `overhang * avgItemHeight` → 窗口上移
- 用户（在非尾部状态下）向下滚动，`bottomSpacer` 剩余高度不足 → 窗口下移
- 新消息到达且 `isAtBottom` 为 true → 窗口不移，新追加自然进入窗口底部

#### 平移算法

```
shiftWindow(direction):
  if isAtBottom: return  // 尾部状态不手动平移
  oldFirst = windowFirst
  
  if direction === "up":
    shift = min(overhang, windowFirst)  // 向上移 overhang 个项
    windowFirst -= shift
  else if direction === "down":
    shift = min(overhang, items.length - windowFirst - windowSize)
    windowFirst += shift
    
  if windowFirst === oldFirst: return
  
  // 1. 重新计算 spacers
  updateSpacers()
  
  // 2. 重新渲染窗口内的项（从 htmlCache 直接取，不做 Markdown）
  windowItems = fullModel.items.slice(windowFirst - overhang, windowFirst + windowSize + overhang)
  reconcile(viewport, windowItems)
  
  // 3. 测量新窗口项的 DOM 高度
  measureVisibleHeights()
  
  // 4. 补偿 scrollTop
  compensation = sumHeight(oldFirst, windowFirst)  // 旧窗口到新窗口之间项的高度差
  container.scrollTop -= compensation  // 因为窗口上移了，scrollTop 要减小以保持视觉位置
```

### 4.4 尾部跟随

```js
// isAtBottom 判定（保持不变）：
isNearBottom(container):
  container.scrollHeight - container.scrollTop - container.clientHeight <= BOTTOM_THRESHOLD

// 尾部状态下新消息处理：
if isAtBottom:
  windowFirst = max(0, items.length - windowSize - overhang)
  // 不需要补偿 scrollTop — 用户一直在看底部
  container.scrollTop = container.scrollHeight
```

尾部状态是滑动窗口性能的关键优化：在流式输出期间，`isAtBottom` 始终为 true，窗口自动保持在尾部，不触发任何平移计算。只有新增消息超出 `windowSize` 时，`windowFirst` 向前推进，但顶部 spacer 缩小的同时容器 scrollTop 已位于底部，用户无感知。

### 4.5 滚动位置恢复

| scrollMode | 行为 |
|-----------|------|
| `auto` | `before.followsTail` → 滚动到底部；否则保持 |
| `bottom` | 强制滚动到底部 + 设置 `isAtBottom = true` |
| `preserve` | 保持当前 scrollTop，不干预窗口位置 |
| `anchor` | 用于历史加载：`scrollTop = before.top + (scrollHeight - before.height)` + 补偿窗口起始变化 |

新增逻辑：窗口平移后需要额外 `scrollTop` 补偿：
```js
// 在普通恢复之后追加
if (windowFirst !== beforeWindowFirst) {
  const compensation = sumHeightsOfRange(fullItems, beforeWindowFirst, windowFirst);
  container.scrollTop += compensation;
}
```

### 4.6 历史加载（`LoadMoreHistory`）

历史加载时，`fullItems` 头部插入新的历史消息，`windowFirst` 增加（因为前面多了项），同时 `topSpacer` 高度增大。

```
历史加载后：
1. fullItems = [...newHistoryItems, ...oldFullItems]
2. windowFirst += newHistoryItems.length  // 保持当前可见项索引不变
3. topSpacer 高度自然增加（因为前面多了不可见的历史项）
4. scrollMode = "anchor" → scrollTop += scrollHeight 增量
```

### 4.7 增量更新（Delta）

当前增量路径（`message.delta`, `tool.started`, `tool.completed`）通过 `chatView.renderConversation` 进入窗口管理器。

窗口管理器检测：
- `model.items.length` 与缓存中 `fullModel.items.length` 对比
- 新增项 → 追加到 `htmlCache` + 如果窗口在尾部 → 直接追加 DOM
- 变更项 → 更新 `htmlCache` + 替换对应 DOM 节点（现有 reconcile 逻辑）
- 删除项 → 从 `htmlCache` 删除 + 从 DOM 移除

## 五、与现有代码的兼容性

### `createConversationView` 接口

```js
// 当前（保持不变）：
export function createConversationView(container, options = {})

// 返回对象接口（增加但不破坏）：
{
  render(model, options = {}),   // 不变，内部已窗口化
  payload(key),                   // 不变
  // 新增调试/配置接口：
  getStats(),                     // 返回 { renderedCount, cachedCount, windowFirst, isAtBottom }
  reconfigure(opts)               // 运行时调整 windowSize/overhang
}
```

### Event delegation

当前 `container.addEventListener("click", handleAction)` 在容器级绑定，不受窗口平移影响 — 事件冒泡始终有效。

### 局部 UI 状态

`captureUIState` / `restoreUIState` 逻辑从 per-node 改为 per-window：
- 窗口平移时，移出窗口的节点的 details/expanded 状态存入 `itemCache`（key → state）
- 移入窗口的节点从 `itemCache` 恢复状态（如果是已有 HTML 缓存但刚从 DOM 移出过的节点）

## 六、实现步骤

| # | 步骤 | 文件 | 关键变化 |
|---|------|------|---------|
| 1 | 在 `components.js` 增加 `HtmlCache` 类和 `heightEstimates` | `components.js` | 新增 `createHtmlCache()`, `estimateItemHeight(kind, content)`, `measureItemHeight(domNode)` |
| 2 | 扩展 `renderConversationModel` 输出 | `components.js` | items 增加 `kind` 和 `estimatedHeight` 字段，不破坏现有消费者 |
| 3 | 在 `conversation-view.js` 重写 `createConversationView` | `conversation-view.js` | 加入 `ConversationWindow` 类，spacer DOM，窗口逻辑 |
| 4 | 窗口平移逻辑实现 | `conversation-view.js` | `shiftWindow()`, `updateSpacers()`, `computeWindowFirst()` |
| 5 | 滚动补偿实现 | `conversation-view.js` | `compensateScroll()`, `restoreScroll()` 扩展 |
| 6 | 增量更新适配 | `conversation-view.js` | `appendDelta()` 方法 |
| 7 | `chat-view.js` 适配 | `chat-view.js` | 无侵入调整，验证接口 |
| 8 | 测试 | `components.test.mjs`, `conversation-view.test.mjs` | 高度估算、窗口平移、scroll 补偿、HTML 缓存、delta 追加 |
| 9 | 性能基准 | 手动测试 | 对比 100/200/500 条消息场景的 DOM 节点数、JS 堆、帧率 |

## 七、还原方案

1. 恢复 `conversation-view.js` 到当前版本（简单回滚）。
2. 恢复 `components.js` 中 `renderConversationModel` 的原始输出（去掉 kind 和 estimatedHeight）。
3. `chat-view.js` 和 `app.js` 无外部接口变化，无需回滚。

## 八、测试策略

### 单元测试（Node, `*.test.mjs`）

| 测试 | 文件 | 覆盖内容 |
|------|------|---------|
| `HtmlCache.getOrRender` 缓存命中 | `components.test.mjs` | 相同 key 应返回缓存，不调用 renderFn |
| `HtmlCache.measure` 更新高度 | `components.test.mjs` | measure 后高度反映到 getHeight |
| 高度估算 | `components.test.mjs` | 空消息/大文本/tool 的估算值合理性 |
| `ConversationWindow` 窗口起始计算（尾部） | `conversation-view.test.mjs` | isAtBottom 时自动定位到尾部 |
| `ConversationWindow` 窗口起始计算（非尾部） | `conversation-view.test.mjs` | 非尾部保持窗口位置 |
| 窗口平移（上移） | `conversation-view.test.mjs` | windowFirst 减小，spacer 更新 |
| 窗口平移（下移） | `conversation-view.test.mjs` | windowFirst 增大，spacer 更新 |
| scrollTop 补偿 | `conversation-view.test.mjs` | 平移后 scrollTop 保持视觉位置不变 |
| 增量追加 | `conversation-view.test.mjs` | isAtBottom 时新消息追加到底部 |
| 历史加载头部插入 | `conversation-view.test.mjs` | items 前插后 windowFirst 偏移正确 |
| 局部 UI 状态保持 | `conversation-view.test.mjs` | 窗口内外移时 details/expanded 保持 |

### 手动/视觉测试

- 空对话 → 单条消息 → 多工具调用 → 长代码块消息（Visual 验证无布局断裂）
- 快速上下滚动（帧率稳定，无白屏/跳动）
- 流式输出（增量消息逐步到达，窗口平滑跟随）
- 加载历史后再滚动（锚点正确）
- 50/100/200/500 条消息场景下的 DOM 节点计数

## 九、性能目标

| 指标 | 当前 | 目标 |
|------|------|------|
| 500 条消息的 DOM 节点数 | ~5000+ | <800 |
| 500 条消息的 JS 堆 (messages+tool calls) | 含大量 innerHTML | < 1MB HTML cache |
| 增量消息到达的渲染时间 (100条时) | 遍历所有 + Markdown | O(1) — 仅缓存读取 |
| 用户从底部到顶部滚动 | 所有节点渲染 | 窗口逐步平移，~60 节点变化 |
