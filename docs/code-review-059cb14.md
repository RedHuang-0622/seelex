# 代码审查报告 — commit 059cb14

> 提交: `059cb14b66231d5e70f8d184fdcccf5663e890de`
> 标题: `fix(actor): deadlock fix, smoke upgrade, actor message boundary, cli memory plan`
> 审查范围（有界，≤15 条发现）: ① 死锁修复（父证据 + merge-back 不在 plan_run 期间触碰主会话、ExportSnapshotFromData 注入、merge-back mailbox 排队）；② seelebridge/actor.go ContextExchanger 消息边界 + main.go 接线；③ merger.MergeBack goal 继承；④ 会话恢复提速（ReadRange limit<=0 只取总数）。
> 方法: `git show --stat` / `git diff` 定位变更，再逐一打开实际源文件核实（不以 diff 单独作证）；对受影响包执行 `go test -count=1`。
> 基线: HEAD == 059cb14，工作区干净（仅 untracked docs/plan 新增）。

## 测试结果

| 包 | 结果 |
| --- | --- |
| `./sessionstore/` | ok (cached) |
| `./seelexctx/...`（含 merger/provider/snapshot/compactor） | ok (cached) |
| `./seelebridge/` | ok `-count=1` 2.063s |
| `./application/core/` | ok `-count=1` 1.093s |

所有受影响包编译并测试通过。

## Confirmed 问题表

| # | 严重度 | 文件 | 符号 | 证据 |
| --- | --- | --- | --- | --- |
| 1 | **High** | sessionstore/sessionstore.go:1677-1678 | `sqlRepository.ReadRange` | 新语义"limit<=0=只取总数"只实现于 `jsonRepository`（:699-700）与 `redisRepository`（:1206-1212），`sqlRepository.ReadRange` 仍为 `if limit <= 0 \|\| offset < 0 { return "invalid range" }`。`loadHistoryTailWindow`（application/core/session_history.go:199）先以 `(0,0)` 探测 → PostgreSQL 后端直接报错 → `resumeSession` 对无 v2 record 的旧格式会话返回错误（session_history.go:67-70），会话无法恢复；有 record 的会话虽容错（history=nil）但尾部窗口读完全失效。三后端语义不一致，属功能回归。 |
| 2 | Medium | sessionstore/sessionstore.go:1206-1212 | `redisRepository.ReadRange` | total-only 探测实现为 `messages, err := repository.Read(ctx, key)` 全量读后只取 `len(messages)`。即会话切换加速（"先探 total 再尾部窗口读"）对 redis 后端是**全量解析两次**（探测一次 + 窗口一次），性能目标未达成；注释只保证语义一致。 |
| 3 | Medium | seelexctx/bridge.go:96-108；seelexctx/provider/trace.go:46 | `ExportSnapshotFromData` / `TraceProvider.Export` | 快照的 Findings/Decisions/TokenEstimate 来自 `Query(telemetry.Query{Limit: 200})`——`telemetry.Query`（Seele/telemetry/contracts.go:108-118）**无 session 过滤字段**，`MemoryTracer.Query`（Seele/telemetry/memory.go:306-309）在 TraceID=="" 时遍历全部 trace。因此多子代理 plan 中每个节点的 ParentEvidence 与 merge-back 的 Findings/Decisions 都取自**进程级全局遥测**（含其他子代理/其他会话事件）：父证据与合并结果互相串扰，并行子代理的决策/发现会重复累积。sessionID 仅写入快照 SourceSessionID，不参与过滤。 |
| 4 | Medium | application/core/chat.go:59-64；application/core/service_input.go:24-36 | `startChat` / `injectPendingSubagentContexts` | 已确认事实：① merge-back 内容以 **user 角色** `AppendHistory` 注入 **engine history**，注入点位于 startChat 提交的当前 user 消息之后、`go runChat` 之前——模型输入（prepareExecutionContext 读 `Engine.History()`，context_controller.go:54）中 merge-back 块位于历史尾部、紧邻当前轮 user 输入；② 注入**只进 engine history，不进镜像 `snapshot.Conversation`**（viewCoordinator.appendMessageLocked 只写镜像，service_snapshot.go:71-77），GUI 显示与模型实际所见不一致；③ 按设计（注释"下一次 ChatStream 开始前注入"），本轮 plan_run 完成后同一轮内 assistant 的回复**看不到**子代理结果，结果只影响后续轮次。影响推断见 Hypothesis H1。 |
| 5 | Medium | application/core/session_history.go:120（name := sessionTitleFromHistory(history)）；application/core/session_scope.go:114-124 | `resumeSession` / `sessionTitleFromHistory` | 提交前 `history` 是全量历史（loadSessionHistory），现在 `history` 是**尾部窗口**（≤HistoryWindow 条）。对无 v2 record 的旧格式会话，`sessionTitleFromHistory` 取窗口内第一条非空 user 消息作为标题——当会话总条数 > HistoryWindow 且首条 user 消息落在窗口外时，恢复后的会话标题从"真实首条消息"变为"窗口内首条消息"，**标题回归**（行为变化）。 |
| 6 | Low | seelebridge/agent_node.go:52,101；seelebridge/subagent_audit_test.go:144-145 | 注释引用 `SetNodeParentEvidence` / `SetSubagentContextSink` | 这两个方法已删除，由 `ContextExchanger`（actor.go）取代；仓库内已无 `func (r *Runtime) SetNodeParentEvidence` / `SetSubagentContextSink`（grep 确认仅注释与测试注释引用）。过期注释会误导后续维护者接线方式。 |
| 7 | Low | main.go:161-167 | `truncateSnapshotGoal` | `content[:maxGoalChars]` 按**字节**截断 200 字符，多字节 UTF-8（中文）输入会在第 200 字节处切断 → 快照 Goal 产生无效 UTF-8 后缀，`snapshot.Format()` 渲染出现乱码（装饰性问题，非崩溃）。 |
| 8 | Low | sessionstore/sessionstore.go:651-702；application/core/session_history.go:197-210 | `readRangeWindowed` + `loadHistoryTailWindow` | 探测（0,0）与窗口读（total-window, window）是两次独立加锁/读文件操作，非原子；若两读之间会话并发增长，total 与窗口可能错位。恢复场景通常无并发写，影响极小，列入观察。 |

## Hypothesis 列表（待验证）

| # | Hypothesis | 验证方法 |
| --- | --- | --- |
| H1 | merge-back 以 user 角色注入历史且位于当前 user 消息之前（历史尾部），模型可能把子代理结果块误读为历史中的普通用户轮次，或将子代理输出当作"上一轮用户要求"而偏离对当前用户问题的回答；且 GUI 不可见导致用户无法追溯回复依据。 | 运行 manual_smoke_test 的 merge-back 场景：plan_run 后同一轮与下一轮分别观察 assistant 回复是否围绕子代理输出；对比把注入点移到 prepareExecutionContext 之后 / ChatStream 之前、且同步写入镜像后的行为差异。 |
| H2 | 遥测全局污染在并行子代理下会导致 Findings/Decisions 在父会话中重复累积（节点 A 的 merge-back 含节点 B 的事件，后续节点再次合并又含 A+B…）。 | 构造含两个并行 agent 节点的 plan，两个子代理各产生不同 Decisions，检查第二个节点的 ParentEvidence 与最终父会话合并内容是否包含第一个节点的事件；或为 telemetry.Query 增加会话/时间窗过滤后对比去重效果。 |
| H3 | PostgreSQL 后端 + 无 v2 record 的旧会话无法恢复（"invalid range"），若生产未启用 sql 后端则降为 Low。 | 配置 `backend: postgres`，写入无 record 的历史会话后调用 `ResumeSession`，观察报错；同时确认配置文件中 backend 取值。 |
| H4 | 旧格式（无 record）且 > HistoryWindow 条消息的会话，恢复后标题改变。 | 构造 >window 条消息的无 record 会话，ResumeSession 后断言 `snapshot.Session.Name` 与真实首条 user 消息标题一致（当前实现会不一致）。 |
| H5 | 本轮 plan_run 的子代理结果不进本轮 assistant 回复是设计取舍还是缺陷：用户预期"跑完计划即得到基于子代理结果的回答"。 | 端到端冒烟：用户输入"执行计划 X"，观察本轮 assistant 回复是否包含子代理产出；若否，确认该行为是否为产品预期，评估是否需要在本轮终态前补注入。 |

## 亮点

1. **死锁修复架构正确**（核心诉求达成）：
   - 消息进（ParentEvidence）走 application 镜像：`app.Snapshot()` → `snapshotView()` 仅 `service.mu.RLock()` + clone（service_snapshot.go:20-27），再经 `ExportSnapshotFromData`（bridge.go:96）从无锁数据面 + telemetry 构造新值对象——**全程不触碰被 ChatStream 持锁的主会话**；`TraceProvider.Export` 只读 telemetry MemoryTracer（memory.go:300 起 RLock），无会话依赖。
   - 消息出（MergeBack）只做 mailbox 排队：`AppendSubagentContext` 取 `service.mu` 短临界区追加 `pendingSubagentContexts`（service_input.go:14-21），注入由下一次 `startChat` 在锁外完成（chat.go:64）。
   - 循环等待分析：runChat 调 `Engine.ChatStream` 时**不持有 service.mu**（chat.go:71-86），无任何 goroutine 持 service.mu 等待主会话完成 → 无死锁环。actor.go 接口注释如实记录了 2026-08-02 冒烟 19 分钟死锁教训。
2. **ContextExchanger 消息边界清晰**：actor.go 新增接口（ParentEvidence 值对象一次性、MergeBack 无锁投递），nil 降级不报错；main.go 单点接线（main.go:130），`SetContextExchanger`/`contextExchanger` 用 `exchangerMu` 双锁保护指针。
3. **merger.MergeBack goal 继承语义正确**：`if parent.Goal == "" && child.Goal != ""` 父目标主导、父空继承子目标（merger.go:47-49）；mergeBack 在无父证据时构造空父快照，子代理 Goal（节点输入）正确继承；copy-on-write（切片拷贝 + `m.mu` 串行化）保证并发安全，且每个节点经 `ParentEvidence()` 各自拿到新快照对象，无跨节点共享突变。
4. **jsonRepository 窗口读优化真实有效**：manifest 计数存在时按 offset 定位 shard，只解析覆盖窗口的 1-2 个 shard（8.4MB/6 shard → 1 shard）；`(0,0)` 探测只读 manifest 不解析任何 shard（sessionstore.go:698-700），并有 `TestJSONRepositoryReadRangeTotalOnly` 覆盖（250 条/3 shard）。
5. **会话恢复提速落地干净**：`loadHistoryTailWindow` 先探 total（limit=0）再取尾部窗口（session_history.go:194-210），`TotalMessages`/`HistoryOffset`/`HasMoreHistory` 语义保持（offset = total-window，clamp 0）；有 v2 record 时以 record/transcript 为准，窗口读仅服务旧格式回退。
6. **tasklist 节点完成事件对齐**：check-node 现在也写 `appendPlanNodeEvent(completed)`（task_service.go:353-357），与 plan_run 事件同构，详情页数据源一致。

## 建议

1. **补齐 sqlRepository.ReadRange 的 limit<=0 语义**（High）：改为 count 查询（或与 json 一致走元数据），三后端契约统一；并补一个跨后端 ReadRange(0,0) 一致性测试（现有测试仅覆盖 json）。
2. **redisRepository total-only 避免全量读**：用 LLEN/计数命令或后端元数据返回总数，否则 redis 后端会话切换加速无效。
3. **TraceProvider 增加会话/时间窗过滤**：`telemetry.Query` 缺 session 过滤字段是根因；至少按 `ExportedAt` 前的时间窗（From/Until）过滤，理想是让 MemoryTracer 支持按 correlation/session 维度查询，避免并行子代理串扰与 Findings 累积。
4. **merge-back 可见性与语义**：注入内容同步写入镜像（或至少以 system/工具角色带标记注入），保证 GUI 与模型输入一致；评估是否在本轮 plan_run 结束后（终态前）补注入，让本轮回复也能引用子代理结果（见 H5）。
5. **标题回归**：旧格式会话标题应优先从 v2 record 取（已覆盖 hasRecord 分支），对无 record 会话可考虑在恢复时用首个"真实" user 消息（通过一次性全量探测标题字段）或接受窗口内标题并在 CHANGELOG 注明。
6. **清理过期注释**：agent_node.go / subagent_audit_test.go 中 `SetNodeParentEvidence`、`SetSubagentContextSink` 引用改为 `ContextExchanger`。
7. **truncateSnapshotGoal 按 rune 截断**（`[]rune` 或 `utf8` 边界），避免 UTF-8 截断乱码。

## 边界说明

- 审查以 HEAD==059cb14 实际源码为准，未修改任何业务文件（本报告为唯一交付物）。
- `go test` 在 4 个受影响包全部通过；未运行全仓 `go test ./...`（时间成本，且无关包未在提交内变更）。
- 遗留的 `review_scratch/` 临时 diff 文件已删除。
