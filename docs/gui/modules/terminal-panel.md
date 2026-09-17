# 下栏终端面板（Terminal panel）详细设计

> 状态：已实现（2026-09-17）
> 总体架构：[`../architecture.md`](../architecture.md)
> 后端模块：[`../../../gui/terminal/README.md`](../../../gui/terminal/README.md)

## 1. 目标与边界

在下栏（中栏底部）提供 VS Code 式的本地终端：可**多开**、可**收起**、可**拖拽
调整高度**。它是**用户的** shell，不是 Agent 能力。

边界（刻意不做）：

- 不进 Snapshot、不进 Agent 工具面、不进 headless 控制面——终端能执行任意命令，
  如果做成模型可调用或可远程编排的接口，等于绕过 `seelebridge/security` 的
  路径门禁与权限档位；
- 不做终端协议解析（ANSI/VT 由 xterm.js 负责，后端只搬字节）；
- 不做持久化：进程随宿主退出结束，终端不是会话数据；
- 不做 shell 集成（提示符解析 / cwd 追踪 / 补全）。

## 2. 分层与文件

| 层 | 文件 | 职责 |
|---|---|---|
| 后端会话管理 | `gui/terminal/terminal.go` | `Manager`（注册表）+ `session`（读泵/终态）+ 纯函数 |
| 后端 PTY | `gui/terminal/pty.go`、`shell*.go` | `go-pty` 适配 + 平台 shell 解析 |
| 桥接 | `gui/terminal_bridge.go` | `Terminal*` 方法、`seelex:terminal` 外投、cwd 取后端工作区 |
| 前端布局 | `gui/frontend/dist/terminal-panel.js` | 布局状态（展开/收起/高度/当前标签）+ xterm 实例 |
| 前端外壳 | `dist/index.html`、`dist/styles.css` | `.terminal-panel` 三态样式与拖拽分隔条 |
| vendor | `dist/vendor/xterm/` | xterm.js 5.3.0 + @xterm/addon-fit 0.10.0（MIT，落盘登记） |

## 3. 契约

Bridge 面（渲染层唯一入口）：

| 方法 | 参数 | 语义 |
|---|---|---|
| `TerminalOpen` | `{shell?, args?, dir?, cols, rows}` | 起一个终端，返回 `{id,title,shell,dir,cols,rows,running,exit_code?}`；`dir` 缺省取当前会话绑定的工作区根 |
| `TerminalWrite` | `id, data(base64)` | 用户输入原始字节 |
| `TerminalResize` | `id, cols, rows` | 前端 `fit()` 后的真实行列 |
| `TerminalClose` | `id` | 杀进程 + 关 PTY + 回收（幂等） |
| `TerminalList` | — | 当前终端元数据（重连对账） |

事件（独立通道，**不是** `seelex:event`）：

```json
{ "id": "term-1", "kind": "output", "data": "<base64>" }
{ "id": "term-1", "kind": "exit", "exit_code": 0 }
```

`output` 走 base64 而不是字符串：PTY 读边界会切断多字节字符，字符串过 JSON 会把
残片替换成 U+FFFD（不可逆）；前端还原成 `Uint8Array` 交给 `xterm.write` 自带
UTF-8 解码。

## 4. 生命周期与终态收敛

```text
Open ──► 读泵 goroutine（pty.Read → base64 → output 事件）
  ▲            │
  │            ├── 子进程退出（Wait）─┐
  │            └── Close（杀 + 关句柄）┘──► reap ──► 关句柄 ──► 排空输出 ──► exit 事件
  └──────────── 读写/尺寸双向 ────────────────────────────────────────────┘
```

收敛顺序是**硬要求**（见 `terminal.go:pump`）：Windows ConPTY 下子进程退出不会关闭
输出管道（伪控制台仍持有写端），只按读端判活会永远阻塞；unix 下子进程退出会让读端
返回 `EIO`，只按 `Wait` 判活则可能丢掉最后一段输出。因此：先 `Wait` 拿退出码 → 主动
`Close` 逼停读端 → 等输出排空 → 才外投 `exit`。

`Close` 幂等是安全要求：读泵收工与用户关闭各调一次，Windows 上重复
`ClosePseudoConsole` 会二次释放同一 HPCON（句柄可能已被复用）——实测会让宿主进程
直接死掉。`ptyProcess.Close` 用 `sync.Once` 兜底。

## 5. 前端布局状态

`seelex.terminal.v1`（localStorage，仅布局）：`{open, collapsed, height}`；高度钳在
`[120px, 72vh]`（`clampTerminalHeight`，上限比例与 CSS `max-height` 一致）。
`terminalTabItems` 给同名 shell 编号（`powershell` / `powershell 2`），
`nextActiveTerminal` 定义关标签后的落点（右邻 → 左邻 → 空态）。

三态：隐藏（`.hidden`，顶栏按钮/`` Ctrl+` `` 可回）→ 收起（`.is-collapsed`，只留
30px 头带）→ 展开。拖拽顶边改 `--terminal-h`；键盘 ↑↓ 微调 24px。

前端只持有布局与 xterm 实例：会话 ID、shell、cwd、尺寸、退出码一律来自后端；尺寸
的唯一来源是 xterm `fit()` 的 `onResize`（前端像素 → 后端 PTY）。

`TerminalOpen` 返回前到达的输出按 `id` 暂存在 `pending`，会话登记后按序补投（首屏
提示符不丢）。

## 6. 与「资源管理器」详情的关系

同一轮还补了资源管理器子页的**详情收起**：内容详情抽屉可折成 28px 竖轨
（`.code-split.is-preview-collapsed`），让工作树 + 提交记录独占子页（"让出内容页"）；
点竖轨还原，打开文件自动解除。它是抽屉的会话内状态（不落盘），与「隐藏工作树/提交
记录」（详情独占）互斥。见 [`right-sidebar.md`](right-sidebar.md)。

## 7. 验证

```text
go test ./gui/terminal -count=1          # 注册表/读泵/回收 + 真机 PTY 往返
node --test gui/frontend/dist/*.test.mjs # 纯函数 + 控制器级契约（假 DOM/假 xterm/假 Bridge）
```

- `TestManagerRealShellRoundTrip`：真 PTY + 真 shell，"未输入已有输出 → 输入回显 →
  `exit` 退出码 0"（真机用例，无 shell 时跳过）。
- `terminal-panel-controller.test.mjs`：多开/切标签/关标签、收起与拖高的持久化、
  早到输出补投、退出封输入、`TerminalOpen` 失败的清理。

## 8. Review 要点

1. 终态收敛顺序是否被改动（Windows 挂死 / unix 丢尾）。
2. `Close` 三条路径（读泵 / 用户 / 宿主退出）是否都幂等且不留 goroutine/进程。
3. `Terminal*` 是否仍不出现于工具面与 headless 分发。
4. 新终端 cwd 是否仍由后端工作区决定（不接受渲染层指定默认路径）。
