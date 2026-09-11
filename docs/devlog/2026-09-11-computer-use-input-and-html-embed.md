# 2026-09-11 computer use 输入注入修复 + 会话内 HTML 渲染块

> 日期: 2026-09-11 | 范围: `seelebridge/tools/computer`（未入库的 computer use
> 原语与 MCP 服务端）、`gui/frontend/dist`（markdown 围栏 → 沙箱 iframe 渲染）、
> 模块 README 与文档。

## 一、computer use 点击注入修复

### 现场

真机驱动 Seelex GUI 时，MCP 的 `click` 一直失败，返回：

```text
computer: mouse_event(down) 失败: The operation completed successfully.
```

现象有两层：点击既报错，又只发出按下、从不发出释放——鼠标停在按下状态，
桌面进入拖拽/框选。

### 根因

`input_windows.go` 用 `mouse_event` 注入鼠标事件，并把系统调用返回的 `err`
当失败判断。`mouse_event` 的返回值是 **void**，Go 侧拿到的是 `Errno(0)` 装箱
后的非 nil error，于是**每一次点击都判定失败**并直接 `return`，
`mouse_event(up)` 永远不会执行。

顺带澄清一处误判：DPI 感知**本来就是对的**（`mcp/main.go` 启动即调
`EnableDPIAwareness`）。先前观察到的"坐标偏 20%"来自验证脚本自己是非
DPI-aware 进程（125% 缩放下坐标被虚拟化），不是工具的问题。

### 修复

- 鼠标注入改走 `SendInput`（`mouseInputEvent`/`sendMouse`）：返回真正插入队列的
  事件数，成功/失败可判定；删除 `mouse_event` 绑定。
- 新增平台无关的 `click.go`：`runClickSequence` 把按下与释放绑成一对，
  **按下失败也要补发一次释放**，序列里不存在"只有按下没有释放"的出口。
- `Drag` 中途移动失败同样先释放再返回；`Scroll` 改走 `sendMouse`。

### 验证

- 红灯：真实桌面探针（当时的实现）直接失败——
  `Click 报错（按下/释放必须成对且成功时返回 nil）：... mouse_event(down) 失败`。
- 绿灯：同一探针 `--- PASS: TestClickReleasesMouseButton`（断言 Click 返回 nil
  且 `GetAsyncKeyState(VK_LBUTTON)` 显示左键已释放）。
- 端到端：重编 `bin/computer-use-mcp.exe` 后按 MCP 协议实际调用 `click`，
  返回 `{"result":{"content":[{"text":"clicked left x1 at (700,500)"}]}}`。
- `go test ./seelebridge/tools/computer/ -count=1`、`go build ./...` 全绿。
- 新增 `click_test.go`（序列语义，跨平台）与 `desktop_probe_test.go`
  （默认跳过，`SEELEX_COMPUTER_DESKTOP_PROBE=1` 才真点一下），并补上本包
  缺失的 `README.md`。

## 二、会话内 HTML 渲染块

### 目标

回答里能给图表/示意图，而不是只给一段文字或源码。

### 契约与安全边界

模型产出的 HTML 属于**不可信内容**，渲染规则是死的：

1. 绝不注入应用 DOM，只放进 `<iframe srcdoc>`；
2. `sandbox="allow-scripts"`，**不给 `allow-same-origin`**（两者同给等于没沙箱）：
   opaque origin 拿不到宿主 DOM / storage / cookie，也够不到 Wails bridge；
3. srcdoc 内嵌 CSP：`default-src 'none'` + `connect-src 'none'` +
   `img-src data: blob:` + 内联样式/脚本 + `form-action 'none'` + `base-uri 'none'`
   —— 自包含图表能画，但没有网络出口；
4. 不给 `allow-forms` / `allow-popups` / `allow-top-navigation`；
5. 源码以转义文本附在块内（"查看源码"），用户可自行核对渲染了什么。

触发方式是显式标记的围栏代码块：

````text
```seelex-html title="任务耗时分布" height=320
<svg viewBox="0 0 100 40">…</svg>
```
````

别名 `html-preview`。**普通 ```html 仍然是源码块**：既有会话里的代码片段
不会因为这次改动被突然执行。`height` 钳制在 120–640px。

### 实现

- `gui/frontend/dist/html-embed.js`：`isHtmlEmbedLanguage` / `parseEmbedInfo` /
  `buildEmbedDocument` / `renderHtmlEmbed` 纯函数，安全属性集中在这一处；
- `markdown.js`：`renderFence` 识别标记语言后转交 `renderHtmlEmbed`，其余不变；
- `styles.css`：`.html-embed` 面板（发丝边框 + 铭牌 + 黄铜信号点 + iframe +
  源码折叠），高度走 `--embed-height`；
- 测试装载器跟进：`markdown.js` 新增 `./html-embed.js` 依赖，9 个用 data URL
  装载的前端测试同时把该依赖内联，避免相对导入在 data URL 下解析失败。

### 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 238 tests / 238 pass / 0 fail
```

新增用例：`html-embed.test.mjs`（标记白名单、参数与高度钳制、CSP、
**不含 allow-same-origin**、srcdoc round-trip、恶意正文无法逃出沙箱）与
`markdown.test.mjs`（`seelex-html` → iframe；`html` → 源码块）。

## 未做 / 风险

- HTML 渲染块只在会话正文的 markdown 通道生效，尚未给模型显式的工具面
  （例如"渲染图表"专用 tool）；当前由模型按契约自己写围栏。
- 未做真机目视验证（本轮 GUI 验证只覆盖轮轴与恢复顺序）。
- 沙箱内的脚本仍可执行（图表需要它）：风险被压到"没有同源、没有网络"，
  但仍然是执行不可信代码，宿主侧的审批与用户判断仍是最后一道闸。
