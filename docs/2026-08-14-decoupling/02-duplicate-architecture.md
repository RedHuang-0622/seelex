# 重复架构 / 双轨兼容盘点

日期：2026-08-14
性质：只读调研

## 1. 输入队列双轨（application/core）

**现状**：同一"会话运行中排队输入"有两个切片 + 两个消费点：

- `service.inputQueue` 与 `service.deferredInputQueue`（`service_state.go`）；
- 消费点 A：`runChat` 结尾（`chat.go:179-190`）合并 `deferred + input` 起下一轮
  （Session-backed 路径：`OnIterationComplete` 看到队列非空 → `return false`）；
- 消费点 B：`OnIterationComplete` 内部直接 drain + `AppendHistory` + `return true`
  （旧装配路径，`chat.go:1325-1348`）；
- 还有 `drainQueuedInputsAfterLoop`（`service_input.go:48`）在中间做提升。

**问题**：两条路径并存是因为"新 Session 装配"与"旧装配"的锁语义不同
（Session 锁内不可重入 AppendHistory）。两条路径各自有测试，改队列语义要双改。

**收敛方向**：
- 只保留 Session-backed 单路径（`return false` + runChat 单点消费）；
- 旧装配路径若已无生产调用方，删除；若仍需兼容，抽成同一 `Queue` 抽象的两个实现
  （一个在 hook 内消费、一个在 runChat 消费），但状态只有一份。

## 2. 子代理上下文双通道（merge-back 回主会话）

**现状**（`application/core/service_input.go`）：

- 通道 A：`service.pendingSubagentContexts`（`AppendSubagentContext` 旧路径）；
- 通道 B：Runtime 侧 `SubagentContextActor` mailbox（`Runtime.DrainSubagentContexts`）；
- `injectPendingSubagentContexts` 把两者**都** drain 后合并注入主会话
  （`service_input.go:28` 注释明说 "legacy local queue"）。

**问题**：注释已承认双轨；`AppendSubagentContext` 是历史兼容入口，生产路径走 mailbox。

**收敛方向**：删除 `pendingSubagentContexts` 与 `AppendSubagentContext`（或改为
`DrainSubagentContexts` 的薄转发），单一来源 = Runtime mailbox。

## 3. 事件体系双轨（workplan event.Sink ↔ session telemetry.Hook）

**现状**：

- workplan 执行事实走 `frameworkevent.Sink`（`plan/events.go` `planEventSink` →
  sessionstore 事件库；`runtime_plan.go:51` 注释）；
- 会话级 llm/tool 意图-效果走 `telemetry.Hook`（`internal/telemetry`：
  `LifecycleHook → DiagnosticHook → StageHook` 装饰链 → tracer）；
- 两条体系无桥（对应 `seele-gap-analysis.md` G5），sessionID 不参与 workplan 事件关联。

**问题**：同一运行的两类事实分别落两套存储/命名，统一检索与审计要写桥。

**收敛方向**：
- 短期（已实施，2026-08-15）：两个订阅入口与关联字段收进
  `seelebridge/events.go`，并为缺失 session_id 的事实轨事件补主会话关联
  （`Runtime.SetEventPersister` 持久化前补全）——seelex 侧自补，无需等待
  Seele 上游（旧「G5」前提已过时，见
  `docs/2026-08-14-probe-context-plugin/seele-gap-analysis.md`）。
- 长期：推荐形态 = **单一追加日志源 + 分层投影**（A 类 plan 事实全量落盘、
  B 类 llm/tool 遥测留内存 + 脱敏摘要落盘、统一查询接口跨源关联），
  详细取舍见
  [06-unified-event-store-decision.md](06-unified-event-store-decision.md)。

## 4. 插件管理器双轨（root plugin ↔ seelebridge/plugin）

**现状**：

- `plugin/`（根）：manifest/loader/skills/MCP 的契约层（`plugin/manager.go`），
  经 backend 接口调用 runtime；
- `seelebridge/plugin`：运行时工具可见性（Define/Undefine/Activate/Filter），
  不含 manifest 文件语义；
- 装配在 `main.go initPluginSystem`：`plugin.NewManager(loader, runtime, runtime, skills)`，
  runtime 同时充当 ToolBackend/MCPBackend/SkillBackend。

**问题**：两份"插件状态"（root 的 plugins map + runtime 的可见性 current），
切换时靠 backend 调用同步；热更新（本讨论主题）必须同时改两层。

**收敛方向**：root manager 保持唯一"插件定义"事实源；`seelebridge/plugin` 只做
执行面（backend 实现，不持有独立定义状态）；热更新时 diff 在 root、apply 走 backend
（见 04 §5）。

## 5. 存储双轨（sessionstore ↔ legacy 会话归档）

**现状**：

- 新路径：`sessionstore`（原子、项目作用域）；
- 旧路径：legacy 归档解码（`application/adapters/adapters.go:902`
  "decode legacy session archive"）、legacy cache 兜底（`core/session_history.go:70`）、
  `runtime.go:438` "未装配 Router 时保持框架内存 history，供测试和兼容调用方使用"。

**问题**：双份读写路径，坏数据/旧格式要长期维护。

**收敛方向**：确定 legacy 只读迁移截止（只读解码保留、写路径全部走 sessionstore）；
删除"内存 history 兜底"或标注仅测试可用。

## 6. 兼容别名（有期限）

- `stream_batcher.BatchSize` 是 `FlushSize` 的兼容别名（`core/stream_batcher.go:24`）；
- `dto.TodoItem` 兼容 TUI/旧契约（`dto/task.go`）；
- `plan/input_adapter.go` 接受一种旧 plan 形状（`mergeReferencedTopLevelNodes`）。

每项建议标注"移除窗口"，到期清理后 `rg "兼容"` 在非注释代码中归零。

## 7. 验收标准

- `rg "legacy local queue|兼容路径|兼容别名"` 只出现在迁移文档，不出现代码注释；
- 每条双轨只有一个写入点、一个消费点；
- 会话注入子代理上下文只经 `DrainSubagentContexts` 单一入口。
