# themes（皮肤包 / 材质包）

## 生态位

`gui/frontend/dist/themes` 是 Seelex GUI 的**换肤层**。它建立在组件库
（`vendor/pico.min.css`）与 Seelex 自有样式（`styles.css`）之上：皮肤包只
覆盖语义 token，组件结构、交互和可访问性都不随皮肤改变。

## 皮肤包契约

一个皮肤包 = 一个 CSS 文件 + `manifest.json` 里的一条登记。

**必须被覆盖的 token（契约，缺一不可）**：

| 组 | token |
|---|---|
| 面 | `--bg` `--panel` `--surface` `--surface-2` `--surface-3` `--border` `--border-strong` |
| 半透明面 | `--surface-blur` `--panel-solid` `--surface-glass` `--overlay` `--surface-opaque` |
| 文本 | `--text` `--muted` `--faint` `--text-bright` `--text-strong` `--text-soft` `--text-mid` `--text-dim` |
| 主信号 | `--accent` `--accent-strong` `--on-accent` |
| 状态 | `--status-running` `--status-done` `--status-failed` `--status-info` `--status-idle` |
| 状态派生 | `--tint-*` 四个、`--border-*` 四个 |
| 代码/刻度 | `--code-bg` `--tick` `--tick-hot` |

**可以不覆盖**：字体（`--font-*`）、字号、圆角（`--r-*`）、动效时长、间距。
它们属于品牌基座，不属于皮肤；要改就走 `styles.css` 的 token 层。

**允许做**：覆盖 `--shadow`/`--shadow-sm`（浅色皮肤需要更轻的投影）。

**不允许做**：改选择器结构、用 `!important`、引入远程字体或图片、写死与
token 无关的颜色（那样切肤会漏色）。皮肤包里出现 `.foo { ... }` 这类选择器
规则时，评审必须问清楚为什么 token 不足以表达。

## 登记与加载

`manifest.json`：

```json
{
  "schema_version": 1,
  "default": "graphite",
  "themes": [
    { "id": "graphite", "name": "石墨黄铜", "mode": "dark", "description": "…",
      "swatches": ["#141b21", "#222c35", "#d9a657", "#e9e4d8"], "file": "" }
  ]
}
```

- `file` 为空 = 默认皮肤（token 就在 `styles.css` 的 `:root`，不额外加载文件）；
- `mode` 决定 `<html data-theme>`（`dark`/`light`），原生控件与组件库据此切换；
- 加载器是 `dist/theme.js`：只允许 `themes/<id>.css` 这种**同源相对路径**，
  `id` 必须是 `[a-z0-9-]`，禁止 `../`、绝对 URL、query/hash（防路径逃逸）；
- 选择结果记在 `localStorage["seelex.theme"]`，启动时先套皮肤再渲染。

## 之后要做的（尚未实现）

- **用户自带皮肤包**：从磁盘目录（如 `config/themes/*.css`）加载需要后端
  路径门禁与只读通道（`PathGate`），当前只有随包内置皮肤；
- 材质/纹理类皮肤需要图片资源，同样要走嵌入式资源白名单。

## Review 指南

- 新皮肤是否覆盖了契约里的全部 token（`theme.test.mjs` 有清单校验）；
- 浅色皮肤是否把半透明面 token 一起换掉（否则弹层/输入建议会漏深色底）；
- 是否引入远程资源或选择器级 hack。
