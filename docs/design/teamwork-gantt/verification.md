# 独立验证报告：里程碑 · 工作项「真甘特几何」（69af50d）

- 验证者：`verify`（独立验证方，非作者；**只验不改**：本报告之外没有动过 `gui/frontend/dist/**` 与任何产品代码）
- 被验对象：HEAD = `69af50d7533cd5c2f5f88455d0d3234369b7310b`（`feat(team-board): 里程碑·工作项换成真甘特几何（wi-gantt-impl）`）

| 文件 | blob（HEAD） | 字节 |
|---|---|---|
| `gui/frontend/dist/team-board-view.js` | `213865d06d583f78323b44edb9e20d11522e40c9` | 97,646 |
| `gui/frontend/dist/team-board-view.test.mjs` | `d6852b077e1d7a7323cc9c3f73198f488f746f2b` | 68,557 |
| `gui/frontend/dist/team-board-preview.html` | `6bbfd1b54b69ed45d662beb1feb36a9261807cdb` | 21,973 |
| `docs/design/teamwork-gantt/README.md`（契约） | `302a7468966be414eb307a4596414e898959dc94` | 19,436 |

（blob 哈希是「我验的就是这份内容」的锚点；`git rev-parse HEAD:<path>` 可复核。）

> **口径变更注记（2026-10-08 追加，不改动本报告的原始读数）**：本报告的读数锚在 `69af50d`，当时的口径是
> **行高 = 常量**（`--team-dag-row-h` 宽栏 38px / 窄栏 30px，内容不撑高）。这条口径**已被用户推翻并由后续工作项改掉**：
> `wi-gantt-fit`（实现）+ `wi-gantt-fit-doc`（稿子/文档同步）—— **行高由内容撑（`min-height` 只兜底）、框随行长、
> 边的 y 走实测**。故本报告里「行高 = `--team-dag-row-h`（宽栏 38px 固定，内容不撑高）」这类判据（§0 摘要、
> §6 偏差 ⑤、§6 复跑表）**不再适用**，以 `README.md` §2.1 与 `fit.md` 为准；其余读数（槽位 / 条宽 / 刻度 /
> 汇总条跨度 / 颜色通道 / 拓扑序 / 幂等 / 闸门…）仍按当时的 revision 有效。

---

## 0. 结论摘要（先看这一段）

**通过（我一手复现并量到读数）**：槽位公式（`slot = max(end(deps))`，dur 恒 1）三方一致（DOM `data-slot` / 我自己按公式算的 / 从 PNG 像素反推的）、条宽 = slotW−2、条高 = `--team-dag-bar-h`、行高 = `--team-dag-row-h`（宽栏 38px 固定，内容不撑高）、刻度间距 = slotW、汇总条跨度 = 名下 min..max、空里程碑零宽菱形不画空条、`.team-dag-scroll` 上限 380px 且到顶滚动、行序 = 拓扑序（倒序声明夹具证明）、幂等（两次渲染 HTML 逐字相同）、条描边 = 既有 `--status-*` 令牌（像素级对上，含未解锁框 ×.55 的混合值）、teammate 色只落在条填充/左端 cap/色点（不沾描边）、role > 6 回绕时真的叠了斜纹、窄栏容器查询真的生效、成环/自指/缺失依赖不崩不假画边且有显形告警、恶意名字零注入、八个字段 + 三个标记在 DOM 里逐个找得到。

**我推翻了一条（作者没报、也没量到）：`F1` 里程碑之间的箭头与线段 y 锚点整体漏了一个框头高（26px），
导致「里程碑之间的依赖」这一项语法在画面上 100% 看不见** —— 箭头被目标框的**框头**完全盖住。
详见 §5.1（含复现与逐条读数）。这条不属于作者交的 8 条偏差，是我这轮新报的。

**其余 8 条作者自报偏差**：**7 条判「可接受」**（其中 ⑤/⑦ 我把口径/数字说准，⑧ 我给不出可复现的读数 → 标「未验到」），
**1 条判「缺陷」**（②：箭头落进任务名列，属低危，但**与 F1 叠加后变成 100% 不可见**，建议与 F1 一起修）。逐条见 §6。

---

## 1. 我怎么验的（可复现命令 + 我自己写的管线）

**不改产品代码**：所有量测脚本与中间产物在 `_tmp/verify-gantt/`（git 忽略、不入库）。
无窗口路径：`node` 里 `import` 产品渲染件出页面 → headless Chrome `--dump-dom` 拿 DOM 读数 + `--screenshot` 拿真像素 → `python + PIL/numpy` 量。

```powershell
# 0) 起静态源（产品渲染件是 ES module，file:// 下 import 不了）
python -m http.server 8137 --bind 127.0.0.1        # 仓库根

# 1) 两条必跑命令（原始输出尾部见 §2）
node --test gui/frontend/dist/*.test.mjs
go test -count=1 ./gui/

# 2) 一次跑完 11 组「夹具 × 宿主宽度 × 滚动位」的 DOM + PNG + 量测（lock / multi 另起一次，共 13 组）
powershell -ExecutionPolicy Bypass -File _tmp/verify-gantt/batch.ps1
python _tmp/verify-gantt/measure.py _tmp/verify-gantt/out/dom-<tag>.html _tmp/verify-gantt/out/shot-<tag>.png --out _tmp/verify-gantt/out/rep-<tag>.json
python _tmp/verify-gantt/check.py <每个 tag>      # → out/check-all.txt（判据 ①–⑧ 的汇总）
python _tmp/verify-gantt/domcheck.py <dom 文件>   # → out/domcheck.txt（判据 ⑩⑫：注入面 + 字段定位）
python _tmp/verify-gantt/final.py                 # → out/final.txt（判据 ①③⑦⑪ + F1 的读数）
```

脚本清单：`_tmp/verify-gantt/{fixtures.js,page.html,run.ps1,batch.ps1,measure.py,check.py,domcheck.py,final.py,detail.py,show.py}`。
`page.html` 里有两处**我自己写的**独立实现（不 import 产品的几何函数）：`mySlots()`（槽位公式）与 `myTopo()`（拓扑序），用来做「三方对照」的第三方。

### 1.1 我自己的夹具（**不复用**作者/设计稿的 fixture，共 8 份）

| 夹具 | 形状 | 用来打哪个判据 |
|---|---|---|
| `rev` | 3 里程碑链 + 6 工作项，**里程碑数组与工作项数组都按拓扑序倒着声明**；五种状态齐 | ①③④⑥⑦⑫ + 幂等。期望槽位 `w-alpha 0 / w-beta 0 / w-gamma 1 / w-delta 2 / w-eps 2 / w-zeta 3` |
| `revdone` | 同 `rev`，只把 `m-impl` 置 done | ⑤ 闸门两支对照（解锁框数变化） |
| `sketch` | 用户手绘 `c→d`、`b→a`、`d→a`（声明顺序 `d,a,c,b`，与拓扑序不同） | ①⑥：期望 **c 0 / b 0 / d 1 / a 2** |
| `lock` | 甲(active) 未 done → 乙**被锁**；乙的两根条在首屏可见，另有同一状态色的未锁对照条 | ⑦ 的 ×.55 混合算式 |
| `parallel` | 两块都无屏障依赖，但乙声明 `depends_on: [甲]`，两块的工作项都在槽 0 → 里程碑边 `gap = −1` | 偏差 ②⑦ 的样本（箭头落进任务名列） |
| `multi` | 丙同时依赖甲、乙（同一源框出 2 条里程碑边） | 偏差 ⑦ 的 lane 分层 |
| `info` | 单行八字段 + 三标记全填 | ⑫ 信息不丢（宽栏 760 + 窄栏 420） |
| `adv` | 成环（里程碑 + 工作项）、自指（里程碑 + 工作项）、指向不存在的工作项/里程碑、空里程碑、9 位 teammate（色板回绕）、恶意名字（id/role/name/goal/description/note/session_id 里塞 `"><img src=x onerror=…>`、`<script>`、`</textarea>`） | ⑧⑨⑩⑪ |

宿主宽度：宽栏 760px、窄栏 420px；滚动位 top / bottom；主题 light。
中间产物：`_tmp/verify-gantt/out/`（13 张 PNG、13 份 DOM、14 份量测 JSON —— 多出的 1 份是同配置的重跑，无第二含义）。

### 1.2 我的三条独立来源（不引用 leader/作者的任何坐标与数字）

- **A**：产品渲染件写进 DOM 的机读属性（`data-slot` / `data-*` / `getBoundingClientRect`）。
- **B**：`page.html` 里我自己按公式 `slot = 无依赖?0:max(slot(dep)+1)` 算的槽位。
- **C**：**从 PNG 里自己定标**：在刻度尺带（ruler 的 y 区间 ∩ `top:16px` 以下）按列统计 `--border-strong` 实色像素，
  取能连成等差链的竖线 → `plot_l = 214`（宽栏）/ `124`（窄栏），`slotW = 64` / `40`；
  再逐行在条的 y 带里扫「该行状态色（未解锁框按 ×.55 混合值）」，取**长度 ≈ slotW−2 且左端落在 `plot_l + k·slotW` 网格上**的连续列段 → 条 x 区间 → `round((x0−plot_l)/slotW)`。
  - `--border-strong` 取自 `getComputedStyle(document.documentElement)`（`styles.css` 的 `:root`，浅色第 34 行 `#d6d2c6`；深色第 177 行 `#474747`）。
  - **定标自检**：PNG 自己量到的 labelW（`plot_l − host_l`）= 208 / 118，与 DOM 的 `--team-dag-label-w`（208 / 118）逐字相同。
  - 文档坐标 ↔ PNG 坐标：宿主带 1px `#ff00ff` outline，PNG 里 magenta 连通块 bbox = `[host.l−1, host.t−1, host.r, host.b]`，与 DOM 宿主矩形精确吻合（如 `rev`：[5,5,766,618] vs host `(6,6)-(766,618)`）→ 两套坐标是恒等映射（DPR=1、无页面滚动）。
  - **判据 ① 里唯一的系统性差**：刻度竖线在列 `plot_l + k·64`（网格原点 = `.team-dag-content` 左沿），而条的左沿在 `215 + k·64`
    —— 恒差 **1px**（行的绘图区被框的 1px border 内缩，刻度尺没有）。我用 `round()` 折算槽位，**差 = 0**；
    1px 的可见影响是「箭头尖比目标条左沿偏左 1px」，肉眼看不出。记在这里，不当缺陷。

---

## 2. 两条必跑命令的原始输出尾部

```
$ node --test gui/frontend/dist/*.test.mjs
✔ formats file sizes (0.3062ms)
ℹ tests 678
ℹ suites 0
ℹ pass 678
ℹ fail 0
ℹ cancelled 0
ℹ skipped 0
ℹ todo 0
ℹ duration_ms 2619.1023
```

```
$ go test -count=1 ./gui/
ok  	github.com/RedHuang-0622/seelex/gui	3.562s
```

（`-count=1` 是为了避开 `(cached)`。全量原始输出在 `_tmp/verify-gantt/out/node-test.txt`（687 行）与 `go-test.txt`。）

---

## 3. 判据 ①–⑥ 的读数

### ① 每条工作项的槽位：DOM / 我算的 / PNG 反推，三方对照

只有「条完整落在滚动容器可见区内、且没被 sticky 尺/框头压住」的行才参与对照（其余如实标成「未量到」，不当失败）。

| 夹具（宿主×滚动位） | 行 → DOM `data-slot` / 我算的 / PNG 反推 | 差 |
|---|---|---|
| `rev` 760 top | w-beta 0/0/0 · w-alpha 0/0/0 · w-gamma 1/1/1 · w-delta 2/2/2（w-eps、w-zeta 视口外） | 0 |
| `rev` 760 **bottom**（scrollTop=112） | w-gamma 1/1/1 · w-delta 2/2/2 · **w-eps 2/2/2 · w-zeta 3/3/3** | 0 |
| `rev` **420** top | w-beta 0/0/0 · w-alpha 0/0/0 · w-gamma 1/1/1 · w-delta 2/2/2 | 0 |
| `sketch` 760 | **c 0/0/0 · b 0/0/0 · d 1/1/1 · a 2/2/2**（= 用户手绘 b0/c0/d1/a2） | 0 |
| `adv` 420 | r1…r9 全 0/0/0（9 位 teammate 行） | 0 |
| `info` 760 / 420 | i-dep 0/0/0 · i-wait 0/0/0 · i-main 1/1/1 | 0 |
| `lock` 760 | l-1a 0/0/0 · l-2a 1/1/1 · l-2b 2/2/2 | 0 |
| `multi` 760 | q1-a/q2-a/q3-a 全 0 | 0 |

未量到（如实列出，原因见括号）：`rev` top 的 w-eps / w-zeta（**视口外**，见 ④）；`rev` bottom 的 w-beta（滚出上沿）、w-alpha（**被 sticky 刻度尺 + 框头完全盖住**）；`adv` 760 的 r8…x-cyc-b（视口外）；`adv` bottom 的 15 行全部（滚到底后可见区落在这份夹具的尾段，行都在视口外）。

### ② 条宽 / 条高 / 行高 / 刻度间距

| 量 | 宽栏 760 | 窄栏 420 | 判 |
|---|---|---|---|
| PNG 量到的条宽 | **62** = slotW(64) − 2 | **38** = slotW(40) − 2 | PASS（每个夹具每行都在 ±0 内） |
| 条 `getBoundingClientRect().height` | **18** = `--team-dag-bar-h` | **16** | PASS |
| 行 `getComputedStyle().height` | **38px**（`--team-dag-row-h`） | **30px**（容器查询） | PASS，且**内容不撑高**（`info` 行填满八字段 + 三标记仍是 38px） |
| 刻度竖线间距（PNG 量） | **64** | **40** | PASS（差 = slotW） |
| 条左沿 `barRect.l − plot 左沿` vs `slot × slotW` | 0/64/128/192 全等 | 0/40/80/120 全等 | PASS |

### ③ 汇总条 / 空里程碑

`sum = min(start of rows)..max(end of rows)`，PNG 里量到的 x 与 `plot_l + s·slotW .. plot_l + e·slotW` 的差恒为 **+1px**（§1.2 那条 1px 系统性差）：

| 夹具 | 里程碑 | `data-sum-start..end` | PNG 量到的 x | 期望 x | 差 |
|---|---|---|---|---|---|
| rev 760 | m-design | 0..1 | 215..278 | 214..278 | +1 / 0 |
| rev 760 | m-impl | 1..3 | 279..406 | 278..406 | +1 / 0 |
| rev 760 | m-verify | 2..4 | 343..470 | 342..470 | +1 / 0 |
| adv 760 | a-good | 0..3 | 215..406 | 214..406 | +1 / 0 |
| info 420 | m-info | 0..2 | 125..204 | 124..204 | +1 / 0 |

**空里程碑**：`adv` 里 5 个空框（a-ghost / a-empty / a-cyc1 / a-cyc2 / a-self）—— 汇总条 `display:none`、菱形 `display:block`、`data-empty="true"`；
- 它们的菱形落点：`a-ghost`（依赖 `a-nope`，缺依赖）→ `s=e=0`；**`a-empty`（依赖 `a-good`，a-good.sum.e=3）→ `s=e=3`**（落在屏障前驱汇总条右端 ✓，与 README §2 口径一致）；
- 非空框（去重后 28 个）汇总条一律 `display:block`、菱形一律 `display:none`；空框（15 个 = `adv` 3 组图 × 5 个空框）反过来；
  **13 组量测里 0 处例外**，且**没有画出任何零宽/空条**。

### ④ 面板最大高度 380 + 滚动 + 被裁的行

| 夹具 | `.team-dag-scroll` clientH / scrollH | 未滚时被裁的行（PNG 里**零像素**） | 滚到底后 |
|---|---|---|---|
| rev 760 | **380** / 492（`max-height:380px`） | w-eps、w-zeta（二者条带内**一个该状态色的像素都没有**） | scrollTop=112，两行的条与箭头**真被画出来了**：w-eps PNG 槽 **2**、w-zeta PNG 槽 **3**（与 `data-slot` 一致） |
| adv 760 | 380 / 1170 | r9 起 7 行**零像素**；r8 只露出上沿（条底 454 > 视口底 453，不计入可量行） | — |
| sketch / parallel / info / lock | 248 / 256 / 210 / 294（**内容比 380 矮 → clientH = 内容高**，不出现空滚动条） | 无 | — |

`max-height` 计算值恒 `380px`；`overscroll-behavior: contain` 在位。

### ⑤ 未解锁框 / 闸门开合 vs `depends_on`（逐框报）

`rev` 760：`m-design(done,locked=false) → m-impl(active,locked=false)`（因为依赖方 m-design 是 done）、`m-verify(pending,locked=true)`；
- 闸门 `m-design→m-impl`：`data-open="true"`，文案「**闸门已放行 → m-design 全部 done**」；
- 闸门 `m-impl→m-verify`：`data-open="false"`，文案「**闸门 → 上一层全部 done 才放行（等 m-impl）**」。
`revdone` 760（m-impl 置 done）：两道闸门**都** `open=true`，m-verify 的 `data-locked="false"`（锁 → 解锁真的跟着 `depends_on` 翻转）。
`lock` 760：`L-1(active) → L-2(depends_on:[L-1])` locked=**true**，闸门 open=**false**（同一条判据 `hasUndoneDep`）。
`parallel` / `multi`：依赖方都是 done → locked=false、闸门 open=true。
`adv` 760（六道闸门）：`a-roles→a-good` 走第三支文案「**无屏障 → a-good 未声明 depends_on（谁都关不住）**」（作者报「本夹具没有样本」的那一支，我这里补上了样本）；
`a-good→a-ghost`（依赖 `a-nope` 不存在）open=false；`a-empty` 依赖 `a-good`(done) → open=true；成环那几道 open=false（互相等）。
**结论**：锁定/闸门与 `depends_on` 逐框一致，未发现「说锁着其实开着」这类不一致。

### ⑥ 行序 = 拓扑序（倒序声明证明）+ 幂等

- `rev`：里程碑数组声明序 `[m-verify, m-impl, m-design]`、工作项声明序 `[w-zeta, w-eps, w-delta, w-gamma, w-beta, w-alpha]`（**两处都倒着**）。
  渲染出来的框序 = `[m-design, m-impl, m-verify]`、行序 = `[w-beta, w-alpha, w-gamma, w-delta, w-eps, w-zeta]`
  → **顺序来自依赖，不来自数组**；逐行检查「前驱必须排在前面」= 0 违反；我自己的 Kahn 拓扑序与 DOM 行序在同一层级集合内一致（`m-impl` 内 `w-delta` 声明在 `w-gamma` 之前，渲染成 `w-gamma` 在前 ✓）。
- `sketch`：声明序 `d,a,c,b` → 行序 `c,b,d,a`（拓扑序）。
- **幂等**：同一份夹具连渲两次，字符串逐字相同（14 组量测的 `pureStringEqual` 全为 true：`rev` c506359d、`sketch` 9e62a61、`adv` beaafc08、`info` 194d5cf7、`lock` 880b5d34、`multi` 79813302 …），
  且插进 DOM 后 `host.innerHTML` 与第二份宿主（`display:none`，同一份夹具再渲一次）的 `innerHTML` 逐字相同（14/14 `sameAsSecondHost`）。

---

## 4. 判据 ⑦–⑫ 的读数

### ⑦ 条描边 = 状态（像素级），令牌从哪儿取的

令牌来源：`getComputedStyle(document.documentElement).getPropertyValue(name)`，即 `gui/frontend/dist/styles.css` 的 `:root`
（浅色第 67–71 行：`--status-running:#b26a00`、`--status-done:#1f9c63`、`--status-failed:#d64545`、`--status-info:#2e6be6`、`--status-idle:#9aa0a8`）。

| 行 | `eff` | 条 `border-color` 计算值 | 令牌 | 一致 | `border-style` |
|---|---|---|---|---|---|
| 全部量测里的可量行 | running/done/review/failed/pending | 例：`rgb(178, 106, 0)` / `rgb(31, 156, 99)` / `rgb(46, 107, 230)` / `rgb(214, 69, 69)` / `rgb(154, 160, 168)` | `#b26a00` … | **50/50 逐字相等**（含 1 份同配置重跑，去重后 46/46） | solid；`pending` = **dashed** |

**PNG 上的真像素**（不是只看 CSS 计算值）：
- `lock`：未锁框里 `l-1a`（done）左描边采样 `(31,156,99)` = `#1f9c63` ✓；锁定框里 `l-2b`（done）顶描边采样 `(131,200,169)`，
  与混合算式 `0.55×#1f9c63 + 0.45×255 = (132,201,169)` 差 **≤1/255** ✓；锁定框里 `l-2a`（running）顶描边 `(212,172,114)` vs `0.55×#b26a00+0.45×255 = (213,173,115)` ✓。
- 全图四色命中（容差 8/通道）：`rev` 图里 running 144px / done 906 / review 156 / failed 12（failed 少是因为那根条被滚动裁掉）——**四色确实出现在画面上**。
- 我另外跑了 `revdone`（解锁更多框）与 `lock`（同色未锁/已锁对照）来分离「颜色对不对」与「透明度对不对」。
- 注：`blended55` 的**全图计数不可靠**（`pending` 的 `#9aa0a8` 与 `--border-strong` 等灰几乎同色，会误命中），所以 55% 的结论我只用**指定坐标采样 + 混合算式对账**，不用全图计数。

### ⑧ 两通道正交（谁在做 / 什么状态）+ 色板回绕斜纹

- `lock`：`l-1a`（role=artist → `--team-dag-role-0` = `#6f5bd6`）在 `(x0+2, 条中线)` 采样 **`(111,91,214)` = `#6f5bd6` 逐字相等**（左端 cap），
  而同一根的左描边 = `rgb(31,156,99)`（绿 = done）→ **两个通道在像素上分开，描边没沾 teammate 色**。
- 条填充：`getComputedStyle(bar).backgroundColor` = `color(srgb 0.435294 0.356863 0.839216 / 0.14)` —— 就是 role 色（111/91/214 各 /255）**alpha 0.14**，
  与 README §5.1「条填充 = teammate 淡色 14%」一致；role 换成 frontend/verify 时三通道各自跟着换（3/3 对）。
- `adv`（9 位 teammate）：`r1…r9` 依次拿到 `--team-dag-role-0 … -5, -0, -1, -2`（第 7/8/9 位回绕到 slot 0/1/2），
  `bandClass` 含 `is-wrapped` 的正好是 **r7/r8/r9 = 3 条**；
  `getComputedStyle(band).backgroundImage` = `repeating-linear-gradient(45deg, rgb(111,91,214) 0px, rgb(111,91,214) 2px, rgba(0,0,0,0) 2px, rgba(0,0,0,0) 5px)`
  （r9 是 `rgb(15,143,158)` = `--team-dag-role-2`，色板回绕到 slot 2 ✓）；未回绕的行 `backgroundImage: none` ✓。
- 里程碑身份通道：`--team-dag-ms-tone` 逐框取自 `--team-dag-ms-tone-0..4`（实测三框 = `#6a7fa8 / #8a7fa8 / #6f8f7f`），与状态色/teammate 色都不同 → 三通道不撞。
- 连线通道：`--team-dag-edge` 从 `.team-board` 上读到 `#3f93d0`，**从 `:root` 读到空串** → 它没有泄露到全局（偏差 ⑥ 的核对点）。

### ⑨ 成环 / 缺失依赖 / 自指（试着推翻）

| 输入 | 结果 |
|---|---|
| 里程碑成环 `a-cyc1↔a-cyc2` | 不崩；两块都渲染出来，框头带「**依赖成环**」告警；层号归 0 并接在末尾 |
| 工作项成环 `x-cyc-a↔x-cyc-b` | 不崩；两条边都画了（`gap=-1`，不是零长线），两行都带「依赖成环」chip |
| 工作项**自指** `x-self.depends_on=["x-self"]` | `data-slot=0`（**不占槽位** ✓）、**没有画出退化自环**（边上没有任何 `x-self->x-self`）、行上显形「依赖成环」 |
| 里程碑自指 `a-self` | 不崩；按「成环」处理并接在末尾，框头显形 |
| 依赖指向不存在的工作项 `x-nope` | 不崩、**不假画边**（`x-ghost` 没有出边）、行上显形「依赖缺失：x-nope」+ `blocked=true` |
| 依赖指向不存在的里程碑 `a-nope` | 同上：不画边、框头显形缺失、闸门 open=false |
| 全图边数 | `adv` 只画出 2 条（那对成环边），其余非法依赖一条没画 ✓ |

一个**零像素**的小瑕疵（不算缺陷，记一下）：负 `gap` 的边在算绕行钩高度时会得到一个常量为正、变量系数为负的算式，
`spanPositive()` 只判「常量 > 0」就放行，于是会插进一个 `<i>` 空元素（渲染高度被浏览器钳成 0，**不画任何像素**）。

### ⑩ 恶意名字（零注入）

名字里塞 `"><img src=x onerror=window.__pwned=N>`、`"><script>…</script>`、`"><iframe src=//evil.example>`、`</textarea><img …>`（覆盖 id / role / name / goal / description / note / session_id）：

- `--dump-dom` 里 **未转义的 `<img` 出现 0 次、`<iframe` 0 次、`<script` 1 次**（那是页面自己的 module 脚本）；
  转义形态 `&lt;img` 出现 67 次；
- 页内 `window.__pwned` = **null**；`document.querySelectorAll("img")` = 0、`iframe` = 0；
- `data-team-item-open/-session/-role` 三个钩子的属性值里是**原样的恶意串**（属性值转义，正确）→ 零注入、零执行。

### ⑪ 窄栏（420px 宿主）容器查询真的生效

| 从哪里读 | `--team-dag-slot-w` | `--team-dag-label-w` | `--team-dag-row-h` | `--team-dag-bar-h` |
|---|---|---|---|---|
| `.team-board`（**板上**，根的那一份） | 64px | 208px | 38px | 18px |
| `.team-dag-scroll`（**滚动容器上**） | **40px** | **118px** | **30px** | **16px** |
| `.team-dag-row`（**行上**） | **40px** | **118px** | **30px** | **16px** |
| **PNG 自己量到的** | **40** | **118** | — | — |

→ 容器查询真的生效（`@container (max-width:520px)`），从板上读到的确实是**没生效的那一份**（提示里说的坑，实测复现）。
并且窄栏下条的实测宽 = 38 = 40−2 ✓、刻度间距 = 40 ✓、汇总条跨度 = 80（2 槽）✓ —— 几何整体跟着缩，**不是**只缩了某一个数。

### ⑫ 信息不许丢（宽栏 + 窄栏都查）

以 `info` 夹具的 `i-main` 为例（`--dump-dom` 里逐个定位）：

| 字段 | 定位方式 | 宽栏 | 窄栏 420 |
|---|---|---|---|
| `name` | `.team-dag-bar-name` 文本 / `.team-dag-name.is-openable` | 「主项」 | 同 |
| `role` | `.chip.team-role`（内含 `.team-dag-dot` 色点） | verify | 同（chip 在 DOM 里，窄栏只是视觉收窄） |
| `status` | `.team-status.is-<eff>` | running（实际显示 `running · 可重派`，因为 `interrupted`） | 同 |
| `depends_on` | 名列 `.chip.team-dep` ×2 + 条上 `.team-dag-depnote` | `depends_on: i-dep, i-wait → 槽 1` | 同 |
| `session_id` | `[data-team-session]` | sess-verify-42 | 同 |
| `worktree` | `.team-dag-wt` | wt/m-info/verify | 同（窄栏 `display:none`，**仍在 DOM**） |
| `goal` | `.team-dag-goal` 文本 | 目标全文 GOAL-TEXT | 同（窄栏隐藏） |
| `description` / `note` | 只在 `title` + `.team-dag-full` | 有 | 同 |
| `blocked/interrupted/live` | `.team-dag-flag[data-flag][data-on]` | true/true/true | 同 |
| 全文（八字段 + 三标记） | `.team-dag-card[title]` 与 `.team-dag-full`（`clip-path:inset(50%)`，DOM 里可见文本） | 两处都有 | 两处都有 |
| 入口钩子 | `data-team-item-open` / `-session` / `-role` / `data-team-session` | 四个全在 | 四个全在 |
| 机读属性 | `article.team-dag-row[data-status|data-eff|data-item|data-role|data-slot|data-dur|data-end|data-depth|data-milestone-id|data-interrupted|data-deps]` | 11 个全在 | 同 |
| 框头字段 | `.team-dag-ms-id / -name / .team-dag-layer / .team-status / 屏障 / .team-dag-lock / .team-dag-ms-count / -content` | 8 项全在 | 8 项全在（部分 `display:none`，仍在 DOM） |

---

## 5. 我试图推翻的结果

### 5.1 【缺陷 · 我这轮新报 · 作者没报】F1：里程碑之间的箭头/线段整体高了一个框头（26px），「里程碑间依赖」在画面上 100% 看不见

**判据**：`.team-dag-frame-head` 是 `position:sticky; z-index:5; background:var(--panel-solid)`（≈不透明）；
依赖边层 `.team-dag-edges` 是 `z-index:1`；框是 `z-index:2`。也就是说**框头永远压在连线之上**。
而渲染件把「汇总条的 y」算错了：

```js
// ganttLayout()（team-board-view.js）—— sum 那一行的 y 少了 --team-dag-head-h
y.set(`sum:${frame.id}`, spanAdd(frameTop, span(GANTT.frameBorder, { "--team-dag-sum-h": 0.5 })));
//          ↑ frameTop + 1 + 0.5*sumH   ← 少了 1 × --team-dag-head-h
const rowsTop = spanAdd(frameTop, span(GANTT.frameBorder, { "--team-dag-head-h": 1, "--team-dag-sum-h": 1 }));
//          ↑ 行那一行是对的
```

而真实版式是「框 border(1) → 框头(26) → 汇总条(22) → 行」，所以汇总条中线 = `frameTop + 1 + 26 + 11`，
代码算的是 `frameTop + 1 + 11` → **所有以汇总条为端点的边（也就是全部里程碑之间的边）整体上移 26px**。

**读数（5 个夹具 × 全部里程碑边，Δ = 实测 − 汇总条中线）**：

| 夹具 | 边 | 源汇总条中线 | 第一条横线的 y | Δ | 目标汇总条中线 | 箭头中心 y | Δ |
|---|---|---|---|---|---|---|---|
| rev | m-design→m-impl | 149.0 | 123.0 | **−26.0** | 309.0 | 282.5 | **−26.5** |
| rev | m-impl→m-verify | 309.0 | 283.0 | **−26.0** | 469.0 | 442.5 | **−26.5** |
| lock | L-1→L-2 | 149.0 | 123.0 | **−26.0** | 271.0 | 244.5 | **−26.5** |
| parallel | p-1→p-2 | 149.0 | 123.0 | **−26.0** | 271.0 | 244.5 | **−26.5** |
| multi | q-1→q-3 / q-2→q-3 | 271.0 | 245.0 | **−26.0** | 393.0 | 366.5 | **−26.5** |

（`--team-dag-head-h` = 26px；表格里的 `Δ` 就是它。`−26.5` = 上面再叠一个箭头自身 0.5px 的半高。）

**后果（像素级）**：箭头与「第一条横线段」正好落进**目标框的框头带**里 → 被框头盖住。
`rev` 图里对 `m-design→m-impl` 整条边逐段统计「该段矩形内真的是边色（#3f93d0, tol 40）的像素占比」：
`[0.0, 51.1, 0.0, 0.0, 0.0]`（横段 1 / 长竖段 / 横段 3 / 短竖段 / **箭头**），`m-impl→m-verify` = `[0.0, 39.1, 0.0, 0.0, 0.0]`
—— 两条边的**箭头、第一条横段、绕行钩、短竖段全是 0.0**（唯一非 0 的是那段长竖线穿出框头带以后的部分）。
把 7 组图（rev 760 top / rev 420 / rev 760 bottom / revdone / parallel / lock / multi）里的 **12 条里程碑边**全部统计一遍：
**箭头像素占比 100% 都是 0.0 —— 没有一条里程碑边画出过可见箭头。**
对照：同一张 `rev` 图里完整可见的工作项边 `w-alpha→w-gamma` = `[62.5, 52.3, 64.1, 74.4, 28.1]`
（<100% 是 1.5px 线在整数像素网格上的固有覆盖率：矩形窗口约 16px，线本身只占约 10.5px —— 所以**只有 0.0 才是「真被盖住」**，部分值不代表遮挡）。
逐像素图（`parallel`，未锁框所以框头**完全不透明**）：
`(284,145)`=边缘色 `(64,145,206)`（竖线可见）→ `(284,245)`=框头底色 `(245,247,245)`（**竖线被完全盖住**）→ `(271,244)` 与 `(207,245)` 都是框头底色（**箭头与绕行钩也被完全盖住**）；
`document.elementsFromPoint(284,245)` 最上层就是 `header.team-dag-frame-head`。
（`lock`/`rev` 里目标框是 `data-locked="true"`（`opacity:.55`），框头变成半透明，边会「透出来」成 `(162,201,228)`，
正好等于 `0.55×(0.97×panel + 0.03×edge) + 0.45×edge` —— 但那只在**被锁**的框上，未锁框就是纯遮挡。）

**期望修法（不由我改，交 leader 决策）**：`ganttLayout()` 里 sum 那一行补上 `"--team-dag-head-h": 1`。
补上以后汇总条 y = 149（正好在框头下沿 138 之下 11px），里程碑箭头自然从框头底下出来。
顺带说明：**这一条与偏差 ③ 是同一台机器**——正是这 26px 把边推进了框头带，才让「被框头遮断」从「偶尔断一截」变成「箭头 100% 不见」。

**证据不代写**：上面每一条都是我自己渲图、自己扫像素、自己读 rect 得到的；我没有引用作者 README §6.3 里「里程碑间箭头数 = Σ depends_on PASS」那句（它只数了边的条数，没量锚点）。

### 5.2 其他「试图推翻」的结果

- 倒序声明 → 行序/槽位不变（没推翻，见 ⑥）。
- 窄栏 → 我从 PNG 自己量到 40/118，且**从板上读到的是没生效的 64/208**（提示里的坑，复现了）。
- 恶意名字 → 零注入（没推翻，见 ⑩）。
- 成环/自指/缺依赖 → 不崩、不假画边、有显形告警（没推翻，见 ⑨）。
- 未解锁框 55% → 采样值与混合算式差 ≤1/255（没推翻，见 ⑦）。
- 幂等 / 滚动保持 / sticky → 幂等逐字相同；`rev` 滚到底后仍能读到正确的槽位（没推翻，见 ④⑥）。

---

## 6. 作者自报的 8 条偏差 —— 逐条裁决

| # | 作者自报 | 我的裁决 | 依据（我这轮的读数） |
|---|---|---|---|
| ① | 同槽依赖的走线按**槽位 gap<1** 判（稿子按 px gap<16） | **可接受** | 实测 px 间距只有两种取值：`gap=0 → 2px`、`gap=1 → 66px`（宽栏）/`42px`（窄栏）；`gap=−1 → −62px`。所以「px<16」与「槽位 gap<1」在 slotW≥2 时**判同一批边**（2 与 66 之间没有灰区）。语义没变，只是判据换了单位，还更稳（不依赖 slotW）。 |
| ② | **目标条在槽 0 时箭尾 7px 落在任务名列** | **缺陷（低危，但与 F1 叠加后变成 100% 不可见）** | 复现（`parallel`，里程碑边 `gap=−1`）：箭头 rect `l=207, r=214`，而绘区原点 `plot_l=214` → **箭头整条落在任务名列里**（`elementsFromPoint(207,245)` 最上层是 `header.team-dag-frame-head`，即它同时被框头盖住）。`multi` 同样 `l=207`。**复现步骤**：任一「依赖方汇总条起点槽 = 源汇总条终点槽」的里程碑边（即依赖方的工作项都在槽 0）。**它不是纯审美**：箭头指向任务名列里的一段空白，"哪里是目标"读不出来。归到 F1 一起修更省事（F1 修完后 `dst.x` 仍在槽 0 → 这 7px 还在，需要单独决定：目标在槽 0 时把箭头翻到条右侧 / 缩到 0 长度 / 或把绕行钩画在条下方）。 |
| ③ | 跨里程碑的边被 sticky 框头遮断 | **可接受（但与 F1 叠加成缺陷）** | 我复现了机制：`rev` 里 `w-gamma→w-eps` 的竖线 x=346，`m-verify` 框头带 432..458 与之重叠；未锁框的框头上采样 = 框头底色（**边 0 像素**），锁框（`opacity:.55`）上采样 `(162,201,228)` = `0.55×(0.97×panel+0.03×edge)+0.45×edge`（边被冲淡但没消失）。修法（把边层抬到框头之上）会让线压住框头文字，取舍合理，且与上一版行为一致 → **可接受**。**但**：正是这 26px（F1）把**全部**里程碑边推进了框头带，让「偶尔断一截」变成「箭头 100% 不见」→ 请连同 F1 一起看。 |
| ④ | 自指依赖不占槽位 | **可接受（正确）** | `adv`：`x-self.depends_on=["x-self"]` → `data-slot=0`、`data-depth=0`，且**没有** `x-self->x-self` 边（全图只有那对成环边）；行上显形「依赖成环」。里程碑自指同理（接末尾 + 显形）。 |
| ⑤ | 行高固定 38px、goal/description/note 只进 title + 隐藏全文 | **可接受，但口径要写准** | `info` 行填满八字段 + 三标记，`getComputedStyle(row).height` 仍是 **38px**（窄栏 30px）→「固定行高、内容不撑高」属实。**但「goal 只进 title」不准确**：`.team-dag-goal` 是**可见的**（宽栏显示 goal 截断到 240 字；只有窄栏 `display:none`）；真正只进 `title`+`.team-dag-full` 的是 `description` / `note`。建议 README 把这句改成「`description`/`note` 只进 title + 隐藏全文；`goal` 另有一条可见的截断行」。 |
| ⑥ | `--team-dag-edge` 是自造 hex（限 `.team-board` 作用域） | **可接受（作用域属实）**，但**要求登记** | 实测：从 `.team-board` 读到 `#3f93d0`（深色 `#6fb6e0`），从 `:root` 读到**空串**；`styles.css` 里 `team-dag` 出现 **0 次**（无第二份 CSS）。→ 作用域封闭属实。它与 `--team-dag-role-0..5` / `--team-dag-ms-tone-0..4` 是同一类自造值；README §8 已经把「要不要登记进 `styles.css` 令牌表」提请 leader —— 我的意见：**保持现状可接受**（状态色仍逐字引用既有令牌，自造值只在看板作用域内），但请 leader 明确拍一下，免得下一轮又开一次同一个问题。 |
| ⑦ | 里程碑边各自从槽 0 起（可能向左回绕） | **可接受（lane 口径属实），但请把 `src/dst` 说清** | 代码级 + 实测：x 锚点是 `src = xSlot(源.sum.e)`、`dst = xSlot(目标.sum.s)`（不是「从槽 0 起」）；**每框的 `lane` 各自从 0 起**这一点属实 —— `multi` 里同一个源框出 2 条边：`q-1→q-3` 钩长 7px（lane 0）、`q-2→q-3` 钩长 13px（lane 1，`reach = hook + lane*6`）。「向左回绕」也属实（`dst < src` 时横段向左走 78/84px，终点 `x=207`），其后果就是偏差 ②。 |
| ⑧ | 框头 sticky 实测 delta −1px | **未验到（我复现不出 −1px）** | 我的读数：`scrollTop=0` 时 `rulerDelta = +4`（刻度尺还没吸住，它在 `.team-dag-content` 的 4px padding-top 之下），各框头 `topVsRulerBottom = 9px`（也没吸住）。**我没能复现「−1px」这个具体数**：它取决于「框头是否已经吸住」，而吸住条件随夹具高度/滚动位变化；我也没构造出「尺子与框头同时处于吸附态」的取样点。所以这条我**不当通过也不当失败**，列为「未验到」（理由见 §7 第 1 条）。可确证的是 CSS 在位（`position:sticky; top:0` 尺子 / `top:var(--team-dag-ruler-h)` 框头）与「滚动时框头确实压在连线之上」（见 F1 的像素图）。 |

---

## 7. 我做不到 / 没验到的（如实）

1. **框头/刻度尺「吸住」状态的 delta**：我 13 组夹具里内容超出 380px 的只有 `rev`（492）与 `adv`（1170）。
   我量到的是「未吸住」时的相对位置（`rulerDelta=+4`、`headVsRulerBottom=+9`）；**「吸住后 delta 应精确为 0/−1」我没量到**，
   因为我没构造「让尺子与框头同时处于吸附态、再去读 delta」的取样点（`?scroll=` 落到既吸附又能整行可见的位置需要专门调）。
   这条标**未验到**（不是失败）。
2. **作者偏差 ⑦ 的 lane 只在 2 条边的样本上验过**（`multi`），`lane > 1`（3 个以上前驱汇聚）没样本。
3. **`team-board-preview.html` 我没有单独打开验**（我这轮用自己写的 `page.html` + 同一份产品渲染件 + `styles.css`；
   预览页本身只是宿主，渲染件相同 → 风险低，但「预览页能不能跑起来」我没验）。
4. **深色主题只验了令牌层面**（`--team-dag-edge` / role / ms-tone 的深色值存在且不同），没出深色图做像素比对。
5. **60 条单测的「语义断言一条都没删」**：我跑了全量 `node --test`（678 pass），但我**没有逐条 diff 断言清单**去证明作者没删过语义断言
   （`team-board-view.test.mjs` 与本轮之前一版相比是 +307/−? 的改动，我只读了它的新增部分标题，没做逐条审计）。
6. **跨里程碑的工作项依赖的两套口径**（见 §7.1）：我报了现象，但没给「谁对」的裁决——那是口径问题，得 leader 定。

### 7.1 顺带发现（不是作者 8 条里的，也不是回归）：同一份 `depends_on` 被两套视角读

`ganttModel()` 的槽位用**全计划**的 work_item 表算（`byID = workItemsOf(plan)`），而行的标记 `blocked` / `missing_deps` 用**里程碑内**的表算
（`workItemEntry(item, byID=本里程碑的 items)`，这段代码在本轮**没改过**，`git show c6ac829:…` 与 HEAD 一致 → **不是本件引入**）。
后果（`rev` 夹具，w-gamma 的依赖 w-alpha 是 **done**）：

```
w-gamma  deps=w-alpha  domSlot=1  flags=[blocked=true,…]  warn=[依赖缺失：w-alpha]
```

→ 条**右移了一格**（几何认这条跨里程碑依赖）、边上也画了出来，可行上却写「**依赖缺失：w-alpha**」+ `blocked=true` —— 同一份事实两处说法相反。
`w-delta / w-eps / w-zeta` 同样（4/4 跨里程碑依赖都被标成「缺失 + 被卡住」）。
**要不要修由 leader 定**（可选修法：标记也用全计划的表算，或把「跨里程碑」单独做成一种 chip 语义）；我只报现象与读数。

---

## 8. 复现用的脚本（都在 `_tmp/verify-gantt/`，git 忽略、不入库）

| 文件 | 作用 |
|---|---|
| `fixtures.js` | 我自己写的 8 份夹具（见 §1.1） |
| `page.html` | 宿主页：注入产品 `TEAM_BOARD_CSS` + `styles.css` 令牌，渲染 `renderTeamBoard()`；页内探针写 `#probe`（含我自己写的 `mySlots()` / `myTopo()` 与 `?hit=x,y` 的 `elementsFromPoint` 栈） |
| `run.ps1` | 单组：headless Chrome `--dump-dom`（走 `cmd /c … > out`）+ `--screenshot`（绝对路径）；`--window-size = 宿主宽+40` |
| `batch.ps1` | 11 组一次跑完并调 `measure.py`（`lock` / `multi` 各另跑一次） |
| `measure.py` | 从 PNG 自己定标（刻度竖线 → plot_l/slotW）→ 逐行扫条推槽位 → 逐段统计边像素 → 采样点 → 三色扫描 |
| `check.py` / `detail.py` / `final.py` / `domcheck.py` / `show.py` | 判据 ①–⑫ 的汇总、逐行明细、F1/③/⑦/⑪ 的读数、注入面与字段定位 |

产物：`_tmp/verify-gantt/out/{shot-*.png(13), dom-*.html(13), rep-*.json(14), check-all.txt, domcheck.txt, final.txt, node-test.txt, go-test.txt}`。

---

## 9. 给 leader 的一句话

**核心几何我复现并量到了，可以签收**；但 **F1（汇总条 y 锚点漏 `--team-dag-head-h`）必须修** ——
它让「里程碑之间的依赖」这一项语法在画面上完全看不见，而作者的自证只数了边的**条数**、没量**锚点**，所以漏了。
修法一行（`ganttLayout()` 的 sum 那一行加一个变量），修完请再出一张图量一次「箭头中心 y == 目标汇总条中线」。

---

# § 修复复核（wi-gantt-reverify）：对 `23d9448` 的独立再验证（**只验不改**）

- 验证者：`verify`（与修复者不是同一个人；本轮只写 `_tmp/` 与本节，**没动 `gui/frontend/dist/**` 与任何产品代码**）
- 被验 commit：`23d9448889edb355fa81ed137a16f56b719f3023`（`feat(team-board): 修里程碑边不可见/箭尾越列/跨里程碑依赖误报（wi-gantt-fix）`）
- 上一轮基线：`69af50d7533cd5c2f5f88455d0d3234369b7310b`（我上一轮验的那一版）

| 文件 | blob（23d9448） | blob（69af50d，改前对照） |
|---|---|---|
| `gui/frontend/dist/team-board-view.js` | `46d5a7a3497f3dcf2b1f1566697d88c23f8190e4` | `213865d06d583f78323b44edb9e20d11522e40c9` |
| `gui/frontend/dist/team-board-view.test.mjs` | `ee631bc0404aa04e09c22fd8a53112aebe7caddc` | — |
| `gui/frontend/dist/team-board-preview.html` | `4d1b3fe1ba1965f437b5911b7a0732ae07fd0cff` | — |

改动面（`git show --stat 23d9448`）：**只有这 3 个文件**（view.js `+83/−20`、test.mjs `+144`、preview.html `+2`）。`styles.css` 没被碰（我 grep：`styles.css` 里 `team-dag|team-board` 命中 **0 次**），`app.js` 没被碰。

## 0. 结论摘要（先看这一段）

- **F1（里程碑边整体上移 26px、整条锚进 sticky 框头带 → 画面上完全看不见）：真好了。**
  「第一条横段中心 y − 源汇总条中线 y」从 **−26.00px → 0.00px**（4 条里程碑边逐条同值）；「箭头中心 y − 目标汇总条中线 y」从 **−26.50 → −0.50**（那 0.5px 是箭头 DOM 盒高被报成 6 而非 7 的一半，见 §2 分母口径）。
  像素：改前第一条横段 `f = 0.027/0.031/0.031/0.030`，**inkInHeadFrac = 1.000（整段压在源框框头带里）**、纯边色命中率 `tolFrac = 0.000`；改后同一批段 **`inkInHead` 全部为「不落在任何框头带里」**，可见样本 `f = 0.857~0.992`，箭头 `ink ≈ 21px²`（= 形状面积，即 100%）。
- **②（箭尾/竖线越进左边「任务名列」）：真的不越列了。** 「所有线段矩形 `left` − 绘图区左沿」的最小值 **−7.750px → +1.250px**；宽栏（slotW 64 / labelW 208）与窄栏（40 / 118）**两档各量一遍，两档同值**。箭头 `left` 从 `plotOrigin−7` 变成 `plotOrigin+2`。
- **③（跨里程碑依赖被读成「依赖缺失」且永久 `blocked`）：真的说真话了。** 三种依赖逐条：跨里程碑且 done（改前「依赖缺失：x-done」+`blocked=true` → 改后「跨里程碑依赖：x-done（只允许同里程碑内）」+`blocked=false`）、全计划不存在（两版都「依赖缺失：zz-nope」+`blocked=true`）、跨里程碑且没 done（改前「依赖缺失：x-run」→ 改后「跨里程碑依赖：x-run」+`blocked=true`）。
- **没退化**：三方槽位一致（74 行 **0** 处不一致）、条宽 = slotW−2（62/38）、汇总条 min..max、空框零宽菱形、行高 38/30（**从行上读**）、滚动上限 380 且到顶滚、幂等（两次渲染逐字节）、恶意名字零注入、窄栏容器查询生效、`styles.css` 无第二份甘特 CSS、`app.js` 未改。逐项读数见 §6。
- **我推翻不到新缺陷。** 只量到 3 条**同一类**的残留（都属「边层在框/行之下」的既有取舍，与我上一轮判「可接受」的 ③ 同源，**且都不是 26px 那一档的毛病**），逐条见 §7。
- **我做不到的**：`lane>1`（3 前驱汇聚）仍无样本；框头「吸住后 delta = 0/−1」这个具体数仍没构造出取样点；深色主题只验了令牌层面。见 §8。

## 1. 两条必跑命令（我一手复现，原始输出尾部）

```
$ node --test gui/frontend/dist/*.test.mjs
✔ formats file sizes (0.4092ms)
ℹ tests 681
ℹ pass 681
ℹ fail 0
ℹ duration_ms 4919.4383
```

```
$ go test -count=1 ./gui/
ok  	github.com/RedHuang-0622/seelex/gui	4.216s
```

（`681 > 678`：修复者新加了用例（`+144` 行）。**我只跑不审**它的断言写法——见 §8 第 4 条。）

## 2. 我怎么量的（可复现，全部在 `_tmp/verify-gantt/`，git 忽略、不入库）

**关键手法：同一份夹具分别喂「改前(`69af50d` 的 view.js)」与「改后(`23d9448` 的 view.js)」两份产品渲染件**
（`git show 69af50d:gui/frontend/dist/team-board-view.js > _tmp/verify-gantt/src-prefix.js`）。
所以下面的「改前」全是我这一轮自己渲出来的读数，**不引用修复者的任何数字**。

- **我这轮自己写的 4 份夹具**（不复用修复者的）：
  | 夹具 | 形状 | 打哪一条 |
  |---|---|---|
  | `chain` | 4 块里程碑 × 各 2 个工作项；`m-a(done)→m-x(done)→m-b(running)→m-c(pending)`；里程碑边 gap = 2 / 0 / 0 / 0（**两种走线支路都有**），目标 `m-c` 是**锁定框**（`opacity:.55`），另有 3 条**跨框的工作项边**（竖线穿过别的框） | F1（锚点 + 逐段墨量）、锁定/未锁两支、§7 的穿行 |
  | `slot0` | 目标汇总条起点槽 = 0（依赖方的活全在槽 0）、源汇总条终点槽 = 1 → 里程碑边 `gap = −1` | ② |
  | `deps3` | 三种依赖：跨里程碑且 done（`x-done`）/ 全计划不存在（`zz-nope`）/ 跨里程碑且没 done（`x-run`），+ 同里程碑对照（`d-e`）；里程碑屏障里再放一个不存在的里程碑 id | ③ |
  | `regress` | 空里程碑（零宽菱形）、9 位 teammate（色板回绕）、恶意名字（id/name/goal/description/note 里塞 `"><img src=x onerror=…>`、`<script>`）、长链（7+2 个工作项，够滚） | 没退化各项 |
- **宿主**：宽 1000（`.team-board` 内联宽，> 容器查询阈值 520）与窄 420；滚动位 `scroll=0` / `scroll=bottom`。
  参数**内联在页面里**（`var CFG = {...}`），URL 不带 query（为什么：见 §8 第 6 条的坑）。
- **管线**：`node` 把产品渲染件内联进宿主页（剥 `export`、补一份 `escapeHtml`）→ headless Chrome `--headless --dump-dom` + `--screenshot`（`--force-device-scale-factor=1`、window = 宿主宽+40 × 1160、页面本身不滚）→ 页内探针把读数（DOM `rect` / `data-*` / 计算样式 / 幂等 / 注入面）base64 写进 `<pre id="probe">` → `python3 + PIL/numpy` 量像素。
- **像素口径（重要，否则 f 会被读错）**：每个场景渲**两张图** —— 正常版 + `noedges` 版（只把 `.team-dag-edges` 设 `display:none`，不动布局）。逐像素解 `p ≈ α·edge + (1−α)·bg`（`bg` 取 noedges 版同像素、`edge` 取 `--team-dag-edge` `#3f93d0`，用投影最小二乘并夹到 `[0,1]`），`ink = Σα`。
  - **分母**：线段用「**设备像素对齐后**的包围盒面积」——Chrome 把 1.5px 的线贴成 **2px** 画（不只是抗锯齿：整列/整行都是纯边色），用 `w×h` 会**低估 1/3**；箭头用它的真实形状三角形 `0.5×7×6 = 21px²`（CSS 是 `border-left:7px / border-top:bottom:3.5px`，DOM 报盒高 6，满量就是 21；读到 `24.58` 的两个样本是因为同一包围盒里还压进了竖线 0.75px 的边条）。**`f` = ink / 分母，1.00 = 整段可见**。
  - 线段与线段在转角处互相压进对方包围盒 ~2px²，所以个别段 `f` 会略 >1；我在表里标出来。
- **坐标恒等**：宿主 1px 洋红 outline 的 PNG bbox 与 DOM 宿主矩形逐边吻合（`maxx/maxy` 差 0）→ PNG 像素 = 视口坐标（DPR=1、无页面滚动）。
  绘图区左沿我另从 **PNG 刻度竖线**自己量（`--border-strong #d6d2c6`）：12 个渲染里 `plotOriginFromTicks == DOM .team-dag-ruler-plot.left` **全部相等**（208 / 118），`slotW` 从刻度间距量到 **64 / 40**。

## 3. F1 读数（`chain` 夹具，4 条里程碑边；滚动视口 y=67..447）

| 里程碑边 | gap | 改前 第一条横段 y / 源汇总条中线 (Δ) | 改后 (Δ) | 改前 箭头 y / 目标汇总条中线 (Δ) | 改后 (Δ) |
|---|---|---|---|---|---|
| `m-a→m-x` | 0 | 116.25 / 143.00 (**−26.00**) | **0.00** | 273.50 / 303.00 (**−26.50**) | **−0.50** |
| `m-a→m-b` | 2 | 116.25 / 143.00 (**−26.00**) | **0.00** | 433.50 / 463.00 (**−26.50**) | **−0.50** |
| `m-x→m-b` | 0 | 276.25 / 303.00 (**−26.00**) | **0.00** | 433.50 / 463.00 (**−26.50**) | **−0.50** |
| `m-b→m-c` | 0 | 436.25 / 463.00 (**−26.00**) | **0.00** | 593.50 / 623.00 (**−26.50**) | **−0.50** |

（`Δy = 0.00` 对**每一条**边、**两种走线支路**（`gap≥1` 直落 / `gap=0` 绕行钩）都成立；`−0.50` 只是箭头 DOM 盒高被报成 6 的一半，像素上是满墨。`--team-dag-head-h` = 26px，就是改前那个 `Δ`。）

**逐段墨量（`f` = ink / 形状面积；`inkInHead` = 该段墨量落在**任何 sticky 框头带**里的比例）**

| 段 | 改前 f | 改前 inkInHead | 改前 tolFrac(纯边色命中) | 改后 f | 改后 inkInHead |
|---|---|---|---|---|---|
| `m-a→m-b` 第一条横段（121px，y116.25→142.25） | **0.031** | **1.000** | **0.000** | **0.992** | 不落框头带 |
| `m-a→m-x` 第一条横段（7px） | **0.027** | **1.000** | **0.000** | **0.857** | 不落框头带 |
| `m-x→m-b` 第一条横段（13px） | **0.031** | **1.000** | **0.000** | **0.917** | 不落框头带 |
| `m-a→m-x` 箭头 | **0.014** | **1.000** | **0.000** | **1.010**（ink 21.22 / 21） | 不落框头带 |
| `m-a→m-b` 箭头 | **0.031** | **1.000** | 0.000 | **1.003**（滚到底的 `chain-bottom-fix`；ink 24.58 含竖线边条） | 不落框头带 |
| `m-x→m-b` 箭头 | **0.031** | **1.000** | 0.000 | **1.003**（同上） | 不落框头带 |
| `m-b→m-c` 箭头（目标**锁定框**） | **0.415**（框头 55% 透出的**残影**，不是线） | 1.000 | **0.000** | **0.866**（ink 21.22/21，锁框但箭头在框之上） | 不落框头带 |

- 改前「`f ≈ 0.03` 而不是 0」的原因是**框头底色 `rgba(251,250,246,.97)`**：线的 3% 会透出来 → 所以表里同时给 `tolFrac`（严格比边色）：**改前每一条都是 0.000，改后 0.367~1.000**。锁框那一档（`.55` 透明度）残影更大（0.415）但**同样一条纯边色像素都没有**。
- **滚动位交叉验证**：滚动到顶时滚出视口的样本（如 `m-b→m-c` 的第一条横段 y=462.25），滚到底的 `chain-bottom-fix` 给出同段 `f = 0.855`（第一条横段）与 `1.003 / 1.003 / 0.866`（三条箭头）——**不是"看不见"，是"在屏幕外"**（我的脚本对每个段都标了 `inView`，表里凡我标"(视口外)"的都以滚到底那一张为准）。
- **新观察（小，1px 量级，我没当缺陷）**：横段的**第一个 1px 列**被**源框的汇总条**吃掉（框在边层之上、汇总条是实色 `--team-dag-ms-tone`）——121px 段少 2px²、13px 段少 2px²、7px 段少 2px²，这就是 `0.992 / 0.917 / 0.857` 的来源。改前同位置的 1px 也这样（`m-b→m-c` 改前 f=0.030 里那点也是它），**不是本件引入**。

## 4. ② 读数（`slot0` 夹具：目标汇总条起点槽 = 0、`gap = −1`）

| 场景 | 改前 `min(rect.left − 绘图区左沿)` | 改后 |
|---|---|---|
| 宽栏（slotW 64 / labelW 208） | **−7.750px**（竖线 `left=200.25`；箭头 `left=201` = 左沿 −7） | **+1.250px**（竖线 `209.25`；箭头 `left=210` = 左沿 +2） |
| 窄栏（slotW 40 / labelW 118） | **−7.750px**（竖线 `110.25`；箭头 `111`） | **+1.250px**（竖线 `119.25`；箭头 `120`） |

- 越列的就是那两段：`gap<1` 走**绕行钩**支路，钩的落点/回折都在 `tipX`，改前 `tipX = plotOrigin − 7`，竖线再减 0.75 → `−7.75`。
- 改后 5 段（钩 → 竖 → 下横 → 回折竖 → 箭头）**全部落在绘图区内**，最小值就是那根竖线的 `+1.25`。
- **箭尖压在目标条上的可读性**（我试着推翻，见 §7 第 3 条）：改后箭尖锚点 = `plotOrigin+2`，目标汇总条左端 = `plotOrigin+1` → 箭头（7×6 三角）落在**条的最左 7px 之上**（箭头 z-index:3 > 框 2，实测该处 ink 满量、`tolFrac` 0.367~0.857）→ **读得出来**，不是"压在条下面"。对照改前：箭头整段（201..208）落在任务名列的空白里，**"到哪里"读不出**。

## 5. ③ 读数（`deps3` 夹具，逐条对照）

| 工作项 | 依赖 | 改前 | 改后 |
|---|---|---|---|
| `d-a` | `x-done`（**别的里程碑里、done**） | 「依赖缺失：x-done」+ `blocked=true` + 无跨里程碑标记（**假话**） | 「跨里程碑依赖：x-done（只允许同里程碑内）」+ `blocked=false` + `data-cross-deps="x-done"` |
| `d-b` | `zz-nope`（**全计划不存在**） | 「依赖缺失：zz-nope」+ `blocked=true` | 同改前（**属实**，`blocked=true`、`data-cross-deps=""`） |
| `d-c` | `x-run`（**别的里程碑里、没 done**） | 「依赖缺失：x-run」+ `blocked=true`（**假话**） | 「跨里程碑依赖：x-run（只允许同里程碑内）」+ `blocked=true`（**两句都是真的**） |
| `d-e` | `d-a`（同里程碑、没 done） | 无告警 + `blocked=true` | 同改前（对照，未受影响） |

- 三种依赖的**真假判据**（`data-flag="blocked"`）逐条核对：`false / true / true / true`，与"依赖到底 done 没 done"一致（改前 `d-a` 说 `true` 是错的：`x-done` 是 done）。
- `data-cross-deps` 是新属性（改前没有这个字段，我的量测脚本一开始还因缺字段报错 → 这本身就说明它是新加的）；行内还有「跨里程碑依赖…」chip，`title`/`.team-dag-full` 全文里也多了 `cross_deps:` / `missing_deps:` 两行。
- 里程碑层面：`d-1` 的屏障 `[d-0, ms-nope]` 两版都写「依赖缺失：ms-nope」（**里程碑 id 不存在，属实**），闸门文案两版都点出被等的两个 dep。

## 6. 没退化（逐项，都是我这轮自己量的）

| 判据 | 读数 |
|---|---|
| 槽位三方一致 | **74 行**（12 个渲染）：DOM `data-slot` / 我按 `slot = max(slot(dep)+1)`（自指、未知 id、成环按同口径归 0）自己算的 / **PNG 反推**（扫条描边色 → `x0` → `(x0−plotOrigin−1)/slotW`）——**0 处不一致** |
| 条宽 = slotW − 2 | DOM 62（64−2）/ 38（40−2）；PNG 也是 62 / 38 |
| 汇总条 = 名下 min..max | `g-1` 槽 0–5、`g-3` 槽 0–2、`slot0` 的 `p-2` 槽 0–2 |
| 空框零宽菱形 | `g-2` `data-empty=true`、`sumBar=null`（不画空条）、菱形 12.728×12.728 落在 `x=529`（= 绘图区左沿 + 前驱汇总条右端 5×64 + 1），`sumNote` 写「槽 5–5 · 空」 |
| 行高 38 / 30（**从行上读**） | `getComputedStyle(row).height` = 38px（宽）/ 30px（窄）→ 容器查询真的生效（`--team-dag-slot-w/label-w` 从 `.team-dag-scroll` 读到 40/118） |
| 滚动 | `--team-dag-scroll-max-h: 380px`；clientHeight 380、scrollHeight **652 / 606 / 532 / 408**（四份夹具）；滚到底后所有段读数仍正确（§3/§4 的 bottom 行） |
| 幂等 | 12 个渲染 × 两次 `renderTeamBoard` **逐字节相同**（`idempotent=true`，同时打印了 `htmlLen`） |
| 恶意名字零注入 | `img/svg/textarea/iframe` 节点 **0**、`.team-board` 内 `script` 节点 0、board HTML 里没有 `<img`；恶意的 `onerror=` / `<script>` **只作为文本**出现在 `title` / `.team-dag-full` 里 |
| 9 角色回绕 | `is-wrapped` 只出现在第 7/8/9 个角色（`r6/r7/r8`）的行上，前 6 个没有 |
| 无第二份 CSS | `styles.css` 里 `team-dag|team-board` 命中 **0 次** |
| 只改了该改的 | `git show --stat 23d9448`：只有 view.js / test.mjs / preview.html；`app.js` 未动 |

## 7. 我试着推翻的结果（★四条）

1. **★ 修 F1 会不会把别的东西带坏：竖段穿过闸门带/框头带时会被截断吗？**
   **会被截断，量到了**：`m-a→m-b` 的竖线（`x=456`、几何长 320px）改后仍被吃掉 **93px**：
   `y=265..291 (27px) 压在 m-x 的框头带下`、`y=299..306 (8px) 与 y=323..342 (20px) 压在 m-x 的**行带**里（行内的 `.chip`/`.team-dag-after` 文本盖住）`、`y=425..462 (38px) 压在 m-b 的框头带下`；整段 `f = 0.658`（改前 0.693 —— 差的那部分正是"改前竖线本身少走 26px"）。
   **能接受**：这是「边层在框**之下**（`z-index:auto < 2`）」的既有取舍（= 我上一轮判可接受的 ③，README §7.1 也写明了），而且改后**没有"整段消失"**——**"整条边最关键的那一点"（第一条横段 + 箭头）已经回到可见区**（§3）。**竖线被截断的位置稳定、断口都在两端，不产生误导性的半条线**。
2. **★ 里程碑边和工作项边会不会互相穿进对方的行里？**
   **会**（这是"边从别的框的行带下面穿过"的必然结果）：我在几个断口上 `elementsFromPoint` 依次命中 `SPAN.chip → SPAN.team-dag-after → DIV.team-dag-plot → DIV.team-dag-card → ARTICLE.team-dag-row`，即线在 **8~27px** 一段上被行内的 chip/文本吃掉。**改前一模一样**（同一个 z-order、同样的断口位置）→ **不是本件引入**。要不要把边层抬到行之上是取舍（抬上去线会压住行里的字），我没推翻，交 leader。
3. **★ 槽 0 的新修法会不会让箭尖压在目标条下面读不出来？**
   **不会**，见 §4 末段：箭尖锚点 = 目标条左端 +1（`plotOrigin+2` vs 条左端 `plotOrigin+1`），箭头整体 7px 宽落在**条的最左缘之上**（箭头 z-index 3 > 框 2），ink 满量（21/21）。**但**它把"越列 7px"换成了"横段起点那 1px 被汇总条压住"（§3 末段），量级小两个数量级，我判可接受。
4. **★ 上一轮判"可接受"的那几条，改动后还成立吗？**
   ① 同槽判据（槽位 gap<1 而非稿子的 px<16）—— 没动，本轮 gap 只有 `0/2/−1` 三档；④ 自指不占槽位、不画退化自环 —— 没动（我这轮量到的是"依赖不存在的 id"这一支：`d-b` slot=0、不画边、只出告警，同口径）；⑤ 行高固定 38/30、goal 可见而 description/note 只进 title+全文 —— 从行上读到 38/30 ✔；⑥ 自造 hex 限 `.team-board` 作用域 —— `styles.css` 0 命中 ✔；⑦ lane 分层 —— 仍只有 `lane 0/1` 样本（见 §8）；⑧ 框头吸住 delta —— 仍没量到那个数（见 §8）。

## 8. 我做不到 / 没验到的（如实）

1. **`lane > 1`（3 个以上前驱汇聚到同一目标框）仍然没有样本**：本轮 lane 只验到 0 与 1（`chain` 里 `m-b` 只有 2 个前驱，两条边 gap 分别是 2 与 0）。
2. **框头"吸住"后的 delta（应为 0 / −1 那个数）** 还是没构造出取样点：我只量到"未吸住"时的相对位置，以及"粘住的框头吃掉连线"这个像素事实（§7.1 的 `head:m-b` 断口就是它），**没量到 delta 本身**。
3. **深色主题**只验了令牌层面（`--team-dag-edge` 深色 `#6fb6e0` 存在且不同），没出深色像素图。
4. 修复者新加的 `+144` 行用例（`681 − 678 = 3` 条）**我只跑了、没逐条审**它的断言写法；新增的 `data-cross-deps` / 全文里的 `cross_deps:` `missing_deps:` 行我读的是 **DOM 事实**，没审单测怎么钉的。
5. 本轮**只验我自己这 4 份夹具的 12 个渲染**；`team-board-preview.html`（修复者只加了两行注释）**没打开验**。
6. **我这条量测管线自己的两个坑**（不是产品问题，记下来免得下一轮再踩）：
   - URL query 里的 `&` 在 PowerShell / `cmd /c` 下会被当**命令分隔符** → 我第一遍 18 张图的 `scroll/noedges` **全部没生效**（已废弃该做法，改成把参数内联进页面 `var CFG`）。
   - 夹具 JSON **内联进 `<script>` 时 `</script>` 会提前闭合脚本** → 我的 `regress` 夹具里那个 `<script>alert(1)</script>` 名字一度把 headless Chrome **卡死**（`--dump-dom` 等 load 事件永远等不到），已按 `<\/` 转义修好并加了 `imgsBefore` 计数自证。

## 9. 给 leader 的一句话

**`23d9448` 把 F1 / ② / ③ 三条都真的修掉了**，我一手复现并逐段量到了读数（Δy `−26.00 → 0.00`、`min(rect.left − 绘图区左沿)` `−7.75 → +1.25`、三种依赖三句话都对、改后每条边都真的画出了箭头）；
**没推翻出新缺陷**，只留下 3 条同源的既有取舍（边在被压的框头带/行带下被吃掉、横段起点 1px 被汇总条压住）——建议按上一轮同口径判**可接受**，不必再修。
