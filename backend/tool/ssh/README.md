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
