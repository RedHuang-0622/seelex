# 内置 SSH 工具（`ssh_*`）：开发文档

> 状态：**设计稿 + 已落地的连接基座**。连接/认证/指纹/执行/探针已实现为 `backend/tool/ssh`（含独立驱动 `backend/tool/ssh/sshprobe`，17 组进程内 sshd 用例通过，`-race` 已在本机跑通）；`ssh_*` 工具族注册、权限路由组、项目内 host 注册表、`limits.ssh` 装配门仍是**设计**。进度见 §14。
> 日期：2026-10-23（连接基座落地同日）
> 上游输入：其他工作任务的真实 SSH 执行反馈（一张诊断链、一条走通的传输路径、七个坑、一套部署节奏）。本文 §1 把它们逐条转成工具层面的硬规矩。
> 证据分级：**Confirmed** = 本轮自读代码或依赖源码（带 `文件:行` / 依赖注释）；**Hypothesis** = 推断，已标出判据。

**一句话**：给 Seelex 一族常驻 `ssh_*` 工具——**库内直连**（`golang.org/x/crypto/ssh`，不 shell out 到 `ssh.exe`）、**四段独立超时**、**`known_hosts` 严格校验 + 显式首次信任**、**凭据进 OS 凭据库**、**项目内 host 注册表（稳定 `id` + 可变 `name`）**、**命令走文件不走一次性参数**、**远端写操作不替人判成功**。

---

## 1. 上游教训 → 工具硬规矩

这一节是本文的**需求来源**，不是背景介绍：每一行右列都是实现里必须能指到的一处判定。

| # | 现象（实测） | 根因 | 工具必须做到的规矩 |
|---|---|---|---|
| L1 | PowerShell → `python -c` → paramiko → bash 四层，`for p in ...` 报 ParserError | 引号被逐层吃掉 | **命令一律走文件/stdin**；`command`（一次性字符串）只允许单行 ASCII 简单命令 |
| L2 | 断言 `'未在后台注册' in mine` 失败，而打印出来明明有这几个字 | 一次性参数经多层代码页转换，脚本里的中文字面量本身被损坏 | **工具层直接拒收内含非 ASCII 的一次性命令**（`SSH_NON_ASCII_INLINE_COMMAND`），强制改走 `command_file` / `stdin`。这条不是防御性洁癖：它是一次半夜排查换来的 |
| L3 | `nohup cmd &` 仍然 `PipeTimeout` | 后台进程占住通道，`nohup` 不脱钩 | detached 一律 `setsid … </dev/null >log 2>&1 &`，**回传 `log_path`** 供增量读取 |
| L4 | 远端 python 无 `-u` → 日志 0 字节 → "挂死"误判，并据此错误改代码 | 输出缓冲 ≠ 进程死亡 | 工具**永不**把"暂时没输出"报成失败或死亡；返回形态里它是 `detached(still_running=true)`（§4） |
| L5 | 240s 模型调用拖超 channel，但远端仍在跑 | 通道超时 ≠ 远端失败 | **三种返回形态**必须可区分；超时后仍然给出 `still_running` 与取回手段（§4、§5） |
| L6 | `Select-String` 搜不到 IP，判定"主机不在 known_hosts"，而连接其实是通的 | `known_hosts` 条目可以是 `|1|salt|hash` 哈希形态 | **禁止文本 grep 判存在性**：存在性由握手期的 `HostKeyCallback` 判定（§6） |
| L7 | 追加内容时对整段做 `\n→\r\n`，已有 CRLF 被二次转换 → `\r\r\n`；清洗后又把每处变成两个换行，diff 上千行，测试仍全绿 | `core.autocrlf=true` 且无 `.gitattributes`；无脑换行归一 | 传输与落盘**一律不做换行归一**；内容走 stdin / sftp 二进制，不经 shell 层（§9） |
| L8 | `Server accepts key: id_rsa` 紧接着 `Permission denied` | 私钥有口令 + 无人输入；**不是 key 没登记** | 探针必须把"认证被拒"与"主机未知/指纹不符"分成不同结论，并各自给出修法（§6.2） |
| L9 | 部署脚本的健康检查对 HTTP 404 也报"通过" | 判据错了：`exit 0` ≠ 业务成功 | 工具**只报告传输与退出码事实，不判定业务成功**；"改完必须回读"写进纪律（§4.2、§9.4） |

L1/L2/L3/L5/L7 是**跨平台通用**的（Windows 宿主 + Linux 远端是最常见组合）；L8/L9 是**诊断口径**，属工具的产品价值本身。

---

## 2. Go 侧库支持：有什么、缺什么

### 2.1 结论表

| 能力 | 库 | 现状（Confirmed） | 结论 |
|---|---|---|---|
| SSH 传输（KEX/加密/认证/通道） | `golang.org/x/crypto/ssh` | `go.mod:golang.org/x/crypto v0.57.0`（**indirect**）；`vendor/golang.org/x/crypto/ssh/` 已存在（含 `client.go`/`session.go`/`server.go`/`tcpip.go`/`mlkem.go`） | **不需要新模块**，只需把它提为直接依赖 |
| 主机指纹校验 / 哈希条目 | `.../ssh/knownhosts` | 同模块、module cache 里有（`knownhosts.go`），但 **vendor 里没有**（`vendor/modules.txt` 只列 `ssh` 与 `ssh/internal/bcrypt_pbkdf`） | import 后 `go mod vendor` 即得，**不加版本** |
| ssh-agent 认证 | `.../ssh/agent` | 同上，**vendor 缺**；`agent.NewClient(rw io.ReadWriter)` 与传输无关 | import + vendor；Windows 需自己接命名管道 `\\.\pipe\openssh-ssh-agent`（Hypothesis：`os.Open` 在部分场景不足，可能需要 `go-winio`，M2 实测裁决） |
| 有口令私钥 | `ssh.ParsePrivateKeyWithPassphrase` | `vendor/golang.org/x/crypto/ssh/keys.go` 已导出；OpenSSH 新格式的 `bcrypt_pbkdf` 也已 vendor | 直接可用 |
| 客户端认证方式 | `ssh.Password` / `PublicKeys` / `PublicKeysCallback` / `KeyboardInteractive` / `RetryableAuthMethod` | `vendor/.../client_auth.go` 全部导出 | 四种方式全支持 |
| 测试用服务端 | `ssh.NewServerConn` + `ServerConfig` | `vendor/.../server.go` 已导出 | **测试可在进程内起 sshd**，不依赖真机（§13） |
| 端口转发 / 隧道 | `(*ssh.Client).Dial`（`tcpip.go`） | 已 vendor | 能力在手，本期不暴露（§12） |
| `~/.ssh/config` 解析 | — | `x/crypto` **不提供** | **本设计不需要**：host 事实来自项目内注册表，不读 `ssh_config`（这正好绕开一个依赖） |
| SFTP 文件传输 | `github.com/pkg/sftp` | **不在依赖里** | 决策点：不引（用 `stdin`/`base64` 传内容）或引（能力更全）。见 §12 待决 D-1 |

### 2.2 库不提供、必须自己搭的三件事（这是本工具的主要工作量）

1. **四段超时**。`ClientConfig.Timeout` 的注释写明它只是"TCP 连接建立"的上限（`vendor/golang.org/x/crypto/ssh/client.go`）；`ssh.Dial` 没有 `ctx` 版本；`ssh.Session` 的方法也都收不到 `ctx`。→ 只能自己组装（§5）。
2. **远端作业化**。库给的是通道，不是作业：`detached` / `channel_timeout` / 增量读日志这三件都得自己实现（§4、§9.2）。
3. **凭据存储**。Seelex 侧对 `keyring` / `wincred` / `CryptProtectData` / `Credential Manager` 零命中（Confirmed），这块是净新增实现；好消息是 Windows DPAPI 的底层绑定 `golang.org/x/sys/windows` 里已经有（§7.1）。

---

## 3. 工具面（名字即契约）

| 工具名 | 路由组 | 动作 | 读/写共享状态 | 说明 |
|---|---|---|---|---|
| `ssh_hosts` | `ro` | `action: list \| show \| refs` | 只读 | 读项目 host 注册表；`refs` 在**改名前**回答"谁引用了我"（§8.3） |
| `ssh_probe` | `ro` | 逐段探针（`connect/auth/banner/fingerprint`） | 只读 | 只连不写：返回**分类结论**而不是裸错误串（§6.2） |
| `ssh_exec` | `rw` | `mode: run \| detached` | 写（远端） | 唯一执行入口；命令走 `command_file` / `stdin`（§9） |
| `ssh_host_edit` | `rw` | `action: create \| update \| rename \| remove` | 写（项目内文件） | 改注册表；`rename` 只改 `name`，`id` 不可变（§8.2） |
| `ssh_trust` | `rw`（默认 `ask`） | 写入/轮换 `known_hosts` 条目 | 写（宿主用户级文件） | 首次信任与指纹轮换的唯一入口（§6.3） |
| `ssh_export_config` | `adm`（默认 `ask`） | 导出到 `~/.ssh/config` | 改宿主用户级配置 | **只在显式要求时**执行，先备份（§8.4） |

**为什么只有 6 个名字**：`bash_read` 已因"只读名牌 + 服务端语法判定"这条分裂面被删（479 次调用里 34.4% 折在判定上，见 `backend/tool/registry/router.go` 的注释）。同样的错不重犯——**不造 `ssh_exec_read`**；只读语义由 `ssh_hosts` / `ssh_probe` 两个**本来就不写**的名字承担，`ssh_exec` 的免打断由权限规则的 `patterns`（匹配命令本体）承担，与 `bash` 同一套口径。

**项目作用域名单坑（Confirmed）**：若 `ssh_exec` 的路径类参数要走项目根解析，它的名字必须同时进 `backend/tool/registry/scoped_names.go` 的 `projectScopedToolNames`——否则装配层会认为它"还没被项目作用域占住"，晚注册的同名工具把它顶掉。这份名单是**唯一一份**，别另抄。

---

## 4. 返回信封

### 4.1 三种形态（L4 / L5 的落点）

```json
{
  "status": "completed | detached | channel_timeout | failed",
  "exit_code": 0,
  "stdout": "…",
  "stderr": "…",
  "truncated": false,
  "encoding_lossy": false,
  "auth_used": "publickey | publickey+passphrase | agent | password | keyboard-interactive",
  "timed_out_segment": "connect | banner | auth | channel | command | null",
  "still_running": false,
  "handle": "…",
  "log_path": "/tmp/seelex-<id>.log",
  "duration_ms": 1234
}
```

| `status` | 何时 | 调用方该做什么 |
|---|---|---|
| `completed` | 远端命令自行结束，通道收到退出状态 | 看 `exit_code`；**不要**把 0 当业务成功（L9） |
| `detached` | `mode=detached`：已 `setsid` 脱钩并返回句柄 | 用 `bg_manage(op=fetch)` 增量取回；用 `bg_manage(op=kill)` 终止 |
| `channel_timeout` | 本端不再等待，**远端可能仍在跑** | `still_running=true` + `handle`；**禁止**据此判定"那边失败" |
| `failed` | 传输/认证/指纹/参数类失败，命令根本没跑起来 | 按 `timed_out_segment` / §6.2 分类修 |

### 4.2 `exit_code` 与"成功"分家（L9）

工具只报告事实：命令跑完没有、退出码是多少、输出是什么。**它不产生 `success: true` 这种字段**——那样等于把"业务判据"从人手里拿走。上游那次"健康检查 404 也报通过"就是因为判据错位；工具的正确姿态是让回读便宜，而不是替人下结论（§9.4）。

---

## 5. 四段独立超时

**四段** = `connect` / `banner` / `auth` / `channel`（上游明确要求的切法）。表里第 5 行 `command` 不是传输段，而是"远端命令自己的时间预算"，单列出来是为了**别把两种超时记成同一笔账**。

| 段 | 覆盖什么 | 库是否提供 | 实现落点 | 超时后语义 |
|---|---|---|---|---|
| `connect` | TCP 建连 | 是（`ClientConfig.Timeout`，**仅此一段**） | `net.Dialer{Timeout}` + `DialContext`（自带 ctx 取消） | `failed`，`timed_out_segment=connect` |
| `banner` | 版本交换 + 算法协商 + banner | 否 | `ssh.NewClientConn` 放进 goroutine，配 `time.After`；到点 `conn.Close()` | `failed`，`=banner`（常见于"主机在但 22 端口不是 SSH"） |
| `auth` | 认证轮次（含 agent 交互、私钥口令） | 否 | 同上，与 banner 分开计时（同一个 goroutine，两个 timer 各记一份） | `failed`，`=auth`；分类见 §6.2 |
| `channel` | 单条命令的通道存活 | 否 | `ssh.Session` + `context`；同时给底层 `net.Conn` 设 `SetDeadline` 兜底 | **`channel_timeout`**（≠ 失败），`still_running=true` |
| `command` | 远端命令自身的时间预算（可选，远小于 channel 时由远端 `timeout N` 兜底） | 否 | 包 `timeout N` 注入（可关） | 与 channel 分开记账，避免把"远端自己超时"记成本端超时 |

要点：**`NewClientConn(c net.Conn, addr string, config)` 是这套设计的缝**——自己拨号（拿到 `net.Conn` 与取消权），库只管握手。不要用 `ssh.Dial`，它把四段压成一段。

默认值建议：`connect=10s` / `banner=10s` / `auth=20s` / `channel=120s`，全部可按 host 覆盖（`limits.ssh.defaults` + host 级字段）。

---

## 6. 主机指纹与首次信任

### 6.1 判据只有一个来源

`knownhosts.New(files...)` 返回的 `HostKeyCallback` 直接交给 `ClientConfig.HostKeyCallback`。它对**哈希条目**（`|1|salt|hash`）按 `hmac-sha1` 匹配（`knownhosts.go` 的 `newHashedHost` / `hashedHost.match`），所以 L6 那个"grep 文本判存在"的做法在这里**结构上不可能发生**。

### 6.2 探针分类表（L8 的落点）

`ssh_probe` 拿到的错误必须分类，而不是把 `ssh: handshake failed: …` 原样丢给模型：

| 观测 | 类型判据 | 结论 | 修法 |
|---|---|---|---|
| 密钥被接受但登录失败 | `"ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"`（`client_auth.go:154`），且 `tried` 里**有** `publickey` | **认证失败**：私钥有口令无人输入 / agent 没跑 / key 被服务端拒 | 提示：解锁 agent（`ssh-add`）或补口令；**不是**"key 没登记" |
| 服务端不接受我们的公钥 | 同上错误，但 `tried` 里只有 `none`（公钥根本没被提交） | **公钥未被接受**：文件缺失/权限/格式/未登记 | 检查 `auth_ref` 指向的私钥是否可读可解析 |
| 主机未知 | `*knownhosts.KeyError` 且 `len(Want)==0` | **首次连接** | 走 `ssh_trust`（§6.3） |
| 指纹变了 | `*knownhosts.KeyError` 且 `len(Want)>0` | **指纹不符**（换机 or 中间人） | 硬失败，必须人确认后轮换 |
| 条目已吊销 | `*knownhosts.RevokedError` | 硬失败 | 不提供"绕过"路径 |
| 算法不交 | `"ssh: no common algorithm for …"`（`common.go:422`） | 协商失败 | 报告双方算法集，不建议放宽 |
| 端口上不是 SSH | `banner` 段超时或版本串非 `SSH-2.0-*` | 目标错 | 提示检查 host/port |

这张表就是上游那条诊断链的固化形态：**"`Server accepts key` 后仍 denied"** 与 **"公钥不被接受"** 在库层面是两种错误，工具必须把它们翻成人话。

### 6.3 首次信任流程（禁止 TOFU 自动写入）

```
ssh_probe → status=failed, class=host_unknown, 带 sha256 指纹（ssh.FingerprintSHA256）
        ↓（人确认：这是谁的机器、指纹对不对）
ssh_trust { id, fingerprint }  → 校验指纹与本次握手一致 → knownhosts.Line(...) 追加一行
```

红线（写进代码注释，不只是文档）：

- **禁止 `ssh.InsecureIgnoreHostKey()`**——库自己的注释就写着 "should not be used in production code"。
- **禁止"连接失败就自动追加"**（这正是 `AutoAddPolicy` 的形态）。写入必须由 `ssh_trust` 这个**显式**动作完成，且要求指纹参数与本次握手结果**逐字相等**（防止"确认的是 A 机器的指纹，写进去的是 B 机器的"）。
- 写入目标默认 `~/.ssh/known_hosts`，可按 host 覆盖 `known_hosts_file`（项目内自有文件也是合法选项，路径同样受项目根约束）。
- 是否对新增条目做 `HashHostname`：可配，**默认跟随目标文件的既有形态**（文件里已有哈希条目就哈希写），不制造混合风格。

---

## 7. 凭据

### 7.1 现状缺口（Confirmed）

Seelex 自己的代码里 `keyring` / `wincred` / `CryptProtectData` / `Credential Manager` **零命中**（`backend/` `application/` `wiring/` `internal/` `contract/` `store/` 全扫，唯一命中是本文），`go.mod` 里也没有凭据库。当前"凭据"的唯一现实做法是上游那条**手工临时文件 + 用完即删**（`%TEMP%` 明文 + 人工生命周期），本工具的职责就是把它消灭掉。

注意"零命中"的准确含义：**缺的是 Seelex 侧的实现，不是底层绑定**——`vendor/golang.org/x/sys/windows` 已经提供了 `CryptProtectData` / `CryptUnprotectData` / `DataBlob`（`zsyscall_windows.go`、`types_windows.go`），Windows DPAPI 这一格**不需要任何新依赖**。

### 7.2 方案

| 平台 | 落点 | 依赖 | 状态 |
|---|---|---|---|
| Windows | DPAPI（`CryptProtectData`/`CryptUnprotectData`），密文文件 `%LOCALAPPDATA%\seelex\ssh\<id>.dpapi` | `golang.org/x/sys/windows`（**已 vendor 且已生成绑定**：`zsyscall_windows.go`） | 本期，零新依赖 |
| Unix | `0600` 密文文件于数据根 `secrets/` 下 + 进程级密钥（无内核钥匙串时） | 无新增 | 本期（安全等级低于 DPAPI，文档标注） |
| 全平台 | OS 钥匙串（Keychain / Secret Service / Credential Manager） | 需新模块 | **规划**（D-2） |

### 7.3 红线与生命周期

- **永不**进 argv、进项目目录、进工具返回值、进日志、进 `git` 可达路径。
- 工具返回里只能出现**引用名**（`auth_ref: "cred:prod-deploy"`）与 `auth_used` 的分类结果，密码明文一次都不出现。
- 生命周期由工具负责：`create` → 可轮换（`update` 覆写）→ 可失效（`remove` 物理删除密文）→ 引用计数（`ssh_hosts action=show` 显示"该凭据被哪几个 host 引用"，删 host 时提示）。
- 认证方式优先级：`agent` → `key(+passphrase)` → `password` → `keyboard-interactive`，实际用了哪一种**必须回报**（`auth_used`），因为"以为在用 key、其实回落到了密码"是排障中最贵的信息。

---

## 8. 项目空间内的 host 注册表

### 8.1 位置与理由

```
<项目根>/.seelex/ssh/hosts.yaml      # 主机注册表（事实）
<项目根>/.seelex/ssh/known_hosts     # 可选：项目自有指纹库
```

- 与既有落点同源：看板在 `<项目根>/.seelex/board/<会话>/`（`docs/arch/seelex-project-board.md`），MCP trace 在 `.seelex/mcp-traces`。**项目根的权威读法是 `Service.ProjectRootFor(sessionID)`**（`application/core/project_board_read.go`）——它来自工作区绑定事实；**不得**回退到"视图当前工作区"，否则会把 A 项目的注册表写进 B 项目。
- `.seelex/` 已在 `.gitignore` 第 14 行整目录忽略 → 注册表天然不入库，满足"凭据引用与生产地址不进版本库"。
- **不直接改 `~/.ssh/config`**：那是宿主的用户级事实，多项目共享。注册表是 Seelex 的事实源，`~/.ssh/config` 只是**导出目标**（§8.4）。

### 8.2 Schema（`id` 与 `name` 分名分责）

```yaml
version: 1
hosts:
  - id: prod-cn                 # 不可变，被脚本/文档/部署目标引用
    name: 生产环境-上海          # 可变，纯展示
    hostname: host.internal        # 纯主机（user 单列，不塞进 hostname）
    port: 22
    user: deploy
    auth_ref: "cred:prod-deploy"   # 凭据引用（§7），绝不内联密码/私钥内容
    known_hosts: strict            # strict | tofu
    known_hosts_file: ""           # 空 = ~/.ssh/known_hosts
    workdir: /srv/app
    deploy_command: ./scripts/auto_deploy.sh
    log_dir: /tmp/seelex-logs
    env_snapshot_command: "uname -a; git rev-parse --short HEAD"
    timeouts: {connect: 10s, banner: 10s, auth: 20s, channel: 120s}
    capability: read               # read | write | deploy（§10 能力分级）
    tags: [prod, cn]
```

**分名分责不是风格问题**：AGENTS.md §6 的 review 项已经钉住这条——"project/session 名称是否仅用于展示，ID 是否仍是 binding、存储和操作的唯一键"。上游那台机器 `Host fashmaker` 正是 `id` 与 `name` 合一的形态，**一旦改名，所有引用同时断掉**。本设计把这个坑变成结构约束：`rename` 只改 `name`，`id` 永不可变。

### 8.3 改名与改址的引用检查

`ssh_host_edit action=rename` 的执行顺序（`update` 改 `hostname`/`port` 时同理）：

1. `ssh_hosts action=refs {id}`：复用既有 `grep_search`/`glob` 在同一项目根内扫 `id` 的出现位置（脚本、文档、CI 配置、`deploy_command` 里写死的 `ssh prod-cn`）。
2. **不自动改**引用：返回 `{ path, line, snippet }` 命中清单 + 一句"这些地方需要跟着改"。
3. 改完 `name`，`id` 与所有引用**逐字不变**——这就是"改名称永远不会打断引用"的机械保证。
4. 有命中的 `id` **不允许** `remove`：要么先清引用，要么显式 `force`（并在返回里列出将被孤立的引用）。

### 8.4 导出到 `~/.ssh/config`（显式、可回滚）

`ssh_export_config` 的纪律：

- 只在用户显式要求时执行；**从不**由 `ssh_host_edit` 顺带触发。
- 先备份：`~/.ssh/config.bak.<UTC时间戳>`，备份路径写进返回值。
- 只写带 `export: true` 的 host（默认 false），且写**受管区块**（`# >>> seelex:begin` … `# <<< seelex:end`），区块外的内容逐字节不动。
- 区块内只有 `Host <id>` / `HostName` / `Port` / `User` / `IdentityFile`（指向凭据落点或 `auth_ref` 对应的 key 路径）/ `IdentitiesOnly yes`。**永不写密码**。
- 导出后回读校验：`ssh -G <id>` 或再次解析受管区块，断言字段一致（L9 的同一条纪律：写完要回读）。

---

## 9. 命令执行纪律

### 9.1 唯一入口 + 内容通道

`ssh_exec` 的入参里，命令只允许三种来源：

| 来源 | 允许的内容 | 为什么 |
|---|---|---|
| `command_file`（**首选**） | 任意文本，UTF-8 落盘后以 `bash -s` / `sh -s` 经 **stdin** 送入 | 只有"文件"能保证字节不被编码层改写（L1/L2） |
| `stdin` | 任意文本，直接走 `session.StdinPipe()` | 同上；注意 `Setenv` 依赖服务端 `AcceptEnv`，**不用它传内容** |
| `command`（内联） | **仅 ASCII 单行简单命令** | 含非 ASCII ⇒ 直接拒（`SSH_NON_ASCII_INLINE_COMMAND`），错误文案必须写出"改用 `command_file`" |

输出解码：一律按 UTF-8；出现非法字节不静默替换，置 `encoding_lossy: true` 并在 `stderr` 尾部追加一行提示。

### 9.2 detached 形态

```sh
setsid <cmd> </dev/null ><log_path> 2>&1 &
```

- 必有 `</dev/null`（否则通道被占，L3）、必有 `>log`（否则 L4 的误判复发）。
- 返回 `detached(handle, log_path)`；增量读取走作业面（§10.3）。
- **不注入**任何语言特定的缓冲参数（`python -u` 是 python 的事）：把"输出可能滞后"作为事实告知（`log_path` + `note`），而不是替用户改命令。

### 9.3 行尾（L7 的落点）

- 传内容走 `stdin` / sftp 二进制，**不经 shell 层做任何转换**。
- 需要落远端文件时：`cat > path <<'SEELEX_EOF'`（带引号的 heredoc，禁展开）或 sftp 二进制写；**不做 `\n→\r\n`**。
- 工具自身回写项目内文件（如注册表）时同样不做换行归一，且必须 `gofmt`/YAML 往返稳定（写前后各自解析一次，断言语义不变）。

### 9.4 写后回读（L9）

任何 `capability: write|deploy` 的远端写操作之后，**必须**用同一工具回读三件套，并把结果贴进结论：

1. 版本类事实：`git rev-parse HEAD` == 刚推的 commit；
2. 进程类事实：PID 变了（重启成功），而不是"命令返回 0"；
3. 行为类事实：关键路径实测一次（**不是**健康检查的 HTTP 码——那是本次事故的假阳性来源）。

这条纪律由提示词与 host 的 `deploy_command` 模板共同承载，**不**做成工具的自动判定。

---

## 10. 权限与装配

### 10.1 路由组（唯一事实源）

在 `backend/limit/policy/policy.go` 的 `DefaultPermissionGroupList()` 里登记（`backend/limit/policy/tool_meta.go` 的 `DeclaredToolMeta` 会跟着自动派生簇属，**不要**再写第二张"工具→组"表）：

| 组 | 名字 | 默认动作 | 依据 |
|---|---|---|---|
| `ro` | `ssh_hosts`、`ssh_probe` | `allow` | 只读、有界、不改任何共享状态 |
| `rw` | `ssh_exec`、`ssh_host_edit`、`ssh_trust` | `allow`（`ssh_trust` 在 `config/seelex.yaml` 里覆写成 `ask`） | 写项目文件 / 写远端 / 写宿主用户级指纹库 |
| `adm` | `ssh_export_config` | `ask` | 改的是**宿主用户级能力面**（`~/.ssh/config`），不是项目资源 |

`ssh_exec` 的危险命令拦截走**既有的 `permission.rules` patterns 机制**（匹配命令本体，与 `bash` 同一套 `policyArgsFor` 口径）：`rm -rf *`、`sudo *`、`systemctl stop *`、`shutdown *` 等默认 `ask`/`deny`。**不为 ssh 另造一套命令白名单**（§8 单一实现纪律：词表只能有一份）。

### 10.2 装配门

```yaml
# config/seelex.yaml
limits:
  ssh:
    enabled: false        # 零值 = 关；关就是关
    defaults: {connect: 10s, banner: 10s, auth: 20s, channel: 120s}
    max_output_bytes: 262144
```

- 与 `limits.async_exec` / `limits.project_board` 同一套口径：**零值 false = 关**，关闭时**一个 `ssh_*` 工具都不注册**（不摆出永远报错的名字），`ssh_exec(mode=detached)` 显式报错、**不静默降级成同步**。
- 注册点：`wiring/registry/registry.go` 新增 `RegisterSSHTools(...)`（该文件是产品工具注册的唯一处，现有 `RegisterContextReadTools` / `RegisterProjectBoardReadTool` / `RegisterGoalTools` 等 21 个 `Register*` 都在这里）；装配顺序由 `wiring/bootstrap` 决定。工具族自身的注册面进 `Runtime.RegisterBuiltins()`（`backend/engine/assemble/runtime_tools.go:248`）。
- `limits.ssh` 的开关块写在 `backend/limit/limits.go` 的 `Limits` struct 里，紧邻 `AsyncExec` / `ProjectBoard`，注释里写明"零值 = 关"与回滚臂。

### 10.3 detached 复用作业面（不新造第二套句柄表）

`backend/tool/registry/README.md` 已经写明：三类作业（`process` / `inline` / `subagent`，常量见 `backend/tool/registry/async_exec.go:87-89`）"共用同一张登记表、同一套状态机（`asyncRun.kind`）"。远端 detached 作业**扩展这张表**（新增 `kind=ssh`），而不是另起一套句柄表：

| 动作 | `kind=ssh` 的实现 |
|---|---|
| `Add` | `ssh_exec(mode=detached)` → 登记句柄，记 `id`/`log_path`/本端偏移 |
| `Status` | 不消费：重连问远端 `ps -p <pid>` 或以 `log_path` 是否增长为准 |
| `Fetch` | 重连 + 从记录的偏移读远端日志增量（`tail -c +<offset>`），推进偏移 |
| `Kill` | 发 `ssh.Session.Signal(SIGTERM)` 或远端 `kill <pid>`，**并在返回值里区分**"信号已发"与"进程已确认退出" |
| `Done` | 销项，保留 `log_path` 供人工复核 |

这样 `bg_manage` / `jobs_manage` 的既有语义（回执即返回、增量消费、终态由执行体判定）**原样复用**，模型不必学第二套读法。代价：这条作业面依赖 `limits.async_exec` 开启——关闭时 detached 能力显式不可用（已在 §10.2 说明）。

### 10.4 能力分级（host 的 `capability`）

默认只读，写与部署**逐 host 显式授权**，三档：

| 档 | 允许的命令面 | 判定落点 |
|---|---|---|
| `read`（默认） | 必须命中**既有的 `bash` allow 模式表**（`backend/limit/policy/policy.go:275` 的 `bashAllowPatterns`，按壳方言分 Shared/POSIX/PowerShell/Cmd 槽）——只读巡检、`git rev-parse`、`ps`、`systemctl status` 这类 | 未命中 ⇒ 落 `ask`；**不新增第二份白名单** |
| `write` | 允许改远端文件/服务状态；危险命令仍受 §10.1 的 deny/ask 规则约束 | host 的 `capability: write` 显式声明 + 调用被规则放行 |
| `deploy` | 跑 `deploy_command` 并执行 §9.4 的三件套回读 | host 的 `capability: deploy` 显式声明；首次使用要求人确认一次 |

不做"按 `capability` 自动提权"：`capability` 只定义**上限**，实际放行仍由权限门（主体×组×位×规则）决定——否则等于在权限模型之外开了第二条授权路径（违反 AGENTS.md §8 的"一份判据 = 一处函数"）。

---

## 11. 上下文纪律（返回有界）

- `stdout`/`stderr` 各有字节上限（`max_output_bytes`，默认 256 KiB），超出**显式**置 `truncated: true` 并给出被截断的方向（头/尾）。
- detached 的日志**永不**整份进上下文：只给 `log_path` + 增量读。
- `ssh_hosts` 默认只列 `id`/`name`/`hostname`/`capability`，`show` 才展开全文（与 `project_board_read` 的"按需读取、有界"同一口径）。
- 工具描述里不写死长表格：权限矩阵在文档里，工具描述只留判据一句话（token 成本是常驻成本）。

---

## 12. 不做的事 / 与其他域的边界

| 不做 | 理由 |
|---|---|
| shell out 到 `ssh.exe` / `plink` / `sshpass` | 三层引号与"无命令行传密码"是上游事故的根因；库内直连把这些结构性地消掉 |
| 读 `~/.ssh/config` 当事实源 | 项目内注册表才是事实；`ssh_config` 只是导出目标（省掉 `kevinburke/ssh_config` 依赖） |
| `InsecureIgnoreHostKey` / 自动追加指纹 | 见 §6.3 红线 |
| 自动判定部署成功 | 见 §4.2；判据在人手里 |
| 桌面能力 | 与 `computer_*` 无关（那是另一族共享外设能力，路由组 `rw_desktop`） |
| 本地命令 | 与 `bash` 无关；远端是本族唯一的执行目标 |
| SSH 服务端 / 完整端口转发（本期） | `(*ssh.Client).Dial` 已可用（`tcpip.go`），但暴露面另行评估——与 `docs/arch/remote-agent-node-feasibility.md` §4 缺口 1（"Seelex 没有网络服务，可走 ssh 隧道复用 stdio"）是同一条线，届时在此文档增章而非另开一份 |

**待决（需要人裁决，不擅自落地）**

- **D-1 SFTP**：引 `github.com/pkg/sftp`（能力全、多一个模块依赖）vs 不引（`stdin`/`base64` 传内容、`cat >` 落文件）。建议：M1–M3 不引，等真有"大文件双向同步"的需求再引。
- **D-2 凭据内核钥匙串**：本期 DPAPI + 0600 文件够用；服务化/多用户部署需要真正的钥匙串（新依赖）。
- **D-3 Windows agent 接入**：命名管道 `\\.\pipe\openssh-ssh-agent` 用 `os.Open` 还是要 `go-winio`（引入新模块）。M2 用真机实测裁决，结论回写本节。

---

## 13. 验收判据（可测）

**核心便利**：`vendor/golang.org/x/crypto/ssh/server.go` 里 `NewServerConn` / `ServerConfig` 都在 ⇒ 测试可以在**进程内起一个 sshd**，全部判据不依赖真机、不进网络。

| # | 判据 | 怎么测 |
|---|---|---|
| A1 | 认证矩阵四种方式都能连上，且 `auth_used` 报得对 | 进程内 sshd × `{password, key, key+passphrase, agent(内存 agent)}` 表驱动 |
| A2 | 非 ASCII 内联命令被拒，错误码与提示正确 | 直接调 `ssh_exec(command="echo 中文")` ⇒ `SSH_NON_ASCII_INLINE_COMMAND` |
| A3 | 文件/stdin 通道传非 ASCII 逐字无损 | 写入含中文的 `command_file`，远端 `sha256sum` 与本端比对 |
| A4 | 四段超时各自可触发、各自可归因 | sshd 侧分别延迟 banner / 拒绝认证 / 命令睡眠；断言 `timed_out_segment` |
| A5 | channel 超时**不**等于失败 | 命令睡眠 > channel 超时 ⇒ `status=channel_timeout`、`still_running=true`、其后仍能 fetch 到完整输出 |
| A6 | 哈希 known_hosts 条目被正确识别 | 用 `knownhosts.HashHostname` 生成条目，断言 `probe` 判 `host_known`（**并**加一条静态检查：指纹判定函数里不出现 `strings.Contains`/正则匹配） |
| A7 | 未知主机 / 指纹不符 / 已吊销三种分类正确 | 三种 `known_hosts` 夹具 × `probe` ⇒ `class ∈ {host_unknown, key_mismatch, revoked}` |
| A8 | `ssh_trust` 拒绝"指纹参数与本次握手不一致" | 传错误指纹 ⇒ 硬失败且**未写盘**（读回文件断言行数不变） |
| A9 | 改名不动 `id`、不动引用 | `rename` 后 `ssh_hosts action=show` 的 `id` 逐字相同；`refs` 命中清单不变 |
| A10 | 有引用时 `remove` 被拒 | 造一条引用 ⇒ `remove` 返回命中清单且文件未变 |
| A11 | 关就是关 | `limits.ssh.enabled=false` ⇒ 注册表里**零** `ssh_*` 名字；`ssh_exec(detached)` 在 `async_exec` 关闭时显式报错 |
| A12 | 权限位 | `sub` / `emp_ro` 对 `ssh_exec` 位缺 ⇒ 不可路由；`ssh_export_config` 仅 `root` 有位 |
| A13 | 行尾不变形 | 对 CRLF 夹具写入并读回，字节相等（对齐 L7 的回归形态） |
| A14 | 输出有界 | 远端持续输出 10 MB（如 `yes` 灌入 `head -c 10000000`）⇒ `truncated=true` 且工具返回字节 ≤ 上限 |

测试落点：单元随包走（`backend/tool/ssh/*_test.go`）；权限/装配回归落 `backend/tool/registry/` 与 `backend/engine/assemble/`；仓库级集成（真机、可选 skip）落 `test/`。

---

## 14. 分期与回滚臂

**当前进度（2026-10-23）**：**连接基座已落地** —— `backend/tool/ssh` 实现了 §5 四段超时、§6 指纹与显式信任、§7 认证优先级与 `auth_used` 回报、§9 命令纪律（文件/stdin 通道、非 ASCII 拒收、三形态返回、有界输出）、§13 的 A2/A3/A4/A5/A6/A7/A8/A13/A14，另有独立驱动 `backend/tool/ssh/sshprobe` 可直接排障。**尚未落地**：`ssh_*` 工具族与装配门（§3/§10）、项目内 host 注册表与引用检查（§8）、`kind=ssh` 作业面扩展（§10.3）、GUI 编辑面（M5）。
落地过程中被用例逼出来的两处修正：①建连必须排在认证材料组装**之前**（否则"缺凭据"会抢报成认证失败，掩盖"主机连不上"）；②`no_credential` 必须与 `auth_rejected` 分成两类（"我们没提交东西"与"提交了被否"）。

| 期 | 交付 | 验收 | 回滚 |
|---|---|---|---|
| **M1 只读探针** | `ssh_hosts` + `ssh_probe` + 注册表读取 + `knownhosts` 接入 + 四段超时 | A4/A6/A7/A11/A12 | `limits.ssh.enabled: false` |
| **M2 执行面** | `ssh_exec(run)` + 命令文件/stdin + 非 ASCII 拒绝 + 输出有界 + 凭据（DPAPI/0600） | A1/A3/A13/A14 | 同上；M2 之前 `ssh_exec` 不存在，无兼容面 |
| **M3 改写面** | `ssh_host_edit`（含 rename/refs/remove 守卫）+ `ssh_trust` + `ssh_export_config` | A8/A9/A10 | 同上（注册表是纯文件，可手改回滚） |
| **M4 远端作业化** | `kind=ssh` 入作业面 + `detached` + 增量 fetch/kill | A5 + 与 `bg_manage` 的一致性用例（同一份输入跑两遍） | 关 `ssh.detached` 子开关，`run` 不受影响 |
| **M5 规划** | SFTP（D-1）／端口转发／内核钥匙串（D-2）／GUI host 编辑面板 | 待定 | — |

每期都必须先写"会红的用例"再实现（AGENTS.md §8.7），交付里列出新增与删除清单（§8.6）。

---

## 15. Review 指南

改这一族之前，先按这 8 条问一遍：

1. 有没有把 `channel_timeout` 说成失败？（违反 L5/A5，这是最贵的错——会诱发"改对的代码被推翻"）
2. 指纹判定里有没有出现文本 grep 或"存在性字符串比较"？（违反 L6/A6）
3. 有没有任何路径会自动追加 `known_hosts`？（违反 §6.3）
4. 内联命令里的非 ASCII 是拒收还是"尽力而为"？（违反 L2/A2）
5. 有没有在哪一处做了换行归一？（违反 L7/A13）
6. `id` 与 `name` 是否又被合并成一个字段？（违反 §8.2 与 AGENTS.md §6）
7. 有没有第二次实现既有的东西——句柄表、命令白名单、`known_hosts` 解析、项目根解析？（违反 AGENTS.md §8）
8. 工具返回里有没有出现密码明文、`success: true` 式业务判定、或本机绝对路径？（违反 §7.3/§4.2/§4 写作规则）

**已知风险（写在这里，别在实现里假装不存在）**

- **非 ASCII 拒收会是"意外失败"的主要来源**：必须配可操作错误文案 + 打点（哪个工具、哪个项目），否则模型会反复撞同一面墙（`bash_read` 的 34.4% 就是前车之鉴）。
- **DPAPI 只同用户可解**：主代理与 GUI 若跑在不同用户下，凭据读不到；报错文案必须点明这一点。
- **`kind=ssh` 进作业面**是本设计里唯一触碰既有共享状态机的改动，风险最高，所以排到 M4 且带独立子开关。
