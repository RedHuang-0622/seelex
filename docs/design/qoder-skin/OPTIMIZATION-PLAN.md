# Qoder 皮肤 · 优化方案

> 上游：`DESIGN-LANGUAGE.md`（设计语言确认稿）。本文只回答一件事：**按那套语言，`styles.css` 要改哪几行、按什么顺序、怎么验收。**
> 立场：每条改动都指到**具体选择器**，并给"改完怎么证明改对了"。

---

## 0. 用户原话 → 可判定判据

| 用户说 | 判据（可验收） | 现状 | 优先级 |
|---|---|---|---|
| 「渐变色从左侧框去到顶栏，很通透」 | 壳承载 `--shell-gradient`；左栏顶 `#EBEBC6`、底 `#D1DAE2`、横向差 0 | **完全没有**（三块平色） | **P0** |
| 「圆角配上一个黑色的下外框真的丑爆了」 | 全库不存在"圆角 + 深色下边/影"的组合 | 至少 5 处 → **已清零**（2026-09-24，见 P0-2） | **P0** |
| 「不能出现过圆角」 | 见设计语言 3.3 四条判据 | 33× `999px`、4× 圆角页签、10 处非 token 半径 | P1 |
| 「不合时宜的阴影」 | 全库"有影的元素"≤ 3 类；影参数满足 4 条质量约束 | 70 处 `box-shadow`，含黑影 2 处 | P1 |
| 「减少不必要的线框，保留文件树里必要的线框」 | 装饰线清零；结构线/数据线保留 | `border-bottom` 39 处待分类 | P1 |
| 「左侧栏的对话列表做的就很好」 | **冻结区，不动** | — | 冻结 |

---

## P0-1　装上环境渐变（✅ 已完成 2026-09-24）

### 现状（证据）

```css
/* styles.css 第 27 节 a)/b) —— 三块平色，没有任何渐变 */
.workspace   { background: var(--surface); }
.left-panel,
.right-panel { background: var(--bg); }   /* #f7f6f2 平色 */
.topbar      { background: var(--panel); backdrop-filter: none; }  /* #fbfaf6 平色 */
```

用户欣赏的那条渐变**一行都没有落地**。这是本方案的第一优先级。

### 改法

**1) `:root` 新增两个契约 token**

```css
/* 壳的环境渐变：等亮度的色温扫（配方与判据见 DESIGN-LANGUAGE.md 1.3） */
--shell-gradient: linear-gradient(180deg, #EBEBC6 0%, #D1DAE2 100%);
/* 两种接缝线：面板边界（中性）与输入面描边（中性） */
--border-hairline: #E6E6E6;
--border-field: #DDDDDD;
```

**2) 壳承载渐变，纸盖上去**

```css
.app-shell { background-image: var(--shell-gradient); background-color: #EBEBC6; }

.topbar,
.left-panel,
.right-panel { background: transparent; }   /* 透出壳的渐变 */

.workspace { background: var(--surface); }   /* 白纸，保持不变 */
```

**3) 接缝线换成中性发丝线**（实机是 1px `#E6E6E6`，不是暖色）

```css
.topbar { border-bottom: 1px solid var(--border-hairline); }
.left-panel { border-right: 1px solid var(--border-hairline); }
.right-panel { border-left: 1px solid var(--border-hairline); }
```

**4) 皮肤契约同步**（`themes/README.md` 的契约清单已按两轴改写，用户口径「皮肤和深浅色分开来做」）

| 组 | 新增 token |
|---|---|
| 面 / 接缝（**深浅轴**，`styles.css`） | `--shell-gradient`（浅/深各一份）`--border-hairline` `--border-field` |
| 品牌（**皮肤轴**，`themes/<skin>.css`） | `--skin-gradient-light` `--skin-gradient-dark` + 主信号 6 个 `--skin-*` |

- 深浅轴在 `styles.css`：`:root`（浅）与 `:root[data-theme="dark"]`（深）各给一份中性基座；
- 皮肤轴在 `themes/<skin>.css`：每套皮肤给**深浅各一条**环境渐变（qoder 抄设计语言 1.3 的配方，
  其余四套按各自主色相做等亮度扫，见 `README.md` 的皮肤表）；
- `--shell-gradient` 由 styles.css 桥接：浅色取 `--skin-gradient-light`，深色取 `--skin-gradient-dark`；
- 已全部落地（本方案 P0-1 于 2026-09-24 完成）。

**5) 不要动 `gui/run_wails.go` 的 `BackgroundColour`**？——要动。窗底应设为渐变起点色 `#EBEBC6`，
否则首帧仍会闪一下旧底色。

### 验收

```bash
python docs/design/qoder-skin/tools/measure-skin.py gradient <截图> 270 4 1054
# 期望：顶端 #EBEBC6 ±2；底端 #D1DAE2 ±2；横向最大通道差 ≤2；R² ≥ 0.99；L 全程波动 <2%
```

外加：顶栏取色与左栏顶端色差 ≤2（"同色相接"成立 → 才有"从左侧框去到顶栏"的连续感）。

---

## P0-2　拆掉「圆角 + 深色下外框」

### 现状（证据 · 5 处，全部可复现）

| # | 选择器 | 丑在哪 |
|---|---|---|
| 1 | `.right-tab.is-active` | `border-radius: r-md r-md 0 0` **＋** `box-shadow: 0 3px 8px rgba(0,0,0,.30)` ＋ `border-color: color-mix(accent 65%…)` ＋ 深色渐变填充 |
| 2 | `.right-tab.is-active::after` | 圆角页签下方再挂一个**深色三角缺口**，自己还带 `0 2px 4px rgba(0,0,0,.25)` |
| 3 | `.right-tab.is-active::before` | 页签顶部一条半透明白高光（拟物玻璃感残留） |
| 4 | `.conversation-tab.is-active, .right-tab.is-active`（第 27 节 j） | `border-radius: r-md r-md 0 0` ＋ `box-shadow: inset 0 -2px 0 var(--accent)` = **圆角 + 2px 黑下框** |
| 5 | `.schedule-pill.is-next` | `border-radius: 999px`（全胶囊）**＋** `inset 0 -2px 0 accent55` = 圆球上贴一条黑底边 |

> 为什么 P0-2 里的 1/2/3 没被第 27 节盖掉：27 节 j) 只重写了 `box-shadow`。
> `::after`、`::before`、`border-color` 三条是**没被覆盖的残留**。

### 改法：页签类元素二选一，不许两个都要

**统一到「胶囊填充」**——因为用户已经认可了会话列表的胶囊选中态，全库只留**一种**选中态：

```css
/* 删除这些： */
.right-tab.is-active { background-image: none; border-color: transparent; box-shadow: none; }
.right-tab.is-active::after  { content: none; }   /* 深色三角，删 */
.right-tab.is-active::before { content: none; }   /* 假高光，删 */

/* 页签不再有圆角与下框，改成与会话行同款的胶囊填充： */
.conversation-tab, .right-tab { border: 0; border-radius: var(--r-md); }
.conversation-tabs, .right-tabs { border-bottom: 0; }   /* 通栏横线一并去掉，见 P1-3 */
.conversation-tab.is-active,
.right-tab.is-active {
  background: var(--surface-2);
  color: var(--text-bright);
  font-weight: 600;
  box-shadow: none;
}
.schedule-pill.is-next { box-shadow: none; background: var(--surface-2); }
```

**验收**：全文件搜索 `inset 0 -2px 0`、`inset 0 -1px 0`、`rgba(0, 0, 0` 三个串，
在**带圆角**的规则里命中数必须为 **0**（加一条自检脚本，见 P2）。

### 落地（✅ 2026-09-24）

1/2/3/4 在第 27 节 j) 收口（页签 = 静止胶囊：同色系薄底 `--hl-fill` + 同色系重字
`--hl-ink`，描边 / 下条 / 投影 / 位移一律不画）；第 5 处与它同一族的另两处（**工作表页签**
`.excel-sheet.is-active` 的 `3px 3px 0 0` + `inset 0 -2px 0 status-running`、**终端页签**
`.terminal-tab.is-active` 的 `inset 0 -1px 0 accent`）在第 27 节 o) 收口，旧声明直接从源头
删掉（不留"被后写覆盖"的残留——P0-2 的 1/2/3 当初就是这么漏掉的）。

自检结果（2026-09-24，`styles.css` 全文）：

```
inset 0 -2px 0 -> 0 处        inset 0 -1px 0 -> 0 处
rgba(0, 0, 0 -> 3 处，全部在 :root[data-theme="dark"] 的 --overlay / --shadow / --shadow-sm
              （token 定义，不在带圆角的规则里；token 本身的改写见 P0-3）
```

---

## P0-3　重写阴影 token：现在的 `--shadow-sm` 本身就是一条"黑底边"

### 现状

```css
--shadow:    0 14px 34px rgba(24,28,36,.14), 0 2px 6px rgba(24,28,36,.06);
--shadow-sm: 0 1px 2px rgba(24,28,36,.05), 0 8px 20px rgba(24,28,36,.07);
```

`0 2px 6px` / `0 1px 2px` 这两段：**偏移 ≤2px、模糊 ≤6px**——按设计语言第 4 节的判据，
这不是"高度"，而是**在元素下方贴了一条深色描边**。用户说的"黑色下外框"，token 层也有一份。

### 改法（满足"深而不黑"四条：同色温 / α≤10% / 模糊≥16px / y≤4px）

```css
/* 颜色改成与暖壳同色温的中性，不用纯黑 */
--shadow:    0 4px 20px rgba(38, 36, 30, .09), 0 1px 4px rgba(38, 36, 30, .05);
--shadow-sm: 0 2px 16px rgba(38, 36, 30, .07);
```

### 逐条处置（70 处 `box-shadow` 里的问题项）

| 选择器 | 现状 | 处置 |
|---|---|---|
| `.composer` | `var(--shadow-sm)` | **删**（实机 composer 是 0 影 + 1px `#DDDDDD` 描边） |
| `.right-tab.is-active` / `::after` | 黑影 ×2 | **删**（P0-2） |
| `.topbar` | `inset 0 1px 0 var(--text-bright) 4%` | 改成 `border-bottom: 1px solid var(--border-hairline)` |
| `.brand-mark` | `0 0 0 1px accent35, 0 6px 18px accent18` | **删光晕**（Qoder 的 brand 是平的） |
| `.primary-button` / `.app-shell .icon-button.primary-button` | `0 6px 16px accent22` / `inset…, var(--shadow-sm)` | 删源规则（第 27 节 h 已用 `!important` 兜住，但源头该清） |
| `.history-button` `.team-editor` `.empty-state .orb` | `var(--shadow-sm)` | 删 |
| `.resize-pill` | `var(--shadow-sm)` | 留（拖拽把手，浮起是语义） |
| `.ui-tooltip` `.modal-card` `.session-menu` `.inline-suggestions` `.toast` | `var(--shadow)` / `var(--shadow-sm)` | 留（真浮层），但换新 token |
| `.file-pdf-canvas` | `var(--shadow)` | 减轻为 `var(--shadow-sm)`（纸的厚度感，不要悬浮感） |
| `.excel-sheet.is-active` | `inset 0 -2px 0 status-running, var(--shadow-sm)` | 去 `var(--shadow-sm)`；下框按 P0-2 处理 |
| `.file-preview-chip.is-active` `.trajectory-row.is-flash` | `inset 0 0 0 1px var(--accent)` | 黑内描边 → 改 `background: var(--surface-2)` |
| `.theme-card.is-active` `.session-row.is-pinned .session-title` | `inset 2px 0 0 var(--tick-hot/accent)` | 左侧信号条 → 统一到胶囊填充 |
| `.wheel-track` `.code-pane-tab.is-active` `.right-tab.is-drag-over` | 语义描边 | 保留（状态色，非装饰） |
| `.scroll-shadows` `.scroll-edges-x` `.axis-segment` | `inset ±10px …` | 保留（滚动暗示是功能，不是装饰） |

**保留不动的语义条**：`.excel-grid .work-row.is-* td:first-child` 的 `inset 3px 0 0 var(--border-*)`
——那是**状态**（运行/完成/失败）的载体，属于"颜色只承载语义"，不是装饰。

---

## P1-1　圆角审计（过圆角）

### 半径阶梯（新的唯一一份）

| token | 值 | 用在哪 |
|---|---|---|
| `--r-xs` | 4px | 行内代码、极小标记 |
| `--r-sm` | 6px | 小按钮、小徽标底 |
| `--r-md` | 8px | 控件、图标键、**发送键**、页签 |
| `--r-lg` | 10px | 卡片、表格外框、代码块 |
| `--r-xl` | 14px | **composer（全屏唯一大圆角）** |
| `999px` | — | 真胶囊：chip / badge / perm / 头像 |

> **`--r-2xl`(16px) 降级/删除**：实机 composer 是 14px，不是 16px。

### 逐条

| 项 | 现状 | 处置 |
|---|---|---|
| `.app-shell .icon-button.action-button.primary-button` | `999px`（圆键） | **改 `var(--r-md)`**——实机是 35×35 的**圆角方**，角剖面 6–8px（圆的话该是 17.5px）。见设计语言 3.1 |
| `.composer` | `var(--r-2xl)` = 16px | 改 `var(--r-xl)` = 14px |
| `.conversation-tab, .right-tab` | `r-md r-md 0 0`（4 处） | **删**，改 `var(--r-md)` 全圆角 + 胶囊填充（P0-2） |
| **`.workspace`（对话区整块 panel）** | `border-radius: 0`（直角） | **✅ 已落地 `var(--r-md)`(8px)**：实机量到 Qoder 的纸四角 7–8px 且四边不缩进（设计语言 3.1 新增那一行），圆角里露的是壳的渐变、不需要留白槽；子节点由既有 `overflow: hidden` 一起裁圆（`styles.css` 第 27 节 n） |
| `.excel-sheet` | `3px 3px 0 0`（硬编码 + 零圆角底边） | **✅ 收敛到 `var(--r-md)`**（第 27 节 o） |
| 33 处 `border-radius: 999px` | 见下 | 逐类判定（现计数 **32**） |
| 非 token 硬编码半径 | `8px×4 / 5px×2 / 6px / 4px / 3px / 2px / 1px` 共 10 处 | 收敛到 token（4px→`--r-xs`、6px→`--r-sm`、8px→`--r-md`、1–3px 归 `--r-xs` 或 0）；工作表页签那处已收敛 |
| `--control-radius` | `var(--r-md)` | 保留（但页签不再用它） |

**33 处 999px 的判定**：真胶囊（保留）——`.badge` `.chip` `.chat-chip` `.perm-chip`
`.team-*chip` `.schedule-pill` `.work-filter` `.trajectory-filter-btn` `.session-status`
`.account-provider` `.theme-current` `.live-diag-badge` `.perf-badge` `.message-round`
`.html-embed-mark` `.file-preview-chip` `.axis-lane-count` `.syringe-*` `::-webkit-scrollbar-thumb`
`.panel-divider::before/after` `.terminal-resize::after` `.file-split-divider` `.resize-pill`；
**需要改**——`.app-shell .icon-button.action-button.primary-button`（→8px）、
`.right-tab.is-active::after`（→删）、`.section-title::before`（装饰圆点，建议删）。

### 过圆角自检脚本（P1 完成后加进 CI）

```python
# tools/radius-audit.py（建议）：扫 styles.css，报"半径 > 半高"与"同层级 ≥2 档"的嫌疑
# 简化版：断言 999px 的出现次数 ≤ 30（现状 33），且大圆角（≥14px）选择器 ≤ 2 个
```

---

## P1-2　线框审计（减少不必要线框，保留文件树）

### 冻结区（用户点名保留，**一律不动**）

```
.stack-button / .stack-list / .session-group / .session-group-head / .session-group-head-row
.session-row / .session-title / .collapse-chevron
```

> 用户原话：「左侧栏的对话列表我觉得做的就很好，当前的」——那么它就不在本次改动范围内，
> 也不作为"重构示范"。改动要绕着它走。

### 删（装饰线）

| 选择器 | 为什么 |
|---|---|
| `.conversation-tabs, .right-tabs { border-bottom: 1px solid var(--border) }` | 页签行底下的**通栏横线**，纯装饰；Qoder 没有 |
| `.message-body h1` / `.message-body h2 { border-bottom: 1px solid var(--border) }` | 正文标题下的横线，机器感；Qoder 标题不带线 |
| `.tool-run-head` / `.io-panel header` / `.queued-message header` / `.html-embed-head { border-bottom }` | 卡片内的头部横线——卡片已有描边，不需要再切一刀 |
| `.instrumentation-head` / `.plan-dsl-summary` / `.modal-header`（视情况） | 同上，逐条判 |

### 留（结构线 / 数据线）

| 选择器 | 类别 |
|---|---|
| **`.tf-row--elbow::before { border-bottom: 1px solid var(--tree-rail) }`** 及文件树同族 | **文件树轨（用户点名要留）** |
| `.message-body th, .message-body td` | 数据表格 |
| `.work-table-head` / `.plan-board-header` / `.instrumentation-row` / `.node-detail-tabs` / `.subagent-tree-head` / `.plan-card-head` | 数据/结构 |
| `.terminal-head` / `.file-pdf-toolbar` | 面板头部（有明确边界语义，留但要统一到 1 档） |
| `.topbar` | 壳/纸接缝（Qoder 实机也有） |
| composer 1px 描边 | "可输入的边界" |

### 颜色与剂量

- 接缝线统一 `var(--border-hairline)`（`#E6E6E6`）；
- 输入面统一 `var(--border-field)`（`#DDDDDD`）；
- **同类线只能一档**：现状 `--border` / `--border-strong` 混用（85 : 25），要按"接缝 / 强调"重新分工。

---

## P2-1　描边色温（✅ 已拍板：方案 A）

### 问题

- `--border: #e6e3da` 是**暖**的（R>B，偏黄）；
- 实机 Qoder 的接缝是**中性**的 `#E6E6E6` / `#DDDDDD`；
- 渐变**底部是冷色** `#D1DAE2`。暖描边压在冷底上会"发脏"——这是配色上的硬冲突。

### 两个选项

| 方案 | 做法 | 代价 |
|---|---|---|
| **A（推荐）** | `--border` 改中性 `#E7E7E4`（极轻暖），并新增 `--border-warm: #e6e3da` 给确实需要暖的地方 | 85 处 `var(--border)` 外观统一变中性，视觉更"干净" |
| B | 保持暖描边，只把壳的接缝线单独换成 `--border-hairline` | 改动小，但暖/冷混用的脏感还在 |

→ **已定案 A**（2026-09-24 用户拍板：按推荐）。理由：渐变底部冷色决定了"中性接缝"是唯一稳定解。
落地见 `styles.css` 的 `:root`：`--border: #e7e7e4` + `--border-warm: #e6e3da`。

---

## P2-2　别把 5 个硬编码色值也忘了

`styles.css` 在 `:root` 之外还剩 5 处硬编码色（已核）：

| 行 | 内容 | 处置 |
|---|---|---|
| 1317 / 1323 / 1324 | `background: #fff` ×2、`color: #1a1a1a` | 这是文档纸张（PDF/docx），**有意例外**，保留并加注释 |
| 3518 | `0 3px 8px rgba(0,0,0,.30), inset 0 1px 0 …#ffffff 34%` | P0-2 删除 |
| 3543 | `color-mix(in srgb, #ffffff 42%, transparent)` | P0-2 删除（假高光） |

---

## 3. 执行顺序（建议一次 PR 走完 P0，P1 分两个 PR）

| 阶段 | 内容 | 影响面 | 验收 |
|---|---|---|---|
| **S1** | P0-1 渐变 | 4 个 token + 4 条规则 + 6 个皮肤文件 + `run_wails.go` | 取色脚本 4 条判据 |
| **S2** | P0-2 拆"圆角 + 黑下框" | ~8 条规则，删除为主 | 自检：圆角规则里命中 `rgba(0,0,0` / `inset 0 -Npx 0` = 0 |
| **S3** | P0-3 阴影 token 重写 + composer 去影 | 1 个 token 块 + 12 条规则 | 目视 + "有影元素 ≤3 类" |
| **S4** | P1-1 圆角审计 | ~15 条规则 | 999px ≤30；大圆角选择器 ≤2 |
| **S5** | P1-2 线框审计 | ~10 条规则 | 装饰线清零；树轨/表格线在 |
| **S6** | P2 色温（需拍板） | 1–2 个 token | 目视 |
| **S7** | 回归 | — | `node --test gui/frontend/dist/theme.test.mjs`、`consistency.test.mjs` |

**注意 S1/S2 的顺序依赖**：P0-2 会删掉页签行底下的通栏线，P1-2 也打算删它——
两处只有一处该动手，**先做 P0-2（改选中态），P1-2 只做"验证它已不存在"**。

---

## 4. 回归与防倒退

1. **取色回归**：`tools/measure-skin.py gradient` 的四个数值写进本文档，每次换代复跑；
2. **黑名单断言**（建议加进 `consistency.test.mjs`）：
   - 含 `border-radius` 的规则里不得出现 `rgba(0, 0, 0` 或 `inset 0 -`；
   - `styles.css` 中 `border-radius:\s*999px` 计数 ≤ 30；
   - `:root` 外不得出现 `#` 色值（文档纸张那 3 行白名单）。
3. **冻结区断言**：`.stack-button` / `.session-group-*` 的规则 hash 在本次改动前后必须一致。
