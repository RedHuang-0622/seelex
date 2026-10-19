# 会话内 HTML 渲染块不再闪屏：帧身份 + 单/双缓冲（2026-10-19）

日期：2026-10-19
范围：`gui/frontend/dist/{html-embed,markdown,components,chat-view,conversation-view,styles}.{js,css}` + 新增 `gui/frontend/dist/embed-buffering.js`
方案书：`docs/2026-10-19-three-defect-fixes/README.md` 第 3 节（方向 B）

## 修了什么

**根因**：`conversation-view.js` 的 `reconcile()` 只要某行的 html 字符串变了就 `elementFromHTML()` + `replaceWith()` 整行替换。行内 `<iframe sandbox srcdoc>` 必然被销毁重建，新帧加载完成前那块区域是空的；流式输出期间 html 每帧都变 ⇒ 每帧重建 ⇒ 连续闪屏。**不是"没做缓冲"这么简单：连"这一帧还是不是上一帧"都认不出来。**

四层修法（全部在前端渲染层，Go 侧零改动）：

1. **帧身份**：`markdown.js` 给每条消息内第 n 个 `seelex-html` 围栏一个稳定 key `<行 key>#<n>`（无行 key 时 `html-embed.js` 退回内容哈希 `content#<hash>`），落到 `data-embed-key`（图 / 落位台 / 帧三处同 key）。
2. **帧领养**：换行**之前**把旧行里的帧按 `(key, srcdoc)` 交给 `embed-buffering.js`；srcdoc 完全一致的帧原样搬进新行，不重新加载。
3. **缓冲挂载（单缓冲）**：内容真变了才在离屏容器 `.html-embed-mount` 里建新帧 → 赋 srcdoc → 等 `load` + 一帧 rAF → 换进可视位。交换前旧帧仍在原位，落位台 `.html-embed-stage` 用 `--embed-height` 兜高度（不塌陷、不露白）。
4. **流式延迟挂载 + 双缓冲开关**：会话还在跑（`chat-view.js` 传 `chat.running`）时**一帧都不新建**，只留同高占位（`data-embed-pending="1"`）；定稿或 `settleMs≈150ms` 无变化后一次挂上。`createEmbedFrameBuffer({ doubleBuffer: true })` 才启用两层绝对定位重叠 + opacity 过渡，默认关。

防串：每次挂载带自增 token，`load` 回来 token 过期就丢弃并卸载；行被移出时撤在途帧、摘监听；在途帧并发上限 4，超出排队。

**沙箱口径一字未改**：`sandbox="allow-scripts"`（无 `allow-same-origin`）、`referrerpolicy="no-referrer"`、srcdoc 内嵌 CSP、不给 forms/popups/top-navigation 全部保持。缓冲层只做搬运与克隆（`cloneNode(false)` 复制属性），唯一改动的属性是离屏副本的 `loading`——`lazy` 在离屏容器里可能永不触发加载，那样 `load` 不来、帧永远挂不上。

## 怎么看是否还闪

1. 让模型输出一个含 `seelex-html` 围栏的回答，且围栏内容**逐帧变化**（例如边算边改的图表）。看那块卡片：变成"虚线占位 → 一次性出现完整图"，**不再**出现"空白 ↔ 有内容"的连续跳动。
2. 定稿后改一下正文别处（比如让同一行里别的文字变化触发换行）：图**不该重载**（滚动位置、帧内动画、鼠标悬停态都还在）。
3. 打开 devtools 元素面板盯住该 `iframe`：不再每帧出现新节点即修好；播放动画期间 `iframe` 节点数应恒为 1。
4. 长会话向上翻历史（历史窗口回退会移除行）：离屏容器 `.html-embed-mount` 里不该堆积孤儿 `iframe`（`document.querySelectorAll(".html-embed-mount iframe").length` 应回落到 0）。

## 证据

- `node --test gui/frontend/dist/*.test.mjs` —— 805 项全绿（含新增 `embed-buffering.test.mjs` 15 项、`html-embed.test.mjs` +2 项、`markdown.test.mjs` +1 项）。
- `node --check` 覆盖 `embed-buffering.js` / `html-embed.js` / `markdown.js` / `components.js` / `chat-view.js` / `conversation-view.js`。
- **未做浏览器实测**（本地无浏览器自动化）：观感类结论（"还闪不闪"）需人眼按上面四条确认；重挂/领养的时序由 `embed-buffering.test.mjs` 的最小 DOM 假体覆盖。

## 已知边界

- 领养靠"把旧帧搬进新行"，搬移会不会在某个引擎里顺便重建帧由引擎决定；**最坏情况退化为与新行重建同价（不会更差）**，而"流式期间零挂载"这一层与搬移无关，闪屏的重复跳动已被它消掉。
- 单缓冲换帧时旧帧在同一帧任务里被撤下，理论上存在一帧级别的空白；需要更平滑的过渡再开双缓冲，两者都是开关。
