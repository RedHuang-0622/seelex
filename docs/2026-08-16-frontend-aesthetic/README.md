# Seelex 前端审美体系探索：Tracebench 轨迹台架

> 状态：**已应用（2026-08-16）**。核心 token 体系、轨迹签名、字号下限与界面词统一已落地 `gui/frontend/dist`（`styles.css`/`index.html`/`chat-view.js`/`components.js`/`app.js`），并同步更新 [`gui/frontend/README.md`](../../gui/frontend/README.md) 与契约测试。弹窗焦点管理、自定义确认、窄窗抽屉、diff/文件预览仍未实现（见「落地建议」第 5 条），后续改造完成后本文保留为设计决策记录。

## 1. 定位

主题：本地 Agent 工程**验证台架**。用户的工作不是聊天，而是观察并干预一次可核验的工程执行。界面用测量仪器的语言组织：会话是连续轨迹（timebase），Plan 是路线图，工作表格是台账，状态是刻度上的打点。

一句话审美主张：**铁蓝石墨 + 暖纸白 + 黄铜信号灯，一切事件都打在一条时间基线上。**

## 2. 与现状的差异

| 维度 | 现状 | Tracebench |
|---|---|---|
| 底色 | 近黑 `#0c0f0e` + 绿色品牌 | 铁蓝石墨 `#141b21` + 黄铜主信号 `#d9a657` |
| 状态语义 | running 琥珀 / done 绿 | 保持 running 琥珀、done 铜绿，主操作/主信号改为黄铜 |
| 字体 | 系统 UI + mono 泛滥 | 三角色：display（Bahnschrift 类测量字）/ body / mono，字号下限上调 |
| 布局 | 三栏卡片堆叠 | 中栏时间基线（trace rail），事件挂线打点 |
| 动效 | 只留 spinner | 唯一动效：运行中一步的黄铜扫掠（reduced-motion 关闭） |
| 文案 | READY / FA / QUEUED 混排 | 统一中文界面词：就绪 / 全权 / 排队 |

## 3. Token 体系

### 色板（核心 6 色）

| token | 值 | 角色 |
|---|---|---|
| `--ink` | `#141b21` | 铁蓝石墨底，非纯黑 |
| `--bench` | `#1b242c` | 面板 |
| `--paper` | `#e9e4d8` | 暖纸白正文 |
| `--brass` | `#d9a657` | 主信号 / 主操作 / 运行中 |
| `--verdigris` | `#6fb58e` | 成功 |
| `--brick` | `#d4695f` | 失败 |

补充：`--steel #7ba3c7`（信息）、`--idle #7e8b90`（空闲）、`--tick #39454f`（刻度线）。

### 字体三角色

| 角色 | 字体 | 用途 |
|---|---|---|
| display | Bahnschrift → Space Grotesk（可嵌入）→ Arial Narrow | 品牌、标签、大数字；拉丁字母可用 tracking，CJK 用重量不用字距 |
| body | Segoe UI Variable + PingFang SC / Microsoft YaHei | 会话正文、表单 |
| mono | Cascadia Mono → JetBrains Mono → ui-monospace | 轨迹、时间戳、工具 IN/OUT、台账数字（tabular-nums） |

字号下限：数据 ≥10.5px，正文 ≥13.5px，标签 12px；废弃现状的 9.5px 数据字。

### 签名

**轨迹刻度 + 黄铜信号灯**：中栏一条垂直时间基线，每条消息/工具/Plan 节点是一个刻度打点；运行中一步是界面唯一动态（黄铜扫掠），完成即“盖章”。签名同时承担信息功能（执行顺序与当前位置），不是装饰。

## 4. 自批评记录

流程要求先排除模板化默认方案：

1. 近黑 + 荧光绿（现状，AI 工具默认聚类）→ 改为铁蓝石墨 + 黄铜 + 铜绿/砖红，保留状态语义但品牌不撞车。
2. 暖奶油底 + 衬线（另一个默认聚类）→ 暖纸白只用作文本色，不翻成背景。
3. 报纸式发丝线密集排版 → 只保留发丝线，但用“时间基线”承载层级，避免纯密度堆砌。
4. 黄铜容易滑向蒸汽朋克 → 只允许黄铜出现在主操作、活动状态、刻度热点；表面全部平涂，无辉光/斜切/装饰。
5. 中文字体约束 → display 角色只作用于拉丁/数字/标签，中文标题用重量与留白分层，不做 letter-spacing 游戏。

## 5. 落地建议（若采纳）

1. 先在 `dist/styles.css` 替换 `:root` token，保持组件结构不变，逐屏核对对比度与状态语义；
2. 中栏加入 trace rail（纯 CSS：`::before` 刻度 + 1px 垂直线），keyed reconcile 不受影响；
3. 文案统一为中文界面词（就绪/排队/全权/追加到队列），同步 `chat-view.js` 与 aria-label；
4. 字号下限修正（9.5px → 10.5px 起），工作表格行高与 tabular-nums 对齐；
5. 弹窗焦点管理、自定义确认、窄窗抽屉属交互改造，与审美体系并行推进；
6. 补 WebView 截图基线（Windows/macOS/Linux）与视觉回归，避免只靠单元测试。
