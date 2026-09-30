# Linux computer use 实测报告（Ubuntu 22.04 / X11 / HEAD `38213d5`）

> 本文件回答一个问题：**Seelex 的 computer use 在 Linux 上到底能不能用**。
> 全部结论都来自 2026-09-25 在 VMware 里的 Ubuntu 22.04 虚拟机上的**实机复跑**，
> 二进制由仓库 HEAD（`38213d5`，即"computer use 平台抽象面 + Linux(X11)/macOS 后端"
> 那次提交）交叉编译得到，未改动任何源码。
>
> 一句话结论：**观察面（截屏 / 窗口 / 焦点 / 光标）与鼠标面（移动 / 点击 / 拖拽 / 滚轮）
> 完全可用；ASCII 键盘注入逐字节精确；非 ASCII（中文 / emoji）走"临时重映射键码"
> 那条路时不稳定，会丢字与串字——这是本包在 Linux 上的一个真实缺陷。**

## 1. 被测对象与环境

| 项 | 值 |
| --- | --- |
| 被测二进制 | `selebridge/tools/computer/mcp`（MCP stdio 作用面），`GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 交叉编译，5,080,574 B |
| 源码版本 | `git log -1` = `38213d5`（2026-09-25 01:34:28 +0800） |
| 客户机 | Ubuntu 22.04.5 LTS，kernel `6.8.0-79-generic`，X11 会话（`XDG_SESSION_TYPE=x11`，GNOME Shell/Mutter） |
| 桌面分辨率 | 1714×937（`xrandr`/`xdpyinfo` 一致） |
| XTEST | 可用（`xtest.Init` 成功；`x11Probe()` 因此返回可用） |
| 生效性判据 | ① X 服务器层：`xev` 收到的事件（`synthetic NO`）；② 应用层：把收到的字节写进文件的"水槽"终端；③ 独立裁判：`xdotool` 读 `_NET_ACTIVE_WINDOW` / `XGetInputFocus` |

## 2. 验收结果（逐条，全 PASS）

| # | 原语 | 结果 | 一手证据 |
| --- | --- | --- | --- |
| 1 | `VirtualScreen` | PASS | `virtual screen=1714x937+0+0`，与 `xdpyinfo` 一致 |
| 2 | `ListWindows` | PASS | 返回 `CUA/CUB` 及矩形、句柄、可见性，与 `xdotool getwindowgeometry` 逐项一致 |
| 3 | `ForegroundWindow` | PASS | 与 `xdotool getactivewindow` 一致 |
| 4 | `CaptureShot` / `SavePNG` | PASS | 落盘 PNG 魔数 `89 50 4e 47 0d 0a 1a 0a`；整屏 843,164 B、区域 42,709 B |
| 5 | `CursorPosition` / `MoveMouse` | PASS | 写入与回读一致（`(437,366)` 等） |
| 6 | `FocusWindow` | PASS | 置前后 `xdotool getwindowfocus` = 目标窗口 |
| 7 | `Click`（点击聚焦） | PASS | 焦点在 B 时，仅一次 `click` 打到 A，`_NET_ACTIVE_WINDOW` 与 `XGetInputFocus` 双双变为 A |
| 8 | `TypeText`（ASCII） | PASS | 点完直接注入 `echo … > step1.txt` + `enter`，文件落地为 `COMPUTER-USE-DEMO-OK` / `Linux 6.8.0-79-generic` |
| 9 | `PressKeys`（组合键） | PASS | `xev` 记录 `Shift_L` 按下 → 键码 → `Shift_L` 释放，`state 0x11` |
| 10 | `Scroll` | PASS | `xev` 记录 `button 5`（向下）逐格成对；终端"区域截图哈希"在滚动前后出现**新状态**（`59c47735e57d` → `68670148342d`），证明视口真的动了 |
| 11 | `Drag` | PASS | `xev` 记录 24 步 `MotionNotify`（`root:(129,851)` 起、终点与入参一致）+ 一次 `ButtonPress/Release button 1` |
| 12 | `ScrollTargets` | 设计性缺失 | Linux 侧 `Capabilities().ScrollTargets = false`，工具族**不注册** `computer_scroll_targets`，调用返回显式 `ErrUnsupported`（见 §5） |

### 2.1 X 层的一手证据（`xev`，`synthetic NO`）

按键序列（注入 `abz` / `ABZ` / `123` / `>|_`）在 `xev` 里的记录：

```text
a            keycode 38 (keysym 0x61, a)          state 0x10
b            keycode 56 (keysym 0x62, b)          state 0x10
z            keycode 52 (keysym 0x7a, z)          state 0x10
Shift_L      keycode 50 (keysym 0xffe1)           state 0x10
A            keycode 38 (keysym 0x41, A)          state 0x11   ← Shift 生效
…
>            keycode …  (keysym 0x3e, greater)    state 0x11
|            keycode …  (keysym 0x7c, bar)        state 0x11
_            keycode …  (keysym 0x5f, underscore) state 0x11
```

键码、Shift 状态、keysym 三项全对——**ASCII 路径在 X 协议层是精确的**。

## 3. 关键发现 A：桌面输入法会"吃掉"注入的 ASCII（环境，不是缺陷）

第一次端到端跑"聚焦终端 → 打字 → 回车"时**完全没生效**。用"按键水槽"（每收到一个字符就落盘）
把问题夹逼出来，并用 **A/B 单变量实验**定位到输入法：

| 注入 | A 组：`ibus engine` = `libpinyin`（默认） | B 组：`ibus engine xkb:us::eng` |
| --- | --- | --- |
| `a` | 无字节 | `61` |
| `A` | 无字节 | `41` |
| `>` | `e3 80 8b`（`》`） | `3e` |
| `1` | `e9 98 bf`（`阿`） | `31` |
| `_` | `e2 80 94`（`—`） | `5f` |
| `x` | 无字节 | `78` |

A 组的现象是**中文输入法的标准行为**：字母进拼音预编辑缓冲（不落盘），标点被转成全角中文
（`>`→`》`、`-`→`—`），数字在候选态下直接上屏。

两条重要旁证把责任从"我们的实现"上摘掉：

1. **参考实现同样失败**：同一台机器、同一目标窗口，`xdotool type` 也进不了水槽；
2. **切引擎即恢复**：`ibus engine xkb:us::eng` 之后，我们的注入逐字节精确。

> 结论：**用 computer use 之前必须确认桌面输入法不在中文态**（或由宿主在演示前切到
> `xkb:us::eng`）。这是所有 X11 注入方案共有的前提，`xdotool` 也一样。

## 4. 关键发现 B（缺陷）：非 ASCII 文本注入不稳定

ASCII 可靠，但**中文/emoji 会丢字、串字**。同一台机器、同一个水槽终端：

```text
注入（原文）: 中文输入：计算机使用在 Linux 上可用
期望码点    : U+4E2D U+6587 U+8F93 U+5165 U+FF1A U+8BA1 U+7B97 U+673A U+4F7F U+7528 U+5728 …

我们的 type_text（实测）: 文入计算使用在在在上用 Linux 用用用
xdotool type --delay 200 : 中文输入            ← 参考实现正确
```

复现与归因（可复跑）：

- **不是输入法**：B 组已把引擎切到英文；`xev`（纯 Xlib 客户端，不经过 ibus）也记录了错序/丢失；
- **不是环境**：`xdotool` 用同一条 XTEST 通道、同一台机器能正确打出 `中文输入`；
- **落在重映射路径上**：ASCII 命中现成键码（精确），非 ASCII 走
  `ChangeKeyboardMapping` 把最高空闲键码临时改成 `0x01000000|码点` 再按一下
  （`input_linux.go` 的 `TypeText` / `remapKeycode`）。实测表现是**映射与按键之间缺少
  一次强制往返/沉降**：多字连打时按下事件拿到的是"下一次映射"或"已还原为空"的状态
  （`xev` 末尾出现的 `keysym 0x0` 就是还原后的残留）；
- **把字间间隔拉到 1.5 s 可以显著减轻但不能消除**（`中输入：` vs 期望 `中文输入：`，
  仍丢 1 字）——所以不是单纯"客户端没来得及同步 `MappingNotify`"。

参考实现的做法可以做对照：`xdotool` 在 `XChangeKeyboardMapping` 之后会 `XSync`，
并且默认每个键之间留 `--delay`（12 ms；本次对照显式给了 200 ms）。

> 现状口径：`computer_type` 的**文档承诺是"支持中文与 emoji"**，Linux 上目前达不到；
> Windows 侧（`SendInput` + 逐 UTF-16 单元）不受影响。修法方向：在重映射与按下之间
> 插入一次服务器往返 + 沉降，并考虑改用 `XkbSetMap`（或为每次重映射走独立键码）。
> **本轮未改代码**：先把现象与判据留证，等确认修法再动实现。

## 5. 关键发现 C：Linux 侧的能力缺口是"设计性"的，不是漏做

| 能力 | Linux(X11) | 说明 |
| --- | --- | --- |
| 截屏 / 光标 / 窗口 / 焦点 | 有 | 纯 Go `jezek/xgb`，不需要任何外部二进制（不依赖 `scrot`/`xdotool`） |
| 鼠标 / 键盘注入 | 有 | XTEST；鼠标键号 1/2/3=左/中/右、4/5=纵向滚轮 |
| `computer_scroll_targets` | **无** | 它回答的是"哪些面板能滚、滚到哪、视口占比"——那是 **UI Automation 的 ScrollPattern** 才有的控件树信息，X11 协议里只有窗口与像素。`Capabilities().ScrollTargets=false` → 工具族**不注册**它（宁可不给，也不给一个编出来的答案）；`computer_scroll` 仍然可用，只是结果里 `unavailable: ["scroll_panel"]` |
| Wayland | 未覆盖 | `XDG_SESSION_TYPE=wayland` 时 `DISPLAY` 通常由 XWayland 提供，注入只作用于 X11 客户端窗口；纯 Wayland 窗口需要另做 `xdg-desktop-portal` 后端。本机是 Xorg，因此代码里没有 Wayland 分支（已在 `x11_linux.go` 文件头声明） |

对 agent / GUI 的暴露路径：

- **装配闸门**是 `seelebridge/runtime_computer.go`：`bridgecomputer.Supported()`（= 平台有桌面）
  且 `SEELEX_COMPUTER_USE` 未关闭，才会注册整族工具。Linux 在 X11 会话下 `Supported()=true`，
  因此 **TUI / GUI 里的模型都能看到 `computer_*`**；
- 权限仍走既有的"主体 × 路由组 × 位"：观察类落 `ro`，输入类落 `rw_desktop`；
- GUI 与 TUI 都只是消费 `Application` 的工具面，**没有平台相关代码**。

## 6. 演示：Linux computer use 能力演示（附素材）

演示脚本（**只用一个作用面：computer use 工具族**）在 VM 内跑，边跑边用
`ffmpeg -f x11grab` 抓桌面：

```bash
# 客户机（Ubuntu）：
export DISPLAY=:0
ibus engine xkb:us::eng            # 前提：桌面输入法不在中文态
bash /tmp/cu-demo.sh               # 剧本：看 → 找 → 点 → 敲 → 滚 → 拖
# 录屏包装（同一次运行里 ffmpeg 抓 1714x936@15fps）：
bash /tmp/cu-demo-record.sh
```

剧本与它的"落地效果"（每一步都有外部核验，不靠工具自证）：

| 步 | 调用 | 落地效果（外部读文件核验） |
| --- | --- | --- |
| 1 | `list_windows` | 列出 `CUDEMO` 的矩形/句柄，与 `xdotool getwindowgeometry` 一致 |
| 2 | `focus_window` | `xdotool getwindowfocus` = `CUDEMO` |
| 3 | `screenshot` | 落盘 PNG（整屏 / 区域），返回引用而不是像素 |
| 4 | `click` | 焦点从别处搬到被点窗口（`xdotool` 独立确认） |
| 5 | `type_text` + `press_keys` | 命令行被执行：`step1.txt` = `COMPUTER-USE-DEMO-OK` / `Linux 6.8.0-79-generic` |
| 6 | `type_text`（中文） | ⚠️ 落盘为 `文入计算使用在在在上用 Linux 用用用`（见 §4 缺陷） |
| 7 | `scroll` | 区域截图出现新状态（`59c47735e57d` ≠ `68670148342d`） |
| 8 | `move_mouse` + `drag` | `xev` 记录 24 步 motion + 成对按键 |
| 9 | `sleep` | 稳定等待；随后重新截图 |

素材（与 TUI 素材并列）：

| 文件 | 说明 |
| --- | --- |
| `artifacts/04_linux_computer_use_demo.mp4` | 31.7 s / 1714×936 / 15 fps / H.264 / yuv420p / 207 KB：一次完整演示录屏 |
| `artifacts/05_gui_linux.png` | Ubuntu 桌面上的 **Seelex Linux GUI（Wails/WebKitGTK）** 全屏截图 |
| `artifacts/06_cu_full_screen.png` | 演示起手：整屏（工具自己截的） |
| `artifacts/07_cu_after_ascii.png` | 敲完 ASCII 命令后，终端区域的截图 |
| `artifacts/08_cu_after_scroll.png` | 滚轮上翻后，同一区域的截图（与 07 内容不同） |

## 7. 顺带确认：Linux GUI 能构建也能跑

同一台客户机上，`~/seelex-linux/seelex-gui`（29,834,016 B，Wails 产物）实测：

```text
file  : ELF 64-bit LSB executable, x86-64, dynamically linked
ldd   : libwebkit2gtk-4.0.so.37 / libgtk-3.so.0 / libjavascriptcoregtk-4.0.so.18 —— 全部命中
启动  : 进程存活（Sl），窗口 "Seelex" 1644×873 @ (70,101)
界面  : 会话列表 / 对话·轨迹 / 输入框（LITE·MAX·HIGH）/ 右栏（状态·工作台·资源管理器）全部渲染
```

依赖包已在客户机内（`libgtk-3-dev` / `libwebkit2gtk-4.0-dev` 均已安装），
即"Wails Linux 构建依赖"这一步在 VM 内已经完成。

> 注意：Linux GUI 是 **cgo + GTK/WebKit** 的产物，无法在 Windows 上交叉编译；
> 仓库当前的 `make build` 只产出 `CGO_ENABLED=0` 的 TUI/headless 平台树，
> `scripts/build-gui.ps1` 只做 Windows。Linux GUI 现在有独立入口：
> `make build-linux-gui VERSION=<tag>`（Docker `ubuntu:22.04` + `webkit2_40`，
> 或 `scripts/build-linux-gui.sh --native` 在 Linux 主机内构建），交付落点为
> **P6 `dist/linux-amd64-gui/`**；完整步骤见
> [`build-linux-gui-delivery.md`](build-linux-gui-delivery.md)。

## 8. 复现清单

```bash
# 宿主（Windows）：交叉编译 MCP 二进制（computer use 是纯 Go，CGO_ENABLED=0 即可）
$env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'; $env:GOFLAGS='-mod=vendor'
go build -o dist/linux-amd64/computer-mcp ./seelebridge/tools/computer/mcp
scp dist/linux-amd64/computer-mcp red@<vm>:/home/red/computer-mcp-head

# 客户机：
chmod +x ~/computer-mcp-head
export DISPLAY=:0 XAUTHORITY="$HOME/.Xauthority"
ibus engine xkb:us::eng                       # 关键前提
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_windows","arguments":{}}}' \
  | ~/computer-mcp-head
```

## 9. 结论与后续

**能**。Linux(X11) 的 computer use 在实机上跑通了"看屏幕 → 找窗口 → 点 → 敲 → 滚 → 拖"整条链路，
且 ASCII 键盘与鼠标在 X 协议层逐项精确（`xev` 可复核）。要在无人值守/演示场景里稳定使用，
需要补齐三件事：

1. **前提检查**：桌面输入法不在中文态（否则 ASCII 被吃）；建议宿主在会话开始前显式切引擎；
2. **缺陷修复**：非 ASCII 注入的重映射路径（§4），修法与判据都已留证；
3. **构建入口**：给 Linux GUI（Wails、需 cgo）补一个构建目标，并把 computer use 一起打进去
   ——**构建入口已补**（`make build-linux-gui` / `scripts/build-linux-gui.sh`，交付落点 P6
   `dist/linux-amd64-gui/`，步骤见 [`build-linux-gui-delivery.md`](build-linux-gui-delivery.md)）；
   把 computer use 的 MCP 二进制一并打进该交付树仍未做。
   （computer use 本身与 GUI 无耦合，已在 TUI/headless 平台树里）。
