# 里程碑 DAG 甘特图 —— 独立验证报告（wi-verify）

角色：**独立验证方**（不是实现作者；本轮只验不改，`gui/frontend/dist/**` 一行都没动）。
待验对象：`team-board-view.js` 的里程碑甘特一节（`renderTeamGantt` / `renderGanttFrame` /
`renderGanttGate` / `renderTeamWorkItem` / `effStatus` / `DAG` 几何 / `TEAM_BOARD_CSS`）、
`team-board-view.test.mjs`、`team-board-preview.html`。
被验提交：`a5f3f7f`（本 worktree 的 HEAD；链 `f050edc` → `c753f85` → `a5f3f7f`）。
视觉契约：`docs/design/teamwork-dag/index.html` + `README.md`。

> 结论先写一句：**用户提的五条口径我都能独立复现**（按行分 teammate 色、行序=依赖不是时间、
> 一个里程碑一个大框且未解锁进不去、面板最大高度 380 + 滚动、线框色=状态），
> 另有 **6 条问题**（1 条中、4 条低、1 条仅提示）与 **4 条已知偏差被复核为真**，逐条列在 §7/§8。

---

## 1. 我独立复现的命令与原始输出尾部

```
$ node --test gui/frontend/dist/*.test.mjs
ℹ tests 665 / suites 0 / pass 665 / fail 0 / cancelled 0 / skipped 0 / todo 0 / duration_ms 5624.8649

$ node --test gui/frontend/dist/team-board-view.test.mjs      # 单文件
ℹ tests 47 / pass 47 / fail 0 / duration_ms 124.5002
（尾部：✔ ⑤ 装配读数转义（注入 <img onerror=...> 不许出裸标签））

$ go test ./gui/
ok  github.com/RedHuang-2022/seelex/gui      3.639s          (exit 0)
```

两条都是全包/全文件跑（不是 `-run` 挑一条）。

## 2. 我自己的夹具与量测路径（不引用任何别人的坐标/结论）

夹具与探针脚本放在 `_tmp/verify-dag/`（**git 忽略，不入库**）；页面一律
`import` **产品渲染件**（不是设计稿）生成，CSS 顺序 = `styles.css`（令牌唯一来源，原样内联）
→ `TEAM_BOARD_CSS`，再用 headless Chrome 出图 + `--dump-dom`：

```
$ node _tmp/verify-dag/gen.mjs                                   # 生成 7 张页
$ powershell -NoProfile -File _tmp/verify-dag/run.ps1            # chrome 出 dom-*.html / shot-*.png
$ node _tmp/verify-dag/analyze.mjs > _tmp/verify-dag/analysis.json   # 读 #probe 的 JSON
$ python _tmp/verify-dag/scan.py shot-palette.png ...            # PIL 扫真图像素
```

chrome 调用口径（踩过的坑照抄）：`--headless=new --force-device-scale-factor=1`（dpr=1 → 图素=CSS 像素）、
`--screenshot=` 给**绝对路径**、`--dump-dom` 必须在 `cmd /c "... > out.html"` 里重定向。

七份自发夹具：主夹具（宽 900/窄 360 两个宿主）、五状态色板（浅/深）、对抗面（成环+缺失+自指+空里程碑）、
9 角色+恶意名字、滚动 200px 版、跨里程碑依赖版。主夹具的关键设计：**里程碑与工作项的数组
序故意倒着声明**（`m-verify` 在 `m-design` 前面、每个里程碑内也是下游在前），所以「行序来自依赖
而不是数组序」是被逼出来的，不是照抄声明序。

## 3. 量到的读数（全部来自我自己的页面）

**① 滚动区（主夹具，宽宿主 900px）**：`.team-dag-scroll` `clientHeight=380`、`scrollHeight=600`、
`overflow-y:auto`、`max-height:380px`，滚动条占宽 10px；内容 `900×600`。
**像素旁证**：`page-main-scrolled.html` 把 `scrollTop=200` 后再截图，`running` 命中从 5150px 掉到
**23px**、`pending` 从 868px 涨到 **1348px**（原本在窗外的 v1/v2 进窗）——滚动是真的在滚。

**② 一个里程碑一个大框、只包自己名下的行**（框边界用内容坐标，`top..bottom`）：

| 框 | layer / 框头 L | rows（框内全部 id） | locked | border-style | opacity | 淡色 |
|---|---|---|---|---|---|---|
| `m-design`(done) | `data-layer=0` / L0 | `[d1, d2]` | false | solid | 1 | `#6a7fa8` |
| `m-impl`(active) | `data-layer=1` / L1 | `[i1, i2]` | false | solid | 1 | `#8a7fa8` |
| `m-verify`(pending) | `data-layer=2` / L2 | `[v1, v2]` | **true** | **dashed** | **0.55** | `#6f8f7f` |

框内 `:scope > .team-dag-rows > .team-dag-row` 与 `querySelectorAll('.team-dag-row')` 结果一致
（无跨框串行）。框头 `position:sticky; top:0px`，`z-index:5`。

**③ 闸门与 `depends_on` 一致**：`m-design->m-impl` `data-open=true`（label「闸门已放行 → m-design
全部 done」，tone `#1f9c63`）；`m-impl->m-verify` `data-open=false`（「等 m-impl」，tone `#9aa0a8`）。
判据：`m-design.depends_on=[]`、`m-impl.depends_on=[m-design(done)]`、`m-verify.depends_on=[m-impl(active)]`。

**④ 行序 = 拓扑序**：DOM 顺序 `d1,d2,i1,i2,v1,v2`、`d=…` 深度分别为 0,1,0,1,0,1，
而夹具数组序是 `v2,v1,i2,i1,d2,d1`（完全相反）。node 侧再补一刀：
把 `milestones` 与 `work_items` 数组**整体 reverse** 后渲染，HTML **逐字节相同**
（`array_order_reversed_same_html: true`，两边 FNV-1a 都是 `a14fc874`）。

**⑤ 依赖边端点落在节点锚点上**（内容坐标）：

| path | from→to | Δy(源) | Δy(目标) | x(viewBox) | x(px) | 节点右缘(px) | Δx |
|---|---|---|---|---|---|---|---|
| `M 22 71 H 34 V 147 H 22` | d1→d2 | 0.00 | 0.00 | 22 | 14.08 | 15.08 | −1.00 |
| `M 22 277 H 34 V 353 H 22` | i1→i2 | 0.00 | 0.00 | 22 | 14.08 | 15.08 | −1.00 |
| `M 22 483 H 34 V 559 H 22` | v1→v2 | 0.00 | 0.00 | 22 | 14.08 | 15.08 | −1.00 |

即：端点 y 与两端节点的**中心 y 完全一致（Δ=0）**；x 侧线落在 14.08px，节点圆盘占
`[6.08, 15.08]`，所以线端**落在节点盘内 1px**（不是错位，是重叠 1px，肉眼不可分）。
边是中性灰 `rgb(144,150,160)`（= `--faint`），不吃状态色也不吃 teammate 色。

**⑥ 线框色 = 状态**（计算样式 `borderTopColor`，与 `styles.css` 浅色令牌逐字比对；
令牌取自 `getComputedStyle(document.documentElement)`：`#b26a00/#1f9c63/#d64545/#2e6be6/#9aa0a8`）：

| 行 | status / eff | border-color | 与令牌 | style | 底色 |
|---|---|---|---|---|---|
| d1 | done | `rgb(31,156,99)` | `#1f9c63` ✓ | solid | `rgba(31,156,99,.1)` |
| d2 | running | `rgb(178,106,0)` | `#b26a00` ✓ | solid | `rgba(178,106,0,.1)` |
| i1 | review | `rgb(46,107,230)` | `#2e6be6` ✓ | solid | `rgba(46,107,230,.1)` |
| i2 | failed + `interrupted:true` | `rgb(214,69,69)` | `#d64545` ✓ | solid | `rgba(214,69,69,.1)` |
| v1/v2 | pending | `rgb(154,160,168)` | `#9aa0a8` ✓ | **dashed** | transparent |

**PIL 扫真图**（浅色 `shot-palette.png` 960×700，五状态各占一行，行的上边框都落在 380px 窗内）：
采样 RGB `running(178,107,3)` / `done(31,156,99)` / `review(46,107,230)` / `failed(214,69,69)` /
`pending(152,157,166)`，期望值 `#b26a00/#1f9c63/#2e6be6/#d64545/#9aa0a8`，逐通道差 ≤ 3。
浅色 `shot-main.png`（960×1400）逐色命中像素数：running 5150、done 6523、review 5106、failed 3102。
**深色** `shot-palette-dark.png`（`data-theme=dark`，令牌取 `styles.css:195` 那一套）：
采样 `(222,162,91)/(76,195,138)/(107,166,255)/(240,113,106)/(144,144,144)` ↔
`#e0a45c/#4cc38a/#6ba6ff/#f0716a/#8c8c93` ✓。

**⑦ 两条正交通道**：全板扫描「谁的计算值等于 role 色」→ 命中的只有
`.team-dag-band|backgroundColor` 与 `.team-dag-dot|backgroundColor` 两类，主夹具 **12 处**（6 行 × 2：
色带 + 色点），宽宿主与窄宿主各 12 处、结果一致；9 角色夹具上是 18 处（9 行 × 2）。
**没有任何边框/文字/背景沾 teammate 色**（`.team-dag-card` 的 border 全是状态色）。
色带实测 `role-0 rgb(111,91,214)`、`role-1 rgb(164,83,143)`、`role-2 rgb(15,143,158)`，
与 `README.md §2` 的表一致；宽度 5px（`--team-dag-band`）。

**⑧ 窄栏（360px 宿主）**：`.team-dag-scroll` 的 `--team-dag-gutter` = **44px**、行 gutter
`flex-basis` = 44px，宽宿主 64px。取值位置说明：容器本身（`.team-board`）仍是 64px，
**44px 要在 `.team-dag-scroll` 或行内 gutter 上读**（容器查询作用在后代，别读容器）。
窄栏下 `.team-dag-note-line/-session/-wt/-ms-content/-ms-deps/-layer` 计算 `display:none`。

## 4. 试着推翻（对抗面）结果

| 挑战 | 结果（我的读数） |
|---|---|
| 里程碑成环 | 不崩；两块照画，框头写「依赖成环」，depth 归 0（框头 `L0`）；无假边 |
| 里程碑依赖指向不存在 | 不崩；框头写「依赖缺失：no-such-ms」，且 `locked=true`（未知依赖按「未 done」算） |
| 工作项依赖指向不存在 | 不崩；**不画边**（对抗页全板只有 3 条边），行上写「依赖缺失：no-such-item」+「被依赖卡住」 |
| 工作项自指 | 不崩；写「依赖成环」，但**画出了一条退化自环边** `M 22 541 H 34 V 541 H 22` → 见 §7 F3 |
| 空里程碑（名下 0 行） | 正常渲染，框头写「尚未排活」，`0 items` |
| 9 个角色（回绕） | 6 个不同色带色（回绕），slot ≥ 6 的 6 行 `is-wrapped=true`（斜纹第二通道真的叠上） |
| 恶意名字 `"><img src=x onerror=...>`、`</span><script>…</script>` | **零注入**：`imgTags=0`、`inlineHandlers=[]`、新增 `script[src]` 0 个、`window.__pwned` undefined |
| 幂等 | 4 份夹具 `renderTeamBoard` 连渲两次字符串**完全相同**（主夹具 hash `a14fc874`×2）；数组反序后渲染 HTML 也相同 |
| 空计划 / null 计划 | `renderTeamBoard` / `renderTeamGantt` 返回 `""`（不留空壳） |

## 5. 信息不许丢（挑工作项 `i2`，逐个给出定位方式）

| 字段 | DOM 定位 | 读到的值 |
|---|---|---|
| `id` | `.team-dag-row[data-item-id]` / `code.team-dag-id` | `i2` |
| `name` | `.team-dag-name`（有 session → 是可点 `<button>`） | `接入看板` |
| `role` | `.team-role`（`<i class="team-dag-dot">` + 文本） | `frontend` |
| `status` | `.team-status` 文本 + `data-eff` | `failed · 可重派` / `failed` |
| `depends_on` | `.team-dag-meta .team-dep` chips | `i1` |
| `session_id` | `.team-dag-session`（text + `title`） | `sess-fe-1` |
| `worktree` | `.team-dag-wt`（text + `title`） | `seelex/frontend-wi-impl` |
| `goal/description/note` | `.team-dag-note-line` 文本 + **`title` 里的全文** | `达成目标：替换里程碑一节 ｜ 描述：renderTeamGantt ｜ 结论：已重派` |
| 标记 blocked / interrupted / live | `.team-blocked` / `.team-interrupted` / `.team-live` | 三个都在（chips：`被依赖卡住`、`可重派`、`现场在`） |
| 其余 | `data-status`、`data-eff`、`data-depth`、`data-milestone-id`、`data-interrupted` | `i2\|failed\|failed\|1\|m-impl\|true` |

`goal/description/note` 的**可见文本**被截到 240 字（`CONTENT_LIMIT`），但 `title` 写的是**未截断全文**，
所以不算丢。窄栏下这行 `display:none` → 见 §7 F6。

## 6. 无第二份 CSS / 没碰不该碰的

```
$ findstr /C:team-dag gui\frontend\dist\styles.css          → 0 命中（exit 1）
$ findstr /S /M /C:team-dag-frame gui\frontend\dist\*       → team-board-view.js / team-board-preview.html / team-board-view.test.mjs
$ git diff --name-only 1f9a40d a5f3f7f -- gui/frontend/dist/app.js   → 空（app.js 本次没改）
$ git diff --stat 1f9a40d a5f3f7f
  team-board-preview.html      | 257 ++++----
  team-board-view.js           | 478 ++++++++++++-------
  team-board-view.test.mjs     | 310 +++++++++----
  3 files changed, 848 insertions(+), 197 deletions(-)
```

`app.js:23` 仍 `import { TEAM_BOARD_CSS, …, renderTeamBoard … }`，`app.js:2317`
`style.textContent = TEAM_BOARD_CSS`（唯一注入点）。旧看板类名 `.team-milestone/.team-stage/.team-work-item`
在 `gui/frontend/dist/*.js|*.css` 里**只剩一行注释**（`team-board-view.js:976`），没有第二套渲染 → 旧看板确实被干掉了。

## 7. 我发现的问题（带复现步骤与读数，决策权交 leader）

**F1（中）闸门带的文案与实际判据不符 —— 会写出「已放行」而上一层其实没 done。**
`renderGanttGate` 的放行条件是「下一块 `depends_on` 全部 done」，但文案把功劳记在
`block.from` 头上。复现：`_tmp/verify-dag/page-adv.html`（对应夹具 `planAdv`）里
`miss-ms`（`pending`、`locked=true`、依赖缺失）与 `empty-ms`（`depends_on:[]`）之间的闸门读到
`data-open=true`、label `闸门已放行 → miss-ms 全部 done` —— 而 `miss-ms` 并**没有** done。
主夹具里这条显不出来（`next.depends_on` 恰好就是 `from`）。建议：文案改按 `block.depends_on` 生成，
或在无屏障依赖时写成「下一块无屏障依赖」。**注意：这不是颜色/几何错，是文案在特定计划形状下会说假话。**

**F2（低）`data-layer` 与框头 `L<n>` 不是同一个量。** 前者是框在排序结果里的**位置**，后者是
**拓扑深度**。成环回落时两者分叉：对抗页里 `cyc-a`/`cyc-b` 读到 `data-layer=2/3` 而框头写 `L0`。
主夹具里两者恰好一致（0/1/2），所以不易察觉。建议属性改名 `data-order` 或让它写 depth。

**F3（低）自指依赖会画一条退化自环边。** 对抗页 `x-self`（`depends_on:["x-self"]`）画出了
`M 22 541 H 34 V 541 H 22`（零高度矩形 + 箭头压在自己节点上）；行上同时有「依赖成环」。
对照：**缺失依赖被正确抑制**（不画边）。设计口径禁止成环，生产数据里应不可达，但作为
「不假画边」的对抗面它没做到。

**F4（低）README §2 的配色口径与实现不符。** README 写「按 role 首次出现次序分配（**先扫
`plan.members[]`**，再补 `work_items[].role`）」；实现里 `roleSlotOf` 全文件只有**一个调用点**
（`renderTeamWorkItem`），槽位来自**渲染序**。复现 `_tmp/verify-dag/slot-order.mjs`：
`members=[artist, frontend]`，但渲染序上第一行属于 frontend → `frontend=slot0`、`artist=slot1`
（与 README 表里的「artist=0」相反）。真正成立的是「同一 JS 上下文内 append-only，
重渲染/换主题/推进轮次不变色」（我验了：同输入两次渲染 role 色不变）。

**F5（提示，不算缺陷）`effStatus` 的 `interrupted` 判定是严格布尔。** `interrupted: "true"`（字符串）
**不**抬红（读 `effStatus({status:"done",interrupted:"true"})` = `"done"`）。后端是 bool 投影，
现在没问题；只是上游若换编码会静默改变线框色，值得在契约里钉死。

**F6（中，且是 leader 拍板过的口径的后果）窄栏下 goal/description/note/session/worktree
在画面上完全读不到。** 容器查询里 `.team-dag-note-line/-session/-wt` 是 `display:none`
（我在 360px 宿主上读到 `display:none`），而 `display:none` 的元素 **hover 也读不到** ——
所以这几个字段在 360px 右栏里只剩 DOM、用户看不到。缓解路径是行名按钮仍可点
（`data-team-item-open` → 子页），但**甘特行本身**在窄栏是看不到目标的。这属于「口径 10 折叠次级信息」
的既然后果，我把它如实报上来，是否改由 leader 定。

## 8. 已知偏差：复核为真（与设计稿/实现自述一致，不是新发现）

**D1 行高写死 72px、没有 `<details>` 折叠块。** 设计稿里 `goal/description/session/worktree`
在 `<details>` 里（`wi-design` 默认展开）；实现是固定 72px 行 + 一行 12px 的 note line + `title` 全文。
几何由 `--team-dag-row-h:72px` 与 `DAG.rowH=72` 互钉（我在 `.team-board` 上读到 `--team-dag-row-h:72px`、
`--team-dag-head-h:26px`、`--team-dag-frame-border:1px`，与 DAG 常量 72/26/1 一致）。

**D2 跨里程碑的工作项依赖仍会画边，且被 sticky 框头遮断。** 复现 `page-cross.html`：
一条 `M 22 71 H 34 V 201 H 22` 的边跨过 `m2` 的框头，`edgeCrossesHead` 读到
`head="m2" overlapY=26 overlapX=7.68`，框头底 `rgba(251,250,246,0.97)`、`z-index:5` > svg `z-index:1`。
像素定点：同一列 x=21 在框头外读到 `(144,150,160)`（边灰），在框头 26px 段内读到 `(248,247,243)`
（= 框头底 97% + 边灰 3%）→ **0.97 不透明把线吃掉了**，实际看不到。

**D3 `roleSlotOf` 是模块级 append-only 登记处 —— 色槽由「渲染序」决定，且随 JS 上下文存活。**
不变量「同一位 teammate 在重渲染/换主题/推进轮次后不变色」我验了，成立（同输入两次渲染 role 色相同、
深色主题下 role-0 仍是 `#a08bf0→rgb(160,139,240)`）；但**跨进程/跨首个渲染**不保证：
本报告里 9 角色夹具的 `r0` 落在 slot 3（因为生成器进程先前渲过 3 个角色），fresh 进程里同一夹具是 slot 0。
这是实现选定的口径（代码注释写了为什么），我把它标出来是因为它决定了「颜色」不是计划里的绝对属性。

**D4 窄栏折叠次级信息 —— 已升格为 §7 F6（有实际可见性后果）。**

**D5 `--team-dag-role-*` / `--team-dag-ms-tone-*` 是自造 hex。** 我在 `documentElement` 上读到空串、
在 `.team-board` 上读到 `#6f5bd6` / `#6a7fa8` → 作用域确实限定在看板内，没往全局令牌里塞新色；
单测里也有「自造色值只许出现在归属色/框描边色板」那条用例（`team-board-view.test.mjs:698`）。

**另外一条我复跑过的护栏**：`team-board-view.test.mjs:678`「几何只在两处写：CSS 的
`--team-dag-*` 与渲染件的 `DAG` 常量必须逐字相同」——固定行高/框头高/边框宽只要有人在一侧改数字，
它就会红；我在 `.team-board` 上读到的 `--team-dag-row-h:72px`、`--team-dag-head-h:26px`、
`--team-dag-frame-border:1px`、`--team-dag-scroll-max-h:380px`、`--team-dag-gutter:64px`、
`--team-dag-band:5px` 与 DAG 常量 72/26/1/—/—/— 对得上（互钉用例通过）。

## 9. 我**没做到 / 没验到**的（如实）

1. **没跑真实 GUI 端到端**（没起 http 源、没在 `app.js` 装配的真实右栏里量）。我用的是
   自建静态页 + 产品渲染件 + `TEAM_BOARD_CSS` + `styles.css` 令牌；`app.js` 装配路径只做了
   **静态核对**（import / `ensureTeamBoardStyles(` / 成员入口钩子）+ Go 守卫测试通过。
2. **没验交互**：`data-tip` 悬浮提示、点击 `data-team-item-open` 打开子页、滚动条拖拽 ——
   只核对了钩子属性/规则存在，没点过。
3. **没验跨浏览器**（只有 Chrome `--headless=new`），也没验 `scrollbar-width:thin` 在
   真实滚动容器上的观感。
4. **没验真实后端投影**：夹具形状照 `README` 冻结的数据形状手写，字段语义（如
   `members[].status`、`plan.version`）我只按契约读，没对齐后端 DTO。
5. **深色主题只验了色板页**（五状态 + 一个 teammate 色），没验「深色 × 窄栏 × 滚动 × 对抗面」的组合。
6. **没有审 `renderTeamQueue` 等本节之外的代码**（只用了它们进/不进 DOM 的事实）。

---

### 复现的最小探针（照抄 `_tmp/verify-dag/probe.js` 的核心，放进页面末尾即可）

```html
<script>
（页面 = styles.css 内联 + TEAM_BOARD_CSS 内联 + 渲染件输出；
  <pre id="probe"> 里塞下面这段的 JSON，再用 cmd /c "... --dump-dom > dom.html" 取回）
const c = document.querySelector(".team-dag-content"), cr = c.getBoundingClientRect();
const s = document.querySelector(".team-dag-scroll");
const out = { scroll: [s.clientHeight, s.scrollHeight, getComputedStyle(s).maxHeight],
  rows: [...document.querySelectorAll(".team-dag-row")].map(r => {
    const n = r.querySelector(".team-dag-node").getBoundingClientRect();
    return { id: r.dataset.itemId, eff: r.dataset.eff,
      border: getComputedStyle(r.querySelector(".team-dag-card")).borderTopColor,
      midY: +(n.top + n.height / 2 - cr.top).toFixed(2), nodeRight: +(n.right - cr.left).toFixed(2) }; }),
  frames: [...document.querySelectorAll(".team-dag-frame")].map(f => f.dataset.milestoneId + ":" +
    [...f.querySelectorAll(":scope > .team-dag-rows > .team-dag-row")].map(r => r.dataset.itemId).join("," )),
  edges: [...document.querySelectorAll("path.team-dag-edge")].map(p => p.getAttribute("d")) };
document.body.insertAdjacentHTML("beforeend", '<pre id="probe">' + JSON.stringify(out) + "</pre>");
</script>
```
