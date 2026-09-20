# 新增「银白冷钢」浅色皮肤（2026-09-20）

> 日期: 2026-09-20 | 范围: `gui/frontend/dist/themes/silver.css`（新）、
> `gui/frontend/dist/themes/manifest.json`、`gui/frontend/dist/themes/README.md`、
> `gui/frontend/dist/theme.test.mjs`（随包皮肤清单断言）
> 承接: [2026-09-11-component-library-and-skins.md](2026-09-11-component-library-and-skins.md)（皮肤包层与 token 契约）

## 1. 目标与边界

用户要求增加一套银白色皮肤，主要色调 `#EEF0F2` / `#C0C2C4` / `#9AA0A7`。

按既有分层，这只发生在**皮肤包层**：新增 `themes/silver.css`（只覆盖 `:root`
语义 token）+ `manifest.json` 登记一条。组件结构、交互、可访问性、字体/圆角/间距
一律不动；不引入远程资源，不写选择器与 `!important`。契约见
`themes/README.md`，由 `theme.test.mjs` 校验。

## 2. 实现

- **皮肤** `themes/silver.css`（light）：三个给定色各归其位——
  `#EEF0F2` 作主底 `--bg`（`--panel/--surface` 逐级更亮到纯白），
  `#C0C2C4` 作分隔 `--border`，`#9AA0A7` 作强分隔 `--border-strong`；
  主信号 `--accent` 取同族更深一档的钢青 `#5C6673`（浅底上要够对比，
  灰阶本身当信号会与描边糊在一起）；`--on-accent` 纯白。
  文本用冷灰阶（`--text` `#2b3138`），状态色沿用四组语义、
  按浅色底加深（done `#3e7a5b` / failed `#b4463c` / info `#3d6b98`）。
  浅色实现必须一并覆盖 5 个半透明面 token（`--surface-blur/--panel-solid/
  --surface-glass/--overlay/--surface-opaque`），否则弹层残留深色底。
- **登记** `manifest.json`：追加 `{id: "silver", name: "银白冷钢", mode: "light",
  file: "themes/silver.css", swatches: [#eef0f2, #c0c2c4, #9aa0a7, #5c6673]}`，
  四个色板前三枚即用户给定三色、第四枚是主信号。`id` 是 `[a-z0-9-]`，
  `file` 是同源相对路径——`theme.js` 的 `themeHref`/`themeID` 安全门禁直接放行。
- **文档** `themes/README.md` 增「随包皮肤」一览（此前这份 README 没有列出
  随包集合，读者只能从 devlog 反查）。
- **测试** `theme.test.mjs` 的「shipped manifest points at real skin files」
  断言更新为 `["graphite","verdigris","paper","silver"]`。

## 3. 验证

```text
$ node --test gui/frontend/dist/theme.test.mjs
# tests 6 / pass 6 / fail 0
#   ├ theme: shipped manifest points at real skin files（含新 id 与路径格式）
#   └ theme: shipped skins cover the whole token contract（39 个契约 token 全覆盖、
#      无 !important、只有 :root 规则）——新皮肤由该用例逐 token 校验

$ node --test gui/frontend/dist/*.test.mjs
# tests 401 / pass 401 / fail 0
```

对比度（WCAG 2.x，临时脚本解析 `silver.css` 的 `:root` 实算，脚本落在
gitignore 的 `_tmp/`，不入库）：

```text
PASS  正文 text / bg         11.49:1   PASS  accent / surface       5.83:1
PASS  正文 text / panel      12.14:1   PASS  on-accent / accent     5.83:1
PASS  正文 text / surface    13.13:1   PASS  status-done / surface  5.07:1
PASS  弱文本 muted / bg       5.16:1   PASS  status-failed / surface 5.41:1
PASS  极弱 faint / bg         3.35:1   PASS  status-info / surface  5.59:1
```

## 4. 未取到的证据 / 未做

- **没有像素证据**：未重启 GUI（避免与用户正在运行的实例抢数据根锁），
  「切到银白皮肤后的整屏观感」目前只有契约用例与对比度实算佐证。下次
  post-commit 重建后可在「设置 → 外观」里点选「银白冷钢」目视确认。
- **原生控件仍未跟随浅色**：`styles.css` 只有 `color-scheme: dark`
  （`index.html` 的 meta 也是 dark），既有浅色皮肤 `paper` 同样如此。
  自绘控件（含 `::-webkit-scrollbar`）走 token 已随皮肤换色，受影响的主要是
  原生下拉/弹层的系统底色。**本轮不改**（属共享基线变更，会影响所有皮肤，
  超出"加一套皮肤"的范围）；若要修，应作为独立改动加
  `:root[data-theme="light"] { color-scheme: light }` 并同时验证两套浅色皮肤。
- **材质/纹理未做**：银白若想加磨砂/拉丝质感需要图片资源，要走嵌入式资源
  白名单（见 `themes/README.md` 的「之后要做的」），本轮只做纯色 token 皮肤。
- **换肤回流当时未做**：终端（xterm）的配色只在创建时从 token 取一次，切到
  浅色皮肤后终端仍是一块深色底——与皮肤包无关，是"JS 取色的消费方"漏了回流。
  同日补记：[2026-09-20-terminal-theme-refresh.md](2026-09-20-terminal-theme-refresh.md)。
