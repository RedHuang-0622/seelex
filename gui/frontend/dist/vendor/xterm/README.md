# vendor/xterm（终端仿真器）

## 生态位

`gui/frontend/dist` 是零构建的静态前端，运行期从 `go:embed` 读取、**没有网络
访问**，因此第三方脚本必须落盘。本目录是 GUI 下栏终端（VS Code 式面板，
`dist/terminal-panel.js`）唯一的终端仿真依赖：xterm.js 负责 ANSI/VT 解析与
渲染，加法的 fit 插件负责按容器像素换算行列数。

## 内容

| 文件 | 用途 | 版本 | 许可 |
|---|---|---|---|
| `xterm.js` | 终端仿真器（UMD，暴露 `window.Terminal`） | xterm 5.3.0 | MIT（见同目录 `LICENSE`） |
| `xterm.css` | 终端基础样式（行/光标/滚动区） | xterm 5.3.0 | MIT（同 `LICENSE`） |
| `addon-fit.js` | 容器自适应插件（UMD，暴露 `window.FitAddon.FitAddon`） | @xterm/addon-fit 0.10.0 | MIT（见 `LICENSE.addon-fit`） |

来源：npm 包 `xterm@5.3.0` 与 `@xterm/addon-fit@0.10.0` 的 UMD 构建产物（各自包内的
终端版与插件版入口），经 unpkg 直接落盘、未做压缩改写；构建期同样不引入 CDN。

## 使用方式

- `index.html` 在模块脚本之前按顺序引入两个 JS：UMD 全局变量必须就绪，
  `terminal-panel.js` 才取得到 `Terminal` / `FitAddon`；`xterm.css` 与
  `styles.css` 的引入顺序是 xterm 在前（Seelex 的 token 覆盖层在后）。
- 主题：xterm 的 `theme` 由 `terminal-panel.js` 从 `:root` 的语义 token 读取
  （背景/前景/光标/ANSI 16 色），换肤不需要改 vendor 文件。
- 尺寸：容器尺寸变化后调 `fitAddon.fit()`，把得到的新行列数回传后端 PTY
  （`Bridge.TerminalResize`），前后端尺寸事实始终一致。

## 升级方式

1. 重新下载上表中的四个文件（版本号同步更新）；
2. 更新本表与 `LICENSE`/`LICENSE.addon-fit`；
3. 跑 `node --test gui/frontend/dist/*.test.mjs`（终端纯函数契约）并在真机
   目视验证一次输入/输出/多开/工具栏交互。

## Review 指南

- 不允许出现指向 CDN / 远程字体 / source map 的引用（运行期无网络）；
- 不允许把 vendor 文件改写成"Seelex 定制版"——主题与尺寸走 API，不改库；
- 新增第三方文件必须同时提交版本与许可信息（本表 + 许可原文）。
