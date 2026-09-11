# 2026-09-11 组件库接入 + 皮肤包（材质包）层 + 控件比例

> 日期: 2026-09-11 | 范围: `gui/frontend/dist`（vendor/themes/theme.js/styles/index/app）、
> 模块 README。用户诉求：前端像"毛胚房"，要一个组件库压上去，并且**必须可换肤、
> 可扩展**，之后要出材质包/皮肤包。

## 三层样式契约（本次定稿）

1. **组件库** `vendor/pico.min.css`（@picocss/pico 2.1.1，MIT，随包嵌入）：
   元素基线与通用组件皮（按钮/表单/表格/折叠/对话框）。它是低特异性、纯 CSS，
   只作用于没被类选择器覆盖的元素——引入它不会推翻既有外观；
2. **Seelex 样式** `styles.css`：语义 token + 组件样式，并把 `--pico-*` 桥接到
   Seelex token，第三方组件因此跟随皮肤换色（不会带进自己的配色）；
3. **皮肤包** `themes/<id>.css`：只覆盖语义 token 的换肤层。深浅、材质都在
   这一层做，组件结构与交互不变。

顺序在 `index.html` 里固定：pico → styles.css → （运行时）皮肤 link。

## 皮肤包机制

- `themes/manifest.json`：`schema_version/default/themes[]`，每项含
  `id/name/mode/description/swatches/file`；`file` 为空 = 默认皮肤（token 在
  `styles.css` 的 `:root`）；
- `theme.js`：清单归一化（丢弃畸形项、按 id 去重、default 必须存在）、
  **安全边界**（id 限 `[a-z0-9-]`，路径只允许 `themes/<id>.css`，拒绝 `../`、
  绝对 URL、query/hash）、切 `<html data-theme>` 与皮肤 `<link>`、记住选择
  （`localStorage["seelex.theme"]`）；
- 设置弹窗新增「外观」区：卡片列出皮肤（色板 + 名字 + 描述 + 当前标记），
  点击即时切换；
- 随包皮肤：`graphite`（默认）、`verdigris`（冷色）、`paper`（浅色）。
  浅色皮肤能成立的前提是本次把 5 个硬编码半透明面 token 化
  （`--surface-blur/--panel-solid/--surface-glass/--overlay/--surface-opaque`）。

契约校验在 `theme.test.mjs`：随包皮肤必须覆盖 37 个 token、不得出现
`!important`、必须只写 `:root` 规则。

## 控件比例（尺寸节奏）

新增 token：`--control-h-sm/--control-h/--control-h-lg`、`--row-min-h`。
图标按钮、徽标、输入框、页签、主次按钮、列表行统一取这几个值；右栏 Agent Team
的动作按钮固定最小宽度（窄栏里不再把"删除"挤成竖排），长备注单行省略；
微标签下限从 9px 抬到 10.5px（10 处字体声明），满足 README 里的字号下限约定。

## 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 244 tests / 244 pass
go build ./...                             # OK
go test ./gui/... ./internal/promptassets/... -count=1   # OK
```

真机（Wails GUI，`%TEMP%` 数据副本）目视验证：

- 皮肤选择器列出三套皮肤并标出「当前」；
- 点击「暖纸白昼」→ 整个界面即时切浅色（含弹层、表单、面板）；
- 点击「石墨黄铜」→ 即时切回深色黄铜；
- 控件比例：徽标成胶囊、按钮/输入同高、右栏团队区按钮不换行。

## 未做 / 风险

- **用户自带皮肤包未实现**：从磁盘目录加载（如 `config/themes/*.css`）需要后端
  路径门禁（`PathGate`）与只读通道；当前只有随包内置皮肤；
- 材质/纹理类皮肤需要图片资源，同样要走嵌入式资源白名单；
- 组件库只接管元素基线：部分历史组件仍是自绘样式，两者混用处的圆角/内边距
  已对齐到 token，但没有把每个组件都改写成组件库组件（那需要重写 DOM 结构）。
