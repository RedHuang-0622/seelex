# 新增「毛玻璃」材质皮肤 + 皮肤→窗口材质的责任链（2026-10-08）

> 日期: 2026-10-08 | 范围: `gui/frontend/dist/themes/glass.css`（新）、
> `gui/frontend/dist/themes/manifest.json`、`gui/frontend/dist/themes/README.md`、
> `gui/frontend/dist/theme.test.mjs`（随包皮肤清单断言）、
> `gui/frontend/dist/window-treatment.js`（新）+ 其单测、`gui/frontend/dist/app.js`（换肤回流）、
> `gui/appearance.go`（新）+ 其单测、`gui/bridge.go`（`Options.AppearancePath` / `Bridge.SetWindowAppearance`）、
> `gui/run_wails.go`（建窗期条件）、`main.go`（`guiAppearancePath`）
> 承接: [2026-09-20-silver-skin.md](2026-09-20-silver-skin.md)（皮肤包层与 token 契约）、
> [2026-09-11-component-library-and-skins.md](2026-09-11-component-library-and-skins.md)

## 1. 目标与边界

用户要求：**支持一套新皮肤，背景不做颜色限制，是"透的毛玻璃"**；并明确这次的落法——
"直接改皮肤对应的 CSS" + "做个责任链模式的改动，通过确认皮肤样式做 Go 侧一行级别的改动"。

两条边界：

- **背景不铺色**：壳（`.app-shell`）不再铺实色底，只留一条半透明渐变；观感来自半透明面 +
  `backdrop-filter`。这与 Qoder 设计语言第 1 节"通透不来自模糊/毛玻璃/半透明"**相反**——
  所以它是一套**刻意的材质皮肤**，不回归 Qoder 那套口径。
- **真透出桌面**必须让窗口让出底色，而 Wails 的窗口透明/亚克力**只能在建窗时设**（见 §4），
  于是"皮肤 → 窗口"的确认要跨进程落一次盘。

## 2. 皮肤包（前端 CSS 层）

- **`themes/glass.css`（新）**：`--skin-gradient-*` 用**半透明**渐变（浅色 `rgba(255,255,255,.58)
  → rgba(205,222,236,.34)`、深色同构），主信号取冷石板色；`.app-shell` 底透明、`.workspace`
  与三块面板用 `color-mix` 半透明面 + `backdrop-filter: blur(22px) saturate(1.35)`。
- **契约之例外**：皮肤包历来"只覆盖 `:root` 品牌 token"，但**滤镜不是 token 能表达的**，
  所以 `glass.css` 除 `:root` 外还带了**材质规则**——选择器只允许落在壳/纸两层
  （`.app-shell` / `.workspace` / `.topbar` / `.left-panel` / `.right-panel`），不深入组件。
  这条例外写进了 `themes/README.md` 的「材质皮肤」一节（判据四条）。
- **`manifest.json`**：登记 `{id:"glass", name:"毛玻璃", ...}`。
- **`theme.test.mjs`**：随包清单断言 5 → 6；既有契约用例（一个 `:root`、8 个品牌 token、
  无 `!important`）对 `glass.css` 逐条仍然成立。

## 3. 责任链：皮肤 → 窗口材质（前端）

- **`window-treatment.js`（新）**：一节责任链 `WINDOW_HANDLERS`，每节 `{skins, treatment}`，
  **第一个认领的胜出**，没人认领 → `opaque`。当前只有一节：`glass → acrylic`。
  加新的材质皮肤只往链上插一节，Go 与组件层不动。
- **`app.js`**：皮肤控制器的 `onApplied` 回流里加一句 `syncWindowAppearance()`——把
  责任链的结果经 `Bridge.SetWindowAppearance(skin, treatment)` 交给宿主。用
  `皮肤:材质` 作钥匙去重（一次换肤 `onApplied` 会跑两次：`<link>` 换上是异步的，
  控制器先立即回流一次、`onload` 再回流一次；切深浅也会重跑同一条）。
- 桥未就绪（浏览器预览 / headless）时静默跳过，**换肤本身不受影响**。

## 4. Go 侧：建窗期一行条件

Wails v2.13 把窗口透明/亚克力落在**建窗**里（证据：`internal/frontend/desktop/windows/
window.go:70-72` 设 `WS_EX_NOREDIRECTIONBITMAP`、`:125-131` 调 `EnableTranslucency`），
**没有运行时开关**。所以 Go 侧读的是**上次落盘**的取值：

- **`gui/appearance.go`（新）**：`windowAppearance{skin, window_treatment}` 的读写 +
  归一（未知词 → `opaque`）。路径为空 / 文件不存在 / JSON 损坏一律回落不透明——
  外观设置永远不该阻断启动。
- **`gui.Options.AppearancePath`（新）**：由宿主按配置责任链给出。
- **`Bridge.SetWindowAppearance(skin, treatment)`（新）**：纯本地写盘，不碰 Application，
  所以不设运行守卫（换肤在任何时刻都该能记住）。
- **`gui/run_wails.go`**：建窗前 `if windowWantsTranslucency(config.AppearancePath)` → 清掉
  `BackgroundColour`（那块实色窗底会把 WebView 身后的亚克力整个盖掉）并设
  `Windows{WebviewIsTransparent, WindowIsTranslucent, BackdropType: Acrylic}`。
  **其余皮肤一字不动**。
- **`main.go guiAppearancePath()`**：复用既有的配置责任链 `runtimeConfigChain`
  （口径与 `ensureConfigFile` 一致，只差"不播种"）——存在即读也即写，全缺则落到
  `<exe>/config/gui-appearance.json`。

**已知代价**：换材质皮肤要**重启**才生效（第 4 步只在建窗跑一次）；窗口常开 `NOREDIRECTIONBITMAP`
的方案被否掉（会把所有皮肤推到那条渲染路径上）。

## 5. 验证

```text
$ go build ./... && go build -tags gui ./...
$ go vet -tags gui ./gui/... .
# 全绿

$ go test ./gui/ -run Appearance -count=1 -v
# 4 passed：读回落（空路径/不存在/损坏）、写读往返、未知材质词归一、无路径 no-op

$ go test ./gui/ -short -count=1
# ok  github.com/RedHuang-0622/seelex/gui  4.1s

$ go test . -short -count=1
# ok  github.com/RedHuang-0622/seelex  46.8s

$ cd gui/frontend/dist && node --test
# tests 723 / pass 723 / fail 0（含新 window-treatment.test.mjs 5 条、
# theme.test.mjs 随包清单断言 5→6）
```

## 6. 未取到的证据 / 未做

- **没有像素证据**：未重启 GUI 目视确认"毛玻璃真透出桌面"。前端是 `embed.FS` 打进二进制
  的，需**重建 GUI** 后在「设置 → 外观」选「毛玻璃」并**重启**，才看得到亚克力。
  本轮的证据只到"契约用例 + 单测 + 编译"。
- **`docs/design/qoder-skin/tools/verify-skins.py` 与 `build-preview.mjs` 未同步 `glass`**：
  它们只服务 Qoder 复刻页的取色验收，且预览页用的是构建期快照；本皮肤要等预览重建后
  再接进去（它的判据——"每套皮肤壳顶互异 / 深浅纸色不同"——对半透明皮肤需要单独定判据）。
- **`BackdropType` 要求 Windows 11 22621+**：低于此版本 Wails 回落到
  `SetTranslucentBackground`（纯透明窗，无亚克力毛面）。本机版本未验证。
- **`--skin-gradient-*` 是半透明的**：`verify-skins.py` 那类"壳顶取色"判据对它会读出
  合成后的颜色（叠加窗口底色），不能直接沿用。
