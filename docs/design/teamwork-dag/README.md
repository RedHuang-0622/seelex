# teamwork 里程碑 / 工作项 DAG —— 静态 UI 稿（设计说明）

设计稿：`docs/design/teamwork-dag/index.html`（单文件自包含：CSS 内联 `<style>`、逻辑内联**普通** `<script>`，
零外部 css/js/字体/图片，双击 `file://` 直接能看，不需要起 http 服务）。
自证工具：`docs/design/teamwork-dag/tools/measure_dag.py`；原始读数：`docs/design/teamwork-dag/evidence/`。

数据形状照抄 leader 冻结的 fixture（`{ plan: { team_id, version, members[], milestones[], work_items[] } }`），
**未新增字段、未改字段名**。顺序的唯一事实是 `depends_on`；状态取值 work_item ∈ pending|running|review|done|failed，
milestone ∈ pending|active|done。

页面里的三个面板：
| 面板 | 宽度 | 内容 |
|---|---|---|
| `board-main-360` | 360px（会话右栏） | 主 fixture（m-design / m-impl / m-verify，6 个工作项） |
| `board-main-720` | 720px（宽栏） | 同一份 fixture，同一套渲染函数 |
| `board-mixed-720` | 720px | 变体：四色 × 两通道正交（done / review / failed+interrupted / running） |

调试用 query（只为自证，不影响默认视图）：`?theme=dark`、`?advance=N`（直接看第 N 轮）、`?scroll=N`。

---

## 1. 状态 → 色 / 语义映射表（线框色只表示状态）

四色**只用仓库既有令牌**（`gui/frontend/dist/styles.css`），不另造 hex；浅色/深色两套由 `data-theme` 基座给出。

| 状态 | 语义 | 描边 | 淡底 tint | 浅色值 / 深色值 | 行内呈现 |
|---|---|---|---|---|---|
| `running` | 正在做 | `--status-running`（`--border-running` 给 chip） | `--tint-running` | `#b26a00` / `#e0a45c` | 实线框 + 节点实心 |
| `done` | 已实现/完成 | `--status-done`（`--border-done`） | `--tint-done` | `#1f9c63` / `#4cc38a` | 实线框 |
| `review` | 正在被 review | `--status-info`（`--border-info`） | `--tint-info` | `#2e6be6` / `#6ba6ff` | 实线框 |
| `failed` / `killed`，或 `item.interrupted === true` | 中断、需要恢复（可重派） | `--status-failed`（`--border-failed`） | `--tint-failed` | `#d64545` / `#f0716a` | 实线框 + chip 后缀 `· 可重派` |
| `pending`（未开始） | 未开始 | `--status-idle` | 无（transparent） | `#9aa0a8` / `#8c8c93` | **虚线框** |

落地口径：`effStatus()` 先把 `interrupted === true` 抬成红，再按 `status` 取色；`killed` 与 `failed` 同色（一条红，不裂成两条）。
milestone 状态另有 chip：`active` 黄 / `done` 绿 / 空或 `pending` 灰；里程碑框的**描边色**是「淡色区分框」，不是状态（见 §4）。

## 2. teammate 配色表（色带 + 色点只表示「这是谁的工作」）

按 `role` **首次出现次序**分配（先扫 `plan.members[]`，再补 `work_items[].role`），登记处 `ROLE_SLOT` 是 append-only 的，
所以**重渲染、推进轮次、切主题都不会改色**（自证：`roleIndex = {artist:0, frontend:1, verify:2}`）。

| slot | 角色（本轮） | 浅色 | 深色 | 备注 |
|---|---|---|---|---|
| 0 | `artist` | `#6f5bd6` | `#a08bf0` | 紫罗兰 |
| 1 | `frontend` | `#a4538f` | `#c98fc3` | 梅红 |
| 2 | `verify` | `#0f8f9e` | `#45c2bd` | 青 |
| 3 | — | `#6b7f2e` | `#a9c46a` | 橄榄 |
| 4 | — | `#4b4fa8` | `#8f93e0` | 靛，**与 info 蓝同色域**（第 5 位起才用到，见 §7 待拍板） |
| 5 | — | `#5a5f68` | `#9aa0aa` | 石墨 |

令牌名 `--team-dag-role-0..5`（本稿自造，语义色仍全部引用既有状态令牌）。

> **实现校订（2026-10-07 · 独立验证 F4）**：本节那句「先扫 `plan.members[]`，再补 `work_items[].role`」
> 只描述**本稿**的做法。产品实现（`gui/frontend/dist/team-board-view.js` 的 `roleSlotOf`）只有一个
> 调用点，槽位按 **role 首次进入渲染的次序**登记（append-only，进程内稳定）；实测
> `members=[artist, frontend]` 而首行属于 frontend 时，frontend 拿 slot 0。**以实现为准**，
> 产品侧已在 `roleSlotOf` 上写明这条更正，别让两边口径继续分叉。
呈现位置**只有两处**：行内左侧 5px 色带（`--team-dag-band`）+ role chip 里 8px 小色点；chip 文字保持中性色，
边框、节点、背景一律不沾 teammate 色。

### 两条颜色通道的正交口径（第 8 条）
- **线框/边框色 = 状态**（`--row-line`，2px 实线，pending 虚线）；状态另给一层**很淡**的 tint 做底（`--row-tint`，`rgba(...,.10)`），
  文字色始终用 `--text/--text-strong`，不拿状态色染整行。
- **teammate 色 = 左侧色带 + chip 色点**（`--role-color`）。
- 同一条行里两个通道同时可见但不打架：外框读状态、内条读归属；图例（页顶）把两组色例并排摆出来。
- 依赖边是中性灰（`--faint`），不吃任何一方的颜色。

## 3. 布局与滚动口径

- **行 = 工作项**：`<article class="wi-row" data-item data-role data-status data-eff data-depth data-order>`；
  行内信息在 DOM 里全都在：`id / role / status / depends_on / d<n>` 在一行，`name` 在第二行（超宽省略号 + `title` 全文），
  `goal / description / session_id / worktree` 在 `<details>` 里（默认折叠，`wi-design` 那行默认展开，截图里能看到）。
- **行序 = 拓扑序**（不是时间）：里程碑之间按 `milestones[].depends_on` 拓扑排序；里程碑内按 `work_items[].depends_on` 拓扑排序，
  同层可并行；并列时回落到 fixture 数组序（稳定、可复现）。`data-depth` 是该工作项在依赖图上的最长路径深度（框头另有 `L<n>` = 屏障链层号）。
- **gutter（`--team-dag-gutter: 64px`）**：行左侧留白，依赖边真的画出来 —— 一个覆盖全内容的 `<svg class="edges">`
  用 `M xs,ys H laneX V yd H xd` 的正交折线从**被依赖行的节点**连到**依赖行的节点**，末端带箭头；同一目标的多条入边走不同 lane（`28 + k*11`）。
  SVG 坐标是**内容坐标**，所以滚动不重绘、不闪。
- **里程碑框**：`<section class="ms-frame" data-ms data-locked data-layer>` 罩住该里程碑**相邻**的若干行；描边 `--team-dag-ms-0..4`
  （低饱和淡色，逐框轮换），填充 `color-mix(in srgb, var(--ms-tone) 5%, transparent)`（极淡）。框头一行读得到
  `id · name · L<n> · status chip · 屏障 ← deps · 🔒/🔓 · n items · content`。
- **里程碑 = 屏障**：`locked = exists dep in depends_on with status !== 'done'`。未解锁的框 `opacity: .55` + `border-style: dashed` + 框头 `🔒 待解锁`；
  两个框之间是一条**闸门带**：横向虚线 + 标注「闸门 → 上一层全部 done 才放行（等 <deps>）」；放行后转成 `--status-done` 并写「闸门已放行」。
- **滚动**：`.board-scroll{ max-height: var(--team-dag-scroll-max-h) /* 380px */; overflow-y:auto; overscroll-behavior:contain; }`，
  滚动条样式跟随主题（`scrollbar-width:thin` + `::-webkit-scrollbar-thumb: var(--border-strong)`）；框头 `position:sticky; top:0` 吸附。
- **两种宽度**：同一份稿子里 360px 与 720px 并排（第二行放变体），宽度是外层 `.viewport` 的固定宽，内部同一套 DOM/CSS。

## 4. 更新（重渲染）口径

- 渲染是**纯函数**：`boardHtml(fixture) -> 字符串`，DOM 只由 fixture + `ROLE_SLOT`（append-only）决定；无时间戳、无随机数、无自增 id。
- 更新方式：先在**离屏**节点上构建新 DOM，再 `replaceChild` 一次性替换（不闪），替换前记 `scrollTop`、替换后写回（越界则夹到 `scrollHeight - clientHeight`）。
- 滚动位置：自证读数 `scrollKeep = true`，`scrollBefore = 140 → scrollAfter = 140`。
- 幂等：同一输入连渲两次，`board-content.innerHTML` 的 FNV-1a 哈希相同 —— `hashA = hashB = c345233a`（页面自证写进 `#dag-probe`）。
- 「推进一轮」按钮：`K = (K+1) % (ROUNDS.length+1)`，累计套用 5 轮脚本化 delta（`running→done`、`pending→running`、`running→review`、
  `failed + interrupted:true`、`interrupted→false` 恢复），走完回到 base —— 顺带证明「同输入同 DOM」。
  里程碑 `status` 也随轮次显式改写（`m-design: done` / `m-impl: active`），于是闸门从「未放行」翻到「已放行」。
- 主题切换**不重渲染**：只翻 `<html data-theme>`，颜色全走 `var(--...)`，SVG 描边也用 CSS 变量 → 不重绘也不闪。

## 5. 自证读数（headless Chrome 无窗口路径，`chrome --headless=new`）

生成物都在 `docs/design/teamwork-dag/evidence/`：`shot-light.png`、`shot-dark.png`、`shot-light-scrolled.png`、
`dom-light.html`、`dom-light-round1.html`、`readings.json`、`readings.txt`。

### 5.1 像素量测（PIL，`--window-size=1440,1200`，dpr=1 → 图片像素 = CSS 像素）
| 图 | 尺寸 | 非空白像素比例（相对 `--bg` #f7f6f2 / #181818） |
|---|---|---|
| `shot-light.png` | 1440 × 1200 | 0.274 |
| `shot-dark.png` | 1440 × 1200 | 0.487 |

四种状态色**真的出现在画面里**（采样点 = 行卡片「上边框中点」邻域内与期望色最近的像素；期望色即既有令牌值）：

| 图 | 行 | 状态 | 采样坐标 | 采样 RGB | 期望 RGB | 距离² |
|---|---|---|---|---|---|---|
| light | `board-mixed-720/wi-mx-1` | done | (397, 618) | (31, 156, 99) | (31, 156, 99) | 0 |
| light | `board-mixed-720/wi-mx-2` | review | (397, 687) | (46, 107, 230) | (46, 107, 230) | 0 |
| light | `board-mixed-720/wi-mx-3` | failed(+interrupted) | (397, 756) | (214, 69, 69) | (214, 69, 69) | 0 |
| light | `board-mixed-720/wi-mx-4` | running | (397, 825) | (178, 106, 0) | (178, 106, 0) | 0 |
| light | `board-main-360/wi-design` | running | (212, 206) | (178, 106, 0) | (178, 106, 0) | 0 |
| light | `board-main-720/wi-design` | running | (768, 166) | (178, 106, 0) | (178, 106, 0) | 0 |
| dark | 同上四条（mixed） | done/review/failed/running | (397,618)/(397,687)/(397,756)/(397,825) | (76,195,138)/(107,166,255)/(240,113,106)/(224,164,92) | 同 | 0 |

页顶图例色例（纯色块中心，作对照）在两套主题下**逐字命中令牌值**：
light running `(105,57)=(178,106,0)`、done `(213,57)=(31,156,99)`、review `(303,57)=(46,107,230)`、
failed `(405,57)=(214,69,69)`、pending `(571,57)=(154,160,168)`；
dark running `(105,57)=(224,164,92)`、done `(213,57)=(76,195,138)`、review `(303,57)=(107,166,255)`、
failed `(405,57)=(240,113,106)`、pending `(571,57)=(140,140,147)`。

已知偏差（如实写）：落在**未解锁框**里的 `pending` 行，读到 `(197,201,203)`（深色 `(91,91,94)`），
不是纯 `--status-idle` —— 因为锁定框整体 `opacity:.55`，观测值 ≈ 0.55×状态色 + 0.45×底色（期望合成值 `(196,199,201)`，距离²=9）。
这正是第 5 条「未解锁要降透明度」的预期结果，不是色板漂移。另有 3–4 行因落在滚动区之外被裁掉，脚本显式标注「不采样」。

### 5.2 DOM 读数（`--dump-dom`）
| board | board 宽 | 滚动容器 client/scroll | 里程碑框 | 框内条数 | 闸门 | 依赖边 | 边几何对照 |
|---|---|---|---|---|---|---|---|
| `main-360` | 342px | 380 / 873（max-height 生效，内容确实超出） | m-design(L0) / m-impl(L1) / m-verify(L2) | 1 / 2 / 3 | m-design→m-impl `open=false`、m-impl→m-verify `open=false` | 6 条 + 6 箭头 | 6/6 起终点与两端节点锚点一致 |
| `main-720` | 702px | 380 / 663 | 同上 | 1 / 2 / 3 | 同上 | 6 条 + 6 箭头 | 6/6 |
| `mixed-720` | 702px | 357 / 357 | m-mixed(L0) | 4 | — | 3 条 + 3 箭头 | 3/3 |

行序 = 拓扑序（`item id` + `depth`，`main` board）：
`wi-design d0` → `wi-impl d1` → `wi-impl-tests d2` / `wi-render d2` / `wi-cases d2` → `wi-accept d3`；
`topo_violations = none`（对每一行检查「所有依赖必须排在它前面」）。
`mixed` board：`wi-mx-1 d0` → `wi-mx-2 d1` / `wi-mx-3 d1` → `wi-mx-4 d2`，`none`。
边几何对照（例）：`wi-impl ← wi-design`，路径起点 `[19.0,169.0]` vs 源节点锚点 `[19,169.0]`，终点 `[19.0,382.0]` vs 目标节点锚点 `[19,382.0]` → ok。

其他自证读数：`stickyStuck = true`（滚到 120 时框头正好吸附在滚动容器顶边，`stickyDelta = 0`）；
`?advance=1` 后 `wi-design=done`、`wi-impl=running`、**m-impl 框 `locked=false`、闸门 `m-design→m-impl open=true`**（屏障放行链路通）。

### 5.3 两通道正交的像素旁证（light 图，直接扫区域取色）

| 通道 | 采样区域 | 读到 RGB | 对应令牌 | 结论 |
|---|---|---|---|---|
| teammate 色带（artist） | 720 面板 `wi-design` 行左侧带 x95–100,y640–699 | `(111,91,214)` | `--team-dag-role-0 #6f5bd6` | 逐字命中 |
| teammate 色带（frontend） | 720 面板 `wi-impl` 行左侧带 x95–100,y830–859 | `(164,83,143)` | `--team-dag-role-1 #a4538f` | 逐字命中 |
| 里程碑框描边（已解锁 m-design） | 720 面板框右边 x1075–1091,y128–289 | `(111,132,171)` | `--team-dag-ms-0 #6a7fa8`(106,127,168) | Δ≤5，未降透明度 |
| 里程碑框描边（未解锁 m-impl） | 720 面板框右边 x1075–1091,y320–499 | `(196–202,…,212)` | 同上 ms-1 经 `opacity:.55` 合成后的观测值 | 虚线 + 变淡都成立 |
| 闸门带虚线 | 360 面板 x20–349,y375–409 / 720 面板 x400–1079 同带 | 951 / 276 个 `≈(154,160,168)` 像素 | `--status-idle` | 闸门带真的画出来了 |

同一张图里「状态色」和「teammate 色」分别落在**不同的几何位置**（框线 vs 左侧色带），互不串色 —— 这就是第 8 条要的正交。

## 6. 落点两案（同一份组件放哪里）

**案 A · 会话右栏内嵌（360px，本稿左面板）**
- 优点：和对话同屏，leader 派活/teammate 干的活与讨论在一起，不需要切换上下文；窄栏天然适合「只看当前」。
- 缺点：342px 可用宽里 64px gutter 占 19%，chip 行必然换行 → 行高不均、一屏只装 2–3 行；
  `name` 只能省略号；**全 DAG 读不出来**，只能读出「当前里程碑 + 下一道闸门」。

**案 B · 宽栏 / 工作台整幅（720px+，本稿右面板）**
- 优点：一行读全 `id/role/status/deps`，gutter 里 6 条依赖边一次看清；一屏能罩住一个完整里程碑框 + 闸门带；
  双里程碑并排时「屏障/未解锁」的对比才成立（本稿看到的正是这个）。
- 缺点：挤占会话阅读宽度；需要一个独立入口（面板切换/抽屉），否则和会话抢空间。

**取舍分析（建议）**：两案不是二选一，而是**同一份渲染函数 + 同一份 fixture 的两种视口**：
360 案做「聚焦当前里程碑」模式（只渲染 `active` 里程碑 + 它前面那道闸门，边只画入边，一屏搞定），
720 案做「全 DAG」模式（本稿默认）。这样右栏负责「我正在等什么」，宽栏负责「整盘怎么走」。
需要 leader 拍板的点见 §7 第 1 条——「聚焦模式」是不是一个允许的口径。

## 7. 待 leader 拍板（1–3 点）

1. **右栏是否允许「聚焦当前里程碑」的裁剪视图**（只渲 active 里程碑 + 入边）。若不允许，360px 案就只能当全 DAG 的滚动窗口用，
   可读性会掉到「一次只看得到两行」；若允许，需要把它冻结成一个明确口径（例如 `focus=current`），否则前端会各自裁剪。
2. **`role` 超过 6 个时的配色**：当前 `slot = size % 6` 会**回绕复用**，第 7 位 teammate 与第 1 位撞色。
   要么把色板扩到 12 个（并确认仍与四状态色不同相），要么回绕时额外加纹理/角标区分。
3. **`--team-dag-scroll-max-h` 是写死 380px 还是跟随容器高**：右栏/宽栏/整幅三种宿主高度差别很大，
   若走「容器自适应」需要 leader 定一个下限（现在的 380px 是照 360–420 建议取的中间值）。

## 8. 未做 / 已知限制

- 未做真机（Wails GUI）挂载：本稿只交付静态稿 + 自证，没有改 `gui/frontend/dist` 里的任何产品代码。
- 未给 `milestone.status === 'done'` 单独做「已收口」视觉（只有 chip 变绿 + 框头 `✅ 已完成`）；若需要「收口折叠」需另定口径。
- 未验证超长 `content` 在 360px 下的截断策略（现在是 `text-overflow: ellipsis`）。
