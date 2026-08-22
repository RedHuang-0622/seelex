# Tech Leader 质询与解答

> 对应工作包：[README.md](README.md)。本节以 Tech Leader 视角对代码
> 设计提出质询，并基于当前工作树的代码事实逐一解答。风险分级：
> P1（应尽快处理）、P2（计划内改进）、P3（观察/长期演进）。

## Q1（P1）：`sessionstore/sessionstore.go` 有 2447 行，仓库自己的
`MEMORY.md` 就定义“单文件 >500 行且职责混合”为上帝文件判据，为什么还能
保持这个形态？要不要拆？

**质询背景**：`Repository` 契约、`Router`、JSON/SQLite/PostgreSQL/Redis
四后端、配置归一化、旧版迁移、事件库都堆在同一个文件里，是当前全仓库
最大的源文件（远超第二名的 `main.go` 1143 行）。

**解答**：

- 事实：该文件内部确实按“契约 → Router → 各后端”分段，函数职责尚算
  清晰，且有 README 说明；但 2447 行已显著超出可审查阈值，任何
  SQL/Redis/JSON 的迁移改动都需要在这个大文件里定位，diff 噪音大，
  新人上手成本高。按仓库自己的判据，它已经是上帝文件。
- 为什么还能稳定工作：测试覆盖充分（`sessionstore_test.go`、
  `durable_history_test.go`、`event_store_test.go` 等约 7.5k 行），
  且 Router 切换/原子语义经过 race 与迁移测试；大文件不等于坏行为，
  但它降低了后续演进的安全边际。
- 建议（P1）：按后端拆分为 `router.go` + `repository.go` +
  `json_backend.go` + `sql_backend.go`（SQLite/PG 共用迁移与占位符
  逻辑）+ `redis_backend.go` + `legacy.go` + `event_store.go`，
  先提取共享 helper（原子写、hash tag、迁移探测），再移动各后端
  方法，最后同步 [sessionstore/README.md](../../sessionstore/README.md)
  的文件索引。拆分本身不改行为，风险主要在机械移动时的丢失。

## Q2（P1）：`main.go` 1143 行，虽然名义上“只负责装配”，但大量产品
工具的 JSON Schema 与 handler 直接内联在 main 里，这还是组合根吗？

**质询背景**：`registerPluginSelfTools`、`registerContextReadTools`、
`registerMCPLoadTool` 等函数把完整 schema map + 闭包 handler 写在
main.go，组合根正在向“工具注册大杂烩”膨胀。

**解答**：

- 事实：main.go 的职责仍是装配与接线（没有业务状态机），但工具族
  注册的“产品内容”已经超过装配本身。以 `plugin_create`/`mcp_create`
  为例，每个工具 30-60 行的 schema 定义放在组合根，不属于装配决策。
- 建议（P1）：把“产品工具族”下沉到独立包（例如
  `internal/producttools/`），按族拆分文件（plugin_self_tools.go /
  context_read_tools.go / mcp_tools.go / project_tools.go），main.go
  只保留 `producttools.RegisterAll(runtime, deps...)` 一行调用。
  另外 schema 用手写 map 重复度高，可考虑从 `docs/gui/schemas/`
  的 JSON Schema 文件生成或直接引用。

## Q3（P2）：GUI 前端 `gui/frontend/dist/` 直接是源码（56KB `app.js`、
`patch_app.py` 打补丁），没有 `src/` 与构建链。这是隐患吗？

**质询背景**：通常 `dist/` 是构建产物；本仓库把 dist 当作唯一事实源，
且有 `patch_app.py` 对生成文件做后处理，缺少标准前端工程结构。

**解答**：

- 事实：`dist/` 内 JS 有 node --test 单测（约 20 个 .test.mjs），
  GUI README 与 CI 明确维护这套“dist 即手写产物”的流程；这不是
  意外，是刻意选择（避免引入 node 构建链依赖）。
- 问题：无构建链意味着无 minify/bundler/源映射；`patch_app.py` 这类
  后处理是脆弱的（生成器升级可能破坏补丁）；56KB 单文件难以做
  模块化演进；diff 噪音大。
- 建议（P2）：至少引入 `gui/frontend/src/` + 一个可复现构建脚本
  （如 esbuild 单步），dist 作为产物提交但由脚本生成；或保持 dist
  手写但拆分为多文件并通过 import map 组织。风险低、收益长期。

## Q4（P2）：`PathGate.normalizePath` 对所有平台无条件 `strings.ToLower`
（[security/pathgate.go](../../seelebridge/security/pathgate.go)），
Linux/macOS 大小写敏感文件系统上会不会误判？

**质询背景**：zone 配置（如 `prefix: docs`）本意是小写约定；但实现把
用户路径也强制小写，在大小写敏感系统上 `Docs/secret` 会被当作
`docs/secret` 命中 zone。

**解答**：

- 事实：`normalizePath` 对每个 path 段执行 `strings.ToLower(p)`，
  与平台无关。后果是策略层（PathGate）的 allow/deny 在 POSIX 上
  可能出现“大小写不同但被视为相同”的误匹配。
- 影响面：PathGate 是策略层，物理 containment 由 ProjectScope 保证
  （canonical + EvalSymlinks，不逃逸即安全）；所以这是策略正确性
  问题而非越权漏洞——例如 `Docs/` 意外落入 `docs` zone 的 allow，
  或真实 `docs` 文件被 deny。
- 建议（P2）：按 `runtime.GOOS` 分支——Windows 保留小写归一化，
  POSIX 保持大小写敏感；同时给 zone 匹配增加规范形式（clean 后
  的绝对/相对路径）而不是字符串前缀。当前测试覆盖了路径遍历与
  控制字符，未覆盖大小写语义，需补平台差异测试。

## Q5（P2）：Application 用一把 `Service.Mu`（RWMutex）保护整个
Snapshot，长任务期间锁外回调与锁内状态机交错。锁粒度是不是太粗？

**质询背景**：`core/internal/state.Core.Mu` 是共享状态内核；`startChat`
锁内构建状态，`runChat` 锁外跑 ChatStream，事件回调（onChunk/工具钩子）
再回到应用层更新 Snapshot。

**解答**：

- 事实：这是刻意的“单写者”模型——同一时刻只有一个 ChatStream 在跑，
  Snapshot 变更必须原子呈现给订阅者；锁内不做 IO 的原则在多处注释
  和代码中成立（发布事件、持久化都在锁外）。
- 代价：锁粒度粗导致任何 Snapshot 读（`Snapshot()`）与高频 bump
  （如调度器 RefreshRuntimeSnapshot）存在竞争面；多前端同时订阅时
  会放大锁等待。当前单会话串行模型下可接受，但已是
  `docs/gui/architecture.md` 中 v2 多 SessionActor 规划的直接动因。
- 建议：v2 前不要盲目细分锁（会破坏 Snapshot 原子性）；可以在
  `Snapshot()` 路径改为无锁不可变指针交换（atomic.Pointer + COW），
  减少读者锁竞争。`race_test.go` 已覆盖主要并发路径，改造需同步
  race 验证。

## Q6（P2）：EventHub 全局单调 seq + 慢消费者丢事件换 resync；多个
订阅者（TUI + GUI 同时连）进度不一致怎么办？

**质询背景**：`deliver` 在缓冲满时清空并注入 `EventResyncRequired`，
发布者不阻塞；不同订阅者可能处于不同 seq。

**解答**：

- 事实：协议以“Snapshot 为基线，Event 为增量”为前提；每个订阅者
  独立持有 lastSeq，收到 resync 后重拉 Snapshot、按新 revision floor
  继续。订阅者之间进度不一致是被设计接受的——慢订阅者退化为
  全量刷新，不影响快订阅者。
- 边界：`seq` 是进程内全局单调；若未来做跨进程 HTTP adapter，
  需要按 scope 独立 seq 或引入进程级序列源，否则重放/分页语义不成立
  （v2 规划已列）。
- 建议（P3）：为 `Event` 增加 `scope` 字段与 per-scope seq 是协议
  演进方向；当前 TUI/GUI 同进程共存没有问题。

## Q7（P2）：Application 与 Runtime 之间有双向投影
（`PublishRuntimeProjections` / `RefreshRuntimeSnapshot`），会不会形成
循环更新？

**质询背景**：main.go 装配了 `SchedulerObserver: app.RefreshRuntimeSnapshot`，
Application 又 `PublishRuntimeProjections` 把可见性/父证据写入 Runtime。

**解答**：

- 事实：不会形成无界循环。Application → Runtime 方向是**只读值拷贝**
  （`RuntimeVisibilityProjection`/`ParentEvidenceProjection`），Runtime
  只读自己的缓存，不回调 Application；Runtime → Application 方向只有
  显式事件源（plan 节点 channel、task 变更 channel、调度器 observer），
  且 `RefreshRuntimeSnapshot` 只发布 `runtime.changed` 事件、不触发
  Runtime 写回。每条边都是单向触发器，闭合后无自激。
- 风险点：调度器 tick 频率较高时（200ms），状态变化可能频繁 bump
  revision；当前有界（事件按需发布），但如果未来把心跳也做成事件，
  需要节流。

## Q8（P2）：`fork_subagents` 在外层工具调用内**同步等待整个 DAG**
完成，工具超时/取消如何传播？大结果怎么读回？

**质询背景**：fork 工具构造 `start → N agent → summary` 后同步
`runPlan`，外层工具在 DAG 结束前不返回。

**解答**：

- 事实：取消经 ctx 级联（fork.Tool.Handle → planExecutor → workplan
  runner → 节点会话）；超时受工具调用超时（`ToolCallTimeout`）与
  `ForkTimeoutSec` 控制。README 与 seelebridge README 均明确记录
  “Waiting for output…” 是预期行为，不能据此判定死锁。
- 大结果边界：summary 拼接前驱输出，可能超过 provider 单条上下文
  预算；因此必须经 `read_tool_result`（分页）或节点详情读回原文，
  外层 `final_output` 截断时不能转述结论。仓库已把这一点写成
  审计约束（“不能凭外层结果声称已审查完整子代理产出”）。
- 建议（P3）：为 fork 结果引入“有界摘要 + 可分页结果引用”的正式
  交付契约（README 已列为待办），把截断风险从运行时提示提升为
  协议字段。

## Q9（P3）：双轨事件（EventHub 快照轨 + sessionstore 事件库事实轨）
为什么不干脆统一？

**质询背景**：plan/节点执行事实先走 `event.Sink` 落库，再投影进
Snapshot；前端增量另走 EventHub。同一事实有两个投影路径。

**解答**：

- 事实：两轨职责不同——快照轨是进程内低延迟增量（可丢，resync
  兜底），事实轨是持久化审计/恢复（不可丢）。长期形态（
  [06-unified-event-store-decision](../2026-08-14-decoupling/06-unified-event-store-decision.md)）
  是“单一追加日志源 + 分层投影”，当前双轨是收敛期形态，事件关联
  字段（sessionID/nodeID 补全）已在 `events.go` 处理。
- 建议（P3）：统一时保留“内存投影缓存 + 持久化事件库”的物理分离，
  但让投影由事件库派生（而非两处独立写入），消除顺序不一致的可能。

## Q10（P2）：插件 include/exclude 是通配符匹配，`include: ["git_*"]`
会不会把未预期的 `git_*` 工具也暴露给模型？

**质询背景**：`matchToolPattern` 用 `path.Match`（`*` 可匹配任意字符，
包括 `_`），插件作者声明的 include 决定了模型可见面。

**解答**：

- 事实：这是**声明式信任模型**——插件 manifest 是机器契约，作者明确
  列出可见工具前缀；`git_*` 暴露所有 `git_` 前缀工具是作者意图，不是
  实现漏洞。真正的防护线在别处：
  - 子代理被排除全局状态工具（plan 族、task 终态、fork_subagents），
    见 `tools/policy.go` 的 `nodeScopeExcludedTool`；
  - 隐藏工具即使被模型构造调用也会被 `ErrToolNotVisible` 拒绝；
  - 文件/Shell 工具始终受 ProjectScope + PathGate + PermissionGate
    约束，与插件可见性正交。
- 建议（P3）：在 `plugins/<name>/plugin.md` 契约中增加工具名白名单
  校验（加载时拒绝未注册工具前缀），把“写错 include”从运行时
  暴露提前到加载期报错。

## Q11（P2）：README 自述 TUI 覆盖率仅 26%，GUI 以 node --test 单测
为主，前端交互回归是否足够？

**质询背景**：全仓 Go 测试 163 个文件；但 TUI 覆盖率低，GUI 无
Playwright 自动化（docs/gui 中属于规划）。

**解答**：

- 事实：TUI 26.1% 说明前端交互层（键盘映射、粘贴折叠、审批面板）的
  回归主要靠手工冒烟；GUI 协议有 `protocol.test.mjs` 覆盖 reducer
  语义，但 DOM 交互（弹窗、拖拽、分栏）没有端到端自动化。
- 影响：核心业务（Chat/Plan/审批/存储）测试充分，风险集中在 UI 壳；
  Alpha 阶段可接受，但发布前应补：
  - TUI：对 `handleKey`/`handleEnter` 的表格驱动测试（当前
    `tui_test.go` 已有基础）；
  - GUI：Playwright 冒烟覆盖“提交 → 流式渲染 → 审批 → 工作表格”。
- 建议（P2）：把 UI 交互测试纳入 CI 准入，至少覆盖主链路。

## Q12（P3）：配置 fallback（`config/` → 根目录；accounts.yaml 在
exe 目录 → cwd）会不会把真实账号/DSN 带到错误位置？

**质询背景**：`accountsPath()` 优先 exe 旁 `config/accounts.yaml`，
回退 cwd；`firstExisting` 用于 seelex.yaml/seele.yaml；2026-08-12
曾发生 dist 内配置被 clean 误删的事故。

**解答**：

- 事实：fallback 是开发/部署兼容设计（go run 时 exe 在临时目录，
  需要 cwd 配置；正式部署放 exe 旁）。安全面已收敛：
  - `sessionstore.Config.Safe()` 对 GUI 隐藏 DSN（只显示 configured）；
  - dist 构建通过 `LOCAL_CONFIG` 显式路径复制配置（AGENTS.md §5）；
  - MEMORY.md 铁律要求清理前查进程、备份、中文预警。
- 残余风险：`firstExisting` 若在 cwd 意外发现陈旧配置会静默使用；
  建议（P3）为 `-config` 增加显式 flag 并让命令行优先级高于 fallback，
  减少“用错配置”的可能。

## 质询小结

- P1 两项都是**结构债**（sessionstore 上帝文件、main.go 工具注册内联），
  不影响当前正确性，但限制演进速度。
- P2 中 PathGate 大小写是唯一明确的**跨平台语义缺陷**，建议优先修；
  其余（前端构建链、TUI 覆盖、协议演进）是投入排序问题。
- P3 项（事件统一、fork 交付契约、配置显式化）与既有 roadmap 方向
  一致，可并入迭代计划。
