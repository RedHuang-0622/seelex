# GUI Terminal（下栏终端后端）

## 生态位

`gui/terminal` 是桌面宿主的**本地终端会话管理**：一个终端 = 一个跨平台伪终端
（Windows ConPTY / unix 原生 pty）+ 一个子进程，多开互不共享状态。主要调用方
是 `gui`（`Bridge.TerminalOpen/Write/Resize/Close/List`，见
[`gui/README.md`](../README.md)）；前端 `gui/frontend/dist/terminal-panel.js`
只画外壳（标签、收起、拖高），ANSI/VT 渲染交给 vendor 的 xterm.js。

它**不是** Agent 能力：终端不出现在工具面、不进 Snapshot、也不在 headless 控制面
暴露（见「职责与非职责」）。

## 职责与非职责

职责：

- 拉起 / 结束 shell 子进程，持有 PTY 句柄；
- 把 PTY 输出按原始字节（base64）外投，把用户输入按字节写入；
- 尺寸同步（前端 fit 后的 cols/rows → PTY）；
- 会话注册表（ID 分配、创建序、上限、退出码）。

非职责（刻意不做）：

- **不解析终端协议**：不做 ANSI/VT 解析、不维护屏幕模型，字节原样搬运；
- **不进 Agent 权限模型**：模型不能调用终端（避免把"任意命令执行"塞进工具面）；
  headless JSON-RPC 也不分发终端方法——它是给**用户**用的本地 shell；
- **不进 Snapshot / 不参与事件水位**：终端与会话状态无关，输出走独立事件名
  `seelex:terminal`，不占 `delivery_seq`、不参与 retry/resync；
- **不持久化**：进程随宿主退出而结束，终端不是会话数据；
- **不做 shell 集成**（提示符解析、cwd 追踪、命令补全）——那需要终端协议层。

## 文件结构

| 文件 | 职责 |
|---|---|
| `terminal.go` | `Manager`（会话注册表）+ `session`（读泵与终态收敛）+ 纯函数（尺寸钳制、环境合并、标题、目录校验）。 |
| `pty.go` | `process` 接口的 PTY 实现（`ptyProcess`）与默认启动器 `ptyLauncher`；`Close` 幂等（见「并发与错误语义」）。 |
| `shell.go` | `ShellFor`：显式 shell > `SEELEX_TERMINAL_SHELL` > 平台候选清单，返回已 `LookPath` 的绝对路径。 |
| `shell_windows.go` | Windows 候选清单（pwsh → powershell → COMSPEC → cmd）与 PowerShell 的 `-NoLogo`。 |
| `shell_other.go` | 非 Windows 候选清单（`$SHELL` → `/bin/bash` → `/bin/sh`），不带参数（见下）。 |
| `terminal_test.go` | 注册表/读泵/回收的离线用例（假进程注入）+ 一条真机 PTY 用例。 |

## 核心实现

- `Manager`：`seq`（ID 分配，`term-<n>`）、`sessions`（ID → `session`）、`handler`
  （事件回调，必须并发安全）；`Open` 前先查上限（`MaxSessions = 16`），`Open`
  失败不留半成品（launcher 失败即返回错误，不注册会话）。
- 依赖注入的 `launcher`（`func(launchRequest) (process, error)`）：默认
  `ptyLauncher` 走真 PTY，测试注入假进程，因此注册表语义不需要真进程也能验证。
- `process` 接口（`Read/Write/Resize/Kill/Wait/Close`）是平台差异的唯一边界：
  跨平台细节全部由 `github.com/aymanbagabas/go-pty` 承担（Windows ConPTY /
  unix `creack/pty`），本包只做接口收窄。
- 尺寸：`normalizeSize` 把 0/负数/超界钳到 `[2, 1000]`（PTY 对 0 尺寸的行为各
  平台不一致，钳住比让内核报错更好）。
- shell 解析：`ShellFor` 先 `exec.LookPath` 再启动——"找不到命令"这类错误在
  `Open` 阶段就暴露，不会留下一个立刻退出的空终端。非 Windows 刻意**不加**
  `-l`：登录 shell 会把 cwd 切到 `$HOME`，与"终端开在工作区根"冲突。
- cwd：`Options.Dir` 只在目录真实存在时生效（`usableDir`），否则回退进程 cwd；
  桥接层传入的默认值是**当前会话绑定的工作区根**（见 `gui/terminal_bridge.go`）。

## 数据流与生命周期

```text
Bridge.TerminalOpen(cols, rows, dir)
  → Manager.Open → ShellFor → ptyLauncher（PTY + CreateProcess）
  → 读泵 goroutine：pty.Read → base64 → Event{kind:output} → Bridge → seelex:terminal
用户按键 → xterm onData → Bridge.TerminalWrite(base64) → Manager.Write → pty.Write
前端 fit → xterm onResize → Bridge.TerminalResize → Manager.Resize → Pty.Resize
终态：子进程退出（Wait）或 Close → session.reap → Event{kind:exit, exit_code}
```

终态收敛顺序（**必须按这个顺序**，否则 Windows 上会卡住）：

1. 等子进程 `Wait()` 拿退出码（唯一调用点，`pty.Cmd.Wait` 只允许调用一次）；
2. 主动 `Close()` PTY 句柄——Windows ConPTY 下子进程退出**不会**关闭输出管道
   （伪控制台仍持有写端），不主动关则读泵永远阻塞；
3. 等读泵排空 → 才外投 `exit`（保证 `exit` 事件排在全部 `output` 之后）。

`Manager.Close(id)` 是"杀子进程 + 关句柄 + 等读泵收敛 + 从注册表摘除"，对已退出
的会话重复调用是幂等的；`CloseAll()`（宿主退出路径）不等待读泵，避免退出被慢
进程拖住。

## 依赖方向

- 允许依赖：`github.com/aymanbagabas/go-pty`（PTY）、标准库。
- 禁止反向依赖：`gui/terminal` 不 import `gui`（否则形成环）、不 import
  `application`；`gui` 单向 import 本包。跨域类型不下沉到 `application/contract`
  ——终端不是应用层契约。

## 并发、错误与安全语义

- 每个 `session` 一个读泵 goroutine；`session.mu` 保护 `info/closed`，
  `Manager.mu` 保护注册表与 `seq`。`Manager.emit` 在读泵 goroutine 上调用 handler
  （Wails `EventsEmit` 本身并发安全）。
- **`Close` 必须幂等**：读泵收工与用户关闭会各调一次 `Close`；Windows 上重复
  `ClosePseudoConsole` 会二次释放同一个 HPCON，句柄值可能已被系统复用——实测会
  让宿主进程直接死掉（无输出、退出码 1）。`ptyProcess.Close` 用 `sync.Once` 兜底。
- 退出码折算：`*exec.ExitError` 取真实码，正常结束 0，其余等待失败记 -1（用户主动
  关闭的会话可能是 -1/1，前端只作提示）。
- 上限与拒绝：超过 `MaxSessions` 直接报错，不静默排队；未知 ID 的读写/关闭都返回
  可展示错误（不 panic）。
- 安全边界：终端以宿主用户身份执行任意命令，这是"本地终端"的固有语义；因此它
  只对桌面渲染层开放（用户显式点击），不进 Agent 工具面、不进 headless 控制面。
  默认 shell 可用环境变量 `SEELEX_TERMINAL_SHELL` 覆盖（本地个人化配置）。

## 扩展方式

- 新增平台：只加 `shell_<goos>.go`（候选清单 + 默认参数）；PTY 侧由 go-pty 承担，
  本包无需改。
- 新增能力（如启动时注入环境、指定 shell 参数）：扩 `Options` 与
  `launchRequest`，并在 `gui/terminal_bridge.go` 的 `TerminalOpenOptions` 同步字段。
- 新增事件种类：扩 `EventKind` 与前端 `terminal-panel.js` 的 `terminalEventOf`
  分派（两边都要有测试）。

## Review 指南

- 终态收敛顺序是否被改动？只按读端判活会在 Windows 上挂死，只按 `Wait` 判活会在
  unix 上丢掉最后一段输出。
- `Close` 路径（读泵 / 用户 / 宿主退出）是否都幂等，是否可能泄漏 goroutine 或进程。
- 新终端的工作目录是否仍来自后端工作区（不允许渲染层传任意路径作为默认值）。
- 是否有人把终端接进了 Snapshot / 工具面 / headless——那会改变权限与安全边界。

## 测试与验证

```text
gofmt -l gui/terminal
go vet ./gui/terminal/
go test ./gui/terminal/ -count=1 -timeout=180s
```

- 离线：`TestManagerEmitsBase64OutputThenExit`（输出按 base64 外投 + 退出码折算）、
  `TestManagerKeepsCreationOrderAndRejectsUnknownSession`、`TestManagerWritesInputAndKillsOnClose`、
  `TestManagerRejectsOpenWithoutHandlerAndOverLimit`、`TestManagerShellFailureIsReported`
  以及 `TestNormalizeSize`/`TestMergeEnvOverridesInPlace`/`TestUsableDirRequiresRealDirectory`/
  `TestTitleForStripsExtension`。
- 真机（本机有 shell 时执行）：`TestManagerRealShellRoundTrip` 起真 PTY + 真 shell，
  断言"未输入前已有输出（提示符/横幅）→ 输入行回显 → `exit` 退出码 0"。

## 文件与函数索引

- `terminal.go`
  - `type EventKind string`：终端事件类别（`output` / `exit`）。
  - `type Event struct`：外投事件（`id` / `kind` / base64 `data` / `exit_code`）。
  - `type Options struct`：开终端请求（shell / args / dir / cols / rows / env）。
  - `type Info struct`：终端元数据（ID、标题、shell、cwd、尺寸、运行态、退出码）。
  - `type EventHandler func(Event)`：事件回调（并发安全由实现方保证）。
  - `type Manager struct`：终端会话注册表（进程级，由 GUI 宿主持有）。
  - `func New(handler EventHandler) *Manager`：创建管理器；handler 为 nil 时 Open 失败。
  - `func (m *Manager) Open(options Options) (Info, error)`：启动一个新终端并返回其元数据。
  - `func (m *Manager) Write(id string, data []byte) error`：把用户输入（原始字节）写入终端。
  - `func (m *Manager) Resize(id string, cols, rows int) error`：调整终端尺寸（前端 fit 后回传）。
  - `func (m *Manager) Close(id string) error`：结束终端：先杀子进程再关 PTY，读泵收敛后回收会话。
  - `func (m *Manager) CloseAll()`：关闭全部终端（宿主退出路径）。
  - `func (m *Manager) List() []Info`：按创建序返回全部终端元数据。
  - `func (m *Manager) Get(id string) (Info, bool)`：返回单个终端元数据。
  - `func (m *Manager) lookup(id string) (*session, error)`：按 ID 取会话（内部）。
  - `func (m *Manager) emit(event Event)`：把事件交给注入的 handler（内部）。
  - `func (s *session) snapshot() Info`：加锁拷贝元数据。
  - `func (s *session) pump(manager *Manager)`：终端的唯一收敛点（等子进程 → 关句柄 → 排空输出 → 外投 exit）。
  - `func (s *session) reap() int`：等待子进程结束并把终态写回元数据。
  - `func (s *session) shutdown()`：杀子进程并释放终端句柄（幂等）。
  - `func exitCodeFromWait(err error) int`：把 Wait 的返回值折算成退出码。
  - `func normalizeSize(cols, rows int) (int, int)`：把请求尺寸钳到可用区间。
  - `func clamp(value, min, max int) int`：整数钳制。
  - `func usableDir(dir string) string`：只在目录真实存在时返回它。
  - `func mergeEnv(base, extra []string) []string`：合并继承环境与附加项（后者覆盖同名键）。
  - `func titleFor(shell string, seq int) string`：给出标签默认标题（shell 基名去扩展名）。
  - `func sequenceOf(id string) int`：从 `term-<n>` 取创建序号。
- `pty.go`
  - `type ptyProcess struct`：`process` 接口的 PTY 实现（`Close` 幂等）。
  - `func (p *ptyProcess) Read(buffer []byte) (int, error)`：读终端输出。
  - `func (p *ptyProcess) Write(data []byte) (int, error)`：写用户输入。
  - `func (p *ptyProcess) Resize(cols, rows int) error`：设置 PTY 尺寸。
  - `func (p *ptyProcess) Kill() error`：请求结束子进程。
  - `func (p *ptyProcess) Wait() error`：等待子进程结束。
  - `func (p *ptyProcess) Close() error`：释放 PTY 句柄（`sync.Once`，重复调用安全）。
  - `func ptyLauncher(request launchRequest) (process, error)`：默认启动器（真 PTY + 真子进程）。
- `shell.go`
  - `const ShellEnvName`：覆盖默认 shell 的环境变量名（`SEELEX_TERMINAL_SHELL`）。
  - `func ShellFor(explicit string, args []string) (string, []string, error)`：解析本平台要拉起的 shell。
- `shell_windows.go`
  - `func shellCandidates() []string`：Windows 默认 shell 优先级（pwsh → powershell → COMSPEC → cmd）。
  - `func defaultShellArgs(shell string) []string`：给 PowerShell 关掉版权横幅。
- `shell_other.go`
  - `func shellCandidates() []string`：非 Windows 默认 shell 优先级（`$SHELL` → bash → sh）。
  - `func defaultShellArgs(string) []string`：不带参数（登录 shell 会改 cwd）。
- `terminal_test.go`
  - `func TestManagerEmitsBase64OutputThenExit(t *testing.T)`：输出按 base64 外投、退出事件与回收语义。
  - `func TestManagerKeepsCreationOrderAndRejectsUnknownSession(t *testing.T)`：创建序与未知会话拒绝。
  - `func TestManagerWritesInputAndKillsOnClose(t *testing.T)`：输入透传与关闭时的杀进程。
  - `func TestManagerRejectsOpenWithoutHandlerAndOverLimit(t *testing.T)`：无 handler / 超上限拒绝。
  - `func TestManagerShellFailureIsReported(t *testing.T)`：shell 解析失败与启动失败都不留半成品。
  - `func TestNormalizeSize(t *testing.T)`：尺寸钳制。
  - `func TestMergeEnvOverridesInPlace(t *testing.T)`：环境合并覆盖语义。
  - `func TestUsableDirRequiresRealDirectory(t *testing.T)`：cwd 目录校验。
  - `func TestTitleForStripsExtension(t *testing.T)`：标签标题派生。
  - `func TestShellForRejectsUnknownExplicitShell(t *testing.T)`：显式 shell 不存在时报错。
  - `func TestShellForHonoursEnvOverride(t *testing.T)`：`SEELEX_TERMINAL_SHELL` 生效。
  - `func TestShellForDefaultResolutionOnHost(t *testing.T)`：默认解析在本机可用。
  - `func TestManagerRealShellRoundTrip(t *testing.T)`：真机 PTY 往返（提示符 → 回显 → exit 0）。
