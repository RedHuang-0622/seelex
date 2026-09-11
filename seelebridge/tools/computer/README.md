# Computer（桌面 computer use 原语）

## 生态位

`seelebridge/tools/computer` 提供桌面 computer use 的**原语层**：截屏、鼠标、
键盘、窗口枚举与聚焦。主要调用方是 `computer/mcp` 这个 MCP stdio 服务端，
由 Codex 之类的外部宿主按 MCP 协议调用；Seelex 自身的运行时**不**依赖它，
不参与会话/工具注册链。

## 职责与非职责

职责：

- 把「看屏幕、动鼠标、敲键盘、找窗口」封装成可测试的 Go 函数；
- 坐标语义统一为虚拟桌面（多显示器并集）物理像素，进程启动即声明
  Per-Monitor V2 DPI 感知（`EnableDPIAwareness`），避免坐标被系统缩放虚拟化；
- 在 MCP 层做 JSON-RPC 编解码与工具名/参数校验。

刻意不做什么：

- 不做业务判断、不做安全审批、不决定"该不该点"——边界由宿主与用户审批负责；
- 不读取或修改 Seelex 会话、不注册成 Seelex 的工具（它是宿主侧能力）；
- 不实现截图压缩策略之外的图像处理。

## 文件结构

| 文件 | 职责 |
|---|---|
| `computer.go` | 领域类型（`Point`/`Rect`/`Capture`/`ClickOptions`）与 `Sleep`；平台无关。 |
| `image.go` | 最近邻缩放（只影响给模型看的像素，不改坐标系）。 |
| `keys.go` | 虚拟键码与组合键解析（跨平台可测）。 |
| `click.go` | 点击序列语义：按下/释放成对、失败必补释放。平台无关、可单测。 |
| `screen_windows.go` / `stub_other.go` | 截屏与 DPI 感知（Windows 实现 / 非 Windows 返回 `ErrUnsupported`）。 |
| `input_windows.go` | 鼠标与键盘注入（SendInput）。 |
| `window_windows.go` | 顶层窗口枚举、矩形、状态与聚焦。 |
| `mcp/main.go` | MCP stdio 服务端：`initialize` / `tools/list` / `tools/call`。 |

## 核心实现

- **输入注入走 `SendInput`，不走 `mouse_event`**：`SendInput` 返回真正插入队列
  的事件数，调用方因此能判断成功/失败；`mouse_event` 返回 `void`，把它的
  返回值当失败判断会让每一次点击都"假失败"。`runClickSequence` 另外保证按下
  与释放成对：按下失败时也补发一次释放，避免鼠标停在按下状态（桌面进入拖拽）。
- **绝对坐标归一化**：`absolutePoint` 按虚拟桌面尺寸把物理像素映射到
  `0..65535`，配合 `MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK`。
- **DPI 感知**：`EnableDPIAwareness` 用 `SetProcessDpiAwarenessContext`
  声明 Per-Monitor V2；不声明时 125% 缩放下注入坐标会落到目标的 80%。
- **键盘文本**：`TypeText` 按 UTF-16 单元逐个注入（支持中文与 emoji），
  组合键由 `PressKeys` 解析后按"修饰键按下 → 主键 → 修饰键逆序释放"下发。

## 数据流

MCP 宿主 → `mcp/main.go` 解析 JSON-RPC → 校验参数 → 调用原语
（`CaptureShot`/`Click`/`Drag`/`Scroll`/`TypeText`/`ListWindows`）→
结果编码为 MCP `content` 文本或 PNG 保存路径 → 返回宿主。

## 依赖方向

只依赖标准库与 Win32（`user32`/`gdi32`/`kernel32`）。不允许反向依赖
`seelebridge` 根包、`application/`、`session/` 等上层；非 Windows 平台由
`stub_other.go` 保持 `go build ./...` 可编译。

## 并发、安全与错误语义

- 原语是**有副作用的**：调用即真实操作桌面，不做并发隔离，调用方自行串行；
- 非 Windows 平台返回 `ErrUnsupported`，不 panic；
- 截屏写入路径由调用方给出，默认落在 `%LOCALAPPDATA%\codex-computer-use\shots`；
- 输入注入不校验目标窗口，权限由宿主的审批策略控制。

## 扩展方式

- 新原语：先在 `computer.go` 定义类型与语义，再在 `*_windows.go` 实现、在
  `stub_other.go` 补桩，最后在 `mcp/main.go` 的 `tools/list` 与 `tools/call`
  登记；
- 只改平台实现的函数必须保持签名与坐标系语义不变（虚拟桌面物理像素）。

## Review 指南

- 系统调用的返回值语义是否被正确解读（`BOOL` 判 0、`SendInput` 判计数、
  `void` 不得参与判断）；
- 任何"按下"路径是否存在没有释放的出口；
- 新增坐标路径是否仍然 DPI 感知、是否仍以虚拟桌面为坐标原点。

## 测试与验证

```text
go test ./seelebridge/tools/computer/ -count=1
```

- `click_test.go`：点击序列（成对、失败补释放、clicks<1 归一）。
- `desktop_probe_test.go`：默认跳过；设 `SEELEX_COMPUTER_DESKTOP_PROBE=1`
  才会在真实桌面上点一下并断言"不报错且左键已释放"（会夺走一次点击，
  只在本机手动验证时用）。
