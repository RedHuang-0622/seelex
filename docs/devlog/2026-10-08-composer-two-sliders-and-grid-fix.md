# composer 只留两条独立拉条：档位滑杆下线 + 滑轨塌成 0 宽的现场修复

日期：2026-10-08
范围：`gui/frontend/dist`（前端 + 契约测试）、`gui/bridge.go`（注释）、
`docs/gui`（模块档 + ADR + 索引）、`docs/devlog`

## 0. 一句话

composer 底部的运行参数只剩**两条各自独立**的拉条（推理强度 / 循环次数）：一次同时
改三项的 Effort 档位滑杆从界面下线（只留 `/effort` 命令）。同时修掉一个真缺陷——
那两条独立拉条此前**根本没有可见的滑轨**：容器只声明了 3 列而拉条有 5 个子元素，两个
子元素被排到第二行，滑轨落进 `auto` 列后宽度塌成 0。用户看到的"唯一一条滑杆"就是
档位那条，两条独立拉条只剩两行小字。

## 1. 现场（用户报告）

> "你应该做的是两个滑杆分别控制而不是一个滑杆同时控制思考强度和循环次数"
> "同时我需要一个我能看到的滑杆，目前的前端对于你来说有点诱导说实现但是其实根本没
> 任何交互出现的滑杆"
> "前端有且只有一个同时控制思考强度和循环次数的 effort 滑杆，我需要把这个变成两个滑杆"

三句话指向同一件事，而且**用户的观察是准确的**：界面上确实只呈现一条能拖的滑杆。

## 2. 根因：网格列数（3）≠ 拉条直接子元素个数（5）— Confirmed

`styles.css` 的 `.composer-knobs .knob-slider` 写的是 `grid-template-columns: auto
minmax(0, 1fr) auto`（3 列），而拉条根元素（`#reasoning-effort-control` /
`#loop-limit-control`）有 **5** 个直接子元素：`knob-label` / `effort-end--min` /
`effort-syringe` / `effort-end--max` / `effort-value`。

CSS Grid 的自动排布于是在第 4、5 个子元素处**换到第二行**，且滑轨（`.effort-syringe`）
落进第 3 列 `auto`：`width: 100%` 在自动尺寸列里没有百分比基准，轨道宽度塌成 0。后果
不是"窄一点"，而是：

- 可见滑轨宽度 0 → 用户看不到任何滑杆；
- 隐形 range（`.syringe-input`，`inset: 0` 的覆盖层）跟着那个 0 宽盒子一起消失 →
  **没有任何命中区，拖不动**；
- 剩下两行 9-10px 小字（起点/终点标签 + 数值）。

而档位滑杆（`data-effort` + `.effort-control` 的 flex，5 个子元素一行）是好的，于是它
成了界面上唯一像滑杆的东西——也就是用户看到的那"唯一一条"。

**证据 1（现场，无窗口读屏，未动用户桌面）**：`.NET CopyFromScreen` 截 composer 区域 → 2x
放大 → `Windows.Media.Ocr`（en-US 引擎，中文标签读不出）。读到的行坐标与"上一行 = 起点标签
+ 塌掉的滑轨、下一行 = 终点标签 + 数值"的两行排布吻合：

```text
[  852,   76] O                       （档位滑杆所在行）
[  887,   77] LITE     [1,000, 77] MAX     [1,046, 77] HIGH
[  121,  122] LOW                     ← 两条拉条的第 1 行（起点标签）
[   90,  152] MAX      [  560, 152] HIGH
[  613,  152] 9999     [1,090, 152] 384   ← 第 2 行（终点标签 + 右对齐数值）
```

**证据 2（真实浏览器复现，before / after）**：把真实 `index.html` 摘掉 `app.js` 的 module
标签，注入一段自报几何的脚本，用无头 Chrome（`--headless=new --dump-dom
--virtual-time-budget=3000 --window-size=1440,900`）读回 `getComputedStyle` 与
`getBoundingClientRect`。同一个页面，只用一条 `<style>` 把列数改回 3 列：

```text
before（3 列）cols=20.8px 338.2px 2px
  root=319,729,369x29  label=319,736  min=344,738,338x10
  syringe=686,729,2x29  barrel=686,740,2x6   ← 滑轨 = 2px 宽（只剩边框），贴在最右侧
  max=319,762,21x10     value=344,762,338x11  ← 终点/数值被挤到第 2 行（y 762 vs 729）

after（5 列）cols=20.8px 16.9px 280.8px 16.9px 17.6px
  root=319,729,369x29  label=319,736  min=344,738,17x10
  syringe=365,729,281x29  barrel=365,740,281x6  input=365,729,281x29   ← 轨宽 281px
  max=650,738,17x10     value=670,738,18x11                            ← 单行
```

after 侧：两条拉条都是**一行 29px**、轨宽 281px / 286px；隐形 range 的盒子与
`.effort-syringe` 完全同起同止（`365,729,281x29`），`elementFromPoint` 在轨内 20% 与 50%
处都命中 `reasoning-effort-range` / `loop-limit-range` —— 看得见也拖得动。

`composer-knobs.js` / `knob-slider.js` 的行为（预览、提交、回滚、上锁）本身是好的，
`node --test` 也一直是绿的——缺陷纯在布局：**行为有测试，形态没有**。

## 3. 改动

1. **档位滑杆下线**（`index.html` 的 `#effort-control` 区块、`app.js` 的
   `createEffortControl` 装配与 `effort-control.js` / `effort-control.test.mjs`）。
   档位（prompt 层 / plan 策略 / ReAct 预算）只经 `/effort` 命令切换；`Bridge.SwitchEffort`、
   `Service.SwitchEffort` 与 `SetReasoningEffort` / `SetLoopLimit` 三个后端入口一个都不删。
2. **列数对齐子元素**：`.composer-knobs .knob-slider` 改成
   `grid-template-columns: auto auto minmax(0, 1fr) auto auto`；780px 以下两端标签
   `display: none`，子元素回到 3 个，列数在同一个断点里收回 3。
3. **契约测试**：新增
   `gui/bridge_test.go::TestEmbeddedComposerKnobGridMatchesChildren`——直接数拉条根元素的
   直接子元素个数（简易标签深度扫描）并与 `grid-template-columns` 的轨道数（按括号深度
   切分，`minmax(0, 1fr)` 里的空格不算分隔）比对，宽 5 / 窄 3。把列数改回 3，用例即红。
4. **文档**：`modules/effort-control.md` → `modules/composer-knobs.md`（模块本体已换：
   档位控件不再存在），`module_dotting.json` 的模块条目与依赖边同步改名，
   `code-review.md` 的 GUI-EFF-001..003 换成 GUI-KNB-001..004，新增
   [ADR-GUI-023](../gui/decisions.md)，ADR-GUI-011 标记为被它取代。

## 4. 验证

- `node --test "gui/frontend/dist/*.test.mjs"` → 714 passed / 0 failed；
- `go test ./gui/ -run 'TestEmbedded'` → 全绿（含新增的列数用例与改指两条拉条的命中区
  归位用例）；
- `go test ./e2e/ -run 'TestGUIModuleRegistry|TestGUIDocumentLinks'` → 模块档改名后
  文档与依赖边仍解析得到。

## 5. 未验证与下一环

- **Wails WebView 内的真机几何未重测**。前端是 `//go:embed all:frontend/dist` 进二进制的：
  改前端必须重建 GUI 才能看到，所以"两条拉条真的能拖了"目前的证据是
  ①真实 Chromium 的无头渲染实测（§2 证据 2，同一内核）+
  ②把列数改回 3 就变红/变 2px 的契约用例。在用户窗口里再确认一次仍需重建二进制。
- 复核方法（本仓既有口径）：静态服务 `gui/frontend/dist` + 无头 Chrome/Edge（可直接用
  `--dump-dom` 自报几何，不必上 CDP）；要测拖动就再读回 `input.value` 与活塞
  `getBoundingClientRect()`，确认输入盒与滑轨盒同起同止、轨内任意处单击都改值
  （`.composer-knobs` 段与 12.9 的归位规则一起复核）。
- 重建二进制走既有三段式：`make stage-gui` → `make smoke-gui` → `make deploy-gui`
  （`make rebuild-gui` 会先 `clean-gui` 删掉工作区 `dist/`，那里面有本机会话与账号副本，
  不要在用户运行时直接跑）。
