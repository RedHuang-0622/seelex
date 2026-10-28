# tools / ssh

## 生态位

Seelex 内置 SSH 工具的**连接基座**：库内直连（`golang.org/x/crypto/ssh`，不 shell out 到 `ssh.exe`）、四段独立超时、`known_hosts` 严格校验、可分类的失败探针、三种返回形态的命令执行。

主要调用方：

- `backend/tool/registry` 的**传输选择器**：探针通/被显式指名时，由
  `registry/ssh_transport.go` 把本包的 `Backend` 适配成 `registry.Transport`
  （执行面），选择规则与固定错误码在 registry 那一侧；
- （规划）`backend/tool/registry` 的 `ssh_*` 工具族——把项目内 host 注册表的一行填成 `Spec` 交进来；
- 现在：`backend/tool/ssh/sshprobe`（本目录的独立驱动，人/脚本用来排障）；
- 测试：全部用例跑在进程内 sshd 上，不依赖真机。

设计依据与逐条纪律见 [`docs/arch/ssh-tool.md`](../../../docs/arch/ssh-tool.md)（§5 四段超时、§6 指纹、§7 凭据、§9 命令纪律、§13 验收判据）。

## 职责与非职责

**职责**：一次连接的建立与关闭、认证材料的选择与"实际生效方式"回报、主机指纹校验与显式信任写入、一次命令的同步/脱钩执行、失败的分段归因与分类。

**非职责**（刻意不做）：

- 不做工具注册与权限判定——归 `backend/tool/registry` 与 `backend/limit/policy`；
- 不做 host 注册表持久化——归上层的项目内 `.seelex/ssh/hosts.yaml`；
- 不做凭据存储——DPAPI/钥匙串属凭据面，本包只接收**引用**；
- 不做业务成功判定——见下文"错误语义"；
- 不做 SFTP / 端口转发（规划，见设计稿 §12 待决 D-1）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `ssh.go` | 包文档 + `Spec` / `Timeouts` / `AuthSpec` / `KnownHostsSpec` / `Client` + 错误码常量 |
| `dial.go` | `Dial`：四段超时装配（`net.Dialer` → `NewClientConn` 双计时器） |
| `auth.go` | `buildAuthPlan`：agent → key(+口令) → password 的优先级，`auth_used` 推断 |
| `auth_agent_windows.go` | Windows 命名管道 `\\.\pipe\openssh-ssh-agent`（`x/sys/windows` CreateFile） |
| `auth_agent_other.go` | unix domain socket（`$SSH_AUTH_SOCK`） |
| `hostkey.go` | `knownhosts` 校验器、哈希条目、`Trust` / `TrustFromProbe`、指纹错误分类 |
| `exec.go` | `Run` / `RunDetached`、命令来源归一、非 ASCII 拒收、有界输出、三态收口 |
| `probe.go` | `Probe` + `classifyDial` / `classifyAuth`（把裸错误翻成分类与下一步） |
| `probe_cache.go` | `Prober`：带 TTL 的探测结论缓存（未探 ≠ 探通；键 = 端点+用户） |
| `transport.go` | `Backend`：执行面（一次一连接）+ `commandRequest` 命令来源纪律 |
| `remote.go` | 远端文件/作业面共享的**字节与路径承载**：base64 载荷、保留退出码、远端错误码、decodeRemoteBase64 |
| `files.go` | 远端文件面：`Backend.ReadFile` / `WriteFile` / `EditFile`（路径 base64、内容走内容通道） |
| `job.go` | 远端作业面：`StartJob` / `JobStatus` / `JobOutput` / `JobKill` / `RunJob` + `ssh:` 句柄语法 |
| `result.go` | `Status` / `Segment` / `Result` 返回信封 |
| `sshprobe/main.go` | 独立驱动：`main` → `run` → `parseFlags` / `buildSpec` / `doProbe` / `doTrust` / `doExec` |
| `ssh_test.go` | 进程内 sshd 夹具 + 全部验收用例 |
| `transport_test.go` | 不需要对端的那部分：命令来源纪律 + 探针缓存三态/并发 |
| `files_test.go` | 远端面判据：路径经 base64 过线、内容走 stdin、作业句柄与"仍在跑" |

## 核心实现

**四段超时**（`dial.go`）。`ClientConfig.Timeout` 只覆盖 TCP 建连，握手/认证无条件上限，`ssh.Session` 也收不到 `ctx`。因此本包自己拨号（拿到 `net.Conn` 与取消权），再在 goroutine 里跑 `ssh.NewClientConn`，用两个计时器分别卡 banner 段与 auth 段；banner 段结束的信号是**第一次认证回调**（`ClientConfig.AuthCallback`）。

**认证**（`auth.go`）。优先级固定为 agent → key(+口令) → password。`AuthCallback` 只做观测（服务端允许哪些方法、已经试过哪些），选路交给库按 `Auth` 顺序走。`auth_used` 由"第一个没出现在 `TriedMethods` 里的候选"推断——单步认证准确；多步认证（partial success）只是近似，已在注释里标注。

**指纹**（`hostkey.go`）。判定一律经由 `knownhosts.New(...)`，因此 `|1|salt|hash` 哈希条目天然被支持——**禁止**用文本匹配判存在性。`Trust` 要求人确认的指纹与本次握手逐字相等，否则硬失败且不写盘；只追加一行，不做换行归一。

**执行**（`exec.go`）。命令只允许三种来源：`CommandFile`（首选，经 `bash -s` 走 stdin）、`Stdin`、内联 `Inline`（**仅单行 ASCII**，含非 ASCII 直接拒）。脱钩形态固定为 `setsid <shell> <script> </dev/null >log 2>&1 &`，脚本用 base64 传输以绕开三层引号与代码页。

**探针**（`probe.go`）。分类不看裸错误串：`knownhosts.KeyError` 的 `Want` 空/非空区分"首次连接"与"指纹不符"；`ClientAuthContext.AllowedMethods` + `TriedMethods` 区分"公钥被接受但被拒"（口令/agent 问题）与"公钥不被接受"（未登记/格式）。

**远端文件面**（`files.go` + `remote.go`）。三个动作只做机械搬运，判据留在工具层：

- 路径一律 **base64 承载**后由远端 `base64 -d` 解码进 shell 变量——路径可能是中文、
  空格、引号、`$()`，拼进命令行就会被远端 shell 重新解释（轻则找不到文件，重则执行了
  路径里的命令）。载荷字母表只有 `A-Za-z0-9+/=`，对任何 shell 都安全；
- 内容一律走**内容通道**（写：`base64 -d > 临时名` 再从 stdin 读；读：`base64 < 文件`
  流回本端）；命令正文里因此不出现一个字节的用户数据；
- 行窗口、替换次数、截断判定都在 Go 这边算，与本地工具面同一份判据（
  `strings.Count` / `strings.ReplaceAll` / 同一套行窗口）；
- 远端环境只要求 `base64`（coreutils / BSD 均有）；缺了给
  `SSH_REMOTE_TOOL_MISSING`，不猜别的解码器。

**远端作业面**（`job.go`）。一次工具调用要能在**本端预算用尽**之后仍可指认远端进程，
所以远端执行的形态是一组文件而不是一条通道：

```
${TMPDIR:-/tmp}/seelex-<host>-job-<nanos>/
  cmd.sh   用户命令正文（内容通道写入）
  run.sh   监管脚本：写 pid → cd → 跑 cmd.sh → 落退出码
  pid      监管进程 pid（setsid 组长：kill -TERM -<pid> 即整棵树）
  out/err  标准输出/错误（落盘：本端随时可以回头取）
  exit     退出码（有它 = 作业结束）
```

`setsid` 不是装饰：否则通道一关，sshd 给整个会话发 SIGHUP，监管进程连同"落退出码"
一起消失，于是"跑完了"永远显示成"还在跑"。

句柄是 `ssh:<base64url(dir)>`（**自描述**：不需要本端保存登记表，而工具调用之间的进程
状态本来就不可依赖）。它是独立的命名空间，与 `bt-` / `bs-` / `bteam-` 语法上互不相认。

## 数据流

```
Spec(注册表一行)
  → dial: connect(dialer) → plan(认证材料) → verifier(knownhosts)
  → NewClientConn(双计时器) → Client{conn, client, authUsed, hostKey}
  → Run/RunDetached → Result{status, exit_code, stdout/stderr, segment, still_running, note}
  → Probe → ProbeResult{class, segment, fingerprint, allowed/tried methods, hint}
```

## 依赖方向

允许：`contract` / `internal` 的零依赖工具、`golang.org/x/crypto/ssh`（含 `knownhosts`、`agent`）、`golang.org/x/sys/windows`（仅 windows 构建）。

禁止：反向依赖 `backend/tool/registry`、`backend/limit/*`、`backend/engine/*`、`application/*`、`gui/*`。本包不认识工具名、权限主体与会话。

## 并发、存储与错误语义

- **并发**：`Client` 非并发安全（一条连接 = 一个远端会话通道），调用方按需新建；`authPlan` 与 `capBuffer` 内部带锁，因为握手回调与输出拷贝跑在别的 goroutine 上。
- **存储**：本包只写 `known_hosts`（仅 `Trust`/`TrustFromProbe`，追加一行）；不写任何其他文件。
- **错误语义**：分三层——`CodeError{Code}`（入参类，机器可判定）、`DialError{Segment, TimedOut}`（连接类，分段归因）、`Result.Status`（执行类，三态）。远端文件面另有一组会话级码：`SSH_REMOTE_PATH_INVALID` / `SSH_REMOTE_FILE_IO` / `SSH_REMOTE_TOOL_MISSING` / `SSH_EDIT_OLD_STRING_NOT_FOUND`——它们**不是**传输码：见码查网络是白费功夫，病灶在文件或环境上。
- **不判成功**：`Result` 没有 `success` 字段。远端退出码非零是 `completed`（命令跑完了），channel 超时是 `channel_timeout` + `still_running`（本端不等待，不是"那边失败"）。业务成功与否由调用方回读判定。
- **远端并发**：远端写是"临时名 + `mv -f`"（不会看到写了一半的文件），但**没有**跨机器的 per-path 互斥——本地 `fs.FileSystem` 的 actor 是进程内锁，跨机器不成立。远端 `EditFile` 是读-替换-写两趟，读与写之间远端文件可能被别处改动。两件事都如实记在这里，不用本地语义假装成立。

## 扩展方式

- 新增认证方式：在 `auth.go` 的 `buildAuthPlan` 里加候选（带 `proto` 协议名，供分类使用），并同步 `AuthName*` 常量；
- 新增分类：在 `probe.go` 加 `Class*` 常量与 `classifyAuth`/`classifyDial` 分支，**必须**同时给 `Hint`（没有下一步动作的分类等于噪音）；
- 新增平台：只加 `auth_agent_<os>.go`，其余文件平台无关；
- 工具族接入：在 `backend/tool/registry` 注册 `ssh_*`，并把名字登记进 `backend/limit/policy/policy.go` 的路由组表（那是权限面的唯一事实源）。

## Review 指南

按这 6 条问一遍（都是踩过的）：

1. 有没有把 `channel_timeout` 说成失败？有没有把"暂时没输出"读成挂死？
2. 指纹判定里有没有出现文本匹配（`strings.Contains` / 正则）？
3. 有没有路径能自动追加 `known_hosts`？`Trust` 有没有弱化指纹相等校验？
4. 内联命令的非 ASCII 是拒收还是"尽力而为"？
5. 内容通道有没有做换行归一？（远端给的字节必须原样回来）
6. `Result` 里有没有悄悄长出 `success` 之类的业务判定？

## 测试与验证

```bash
go test ./backend/tool/ssh/ -count=1 -timeout=180s
go test ./backend/tool/ssh/ -count=1 -race -timeout=180s   # Windows 本机亦已跑通
gofmt -l backend/tool/ssh && go vet ./backend/tool/ssh/...
```

用例覆盖（进程内 sshd，无真机、无外网）：非 ASCII/多行内联拒收、命令来源互斥、退出码非零仍为 `completed`、channel 超时不是失败、脚本经 stdin 字节透明（CRLF + 中文）、输出有界与 `truncated`、未知主机 → 显式信任 → 哈希条目可识别、指纹不符不写盘、指纹不符分类、密码被拒分类、服务端不接受公钥分类、`auth_used` 报告 key/key+口令、banner 段超时归因、连不通优先于缺凭据、`Result` 无业务成功字段。

远端面（`files_test.go`，同一套进程内 sshd）：读时路径经 base64 过线（命令正文里没有路径原文、生成的脚本是单行 ASCII）、内容按字节回来（CRLF + 中文不变）、远端 IO 失败带文件码不是传输码、写时内容走 stdin（命令正文里没有内容）、替换是"全部出现处 + 计数"、`old_string` 不存在带码、作业句柄铸造/回读、外来句柄（`bt-`）被显式拒绝、起作业回读**远端展开后**的目录并拿到监管 pid、作业结束读输出与退出码、预算用尽给 `channel_timeout + still_running + handle + log_path` 且不报成失败。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### auth.go

- `func newAuthPlan() *authPlan` — newAuthPlan 构造计划并初始化认证段信号通道。
- `func (p *authPlan) authStarted() <-chan struct` — authStarted 返回"已进入认证段"的信号通道（关闭即代表握手完成）。
- `func (p *authPlan) markAuthStart()` — markAuthStart 只关一次通道：认证段可能被多次调用（每种方法一次）。
- `func buildAuthPlan(spec AuthSpec) (*authPlan, error)` — buildAuthPlan 按 spec 组装候选认证方式。
- `func (p *authPlan) methods() []xssh.AuthMethod` — methods 返回交给 ClientConfig.Auth 的方法列表（顺序即优先级）。
- `func (p *authPlan) observe(ctx *xssh.ClientAuthContext) (xssh.AuthMethod, error)` — observe 是 ClientConfig.AuthCallback 的实现：只观测，不改变选路。
- `func (p *authPlan) startedAuth() bool` — startedAuth 报告握手是否已经进入认证段（banner 段计时据此停止）。
- `func (p *authPlan) usedName() string` — usedName 推断**实际生效**的认证方式。
- `func (p *authPlan) offeredProtos() []string` — offeredProtos 返回我们提交过的协议层方法名（分类用）。
- `func (p *authPlan) serverAllowed() []string` — serverAllowed 返回服务端允许的方法列表快照（空 = 没走到认证段或观测不到）。
- `func (p *authPlan) triedMethods() []string` — triedMethods 返回已尝试且失败的方法名快照。
- `func (p *authPlan) serverAllows(proto string) bool` — serverAllows 报告服务端是否接受某协议层方法。
- `func containsString(list []string, want string) bool`
- `func openAgent(explicit string) (io.ReadWriteCloser, error)` — openAgent 打开 agent 通道：优先显式套接字，其次平台默认位置。

### auth_agent_other.go

- `func defaultAgentSocket() string` — defaultAgentSocket 返回 unix 上 agent 的默认套接字路径（来自 $SSH_AUTH_SOCK）。
- `func dialDefaultAgent() (io.ReadWriteCloser, error)` — dialDefaultAgent 打开平台默认的 agent 通道。
- `func dialAgent(name string) (io.ReadWriteCloser, error)` — dialAgent 打开指定路径的 agent 套接字。

### auth_agent_windows.go

- `func defaultAgentSocket() string` — defaultAgentSocket 返回 Windows OpenSSH agent 的默认管道名。
- `func dialDefaultAgent() (io.ReadWriteCloser, error)` — dialDefaultAgent 打开平台默认的 agent 通道。
- `func dialAgent(name string) (io.ReadWriteCloser, error)` — dialAgent 打开指定名字的 agent 通道（Windows：命名管道）。

### dial.go

- `func (e *DialError) Error() string`
- `func (e *DialError) Unwrap() error`
- `func Dial(ctx context.Context, spec Spec) (*Client, error)` — Dial 建立连接。四段超时各自独立生效。
- `func dial(ctx context.Context, spec Spec) (*Client, *dialTrace, error)` — dial 是 Dial 的实现，额外返回观测轨迹供 Probe 分类。

### exec.go

- `func (c *Client) Run(ctx context.Context, req Request) (Result, error)` — Run 执行一次命令并等待结束（或等到 channel 段超时）。
- `func (c *Client) runCommand(ctx context.Context, command string, stdin io.Reader, env map[string]string, maxOut int, note string) (Result, error)` — runCommand 是"一条命令在一条会话里跑完"的**唯一实现**：命令串 + 可选 stdin 内容。
- `func (c *Client) RunDetached(ctx context.Context, req DetachRequest) (Result, error)` — RunDetached 让命令在远端脱钩运行并立刻返回日志路径。
- `func (c *Client) resolve(req Request) (resolvedCommand, error)` — resolve 把三种命令来源归一成一种可执行形态，并执行"单行 ASCII"校验。
- `func (c *Client) scriptFor(req DetachRequest) (string, error)` — scriptFor 取出脚本文本（文件优先，其次 stdin 字面量），并做 UTF-8 校验。
- `func validateInline(cmd string) error` — validateInline 是"一次性参数"的白名单：单行 + 纯 ASCII。
- `func (c *Client) applyWait(res *Result, waitErr error)` — applyWait 把 Wait 的结果翻成 Result（exit code 与三态语义）。
- `func fillOutput(res *Result, stdout, stderr *capBuffer, note string)` — fillOutput 落统一输出字段：有界、编码有损可判、换行不做归一。
- `func newCapBuffer(limit int) *capBuffer`
- `func (b *capBuffer) Write(p []byte) (int, error)`
- `func (b *capBuffer) String() string`
- `func (b *capBuffer) Truncated() bool`
- `func shellQuote(s string) string` — shellQuote 把任意字符串安全地变成 POSIX shell 的单引号字面量。
- `func detachedHandle(hostID, pid string) string` — detachedHandle 生成脱钩作业句柄：稳定前缀（host id）+ 远端 pid + 时间戳。

### files.go

- `func (b *Backend) ReadFile(ctx context.Context, path string) (FileContent, error)` — ReadFile 读一个远端文件的全部字节（Content 的窗口切分由调用方做）。
- `func (b *Backend) WriteFile(ctx context.Context, path, content string) (int, error)` — WriteFile 覆盖写一个远端文件（内容走内容通道；父目录按需创建）。
- `func (b *Backend) EditFile(ctx context.Context, path, oldString, newString string) (int, error)` — EditFile 在远端文件里做"全部出现处替换"，返回替换次数（与本地 actor 的 Edit 同语义）。

### files_test.go

- `func newClient(t *testing.T, cfg serverConfig) *Client` — newClient 起一个假 sshd 并连上去（各用例共用，省掉重复的 known_hosts 样板）。
- `func newBackend(t *testing.T, cfg serverConfig) *Backend` — newBackend 同上但要 Backend（Backend 面每次调用自建连接）。
- `func assertWireSafe(t *testing.T, command string)` — assertWireSafe 断言命令正文是"单行 + 纯 ASCII"（否则它就不该走内联）。
- `func decodeFirstPayload(t *testing.T, command string) string` — decodeFirstPayload 取出脚本里第一个 base64 载荷并解码（判据：路径必须这样过线）。
- `func TestReadFileCarriesPathAsBase64(t *testing.T)`
- `func TestReadFileReportsRemoteIOFailure(t *testing.T)`
- `func TestWriteFileSendsContentOnStdin(t *testing.T)`
- `func TestEditFileReplacesAllAndCounts(t *testing.T)`
- `func TestEditFileReportsMissingOldString(t *testing.T)`
- `func TestJobHandleRoundTripAndForeignRejected(t *testing.T)`
- `func jobServerConfig(t *testing.T, exitLine, outPayload string) serverConfig` — jobServerConfig 是一个"作业面假远端"：按命令形状回答起/查/取。
- `func TestStartJobMintsSelfDescribingHandle(t *testing.T)`
- `func TestRunJobCompletedReadsOutputAndExitCode(t *testing.T)`
- `func TestRunJobStillRunningCarriesHandle(t *testing.T)`

### hostkey.go

- `func (v *hostKeyVerifier) callback(hostname string, remote net.Addr, key xssh.PublicKey) error` — callback 是交给 ClientConfig.HostKeyCallback 的函数。
- `func (v *hostKeyVerifier) observed() recordedKey` — observed 返回本次握手看到的主机公钥。
- `func knownHostsFiles(spec KnownHostsSpec) []string` — knownHostsFiles 解析生效的指纹库文件清单（只保留**存在**的）。
- `func newHostKeyVerifier(spec Spec) (*hostKeyVerifier, error)` — newHostKeyVerifier 构造指纹校验器。
- `func Trust(spec Spec, key xssh.PublicKey, wantFingerprint string) (path string, line string, err error)` — Trust 用**已确认的指纹**向指纹库追加一条记录。
- `func needsNewlinePrefix(path string) bool` — needsNewlinePrefix 报告目标文件末字节是否缺换行（缺了才补，不缺就不动）。
- `func TrustFromProbe(ctx context.Context, spec Spec, wantFingerprint string) (path, line, class string, err error)` — TrustFromProbe 用一次探测观测到的公钥去信任主机。
- `func classifyHostKeyError(err error) (class, hint string, ok bool)` — classifyHostKeyError 把指纹类错误翻成分类（非指纹类返回 false）。

### job.go

- `func (b *Backend) StartJob(ctx context.Context, command, workdir string) (JobStarted, error)` — StartJob 起一条远端受管作业并立刻返回受理回执（不等待命令结束）。
- `func (b *Backend) JobStatus(ctx context.Context, handle string) (JobState, error)` — JobStatus 读一次远端作业状态（running / exited / gone）。
- `func (b *Backend) JobOutput(ctx context.Context, handle string, maxBytes int) (string, string, bool, error)` — JobOutput 取回作业当前的 stdout / stderr（各自有上限；超限标 truncated）。
- `func (b *Backend) JobKill(ctx context.Context, handle string) (string, error)` — JobKill 终止远端作业，返回"发了什么"的回执（**不**宣称进程已死，见 jobKill）。
- `func (b *Backend) RunJob(ctx context.Context, command, workdir string, budget time.Duration) (Result, error)` — RunJob 起一条作业并等到结束或预算用尽：两种结局在 Result 里如实区分。
- `func sleepCtx(ctx context.Context, d time.Duration)` — sleepCtx 是可取消的等待（轮询间隔）。
- `func (c *Client) startJob(ctx context.Context, command, workdir string) (JobStarted, error)` — startJob 起一条受管作业（命令正文经内容通道写入作业目录）。
- `func (c *Client) jobStatus(ctx context.Context, handle string) (JobState, error)` — jobStatus 读一次状态读数。
- `func (c *Client) jobOutput(ctx context.Context, handle string, maxBytes int) (string, string, bool, error)` — jobOutput 取回 out / err 的当前内容（未产出 = 空，不是错误）。
- `func (c *Client) readJobLog(ctx context.Context, dir, name string, limit int) (string, bool, error)` — readJobLog 读作业目录里的一个日志文件。
- `func (c *Client) jobKill(ctx context.Context, handle string) (string, error)` — jobKill 终止远端作业（整棵进程树：监管进程是 setsid 的组长，负 pid 即整组）。
- `func supervisorScript(workdir string) string` — supervisorScript 生成监管脚本（base64 承载，因此可以是多行）。
- `func parseStartDir(stdout string) string` — parseStartDir 从起作业的回执里取出展开后的目录。
- `func mintJobHandle(dir string) string` — mintJobHandle 把远端目录铸成自描述句柄（`ssh:<base64url(dir)>`）。
- `func IsRemoteJobHandle(handle string) bool` — IsRemoteJobHandle 报告句柄是不是远端作业句柄（`ssh:` 族）。
- `func ParseJobHandle(handle string) (string, error)` — ParseJobHandle 解出句柄里的远端目录；不是本族的句柄给一句可照做的错误。
- `func parseJobState(stdout string) (JobState, error)` — parseJobState 解析状态读数（`pid=<n>` + `running\|exited <code>\|gone`）。

### probe.go

- `func Probe(ctx context.Context, spec Spec) ProbeResult` — Probe 只连不写：建立连接、报告分类，然后立刻关掉。它不写 known_hosts、
- `func classifyDial(trace *dialTrace, err error) (class, hint string)` — classifyDial 把连接期失败翻成分类 + 可行动提示。
- `func classifyAuth(trace *dialTrace, err error) (class, hint string)` — classifyAuth 是 L8 那条诊断链的固化形态。
- `func timedOut(trace *dialTrace, err error) bool` — timedOut 报告这次失败是否属于"某一段超时"。

### probe_cache.go

- `func NewProber(ttl time.Duration, probe func(ctx context.Context, spec Spec) ProbeResult) *Prober` — NewProber 造一个探针缓存（probe 为 nil 时用本包的 Probe；ttl<=0 取默认）。
- `func ProbeKey(spec Spec) string` — ProbeKey 是缓存键：端点 + 用户（同一台机器换个用户是另一次认证）。
- `func (p *Prober) Cached(spec Spec) (ProbeResult, bool)` — Cached 只读缓存（ok=false = 未探或已过期）。它**不发网络请求**。
- `func (p *Prober) ProbeNow(ctx context.Context, spec Spec) ProbeResult` — ProbeNow 当场探一次并写缓存（显式指名者付的那一次）。
- `func (p *Prober) Invalidate(spec Spec)` — Invalidate 丢掉一条结论（端点/凭据变更后由装配层调用）。

### remote.go

- `func encodePayload(value string) string` — encodePayload 把任意字符串折成 base64（字节透明：中文、CRLF、二进制都一样）。
- `func decodeVar(variable, value string) string` — decodeVar 生成"把 base64 载荷解码进 shell 变量"的表达式（路径与临时目录都走它）。
- `func stdinOrNil(data string) io.Reader` — stdinOrNil 是 runCommand 的入参适配：空数据 = 不给 stdin（不是给一个空管道）。
- `func unsupportedRemoteTool(stderr string) bool` — unsupportedRemoteTool 报告本端脚本要用的工具在远端不存在（stderr 里是 sh 的原文）。
- `func remoteScriptError(op string, res Result) error` — remoteScriptError 把辅助脚本的非零退出翻成带固定码的错误。
- `func decodeRemoteBase64(encoded string, truncated bool) ([]byte, bool)` — decodeRemoteBase64 解一份远端回来的 base64 流。
- `func (b *Backend) runInline(ctx context.Context, script, data string) (Result, error)` — runInline 跑一段**本包生成的**脚本（单行 ASCII），可选把 data 作为它的 stdin。
- `func scriptDir(hostID, kind string, now time.Time) string` — scriptDir 造一个远端作业/临时目录（ASCII、可预测、带时间戳与主机 ID）。
- `func sanitizeHostID(hostID string) string` — sanitizeHostID 把主机 ID 收成"可以出现在路径里"的形态（空 = "ssh"）。

### result.go

- `func (r Result) Ran() bool` — Ran 报告命令是否真的在远端跑过（channel_timeout 也算跑过）。
- `func (r Result) Failed() bool` — Failed 报告本次是否属于"根本没跑起来"。

### ssh.go

- `func (e *CodeError) Error() string`
- `func (e *CodeError) Unwrap() error`
- `func codeErr(code, format string, args ...any) error`
- `func DefaultTimeouts() Timeouts` — DefaultTimeouts 是出厂默认值（可按 host 覆盖）。
- `func (t Timeouts) withDefaults() Timeouts` — withDefaults 补齐零值，避免"零 = 无限等"。
- `func (s Spec) Addr() string` — Addr 返回 host:port 形态（连接与指纹核对都用它）。
- `func (s Spec) maxOutput() int` — maxOutput 返回生效的输出上限。
- `func (c *Client) Close() error` — Close 关闭连接。可重复调用。
- `func (c *Client) Spec() Spec` — Spec 返回本连接使用的规格（探针与审计面用）。
- `func (c *Client) AuthUsed() string` — AuthUsed 返回**实际生效**的认证方式（agent / publickey / publickey+passphrase /
- `func (c *Client) HostKeyFingerprint() string` — HostKeyFingerprint 返回本次握手观测到的主机公钥指纹（SHA256，OpenSSH 形态）。
- `func (c *Client) HostKey() xssh.PublicKey` — HostKey 返回本次握手观测到的公钥（Trust 用；nil = 没连上）。

### ssh_test.go

- `func startServer(t *testing.T, cfg serverConfig) *testServer` — startServer 起一个进程内 sshd，t.Cleanup 自动关停。
- `func serveConn(conn net.Conn, srvCfg *xssh.ServerConfig, cfg serverConfig)` — serveConn 跑一条连接的握手与会话循环。
- `func serveSession(ch xssh.Channel, chReqs <-chan *xssh.Request, cfg serverConfig)` — serveSession 处理一次 exec：调用测试处理器，然后回 exit-status。
- `func sendExitStatus(ch xssh.Channel, code uint32)` — sendExitStatus 供测试处理器主动给出非零退出码。
- `func knownHostsLine(addr string, key xssh.PublicKey, hash bool) string` — knownHostsLine 造一条 known_hosts 条目；hash=true 时用哈希主机名形态。
- `func writeKnownHosts(t *testing.T, lines ...string) string` — writeKnownHosts 写一个临时 known_hosts，返回路径。
- `func specFor(t *testing.T, srv *testServer, khPath string, auth AuthSpec) Spec` — specFor 是连接假 sshd 的 Spec 工厂。
- `func writeKeyFile(t *testing.T, passphrase string) (string, xssh.PublicKey)` — writeKeyFile 生成一把 ed25519 私钥并落盘（可带口令），返回路径与公钥。
- `func TestValidateInline(t *testing.T)`
- `func TestResolveRejectsNonASCIIInline(t *testing.T)` — TestResolveRejectsNonASCIIInline 证明拒收发生在**拨号之前**（Client 零值即可判定）。
- `func TestResolveCommandSourcesMutuallyExclusive(t *testing.T)` — TestResolveCommandSourcesMutuallyExclusive 证明三种来源互斥且必填其一。
- `func TestRunCompletedAndExitCode(t *testing.T)`
- `func TestRunChannelTimeoutStillRunning(t *testing.T)`
- `func TestScriptChannelIsByteTransparent(t *testing.T)`
- `func TestRunOutputBounded(t *testing.T)`
- `func TestProbeHostUnknownThenHashedTrust(t *testing.T)`
- `func TestProbeKeyMismatch(t *testing.T)`
- `func TestProbeAuthRejectedPassword(t *testing.T)`
- `func TestProbeNoAuthMethod(t *testing.T)`
- `func TestAuthUsedReportsKey(t *testing.T)`
- `func TestAuthWithPassphrase(t *testing.T)`
- `func TestProbeUnreachableWinsOverMissingCredential(t *testing.T)` — TestProbeUnreachableWinsOverMissingCredential 是分段归因顺序的回归：
- `func TestProbeNoCredential(t *testing.T)` — TestProbeNoCredential 证明"我们没提交凭据"与"凭据被拒"是两回事。
- `func TestProbeBannerTimeout(t *testing.T)`
- `func TestResultEnvelopeHasNoSuccessField(t *testing.T)`

### transport.go

- `func NewBackend(spec Spec) *Backend` — NewBackend 造一个 ssh 执行面。
- `func (b *Backend) Run(ctx context.Context, command, workdir string) (Result, error)` — Run 执行一条命令：新连一次、跑完关掉。连接期的失败带分段归因（DialError），
- `func (b *Backend) Probe(ctx context.Context) ProbeResult` — Probe 探一次（只连不写）：选择器要的就是这一个结论。
- `func commandRequest(command, workdir string) (Request, error)` — commandRequest 把"脚本文本"归一成一次执行请求（命令来源纪律的唯一实现）。
- `func isInlineSafe(command string) bool` — isInlineSafe 报告命令能否安全地走内联（单行 + 全 ASCII + 不含 NUL）。

### transport_test.go

- `func TestCommandRequestRoutesMultilineAndNonASCIIToStdin(t *testing.T)`
- `func TestCommandRequestRejectsEmptyCommand(t *testing.T)`
- `func TestIsInlineSafe(t *testing.T)`
- `func TestProberThreeStates(t *testing.T)` — TestProberThreeStates 未探 / 有结论 / 过期 三态与"键按端点+用户分"。
- `func TestProberConcurrentSessions(t *testing.T)` — TestProberConcurrentSessions 并发读同一缓存不串结论（-race 下同时验证锁）。
