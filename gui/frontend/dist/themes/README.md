# themes（皮肤包 / 材质包）

## 生态位

`gui/frontend/dist/themes` 是 Seelex GUI 的**换肤层**。它建立在组件库
（`vendor/pico.min.css`）与 Seelex 自有样式（`styles.css`）之上。

## 两轴换肤：深浅 × 皮肤

外观由**两条正交的轴**决定，互不耦合：

| 轴 | 决定什么 | 载体 | 取值 |
|---|---|---|---|
| **深浅（mode）** | 中性基座：面 `--surface*`/`--bg`/`--panel`、文本 `--text*`、描边 `--border*`、状态色 `--status-*`、半透明面、投影 `--shadow*`、刻度 `--tick*` | `styles.css`（`:root` 浅色、`:root[data-theme="dark"]` 深色） | `light` / `dark` |
| **皮肤（skin）** | 品牌：主信号（`--accent*` / `--on-accent`）与环境渐变（`--shell-gradient`） | `themes/<skin>.css` | `qoder` / `graphite` / `verdigris` / `paper` / `silver` |

两者可自由组合（当前 6 皮肤 × 2 深浅 = 12 种外观）。
**加一套皮肤 = 加一个 CSS 文件 + `manifest.json` 里的一条登记**，不改任何 JS，
也不改深浅。若是**材质皮肤**（如 `glass`），皮肤文件里可以额外带一组材质规则——
见下文「材质皮肤」。

## 随包皮肤（皮肤轴）

| id | 名字 | 主信号（浅 / 深） | 环境渐变（浅 → 深） |
|---|---|---|---|
| `qoder` | Qoder（默认） | 墨黑 / 象牙白 | 黄 `#EBEBC6`→青蓝 `#D1DAE2` ｜ 暖近黑→冷近黑 |
| `graphite` | 石墨黄铜 | 黄铜压深 / 黄铜提亮 | 暖象牙→冷钢 ｜ 铁蓝石墨 |
| `verdigris` | 铜绿钢青 | 铜绿压深 / 铜绿提亮 | 淡青绿→钢青 ｜ 青绿→钢青（暗） |
| `paper` | 暖纸白昼 | 暖铜 / 暖铜提亮 | 暖纸→微冷纸 ｜ 暖褐近黑→冷褐近黑 |
| `silver` | 银白冷钢 | 钢青灰 / 钢青灰提亮 | 银白→冷灰 ｜ 冷近黑→更深冷近黑 |
| `glass` | 毛玻璃（材质皮肤） | 冷石板 / 冷石板提亮 | 半透明白→冷蓝 ｜ 半透明近黑→冷近黑 |

> 环境渐变的配方与判据（等亮度 / 纯竖直 / 线性 sRGB）见
> `docs/gui/design/qoder-skin/DESIGN-LANGUAGE.md` 第 1 节。

## 深浅基座（mode 轴）

- 浅色：`styles.css` 的 `:root`；深色：`styles.css` 的 `:root[data-theme="dark"]`。
- 两条基座都**必须**给全契约 token（缺哪个，切过去就在那个 token 上漏色）。
- 皮肤可以**完全不碰**中性 token——那是深浅的事。

## 皮肤包契约

一个皮肤包 = 一个 CSS 文件 + `manifest.json` 里的一条登记（`skins[]`）。

**必须被覆盖的品牌 token（契约，缺一不可）**：

| 组 | token |
|---|---|
| 主信号 · 浅色 | `--skin-accent-light` `--skin-accent-strong-light` `--skin-on-accent-light` |
| 主信号 · 深色 | `--skin-accent-dark` `--skin-accent-strong-dark` `--skin-on-accent-dark` |
| 环境渐变 · 浅色 | `--skin-gradient-light` |
| 环境渐变 · 深色 | `--skin-gradient-dark` |

`styles.css` 把上面这些桥接成组件层真正读的 token：

```css
:root { --accent: var(--skin-accent-light, …); --shell-gradient: var(--skin-gradient-light, …); }
:root[data-theme="dark"] { --accent: var(--skin-accent-dark, …); --shell-gradient: var(--skin-gradient-dark, …); }
```

**可以不做**：中性 token（面 / 文本 / 描边 / 状态 / 半透明面）——它们属于深浅基座，
皮肤不改也能正确显示。

**允许做**：`--shadow` / `--shadow-sm` 的覆盖（浅色皮肤需要更轻的投影），
前提是**放在 `:root` 里且两个深浅都成立**；做不到就别改（交给深浅基座）。

**不允许做**：改选择器结构、用 `!important`、引入远程字体或图片、写死与
token 无关的颜色；皮肤包里只允许出现 **一个 `:root { … }`**，出现别的选择器规则时，
评审必须问清楚为什么 token 不足以表达。

## 材质皮肤（例外：可以带材质规则）

纯色皮肤只用 token 就能表达。但**毛玻璃/磨砂**这类材质的"透"靠的是
`backdrop-filter` 模糊，而滤镜**不是 token 能表达的**（同源皮肤文件是按
`<link>` 加载的整份样式表，加载顺序在 `styles.css` 之后，所以它天然能覆盖
壳/纸这两层）。这是 `themes/<skin>.css` 唯一被允许出现 `:root` 之外选择器的场景。

判据（`glass` 是本条的第一个落点）：

- 品牌 token 契约照旧（8 个全给、深浅都齐、不写 `!important`、只有一个 `:root`）；
- 额外的选择器**只允许落在"壳 / 纸"两层**——`.app-shell` / `.workspace` /
  `.topbar` / `.left-panel` / `.right-panel`，用来铺半透明底与 `backdrop-filter`；
- **画布层（`<html>` 的底色）另外要一条，但它不另起选择器**：写进皮肤自己的
  `:root { … }` 里（`background-color: transparent`），理由见下「画布让位」；
- **不允许**深入任一组件类（`.message` / `.right-tab` / `.modal` …）；
- 材质会带来全窗 `backdrop-filter` 的 GPU 代价——把模糊放在有限区域
  （纸 + 三块面板），别放在 `.app-shell` 上（那是全窗一层，流式刷新时每帧都要渲一遍）。
- **glass 的三栏不透明度是递进的**：左栏 38% → 对话区 66% → 右栏 88%，
  模糊半径同步增加。三块不是同一材质的复制品；测试会钉住这三档顺序，防止后续
  样式收敛时又退回“全栏同透明度”。

### 画布让位（`<html>` 的底色）——"透出桌面"的最后一层

`styles.css` 的浅色基座写着 `:root { background: var(--bg) }`，组件库又把
`--pico-background-color`（= `--bg`）铺在 `:where(:root)` 上。两层合起来让 `<html>`
**整块实色**——它铺在窗之上、壳之下，是壳之外唯一还实色的层。不点掉它，窗让得再干净
也看不见：`backdrop-filter` 糊的是这一片平地，观感塌回"半透明的纯色面板"。

写法的硬约束（`theme.test.mjs` 有静态断言守住）：

- **只能写在 `:root { … }` 里**，不另起 `html` / `body` 选择器。`:root` 是伪类
  （特异性 0-1-0），`html` / `body` 只是类型选择器（0-0-1）——另起选择器**压不过**
  基座那条；而且会破坏"皮肤包里只有一个 `:root`"的契约。
- 非材质皮肤**不许**动画布底：它们的"底"来自深浅基座，透明了反而露出窗的实色底。

> 代价：换材质皮肤要**重启**才生效（窗在第 3 步才让位）。在重启之前，画布已经透明、
> 窗还是旧的实色底——看到的是窗底色透过半透明面板，属预期内的中间态。

### 材质皮肤与「窗口」的接口（毛玻璃要真透出桌面时才需要）

CSS 里的半透明 + `backdrop-filter` 只能糊**应用自己的层**（壳的渐变、面板下的内容）。
要真的透出**桌面**，窗口本身得让出底色——而 Wails 的窗口透明/亚克力（`Windows.
WebviewIsTransparent` / `WindowIsTranslucent` / `BackdropType`）**只在建窗时能设，没有
运行时开关**。所以这条链是：

1. 前端 `window-treatment.js` 用**责任链**从皮肤 id 确认出窗口材质（`glass → acrylic`，
   其余 → `opaque`）；
2. `app.js` 在换肤回流时经 `Bridge.SetWindowAppearance(skin, treatment)` 落盘到
   `gui-appearance.json`（路径由宿主的配置责任链给，见 `main.go guiAppearancePath`）；
3. `run_wails` 在**下次建窗**时读它，是 `acrylic` 就透明建窗（并清掉 `BackgroundColour`
   那块实色窗底）。

CSS 那一侧还有同源的一条：**画布（`<html>` 的底色）也必须让位**——窗与画布缺一个，
都只糊得出应用自己的层（详见上节「画布让位」）。两条同属材质皮肤的契约。

**换材质皮肤要重启才生效**（第 3 步只在建窗跑一次），其它皮肤不受影响。加一套新的
材质皮肤只动第 1 步那条链（外加它自己的 CSS），Go 与组件层不用碰。

## 登记与加载

`manifest.json`（schema 2）：

```json
{
  "schema_version": 2,
  "default_skin": "qoder",
  "default_mode": "light",
  "skins": [
    { "id": "qoder", "name": "Qoder", "description": "…",
      "swatches": ["#1f2328", "#ebebc6", "#1b1a17", "#e9e9ec"], "file": "themes/qoder.css" }
  ],
  "modes": [
    { "id": "light", "name": "浅色", "description": "…" }
  ]
}
```

- `skins[].file` 为空 = 缺省皮肤（品牌 token 走 `styles.css` 里的 `var(–skin-*…, 兜底)`）；
- `modes[].id` 只认 `light`/`dark`，决定 `<html data-theme>`；
- 加载器是 `dist/theme.js`：
  - 皮肤路径只允许 `themes/<id>.css` 这种**同源相对路径**，`id` 必须是 `[a-z0-9-]`，
    禁止 `../`、绝对 URL、query/hash（防路径逃逸）；
  - 两条轴各自持久化：皮肤记 `localStorage["seelex.skin"]`，深浅记
    `localStorage["seelex.mode"]`；启动时先套皮肤再渲染。

## 之后要做的（尚未实现）

- **用户自带皮肤包**：从磁盘目录（如 `config/themes/*.css`）加载需要后端
  路径门禁与只读通道（`PathGate`），当前只有随包内置皮肤；
- 材质/纹理类皮肤需要图片资源，同样要走嵌入式资源白名单。

## 换肤回流（JS 取色的消费方）

换肤只做两件事：改 `<html data-theme>` 与皮肤 `<link>` 的 `href`。凡是
**在 JS 里取一次 token 就缓存下来**的消费方，CSS 变量变了它不会自己重取，必须
在"外观已生效"时再取一次：

- 回流口：`theme.js` 的 `createThemeController({ onApplied })`，回调入参
  `{ skin, mode }`；切皮肤与切深浅都会触发。外链皮肤在 `<link>` `load` 之后
  再回调一次（`load` 之前读到的仍是旧 token），缺省皮肤立即回调；
- 现有消费方：**下栏终端**。xterm 的 `theme` 只在创建时从
  `--code-bg`/`--text`/`--accent`/… 取色，由 `terminal-panel.js` 的
  `refreshTheme()` 重取，`app.js` 把它接到 `onApplied`。漏了这一步的现象是：
  换成浅色皮肤后终端仍是一片深色底；
- 新增"创建时读 token"的消费方时，同样接到这个回流口，别只在创建时读一次。

## Review 指南

- 新皮肤是否覆盖了契约里的 8 个品牌 token（`theme.test.mjs` 有清单校验）；
- 皮肤是否只出现 `:root { … }` 一个选择器（不得覆盖中性 token 或写选择器 hack）；
- 深浅基座是否两条都给全中性 token（`theme.test.mjs` 有清单校验）；
- 是否引入远程资源；
- 换肤回流：新皮肤在**已开终端**上是否也生效（xterm 配色靠
  `terminalPanel.refreshTheme()` 回流，`theme.test.mjs` 有回流用例守住）。
