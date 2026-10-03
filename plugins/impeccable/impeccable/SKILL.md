---
description: 前端设计纪律（Impeccable）：设计/重设计/评审/打磨 UI 时的工艺底线、命令路由与确定性检测器接线。覆盖落地页、仪表盘、表单、设置、空状态、组件与响应式；不接后端与非 UI 任务。
---

# impeccable：前端设计纪律

**出处**：移植自 [`pbakaus/impeccable`](https://github.com/pbakaus/impeccable)（Apache-2.0，上游是 Anthropic
`frontend-design` skill 的后继）。本文件是 **Seelex 侧的接线与口径**；逐字纪律在
`reference/craft-floor.md`。许可与更新方式见 `plugins/impeccable/README.md`。

它把两样东西交到你手里：**一段工艺纪律**，和 **61 条确定性检测规则**（CLI，无 LLM、无 API key）。
纪律管方向，检测器管机械项——**两者都不能互相替代**。

## 1. 核心原则

- **全力以赴，不投降**：交付物必须完整（除必须由用户提供的素材），不做半成品。
- **敢**：独特、漂亮、出色、有启发；平庸的"安全牌"是失败。
- **brief 最大**：用户钉住的审美、年代、材质、字体、配色**照做**，即使它与下面的
  "拒绝清单"冲突。把清晰 brief 掰成你自己的口味 = 失败。
- **refinement 保身份，redesign 换世界**：打磨保留现有身份/行为/文案/范围外一切；
  重设计保留产品事实、内容、功能与约束，但把旧外观当**反参照**——绝不在废弃的外观上继续抛光。
- **视觉权威是证据，不是文件名**：没有 `DESIGN.md` 不等于绿地；先看代码、token、
  已有 CSS/组件再判断"保留 / 扩展 / 替换"。
- **有界验证，不进循环**：完整构建 → **一轮批量检查**（桌面与移动一起看，别一趟趟截图）
  → 一次批量修完 → 至多再一轮确认 → **停手**。开放式自我 QA 烧用户的钱，而且做得比 finish
  环节更差。

## 2. 先定模式：这个 surface 上"访客的成功"长什么样

| 模式 | 场景 | 成功 = |
|---|---|---|
| **Persuade** | 落地页、营销、campaign、定价 | 访客决定并行动；设计就是产品 |
| **Operate** | App UI、仪表盘、编辑器、后台、设置、工具 | 访客完成任务；可扫读、一致、符合平台预期优先于表达 |
| **Read** | 文档、文章、指南、帮助、changelog | 访客看懂；先为理解排版，再让阅读有留存价值 |
| **Experience** | 作品集、画廊、showcase | 访客进入作品本身；首屏让作品说话，界面退场 |

按**这个 surface** 选模式，不按产品选：工具站的落地页仍是 Persuade，时装屋的文档仍是 Read。

## 3. 命令路由

| 命令 | 类别 | 做什么 |
|---|---|---|
| `init` | Build | 采集持久产品事实写入 `PRODUCT.md`（`teach` 是别名） |
| `shape [feature]` | Build | 写代码前先规划 UX/UI |
| `document` | Build | 从现有代码生成 `DESIGN.md` |
| `extract [target]` | Build | 把可复用 token/组件抽进设计系统 |
| `critique [target]` | Evaluate | UX 评审：层级、清晰度、情感共鸣（带评分） |
| `audit [target]` | Evaluate | 技术质量：a11y、性能、响应式 |
| `polish [target]` | Refine | 发版前最后一遍质量收口 |
| `bolder` / `quieter` | Refine | 放大无聊的 / 压住过吵的 |
| `distill` / `harden` / `onboard` | Refine | 剥到本质 / 错误与 i18n 与边界 / 首启与空状态 |
| `animate` / `colorize` / `typeset` / `layout` / `delight` / `overdrive` | Enhance | 动效 / 策略性用色 / 字体层级 / 间距节奏 / 记忆点 / 超越常规 |
| `clarify` / `adapt` / `optimize` | Fix | 文案与报错 / 多设备适配 / 性能 |
| `live` / `generate` | Iterate | 浏览器里选元素迭代；或生成变体让人挑 |

路由规则（与上游一致）：

- **没有参数**：给出上面这张菜单让用户挑，**绝不自动跑命令**。
- **显式或明确要跑某命令**：取它的 playbook 后执行；两个命令都像就问一次。
- **其它情况**：当一般设计工作做——缺 `PRODUCT.md` 的**新 surface/换世界**先走 `init`；
  对现有代码的窄改动直接按现状做，事后**提议**补 `init` 而不是卡住。

命令的 playbook（每个命令一份，上游 `skill/reference/<command>.md`）不在本仓库里，
按需取：

```bash
BASE=https://raw.githubusercontent.com/pbakaus/impeccable/main/skill/reference
curl.exe -sSL "$BASE/audit.md"     # 或 polish.md / critique.md / new-work.md …
```

## 4. 工作流（Seelex 口径：纪律 + 证据）

1. **先读现状**：`PRODUCT.md` / `DESIGN.md`（若有）、目标文件、现有 token 与组件。
   不许假设绿地。
2. **动 UI 前必读** `reference/craft-floor.md`——工艺底线、绝对禁令、检测器抓不住的反射。
   纯规划阶段不必读。
3. **改**：`write_file` / `edit_file`。
4. **机械取证**（61 条规则）：

   ```bash
   npx -y impeccable detect --json <改动路径>     # 首次会下载引擎二进制到 ~/.impeccable/bin
   npx -y impeccable detect src/ index.html       # 目录 / 文件
   npx -y impeccable detect https://example.com   # 渲染后的页面（用本机 Chrome/Edge）
   npx -y impeccable ignores list                 # 看豁免
   ```

   退出码：`0` 扫完无 primary findings / `2` 有 primary findings / `1` 有目标没扫成。
   人读报告走 **stderr**（`2> findings.txt`），机器读用 `--json`（stdout）。
   单文件豁免写行内注释：`<!-- impeccable-disable overused-font: 品牌字体 -->`。

5. **人眼取证**：`computer_screenshot` 看真实渲染，桌面与移动**同一轮**看；越界文字、
   溢出、对比度都要看**计算值**而不是印象。
6. **冒烟**：关键路径逐个入口点一遍（打开/输入/切换/滚动/错误态），失败处留截图锚点。
7. **收口**：把「改了什么 + 检测器读数 + 渲染证据」一起交给用户；不要只说"已优化"。

> **干净 ≠ 好看**：检测器通过只是**证据之一**，不替代看渲染结果。
> 反过来，视觉好看也不能豁免机械项——两者都要。

## 5. 硬纪律摘要（全文见 `reference/craft-floor.md`）

**Verify**（对"已建成结果"的检查，不是意图）：正文与占位文字对比度 ≥4.5:1、大字 ≥3:1
（彩色面上的次级文字要**用该色相调**，不许用灰）；阴影要有偏移和柔和模糊（零偏移彩色晕圈
是装饰）；间距"紧组、松隔"、标题上方比下方留白多；正文 65–75ch、display ≤6rem、
tracking 下限 −0.04em；动效**一个作者性瞬间**而不是到处特效，也不用每段相同入场；
状态要齐（hover/disabled/loading/error/empty）+ 真内容 + 键盘聚焦；
**浏览器原生表面**（选区、caret、滚动条、focus ring、下划线偏移、表格数字）也要用配色
驯服——这是"被建成"而不是"被拼起来"的最便宜信号，也是模型漏得最狠的一项。

**Refuse**（类别默认值，不是绝对禁令——但 brief 没点名时用它就意味着你没在做决定）：
一样大的「图标+标题+文字」卡片做页面骨架（卡片是懒容器，**卡片套卡片永远错**）；
hero-metric 模板（大数+小标签+辅助统计）；标题上方的 kicker/eyebrow（**这条是禁令**）；
无信息的 01/02/03 章节编号；不需要打断的 modal；渐变文字；当装饰的玻璃拟态；
>1px 的彩色 `border-left/right` 侧条纹；非新粗野世界的硬偏移阴影；无意义的
sparkline/进度环/圆角软阴影块；把等宽字体当"技术感"戏服；用系统 display 字体
（Impact/Arial Black/平台 sans）当自家世界的嗓音——**要自托管一款性格对的字**；
emoji/Unicode 字符当图标系统；几何遮罩冒充有机轮廓；按品类而不是按使用场景挑明暗。

**Codex 三律**：tracking 止于 −0.04em（−0.02~−0.03em 通常更好读）；elevation 只声明一次
（边框**或**阴影，1px 边框 + 宽软阴影 = 幽灵卡片），卡片圆角 12–16px、胶囊留给小控件；
背景是材料，只从主题世界的纹理来（条纹/网格底需要真的有画布、地图、蓝图在下面）；
真插画或不做——"速写风 SVG"读起来业余。

## 6. 边界

- **不接**后端/非 UI 任务。
- `live` 模式只对**本地 checkout**（dev server 或本地静态 HTML）；往线上站点注入
  localhost helper 是**不支持**的，也不要为了它削弱生产 CSP。线上只做只读检测：
  `npx impeccable detect <url>`。
- **不要顺手修 drift**：`DOCTOR`/`CONTEXT_STALE` 这类报告只报告、不擅自修，除非用户要求。
- 上游 hook（编辑时自动跑检测器）在 Seelex 里没有对应的宿主 hook 面；本 skill 把检测
  放在你自己的**取证步骤**里跑（第 4 步），口径不变：改完就测，别等收口。
