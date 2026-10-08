# composer 运行参数重排：思考强度贴发送键，循环次数进运行状态弹窗（含「无限循环」）

日期：2026-10-08
范围：`gui/frontend/dist`（前端 + 契约测试）、`docs/gui`（模块档 + ADR + CHANGELOG）、
`docs/devlog`

## 0. 一句话

输入框底部并排两条拉条（思考强度 / 循环次数）整块退场：**思考强度**贴到发送键之左，
**循环次数**挪进运行状态弹窗——有限上限从 9999 收成 **768**（max 档默认 loops），
"≈无上限"改由 **「无限循环」勾选**表达（复用既有哨兵 `limits.input_loop_limit`）。
后端一个字段都不改，改的是它们该待的地方。

## 1. 现场（用户口径）

> "两个滑杆在下面真的太丑了"、"循环 loop 最高变成 700 多的一个数字配置，然后支持勾选
> 无限循环，这个循环次数可以在运行状态的面板里面改。然后思考强度就放在发送按钮旁边即可。"

## 2. 诊断（为什么丑）— 三条，前两条 Confirmed

**证据 1（现场真机截图，无窗口读屏）**：截 composer 区域放大后逐字读回，底部那一行是：

```text
思考  LOW  ·············（细虚线轨道，左端一小段绿填充）············  MAX   LOW
循环    1  ●············（几乎无填充）···················· 9999   323
```

五个微字（标签 / 起点 / 终点 / 数值）挤在一行 9-10px 里，两条轨道各只占 composer 一半
宽，循环那条按 323/9999 ≈ 3% 填充，看起来像"没生效"。

**证据 2（代码）**：`.syringe-barrel { background: var(--ink); }` —— `--ink` 是旧设计原型
`docs/2026-08-16-frontend-aesthetic` 里的 token，**已不在 `styles.css` 的 token 表里**
（全仓 grep 只命中那份原型与旧快照）。`var(--ink)` 无值 → 该声明在计算值阶段失效 → 轨道
背景取初始值（透明），只剩一条 1px `--border-strong` 描边。这正是"滑杆看起来像一根淡虚线"
的成因。（同一文件里 `.knob-label` 的 `color: var(--text-muted)` 也是同一个问题：token 不存在。）

**证据 3（判断，Hypothesis）**：两条拉条并排在同一行，是"空间巧合"而非逻辑相邻——思考
强度属于"这一次输入"，循环次数属于"这一趟运行"。用户给出的重排方向与这个语义一致。

## 3. 改动

1. **思考强度贴发送键**：`#reasoning-effort-control` 从 `.composer-knobs` 挪进
   `composer-actions`（发送键之左），加类 `.effort-inline`（固定窄宽 138px）。
2. **循环次数进运行状态弹窗**：`#loop-limit-control` 挪进 `#runtime-modal` 的
   `.runtime-knobs` 段，旁边新增 `#loop-limit-infinite`（「无限循环」勾选）。
3. **上限 768 + 无限循环哨兵**：`composer-knobs.js` 的 `LOOP_LIMIT_MAX` 由 9999 改为
   **768**（`application/prompt/effort.go` 的 `effortProfiles["max"].maxLoops`）；新增
   `LOOP_LIMIT_INFINITE = 9999`（= `limits.input_loop_limit` 默认值）。勾选 → 下发哨兵、
   滑杆停用、读数显示 `∞`；取消 → 回到最后一份有限值。**不新增后端字段**，
   `validateLoopLimit` 的上界不变。
4. **滑轨修形**：`.syringe-barrel` 的 `background: var(--ink)` → `var(--surface-3)`（轨道
   有了底色），高度 6px → 8px；新增可见滑块 `.syringe-handle`（`pointer-events: none`，
   命中区仍是铺满滑轨的隐形 range）。锁定态由 `border-style: dashed`（读起来像"坏了"）
   改为同色系描边 + 滑块降透明。
5. **输入框底部 `.composer-knobs` 行整块删除**，连同它的 780px 断点。
6. **契约测试**：`TestEmbeddedComposerKnobGridMatchesChildren`（列数 == 子元素数）随
   `.composer-knobs` 一起退役，换成 `TestEmbeddedLoopLimitBounds`（768 / `max="767"` /
   9999 三个数字同源）；嵌入布局用例改钉"思考强度先于 Runtime modal、循环与勾选在
   modal 内、`id="effort-control"` 归零"。

## 4. 验证

- `node --test "gui/frontend/dist/*.test.mjs"` → **719 passed / 0 failed**（含 `composer-knobs.test.mjs`
  新增的「无限循环」四条链、`element-registry` 的 `loop-limit-infinite` 登记、`theme` 的
  "圆角块里不许出现纯黑"——可见滑块的投影因此走 `color-mix(var(--text-bright) …)`）；
- `go test ./gui/ -run TestEmbedded -count=1` → ok；
- `go test ./gui/ -count=1` → ok；
- `go build ./...` → ok。

## 5. 未验证与下一环

- **Wails WebView 内的真机几何未重测**。前端是 `//go:embed all:frontend/dist` 进二进制的：
  改前端必须重建 GUI 才能看到。本轮未重建（用户窗口在跑，且 `make rebuild-gui` 会先
  `clean-gui` 删工作区 `dist/`，里面有本机会话与账号副本——见 `MEMORY.md`）。
- 复核方法（本仓既有口径）：静态服务 `gui/frontend/dist` + 无头 Chrome/Edge 读回
  `getBoundingClientRect`，确认每条隐形 range 与自己的 `.effort-syringe` 同起同止、轨内
  任意处单击都改值；重排后这条要重跑一次（历史上跑过，数据见
  [`2026-10-08-composer-two-sliders-and-grid-fix.md`](2026-10-08-composer-two-sliders-and-grid-fix.md)）。
- 重建二进制走既有三段式：`make stage-gui` → `make smoke-gui` → `make deploy-gui`。
- 若 `limits.input_loop_limit` 被配置成小于 9999，「无限循环」下发会以后端校验失败告终
  （前端回滚勾选并弹 toast）。这是既有哨兵口径的边界，不是本轮引入的缺陷；要消灭它得让
  快照带上上限，属于下一轮。
