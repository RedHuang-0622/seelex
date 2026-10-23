# tools / ssh

## 生态位

Seelex 内置 SSH 工具的**连接基座**：库内直连（`golang.org/x/crypto/ssh`，不 shell out 到 `ssh.exe`）、四段独立超时、`known_hosts` 严格校验、可分类的失败探针、三种返回形态的命令执行。

主要调用方：

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
| `result.go` | `Status` / `Segment` / `Result` 返回信封 |
| `sshprobe/main.go` | 独立驱动：`main` → `run` → `parseFlags` / `buildSpec` / `doProbe` / `doTrust` / `doExec` |
| `ssh_test.go` | 进程内 sshd 夹具 + 全部验收用例 |

## 核心实现

**四段超时**（`dial.go`）。`ClientConfig.Timeout` 只覆盖 TCP 建连，握手/认证无条件上限，`ssh.Session` 也收不到 `ctx`。因此本包自己拨号（拿到 `net.Conn` 与取消权），再在 goroutine 里跑 `ssh.NewClientConn`，用两个计时器分别卡 banner 段与 auth 段；banner 段结束的信号是**第一次认证回调**（`ClientConfig.AuthCallback`）。

**认证**（`auth.go`）。优先级固定为 agent → key(+口令) → password。`AuthCallback` 只做观测（服务端允许哪些方法、已经试过哪些），选路交给库按 `Auth` 顺序走。`auth_used` 由"第一个没出现在 `TriedMethods` 里的候选"推断——单步认证准确；多步认证（partial success）只是近似，已在注释里标注。

**指纹**（`hostkey.go`）。判定一律经由 `knownhosts.New(...)`，因此 `|1|salt|hash` 哈希条目天然被支持——**禁止**用文本匹配判存在性。`Trust` 要求人确认的指纹与本次握手逐字相等，否则硬失败且不写盘；只追加一行，不做换行归一。

**执行**（`exec.go`）。命令只允许三种来源：`CommandFile`（首选，经 `bash -s` 走 stdin）、`Stdin`、内联 `Inline`（**仅单行 ASCII**，含非 ASCII 直接拒）。脱钩形态固定为 `setsid <shell> <script> </dev/null >log 2>&1 &`，脚本用 base64 传输以绕开三层引号与代码页。

**探针**（`probe.go`）。分类不看裸错误串：`knownhosts.KeyError` 的 `Want` 空/非空区分"首次连接"与"指纹不符"；`ClientAuthContext.AllowedMethods` + `TriedMethods` 区分"公钥被接受但被拒"（口令/agent 问题）与"公钥不被接受"（未登记/格式）。

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
- **错误语义**：分三层——`CodeError{Code}`（入参类，机器可判定）、`DialError{Segment, TimedOut}`（连接类，分段归因）、`Result.Status`（执行类，三态）。
- **不判成功**：`Result` 没有 `success` 字段。远端退出码非零是 `completed`（命令跑完了），channel 超时是 `channel_timeout` + `still_running`（本端不等待，不是"那边失败"）。业务成功与否由调用方回读判定。

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
