# 模块化、多二进制目标架构设计

- 日期：2026-10-15
- 状态：**提案，未裁决，未实施**。目标是「业务模块可独立开发、测试和运行，必要时独立重启」——**不是让每个 Go package 都变成进程**。
- 与同日前置文档的关系：`docs/2026-10-15-product-architecture-pre-design/global-review.md` 已给出「推荐混合架构」「模型 A/B」的有限证据结论；本文在其上补充逐二进制契约、依赖证据与 Seele 复用清单，结论一致。

---

## 1. 逻辑模块图（现状，〔源码〕依据各模块 README）

```
┌────────────────────────────── 前端层（只消费 Application DTO/Event） ──┐
│  tui/（Bubble Tea，默认入口）   gui/（Wails/WebView + frontend + PTY）  │
└───────────────────────────────┬───────────────────────────────────────┘
                                │ AppController / gui.Bridge（Snapshot + Event + Ack/Replay）
┌───────────────────────────────▼───────────────────────────────────────┐
│ application/  应用层（用例编排 + 权威状态 model.Snapshot + event.Hub）    │
│   core（Service/view_state/context_runtime/prompt_layer/worktable…）    │
│   contract（ports + dto：零依赖 DTO/枚举唯一编码 state_codec）           │
│   model（Snapshot/前端协议 DTO，仅依赖 dto）  event（Hub）  approval prompt│
└───────┬───────────────┬───────────────┬───────────────┬───────────────┘
        │ SessionPort   │ WorkspacePort │ RuntimePort   │ PluginPort/SkillPort
┌───────▼──────┐ ┌──────▼─────┐ ┌───────▼───────────────┐ ┌───────────────┐
│ session/     │ │ workspace/ │ │ seelebridge/（组合根+  │ │ plugin/ skill/│
│ 会话资源唯一  │ │ 项目定义+   │ │  防腐层：Runtime 装配、│ │ 插件/技能     │
│ 所有者(Domain│ │ binding+   │ │  tools/router、async  │ │ 生命周期与    │
│ actor)       │ │ 只读 git 面│ │  Job、mcpstack、      │ │ 可见性事务    │
└───────┬──────┘ └────────────┘ │  workunit 生命周期、   │ └───────────────┘
        │                       │  computer/image、角色  │
┌───────▼───────────────────────▼──────────────┐
│ sessionstore/  原子、项目作用域持久化（JSON v8：分片追加 + module head 发布点）│
└──────────────────────┬───────────────────────┘
                       │ Seele v0.3.4（远程模块，无产品语义）
        types / session / agent / telemetry / accountpool /
        seelectx(storage·cache·tracer) / event / jobs / workplan / tools(mcp·microhub) / limits
```

**状态所有权与依赖方向（单向）**：

| 模块 | 状态所有权 | 可依赖 | 禁止 |
|---|---|---|---|
| tui/gui | 仅 UI 态 | application 门面 | 直接触达 store/Seele |
| application | Snapshot、revision、输入队列、审批 | contract/model/event 同级 + 端口 | 依赖 seelebridge 具体类型 |
| session | 会话资源（actor 单 goroutine） | sessionstore、Seele types | 反向依赖 application |
| sessionstore | 数据根（单写者）、head 发布点 | Seele storage/event | 依赖上层用例 |
| seelebridge | 引擎 bundles、Job 表、MCP 栈、workunit | Seele 全部 + sessionstore/workspace | 被 frontend 直接依赖 |
| workspace | 项目/binding | 无业务依赖 | 存会话历史 |
| plugin/skill | 插件/技能可见性快照 | skill、seelebridge 策略 | 拥有会话状态 |

共享契约层（多进程可复用性已核实〔源码〕）：`application/contract/dto`（import 仅 stdlib）、`application/model`（仅依赖 dto）、`internal/` 零依赖叶子（buildinfo、frontmatter、promptassets、bootseed、winhide）。`contract/ports.go` 是进程内接口（含 `<-chan`、回调、Seele 类型），**不是 wire 契约**。

## 2. 部署进程图

### 2.1 现状（〔源码〕）

```
┌─────────────────────────────┐         stdio          ┌──────────────────────────┐
│ seelex(.exe)  主二进制       │ ─────────────────────▶ │ MCP server 子进程群        │
│  -frontend tui|gui|headless| │  (Seele tools/mcp 拉起) │ (第三方/自带，无自动重启)   │
│  |backend                   │                        └──────────────────────────┘
│  内嵌：application+bridge+  │         stdio          ┌──────────────────────────┐
│  sessionstore（单写者）      │ ◀───────────────────── │ computer-use-mcp(.exe)    │
└─────────────────────────────┘   外部宿主(Codex等)调用   │ 已存在的第二进程先例       │
                                 （它是 server 角色）     └──────────────────────────┘
```

可执行入口仅两个〔源码〕：`main.go`（组合根，四前端模式 + pprof/gui 等 build tag 变体）与 `seelebridge/tools/computer/mcp/main.go`（独立 stdio MCP 服务端，无 Makefile 目标，需手工构建）。

### 2.2 目标（〔提案〕，分阶段，门禁驱动）

```
阶段 G0（现在可做的纯模块化，进程数不变）
┌─────────────────────────────────────────────────────────────────────┐
│ seelex 主二进制（模块化单体）                                          │
│  内部边界硬化：端口窄化、wire 契约（dto/model）先行、禁止按层分支        │
└──────────────┬──────────────────────────────────────────────────────┘
               │ stdio/MCP                          （不变）
               ▼
        MCP server 子进程群 / computer-use-mcp

阶段 G1（第一个新进程：可控执行路径远端驱动验证，对应 remote-agent L1）
┌──────────────────────┐   本地 IPC（stdio/pipe + JSON envelope +    ┌──────────────────────┐
│ seelex 主二进制       │   traceparent 注入）                        │ seelex-exec-node     │
│ Application/会话/     │ ◀────────────────────────────────────────▶ │ （候选名）            │
│ sessionstore 单写者   │   命令/事件/取消/租约                        │ 可控 Tool/Job 执行     │
└──────────────────────┘                                            │ （bash/computer 子集）│
                                                                    └──────────────────────┘
阶段 G2（可选，仅当 Dot 方向裁决通过，对应 remote-agent L2）
┌──────────┐  loopback/ssh + 授权   ┌────────────────────┐
│ seelex    │ ───────────────────▶ │ seelexdot 实例      │（远程执行单元，
│ （Boss=   │  workunit 端口 remote │ （模型 A：同一底座   │ 各自 data root）
│  controller│  实现（L2 阶梯）     │  的可选执行实例）    │
└──────────┘                       └────────────────────┘
```

L3（RuntimePort 整端口远程化）按 remote-agent-node-feasibility 结论**不做**：5 处 `<-chan` 成员把进程内所有权钉死。

## 3. 候选二进制契约

### 3.1 `seelex`（主二进制，已存在，G0 硬化）

| 项 | 内容 |
|---|---|
| 输入 | 用户输入（tui/gui/headless）、`config/` 四链（accounts/mcp/search_engine/seelex.yaml）、`-store/-plugins/-permission/-frontend` flags |
| 输出 | Snapshot/Event（前端）、会话与项目数据（sessionstore）、mcp-traces、诊断（/trace /diag、pprof tag） |
| 命令契约 | Application Service 方法（进程内；G1 起对 exec-node 暴露子集） |
| 查询契约 | Snapshot() + 增量 Event（revision/seq/resync 语义〔源码〕`application/event/hub.go:105-120`） |
| 事件契约 | EventHub（有界 replay 窗口 1024；溢出强投 resync.required） |
| 存储归属 | **唯一写者**：data root（sessionstore JSON v8） |
| 启动/关闭 | main.go 组合根拓扑序装配；graceful close（gui.Bridge）；`CloseAsync`/`CloseSessionAsync` 回收作业与临时目录 |
| 健康检查 | 现状无独立健康端点；〔提案〕headless/backend 模式加 `/healthz`（pprof mux 同端口或独立），上报版本、会话数、Job 在途数 |
| 故障恢复 | 会话冷恢复（LoadTail/恢复说明 role=system/幂等，MEMORY.md T-RESUME 约定）；崩溃残尾截断（`jsonl_io.go:10-43`） |
| 开发调试 | pprof tag、`SEELEX_REQUEST_LOG`、/diag、e2e ScriptedEngine、live 探针测试 |

### 3.2 `computer-use-mcp`（已存在，作为第二进程先例）

| 项 | 内容 |
|---|---|
| 输入 | stdio 上的 MCP 请求（宿主为 Codex 等外部进程或 Seelex 自身） |
| 输出 | MCP 响应（computer_* 桌面原语结果） |
| 契约 | MCP 协议（Seele `tools/mcp` provider 侧实现）；无 Seelex 产品语义 |
| 存储归属 | 无（无状态执行面） |
| 启动/关闭 | 由宿主经 stdio 拉起/关闭；无自动重启（与 G3 反例同源，〔提案〕由控制方加看门狗） |
| 健康检查 | MCP ping |
| 故障恢复 | 宿主重拉；〔提案〕熔断事件接入统一 trace（现仅主进程侧 mcpstack 记录） |
| 开发调试 | 手工 `go build ./seelebridge/tools/computer/mcp`；〔提案〕纳入 Makefile 与发布清单 |

### 3.3 `seelex-exec-node`（候选，G1，名称草案〔提案〕）

定位：**唯一建议近期新增的二进制**——选一条可控 Tool/Job 执行路径（建议 scoped bash + computer 子集）验证远端驱动，对应 global-review 执行摘要第 5 条。

| 项 | 内容 |
|---|---|
| 输入 | 主进程经 IPC 下发的执行命令（argv/环境/工作目录/超时/资源限额/取消 token/deadline/traceparent） |
| 输出 | stdout/stderr 流（分块引用，大结果走文件引用不落 envelope）、退出码、资源用量 |
| 命令契约 | exec.start / exec.cancel / exec.lease-renew（新 wire 契约，放 `application/contract/dto` 同级新包，禁止序列化 Go interface） |
| 查询契约 | exec.status（按 job id） |
| 事件契约 | exec.output（流）/ exec.exit（终态 exactly-once 语义由租约+去重保证） |
| 存储归属 | 无自有存储；日志回传主进程（沿用 1MiB 封顶纪律） |
| 启动/关闭 | 主进程拉起（stdin/stdout pipe 或 loopback）；空闲 TTL 自退；主进程关闭时整树终止（沿用 Job Object/进程组装配 H1） |
| 健康检查 | ping 命令 + 租约心跳；主进程侧看门狗重拉（解决 G3 无自动重启形态） |
| 故障恢复 | 主进程重启后按 job id 对账：node 侧无记录的 job 一律按 interrupted 走 T-RESUME 恢复约定 |
| 开发调试 | 可独立 `go run` + 手喂 JSON envelope；e2e 用 fake transport 复用同一契约测试 |

**为什么先拆它**：故障隔离收益最直接（工具/子进程崩溃不拖垮主进程）、契约面最小（无会话状态所有权转移）、可逆（失败可退回进程内 router，端口不变）。

### 3.4 `seelexdot`（候选，G2，仅当产品裁决通过）

按模型 A（见 §6）：同一产品底座的可选执行实例。契约 = workunit 端口的 remote 实现（remote-agent L2）：输入为 workunit 工作分派（含凭据子集、worktree 约定），输出为工作记录/制品引用；**各自 data root**（remote-agent 文档的硬约束）；启动关闭、健康检查、恢复同 3.3 的租约/对账模式，另加授权与审计（模型 A 下由主实例签发能力凭据）。裁决前不展开字段级设计。

### 3.5 不设独立二进制的模块（不应拆出）

| 模块 | 不拆原因〔提案，附依据〕 |
|---|---|
| application/core | 状态权威与 revision 所有者；拆出会把每次 Snapshot/事件变成 IPC 往返，违背「状态私有/消息进出」的效率前提 |
| sessionstore | data root 单写者/单进程锁是原子性前提；拆出需重造分布式锁与跨进程事务 |
| workspace | 纯函数型只读面，无独立生命周期 |
| event.Hub | 进程内消息骨干；有界重放不是耐久队列，不等于 broker |
| plugin/skill | 切换事务需与 tools/MCP 状态同进程回滚（AGENTS.md Review 清单） |
| prompt/context 装配 | 与会话历史同读同写，拆出即分布式上下文一致性问题 |

## 4. 三种架构对比

| 维度 | A. 模块化单体 | B. 多进程本地服务 | C. 混合（推荐） |
|---|---|---|---|
| 形态 | 单进程，端口+契约硬化 | Application/Runtime/Store/Exec 各为进程，本地 IPC | 单体为主 + computer-use-mcp（已有）+ exec-node（G1）+ 可选 dot（G2） |
| 独立开发/测试 | 包级可做（e2e 门禁已有先例） | 进程级 | 关键路径进程级，其余包级 |
| 独立重启 | 否 | 是（但状态归属复杂） | 执行单元可重启，会话面不动 |
| 故障隔离 | 弱 | 强 | 执行面强、状态面保持简单 |
| 性能 | 零 IPC/序列化成本 | 每交互一次 IPC+序列化 | 仅执行路径付费 |
| 一致性 | 单写者原子性现成 | 跨进程事务/对账新造 | 数据根仍单写者 |
| 部署 | 单 exe | 多 exe+版本矩阵+看门狗 | 渐进，每步可回退 |
| 对齐约束 | 已满足 | 违背「无 net.Listen、data root 单锁」现状，需先补大量基建 | 沿 remote-agent L0→L1→L2 阶梯，门禁驱动 |
| 主要风险 | 边界腐化（靠 e2e 机械门禁防） | 过度工程：为「拆」而付 IPC/调度/部署全成本 | 中间态拉长（靠门禁与退出条件控制） |

**推荐 C（混合架构）**，与 global-review 结论一致。退出条件：G1 若证明执行路径远端化收益不抵成本，退回纯 A 并保留契约硬化成果——两方案在 G0 之前是同一段路。

## 5. Seele v0.3.4 可复用组件清单（基于实际依赖版本）

版本依据〔源码〕：`go.mod:25` 固定 `Seele v0.3.4`，无 replace；源码位于模块缓存 `github.com/!red!huang-0622/!seele@v0.3.4/`。「使用证据」列为 Seelex 侧 import 命中点。

| Seele 包 | 能力（无产品语义） | API/源码依据 | Seelex 使用情况 | 兼容限制/缺口 |
|---|---|---|---|---|
| `types` | 跨模块数据模型 + 最小 Completer 接口 | types package | 大量（`session/ports.go:18` 等） | — |
| `session` | ChatLoop（Chat/ChatStream，组装 history/telemetry） | session package | 在用（`main.go:19`） | 会话历史所有权在 Seelex 侧（seele-v2 文档 §存储模型） |
| `agent` | LLM client + 工具 runtime 大装配（NewWithComponents） | agent package | 在用（`seelebridge/request_log.go:26`） | — |
| `telemetry` | Hook/Trace/Metric/Audit 原语；MemoryTracer、**OTelTracer**、三类 Sink 插拔口 | `telemetry/README.md:14,23`、`telemetry/otel.go` | MemoryTracer 在用；**OTelTracer/Sink 未接线** | MemoryTracer append-only 无界（历史基线 P1） |
| `accountpool` | 泛型 P2C 租约池 New[T]/Register/Acquire/Resolve/Enable/Disable | `accountpool/pool.go:97-205` | 在用（`seelebridge/runtime.go:319`） | — |
| `seelectx/storage` | 分片存储 | seelectx/storage | 在用（`sessionstore/sessionstore.go:24`） | Seele 缺口 5 条（项目级知识、栈原语、LoadTail、段压缩、单元化 History）由 Seelex 适配器隔离（seele-v2-runtime-architecture.md） |
| `seelectx/cache` | 会话/响应缓存、文件后端 | seelectx/cache | **未复用** | 与 sessionstore 释放面策略的叠加关系未评估 |
| `seelectx/tracer` | SimpleTracer 树形追踪 | seelectx/tracer | 未用（走 telemetry.MemoryTracer） | 二选一，避免双 tracer |
| `event` | 可序列化运行时事件契约 + Sink 投递 | event package | 在用（`sessionstore/event_store.go:10`） | — |
| `jobs` | 异步作业根能力 dispatch→observe/fetch/kill/retire | jobs package | 在用（`seelebridge/teamwork/teamwork.go:24`、workunit contract） | 两张作业表已裁决「像而不并」（workunit-ports-and-assembly.md） |
| `workplan`（+`core/*`、`codec`、`runtime`） | DAG 执行内核 + 拓扑序列化 | workplan 子包 | 部分（`sessionstore/checkpoint_store.go:8` 用 core/types） | **codec/runtime 主体未复用**，Seelex 自有 plan 执行在 seelebridge；重造前须先评估替换 |
| `tools/mcp` | MCP provider + breaker | tools/mcp | 在用（`mcpstack/breaker.go:7`） | 无自动重启（§审查 G3） |
| `tools/microhub` | MCP hub（hub.go/router.go） | tools/microhub | **未复用** | 多 MCP server 聚合场景优先评估它，不在 Seelex 重造 hub |
| `tools/permission` | 权限门 | tools/permission | 在用（`main.go:20`） | — |
| `limits` | RPM/TPM/并发/429 重试装配到 ChatCompleter | limits package | **未复用**（Seelex 自有 `runtime_limits_overlay.go`） | 重复嫌疑，按 §8 纪律评估合并方向：能结构满足就删自有 |
| `config`/`errors` | YAML 配置读取、统一错误信封 | config/errors | 未用（Seelex 自有 bootseed/config 链） | 四链配置是 Seelex 产品语义，不强行替换 |

原则：seelebridge 是适配层，**优先复用 Seele，不在 Seelex 重造引擎能力**（AGENTS.md §1）；上表「未复用」四件（cache、microhub、limits、workplan codec/runtime）是下一阶段复用评估清单，评估结论要么是替换为 Seele 实现并删除自有（列删除清单），要么写明「为什么像却不并」。

## 6. Seelex / seelexdot / seelexboss 定位

事实〔源码+文档〕：`seelexdot`/`seelexboss` 在**代码与脚本中零命中**，仅出现于两份文档——`docs/2026-10-13-seelex-dot-a2a-star-topology/README.md`（用户原词列表，未裁决概念）与 `docs/2026-10-15-product-architecture-pre-design/global-review.md`（明示 Dot 是未裁决扩展方向、Boss 是否等于 main Seelex controller 是开放问题 D02）。**仓库内不存在对应代码实体，不能当已实现产品或既定二进制设计。**

文档不足以定案，给出备选模型（沿用 global-review，不新增第三套叙事）：

| | 模型 A（建议优先讨论） | 模型 B（暂缓） |
|---|---|---|
| Seelex | 产品主干 + 共享底座（个人本地 Agent） | 共享底座 + 个人端 |
| seelexboss | 主实例中 controller 角色的**昵称**，不是独立二进制 | 独立组织控制产品（需多租户/组织身份/审计/凭据管理，范围显著扩大） |
| seelexdot | 同一底座编译出的**可选执行实例**（G2，模型 A 下是 seelex 的一种运行形态而非新产品） | 独立授权执行产品 |
| 共享底座 | application/contract/dto、model、sessionstore、seelebridge、internal 零依赖叶子 | 同左 + 新增组织层 |

两种模型的共同约束：共享引擎与执行契约（dto/model 层）；**禁止**在根 Runtime 写 `if product == boss/dot` 按产品分支——产品差异只允许出现在装配表（composition root 的 flag/entrypoint），符合 AGENTS.md §8 第 5 条「层差异只出现在装配表与策略调用点」。裁决前，多二进制设计只承诺到 §3.3 的 exec-node；dot/boss 仅保留 G2 占位。

## 7. 落地门禁（〔提案〕，与 global-review 第 8 条对齐）

1. **G0 边界硬化**（不改进程数）：wire 契约包（dto 同级）定义 exec 命令/事件/取消/租约；e2e 机械门禁钉住「契约包零依赖」与「无按产品/按层分支」；性能基线按 perf 文档 §4 跑一轮存档。
2. **G1 exec-node**：同一套契约先在进程内 transport 跑通（fake transport e2e），再换 stdio/pipe transport；验收含取消延迟、故障注入（node 崩溃/主进程重启对账）、traceparent 端到端。
3. **G2 dot**：仅当产品模型裁决完成；另立项，附授权/审计/各自 data root 设计。
4. 每步交付按 AGENTS.md §8 列「删除清单」（删掉的重复实现 → 新唯一位置），证据是结构性（编译期断言/只剩一份），不只是用例全绿。
