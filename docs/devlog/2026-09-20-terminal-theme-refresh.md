# 换肤回流：终端配色跟随皮肤（2026-09-20 补记）

> 日期: 2026-09-20 | 范围: `gui/frontend/dist/terminal-panel.js`（新增 `refreshTheme`）、
> `gui/frontend/dist/theme.js`（新增 `onApplied` 回流口）、`gui/frontend/dist/app.js`（接线）、
> `theme.test.mjs` + `terminal-panel-controller.test.mjs`（回归）、`themes/README.md`、
> `gui/frontend/README.md`
> 承接: [2026-09-20-silver-skin.md](2026-09-20-silver-skin.md)（银白冷钢皮肤）

## 1. 现象与根因

用户反馈：换到「银白冷钢」后**终端的背景色没跟着变**——界面白了，终端还是一块深色底。

根因不在皮肤包：`silver.css` 覆盖了契约里全部 39 个 token（含 `--code-bg`），
`.terminal-body` 也确实是 `background: var(--code-bg)`，CSS 侧跟着换。
漏的是 **xterm 实例的配色**：`defaultTerminalFactory` 在
`new Terminal({ theme: terminalTheme() })` 时从 token 取一次色就固定住
（`background/foreground/cursor/selectionBackground` + 16 个 ANSI 色），
而 `themeController.apply()` 只做两件事——改 `<html data-theme>`、换皮肤
`<link>` 的 `href`。**没有任何代码通知终端重取 token**，xterm 画布于是继续画
旧皮肤的底色。换肤对「JS 里取色的消费方」不是纯 CSS 行为。

## 2. 复现（先红）

`terminal-panel-controller.test.mjs` 的假 DOM 增加"当前皮肤 token 表"
（`installFakeGlobals(viewport, tokens)`，`getComputedStyle().getPropertyValue`
从表里读），假终端与 `defaultTerminalFactory` 同口径：创建时快照一次配色。
`theme.test.mjs` 则加一条"换肤后要广播已生效"的用例。

```text
$ node --test gui/frontend/dist/terminal-panel-controller.test.mjs gui/frontend/dist/theme.test.mjs
  修复前：
  TypeError: harness.panel.refreshTheme is not a function
  AssertionError: Expected values to be strictly deep-equal:
    + actual - expected
    actual: [], expected: [ 'silver' ]
```

## 3. 修复

- `terminal-panel.js`：新增 `refreshTheme()`——重读 token 并把 `theme` 套回
  **全部活着的终端**（已关闭的不碰），返回刷新台数。`createTerminalPanel`
  的返回面里与"局部布局面"并列加一组"换肤回流面"。
- `theme.js`：`createThemeController({ onApplied })`。`apply()` 在
  `<html data-theme>` + 皮肤 `<link>` 落地后回调：外链皮肤是异步资源，
  除立即兜底一次外，再挂 `<link>.onload` 回流——CSS 落地那刻读到的 token
  才是新皮肤色值（这也顺手盖住"终端在皮肤 CSS 加载完成前创建"的启动竞态）。
  回流方抛错逐个吞掉，不影响换肤本身。
- `app.js`：`initialiseTheme()` 里把 `onApplied` 接到
  `terminalPanel.refreshTheme()`（`terminalPanel` 在模块顶层已创建，回调里
  引用它安全）。换肤入口（设置里的皮肤卡）无需改动——它走的是同一个
  `controller.apply()`。

## 4. 验证（红→绿）

```text
$ node --test gui/frontend/dist/theme.test.mjs gui/frontend/dist/terminal-panel-controller.test.mjs
# tests 19 / pass 19 / fail 0（含两条新用例）

$ node --test gui/frontend/dist/*.test.mjs
# tests 403 / pass 403 / fail 0（修复前 401）

$ go test ./... -count=1 -timeout=120s
# 全绿，exit 0

$ node --check（app.js / theme.js / terminal-panel.js / 两个测试文件，复制成 .mjs 后查）
# OK ×5
```

重建 GUI 基线二进制（`go build` 直写 `dist/seelex-gui-dev/seelex-gui.exe`，
Go 会把被运行中实例占用的旧文件改名成 `seelex-gui.exe~`）：

```text
$ go build -tags "gui,desktop,production" -trimpath -ldflags "-s -w -H windowsgui
    -X .../buildinfo.Version=dev -X .../buildinfo.DefaultFrontend=gui" -o dist/seelex-gui-dev/seelex-gui.exe .
# exit 0 → seelex-gui.exe 30666240 B / 17:56:11 / sha256 12E217B5…F59F8A7
#          seelex-gui.exe~ 30660096 B / 17:48:02（上一版，含皮肤但无本次回流）

$ 字节级取证（嵌入前端）
# 新版：refreshTheme=HIT@16135164  notifyApplied=HIT@15868780  silver.css=HIT@15376643
# 上一版（~）：refreshTheme=MISS       notifyApplied=MISS        silver.css=HIT@15376643

$ scripts/seelex-flow.ps1 -Stage Smoke -SmokeTarget dist/seelex-gui-dev/seelex-gui.exe
# [1/2] version exit=0 output=dev PASS；[2/2] boot exit=0 ready=True PASS
```

## 5. 未做 / 边界

- **无像素证据**：未重启 GUI，浅色皮肤下终端底色的目视确认留给下一次重启
  （换肤回流的行为由上面两条契约用例守住）。
- 只回流 `theme`：字号/字体等不随皮肤变，不参与回流。
- 原生控件 `color-scheme` 仍为 `dark`（与 `silver-skin` 记录同一笔未做项）。
