# SSH 工具族：内置工具的双传输策略与责任链自适应

> 状态：**设计稿 + 已跑的孤立 MVP**。本文回答"如何让 read/write/edit/bash/manage 同时具备本地与 SSH 两种执行面，并由项目状态自动选择"。产品侧**尚未改动任何注册或派发**；可行性验证代码在 [`../2026-10-23-ssh-tool-transport-mvp/`](../2026-10-23-ssh-tool-transport-mvp/)（`go run ./docs/2026-10-23-ssh-tool-transport-mvp`）。
> 日期：2026-10-23
> 关联：[`ssh-tool.md`](ssh-tool.md)（连接基座：四段超时 / 指纹 / 命令纪律）、[`../../backend/tool/ssh/README.md`](../../backend/tool/ssh/README.md)（已落地的基座实现）。

## 1. 为什么需要这一层

连接基座解决的是"怎么连上一台机器并跑一条命令"。但模型真正用的是**读文件、写文件、改一段、跑命令、管理后台作业**这五件事。如果它们只能作用于本机，那"项目在远程主机上"这个前提就得不到任何工具支持——模型会去猜路径、用 `bash` 拼 `cat`，把纪律（内容通道、编码、超时三分）全部绕开。

因此要的不是"再来五个新工具名字"，而是：**同一套工具语义，两个执行面，按项目状态选**。

## 2. 现状（已核对到行）

| 事实 | 出处 |
| --- | --- |
| 项目作用域工具名单是唯一一份：`read_file` / `grep_search` / `glob` / `write_file` / `edit_file` / `bash` | `backend/tool/registry/scoped_names.go` 的 `projectScopedToolNames` |
| 后台作业管理工具的本名是 `bg_manage`（用户口中的 "manage"） | `backend/tool/registry/job_tools.go:32`、`router.go:127` |
| 工具注册唯一处：`Router.Register()` 逐个注册 | `backend/tool/registry/router.go` |
| **框架已有责任链**：`type MetaMiddleware func(name string, meta ToolMeta, next ToolHandler) ToolHandler` | `vendor/github.com/RedHuang-0622/Seele/tools/tools.go` |
| 链的装配顺序：event → pathGate → permission | `backend/tool/registry/registry_state.go:42` |
| 连接基座已落地：四段超时、指纹校验、`Run`/`RunDetached`、`Probe` 分类 | `backend/tool/ssh/`（17 组进程内 sshd 用例通过） |

结论：**不需要发明新的插拔点**。策略选择做成链上的一个 `MetaMiddleware` 即可，且"责任链"不是我们要新造的架构，而是既有架构的空位。

## 3. 目标与非目标

**目标**

1. `read_file` / `write_file` / `edit_file` / `bash` / `bg_manage` 五个工具都有 SSH 执行面，**语义与本地一致**（同一套参数、同一套返回形状）。
2. 执行面由**策略**选择，可显式指名，也可按项目状态自适应。
3. 连接变更与初始化时**必须验连通性**，失败要说清哪一段坏。
4. SSH 导致的失败有**独立错误码**，与"本地参数错"分开，模型才能据此换招。
5. 代码**最大复用**：一份 handler 骨架 + 两份传输实现，不复制五份逻辑。

**非目标**

- 不重写 Seele 的 builtin 工具，也不改它们的参数 schema；
- 不动权限模型（主体 × 路由组 × 位），SSH 不另造权限词表；
- 本期不引入 SFTP 依赖（D-1 待决），文件操作经 `bash` + `base64` 承载，见 §5.3；
- 不做端口转发、不做多跳跳板。

## 4. 架构：一条策略接口 + 一个链中间件

```
                    ┌───────────────── 责任链（既有 MetaMiddleware）─────────────────┐
tool call ──▶ event │ pathGate（本机路径闸门 / SSH 下改为远端路径闸门） │ permission │──▶ 终端 handler
                    └──────────────────────────────────────────────────────────────┘
                                                                        │
                                                            ┌───────────┴───────────┐
                                                            │  TransportSelector    │  ← 新增的一个中间件
                                                            │  按项目状态决定走哪个  │
                                                            └───────────┬───────────┘
                                                                        │
                                          ┌─────────────────────────────┴─────────────────────────────┐
                                          │                                                           │
                                   LocalTransport                                              SSHTransport
                                （现行为，零改动）                                    （backend/tool/ssh 基座）
```

### 4.1 策略接口

```go
// Transport 是"五件事"的执行面抽象。两个实现：本地、SSH。
type Transport interface {
    Name() string                                   // "local" | "ssh"
    Read(ctx, path, limit) (Content, error)
    Write(ctx, path, content) (WriteResult, error)
    Edit(ctx, path, oldText, newText string, replaceAll bool) (EditResult, error)
    Exec(ctx, script, opts) (ExecResult, error)     // 命令内容只走文件/stdin
    Manage(ctx, op, handle) (ManageResult, error)   // 后台作业：起/查/杀/列
}
```

**为什么是这五个方法而不是五个工具**：工具是"给模型看的名字和 schema"，传输是"把意图落到某个文件系统/进程空间"。前者保持稳定（模型不用学新名字），后者可替换。五个 handler 各自只做「解析参数 → 调 `transport.X` → 组装返回」，因此**一份 handler 骨架服务两个执行面**。

### 4.2 责任链上的位置

`TransportSelector` 是一个 `MetaMiddleware`，插在 **pathGate 之后、终端 handler 之前**：

```go
func TransportSelector(resolver Resolver) frameworktools.MetaMiddleware {
    return func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
        if !eligibleTool(name) {                 // 只有五个工具走双面
            return next
        }
        return func(ctx context.Context, call frameworktools.ToolCall) (frameworktools.ToolResult, error) {
            transport, decision := resolver.Resolve(ctx, name)   // 纯函数，不做 IO 重活
            return runWithTransport(transport, name, call, next)
        }
    }
}
```

**为什么放在 pathGate 之后**：pathGate 判的是"这个路径允不允许碰"。本地面判本机路径，SSH 面必须判**远端路径**——两者的判据不同（同一字符串在两边含义不同）。若选择器在 pathGate 之前，闸门就会拿本机规则去判远端路径，产生"看起来通过了但实际越界在远端"的错觉。因此顺序必须是：**先决定执行面，再用该面的路径规则过闸门**。这一点在落地时要求 pathGate 支持"按 transport 换根"，属 M3 的工作量（见 §8）。

### 4.3 自适应选择判据（纯函数，可单测）

| 项目状态 | 结果 | 理由 |
| --- | --- | --- |
| 未配 SSH 端点 | `local` | 老行为，零风险 |
| 配了端点但 `enabled=false`（缺省） | `local` | 配了不等于要用；必须显式开 |
| 配了 + 启用 + 最近一次探针 ok | `ssh` | 唯一走远端的格 |
| 配了 + 启用 + 探针失败/未探过 | `local` **+ reason** | 降级但不静默：返回值里带 `degraded_reason` |
| 调用方显式指名 `ssh`（工具名后缀 `_ssh` 或 `transport` 参数） | `ssh`（失败即错，不降级） | 显式意图优先于自适应 |
| 项目未绑定（无工作区） | `local` | 无远端可指 |
| 子代理 / 只读主体 | 跟随其父项目决策 | 不因角色改变执行面 |

两条硬规矩：**隐式选择可以降级，显式选择不许降级**；**降级必须留痕**（`degraded_reason`），否则"为什么这次在本机跑"就无从追查。

### 4.4 五个工具的双面语义

| 工具 | 本地面 | SSH 面 | 差异提示 |
| --- | --- | --- | --- |
| `read_file` | `store/workspace.ReadFile`（可见性 + 上限） | 远端读（`bash` + 限制上限） | 返回里带 `transport:"ssh"` |
| `write_file` | 原子写 + 可见性校验 | 远端写（`base64 -d` 经 stdin，避免引号/编码） | 同上 |
| `edit_file` | 唯一串匹配校验 | 远端 read-modify-write，**同一份唯一串判据** | 判据函数不得有两份（AGENTS §8） |
| `bash` | 本机进程 + 作业面 | 远端 `Run`/`RunDetached`（基座已实现三分收口） | `channel_timeout` 在两面上语义一致 |
| `bg_manage` | 本机句柄表 | 远端作业句柄（远期：读远端日志增量） | 句柄**命名空间必须带 host 前缀**，否则两边 handle 会撞 |

## 5. 关键实现点

### 5.1 一份 handler，两份传输

五个 handler 的骨架完全相同，差异只在 `transport` 变量：

```go
func readFileHandler(t Transport) handler {
    return func(ctx context.Context, call Call) (Result, error) {
        req, err := parseReadArgs(call)          // 参数解析唯一一份
        if err != nil { return failed(CodeBadArg, err) }
        content, err := t.Read(ctx, req.Path, req.Limit)
        if err != nil { return failed(codeFor(t, err), err) }   // 错误码按面归一
        return ok(content)
    }
}
```

**不允许**出现 `readFileSSHHandler` 这类平行实现——那会让"唯一串匹配"、"上限"、"可见性"三份判据各自漂移，而这正是 AGENTS §8 禁止的形态。

### 5.2 错误码：SSH 失败必须可辨认

工具错误信封增加 `error_code`（机器判定）与 `next_step`（人读）。SSH 面专用码：

| 码 | 触发 | 模型该怎么做 |
| --- | --- | --- |
| `SSH_CONNECT_FAILED` | connect 段失败（DNS/端口/防火墙） | 不要重试同一路径；报告网络结论 |
| `SSH_HOST_UNTRUSTED` | 首次连接（known_hosts 无条目） | 走显式信任流程（附指纹） |
| `SSH_HOST_KEY_MISMATCH` | 指纹不符 | 硬停，人工核对 |
| `SSH_AUTH_FAILED` | 认证被拒（口令/公钥/agent） | 换凭据，不要改代码 |
| `SSH_NO_CREDENTIAL` | 本端无可用认证材料 | 补凭据，不算命令失败 |
| `SSH_BANNER_TIMEOUT` / `SSH_AUTH_TIMEOUT` | 对应段超时 | 区分"网络慢"与"认证卡住" |
| `SSH_CHANNEL_TIMEOUT` | 通道段超时 | **不是失败**：`still_running=true`，走日志取回 |
| `SSH_PATH_NOT_FOUND` | 远端路径不存在 | 先列目录确认 |
| `SSH_REMOTE_CMD_FAILED` | 远端命令非零退出 | 读 stderr，属业务结论 |

复用判据：**码由 `backend/tool/ssh` 的 `ProbeResult.Class` 与 `Result.Status` 映射而来**（一张表，一处函数），不新造分类法。

### 5.3 文件操作的承载方式（D-1 未决前）

SFTP 需要新依赖 `github.com/pkg/sftp`。本期取**零依赖路径**：

- 读：`bash -c 'cat -- <path>'`（上限由本端截断，标 `truncated`）
- 写：`bash -c 'base64 -d > <path>'`，内容经 **stdin** 送 base64（ASCII，绕开三层引号与代码页）
- 编辑：读 → 唯一串替换 → 写；三处判据复用同一函数

代价：多一次往返、远端须有 `base64`/`cat`（coreutils，Linux 与 macOS 均有）。收益：本期零依赖、行为完全可测。SFTP 落地后只替换 `SSHTransport` 的文件三方法，**接口与 handler 不动**——这正是策略接口的价值。

### 5.4 连通性测试：三个时机，同一判据

| 时机 | 行为 | 失败处理 |
| --- | --- | --- |
| 新建项目填了 SSH | 创建后立刻 `Probe` | 项目建成即成功，连接标记 `unhealthy` + 原因（**不回滚项目**） |
| 修改连接配置 | 保存前 `Probe` | 返回分类；写不写由调用方决定（默认：写入但标记 `unhealthy`） |
| 每次连线前的懒探针 | 缓存结果 TTL 内不重复探 | 失败则降级到 local 并留痕 |

三处调用**同一个 `Probe`**，不允许在 UI 层另写"测一下端口通不通"。

### 5.5 MVP 实测出的两个缺口

跑完 [`2026-10-23-ssh-tool-transport-mvp`](../2026-10-23-ssh-tool-transport-mvp/)（9/9 场景通过）后确认：

**F1（已修正）非 ASCII 路径不能内联。** 远端路径含中文时，内联命令变成非 ASCII，被基座按纪律拒收（`SSH_NON_ASCII_INLINE_COMMAND`）。这不是基座的错，是本设计原先的疏漏：**凡进命令串的字符串（路径、参数）都必须先 base64**。

```sh
cat -- "$(printf %s '<b64 path>' | base64 -d)"
printf %s '<b64 content>' | base64 -d > "$(printf %s '<b64 path>' | base64 -d)"
```

**F2（待 M2 修）基座不能同时表达"命令 + stdin 数据"。** `Request` 的 `Inline`/`CommandFile`/`Stdin` 互斥，而"写文件"需要命令与数据同时给。MVP 只能把载荷内联进命令串（小文件可行，大文件受命令行长度限制）。落地方案：给 `Request` 加 `Data` 字段（喂给命令的 stdin 数据），与"脚本来源"解耦。

## 6. 数据形状（草案）

```jsonc
// 工具返回（成功）
{ "content": "...", "truncated": false, "transport": "ssh", "host": "deploy.example:22" }

// 工具返回（SSH 类失败）
{ "error_code": "SSH_HOST_UNTRUSTED", "message": "...", "fingerprint": "SHA256:...", "next_step": "..." }

// 传输决策（进轨迹，供事后归因）
{ "transport": "local", "reason": "probe_failed", "degraded": true, "detail": "auth_rejected" }
```

`transport` 与 `degraded` 必须进轨迹：否则"这次为什么在本机改了文件"永远查不清。

## 7. 验收判据

| 编号 | 判据 | 验证方式 |
| --- | --- | --- |
| A1 | 五个工具在两个执行面上参数与返回形状一致 | 表驱动：同一输入 × 两 transport |
| A2 | 同一 handler 代码，无平行实现 | 代码检查：`grep -c "_sshHandler"` 为 0 |
| A3 | 选择判据是纯函数，六格全覆盖 | 单测：状态矩阵 |
| A4 | 隐式可降级、显式不降级 | 单测：指名 ssh 且探针失败 ⇒ 报错而非回落 |
| A5 | 降级必留痕 | 断言 `degraded_reason` 非空 |
| A6 | SSH 失败映射到独立错误码 | 单测：Class → Code 映射表 |
| A7 | `channel_timeout` 不被报成失败 | 与基座同判据 |
| A8 | 连通性测试三处共用同一 `Probe` | 代码检查：仅一处调用 |
| A9 | 远端超限输出标 `truncated` | 单测 |
| A10 | 句柄命名空间带 host 前缀 | 单测：两 host 的同 pid 不冲突 |

## 8. 分期

| 期 | 内容 | 出口 |
| --- | --- | --- |
| **M0（已做）** | 孤立 MVP：策略接口 + 责任链 + 选择矩阵 + 错误码映射，跑在进程内 sshd 上 | `docs/2026-10-23-ssh-tool-transport-mvp` 全部场景 PASS（实测 9/9） |
| M1 | `Transport` 落地到 `backend/tool/registry`：五工具改走接口，Local 实现保持现行为 | 现有测试全绿（行为零变化） |
| M2 | `SSHTransport` + 错误码映射 + 连通性三时机 | 真机冒烟：read/write/edit/bash 全通 |
| M3 | pathGate 按 transport 换根（远端路径规则） | 越界用例：远端 `../` 被拒 |
| M4 | `bg_manage` 远端作业面（含日志增量取回） | 远端长任务可查可杀 |
| M5 | 工具暴露开关（`limits.ssh.enabled`）与权限路由组登记 | 关掉后工具列表里与 SSH 相关的项消失 |

## 9. 风险与待决

- **R1 远端命令面依赖 coreutils**：非 Linux 远端（如 busybox）可能没有 `base64 -d` 的 `-d` 写法。缓解：探测失败即回 `SSH_REMOTE_UNSUPPORTED` 并提示装 SFTP。
- **R2 pathGate 换根是本设计唯一的"必须改动既有行为"处**：若 M3 延后，SSH 面的路径越界只能靠 handler 自己再判一次——那会出现第二份判据，必须避免。**M3 不应晚于 M2 上线**。
- **R3 降级噪音**：网络抖动会让"该走远端"的时刻走本机，导致改错文件。缓解：显式意图优先 + 降级必留痕 + 轨迹里 `transport` 可查。
- **D-1 SFTP**：仍待决（见 `ssh-tool.md` §12）。

## 10. 孤立 MVP 说明

位置：`docs/2026-10-23-ssh-tool-transport-mvp/`（一次性工作包，**不进产品**）。

```bash
go run ./docs/2026-10-23-ssh-tool-transport-mvp
```

它证明的是**接线可行**，不是最终实现：

- 同一份 handler 骨架跑在 `LocalTransport` 与 `SSHTransport` 上，结果一致（A1/A2）；
- `MetaMiddleware` 形状的责任链可用，顺序与短路符合预期；
- 选择矩阵六格可判定（A3/A4/A5）；
- SSH 失败映射到独立错误码（A6/A7）；
- 进程内 sshd 夹具承载 exec 面（不依赖真机、不联网）。

**MVP 里被简化掉的**：远端文件命令面是夹具内置的一小撮命令（`cat -- `、`base64 -d > `），不是真 shell；`bg_manage` 远端面只做到句柄命名。这些属 M2 的落地工作量，MVP 不假装它们已完成。
