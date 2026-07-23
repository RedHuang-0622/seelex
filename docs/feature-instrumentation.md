# Seelex 功能打点表

> 更新日期：2026-07-24
> 产品版本：v0.1.0-alpha.1
> 产品目标：构建可切换专业 Plugin 形态的工科全栈 Agent；TUI 面向高效工作，Wails GUI 面向毕设、课程项目和成果交付。

## 状态定义

| 状态 | 含义 |
|------|------|
| ✅ 已完成 | 已进入主链路，有自动化验证，可作为后续能力依赖 |
| 🟡 部分完成 | 主体存在，但有明确缺口、限制或缺少闭环验证 |
| ⛔ 阻塞 | 入口存在但受上游能力限制，当前不能提供承诺行为 |
| ⬜ 计划中 | 已明确目标和完成标准，尚未进入实现 |

## 北极星指标

| 指标 | 当前基线 | 阶段目标 | 度量方式 |
|------|----------|----------|----------|
| Plugin 切换成功率 | 单元测试覆盖成功与回滚 | fake backend 100%，真实 MCP ≥ 99% | 激活结果、回滚结果、最终一致性 |
| Plugin 切换延迟 | 未持续记录 | 无 MCP P95 < 100ms；含 MCP 按 server 单列 | `plugin.activate.duration` |
| 工具强制门控覆盖率 | 默认 manual 模式 + 白名单 | 高风险工具 100%，全工具 100% 有决策记录 | tool call 与 permission decision 对账 |
| Chat 事件可恢复率 | EventHub 支持 resync | 断线/背压场景 100% 可由 Snapshot 恢复 | Seq 跳号与 Snapshot 对账测试 |
| Application 覆盖率 | 67.4% | ≥ 75%，关键错误路径 ≥ 90% | `go test -cover` |
| TUI 适配覆盖率 | 6.2% | ≥ 40%，输入/交互/resize 主路径全覆盖 | `go test -cover` |
| 专业 Plugin 垂直闭环 | FreeCAD 技能 + 插件 manifest 存在 | CAD 1 个、Dev 1 个 | 真实任务从输入到产物的 E2E |
| 多前端协议兼容 | TUI/GUI 共用 Core，Snapshot/Event protocol v1 | 远程前端复用 v1 并建立兼容矩阵 | Bridge contract tests + reducer tests + 后续 JSON-RPC contract tests |

## 功能打点表

| 阶段 | 功能点 | 输入 | 输出 | 预计工作量 | 依赖 | 度量指标 | 完成标准 | 当前状态与证据 |
|------|--------|------|------|------------|------|----------|----------|----------------|
| 内核 | Headless Application Core | 用户输入、Engine 事件、Interaction 决议 | Snapshot、有序 Event、业务动作 | 已完成 | Seele Engine | application 测试通过率、Event Seq 连续性 | TUI 不持有 Engine/Plugin/Session；核心可无 UI 测试 | ✅ `application/`、`tui.AppController` |
| 内核 | 流式 Chat 生命周期 | Prompt、context cancel、chunk | Message delta、最终历史、错误状态 | 已完成 | Engine ChatStream | 首 token、完成率、取消延迟 | 并发 Chat 被拒绝；取消和错误不遗留 Running 状态 | ✅ 已有无 UI Chat 测试 |
| 内核 | EventHub 与重同步 | Snapshot revision、业务事件 | Seq 单调 Event、`resync.required` | 已完成 | 无 | 丢事件率、resync 成功率 | 订阅缓冲溢出后客户端可拉 Snapshot 恢复一致状态 | ✅ 已有背压测试 |
| 内核 | MCP 调用追溯中间件 | MCP 工具调用（所有 Server） | 可追溯、可回滚的调用记录 | 已完成 | `mcpstack/` 7 个文件 | 记录完整率、查询速度 | 每个 MCP 调用（含熔断事件）自动记录；支持 ByServer/ByTool/Latest 查询 | ✅ `mcpstack/` 18 个测试 |
| 内核 | 熔断事件通道 | 熔断器状态变化 | 异步 event → mcpstack 记录 | 已完成 | Seele breaker + `mcpstack/breaker.go` | 事件不丢失、不阻塞调用链 | 6 个埋点（opened/half_open/closed/recovering/recovered）经 channel→goroutine 写入 trace | ✅ `mcp_integration_test.go` 9 个测试 |
| 内核 | 存储接口解耦 | 写入、读取、列表、删除 | Storage 接口 + FileStore 实现 | 已完成 | `seelectx/storage` | 可替换实现、空路径 no-op | 框架只定义接口不提供默认路径；存储实现可切换 | ✅ `storage_test.go` 兼容测试 |
| 内核 | Effort 四级行为体系 | /effort lite/medium/high/max | Effort prompt 注入、MaxLoops 差异化（20/64/512/1024） | 已完成 | PromptStack、Engine | 等级切换次数、各等级完成率 | 四级指令注入、循环限制、模型选择策略（规划中） | ✅ `application/effort.go` 37 个测试 |
| 内核 | Plan/WorkPlan DAG 引擎 | plan_load JSON、邻接表 DAG | plan_run 逐节点执行 + 进度回调 + per-node 结果 | 已完成 | Seele v0.0.4 WorkPlan | 节点完成回调延迟、DAG 正确率 | 5 个 plan_* 工具完整可用；每节点完成后实时通知 TUI | ✅ `workplan_handler.go` 输出 per-node 状态 |
| 内核 | Plan 可视化面板 | PlanState Snapshot | TUI 四级 Effort Plan 面板（lite 单行 / medium 打点表 / high 节点树 / max 全框表） | 已完成 | `tui/plan.go` | 面板渲染正确率、实时更新延迟 | 各 Effort 下 Plan 进度随 plan_run 执行逐节点刷新 | ✅ `tui/plan.go` 4 套渲染器 |
| 内核 | 系统诊断命令 | /diag 命令 | Go 运行时、内存、Plugin 列表、Account 池、Skill 清单 | 已完成 | `application/diag.go`、Snapshot | 诊断信息完整性 | /diag 返回完整系统状态快照（≤ 1ms 收集） | ✅ 已接入 `/diag` |
| 内核 | TUI 周期性刷新 | tea.Tick 3s | Chat.Running 期间自动重绘时间显示 | 已完成 | Bubble Tea tickEvery | CPU 开销 | streaming 无新 chunk 时 elapsed 时间仍实时刷新 | ✅ `tui/stream.go` + `tui/tui.go` |
| 内核 | 粘贴折叠 | 多行/高频粘贴 | `[Pasted text #N +M lines]` 占位 | 已完成 | Bubble Tea textarea | 大文本不误发 | 3 行以上粘贴折叠为占位符，确认后展开 | ✅ `tui/tui.go` |
| 内核 | Skill 用户上下文 | `#skillname 需求`、活动 Skill 栈 | 条目化 Skill 名称/指令 + 完整原始问题 | 已完成 | Skill Registry、PromptStack、Chat queue | Skill 发送完整率、system prompt 隔离率、UI 还原率 | 有需求立即发送；空需求只激活；每轮携带活动 Skill；system prompt 不含 Skill 清单/指令；UI 只显示原文 | ✅ `application/input.go`、`application/skill_context.go`、`application/chat.go`；单元/集成/队列/历史测试 |
| 内核 | 配置路径固定化 | 无 | `<binary-dir>/config/accounts.yaml` 绝对路径 | 已完成 | os.Executable | 配置确定性 | 不移用工作目录，始终读二进制同目录 config/accounts.yaml | ✅ `main.go accountsPath()` |
| 内核 | Goal Skill 无限制 | #goal skill 加载 | SetMaxLoops(9999)，退栈恢复 effort 值 | 已完成 | EffortManager | Goal 任务不因 loop 限制中断 | Goal 复杂编排不受 MaxLoops 约束 | ✅ `application/input.go` |
| 形态系统 | Plugin Manifest | `plugins/*/plugin.md` | Plugin、工具过滤、Prompt、Skill、MCP 配置 | 已完成 | frontmatter loader | 加载成功率、schema 拒绝率 | 非法 schema/名称/路径被拒绝，合法定义可发现 | ✅ 7 个 Plugin 全部可用 |
| 形态系统 | Plugin 切换事务 | 当前 Plugin、目标 Plugin | 工具/Skill/MCP 的一致激活状态 | 已完成 | Seele Tool Holder、MCP | 成功率、回滚成功率、耗时 | 任一步失败后恢复旧 Plugin；并发切换串行一致 | ✅ 回滚与并发测试已存在 |
| 形态系统 | Plugin 可观测状态 | 激活请求、阶段变化 | activating/active/failed 状态与耗时 | 2—3 人日 | Plugin Manager、EventHub | 分阶段耗时、失败阶段分布 | UI/日志能定位 attach、tool、skill、detach 哪一步失败 | ⬜ 当前只有最终结果 |
| 形态系统 | CAD Plugin 最小闭环 | 自然语言 CAD 任务 | FreeCAD 模型、命令记录、可验证产物 | 10—20 人日 | Plugin/MCP、FreeCAD executor | 任务成功率、重放一致率、几何校验通过率 | 完成一个零件从描述→建模→导出→重放验证 | 🟡 Plugin.md + 7 个 CAD Skill 已具备，E2E 链路待验证 |
| 形态系统 | Dev Plugin 最小闭环 | Issue/需求、代码仓库 | 方案、代码、测试、Review 报告 | 7—12 人日 | Plugin/Skill、Shell/Git tools | 一次通过率、测试通过率、人工返工次数 | 完成一个真实仓库需求并生成可审查变更 | ⬜ 尚无专用 Plugin |
| Skill | 全局与 Plugin Skill | `SKILL.md`、Plugin 私有 Skill | 可查询、可补全、可激活的 Prompt | 已完成 | Skill Loader/Registry | 加载数、冲突数、激活耗时 | 全局与当前 Plugin Skill 集合一致且可测试 | ✅ 16 个 Skill（9 global + 7 CAD），覆盖率 82.6% |
| 安全 | ApprovalBroker + Interaction 面板 | ApprovalRequest、用户选项 | 同步决议、TUI 交互面板（箭头/数字选择） | 已完成 | EventHub、TUI dialog | 超时率、取消率、重复 resolve | 超时/取消/关闭均唤醒等待者；TUI 面板完整交互 | ✅ Broker 和 TUI resolve 已实现 |
| 安全 | 强制 Permission Gate | Tool call、-permission flag | manual（默认，白名单 + 审批）/ full_access（显式开启） | 🟡 基础完成 | Seele permission middleware | 门控覆盖率、误放行数、决策延迟 | manual 模式白名单工具自动放行，其他弹审批框 | 🟡 默认已收紧；`seele.yaml` 规则文件尚未强制执行 |
| 会话 | 保存与列表 | Engine History、Session ID | 持久化会话、元数据列表 | 已完成 | Seele Store | 保存成功率、存储耗时 | `/new` 保存当前历史，`/sessions` 可查询 | ✅ 已进入主链路 |
| 会话 | 恢复会话 | Session ID、持久化 History | Engine 与 Snapshot 同步恢复 | 已完成 | EnginePort.ReplaceHistory、SessionPort | 恢复成功率、历史一致率 | 恢复后下一轮 Chat 使用被恢复上下文 | ✅ `application/app.go resumeSession` + command/application tests |
| 上下文 | 压缩、合并与快照 | 长对话、上下文片段 | 受控 Token 上下文、快照 | 已有基础，集成 4—7 人日 | `seelexctx`、Engine | Token 节省率、关键信息保留率 | 长任务不超窗，关键约束在压缩后可回归验证 | 🟡 工具包测试较好，产品链路不足 |
| 前端 | TUI 工作入口 | 键盘、终端尺寸、application Event | CLI/TUI 交互界面 | 已完成，补测 3—5 人日 | Bubble Tea、Application | 输入延迟、渲染错误率、覆盖率 | Chat、补全、审批、Plugin 切换和 resize 有回归测试 | 🟡 主功能可用，覆盖率 6.2% |
| 协议 | Snapshot 分页与版本 | 历史游标、客户端能力 | 分页消息、`protocol_version=1` | 已完成（Core/GUI） | Application DTO | Snapshot 大小、分页延迟、兼容测试数 | 长会话分页加载；不兼容客户端收到可识别错误 | ✅ Core/GUI 已实现版本、seq、revision floor 与分页；远程 sidecar 另立功能点 |
| 协议 | JSON-RPC/stdio sidecar | RPC request、订阅连接 | response、event notification | 5—8 人日 | 稳定 DTO、协议版本 | RPC 成功率、事件延迟、异常退出率 | Node 测试进程可完成 snapshot→chat→approval→cancel | ⬜ 仅远程/IDE 客户端需要 |
| 前端 | Wails GUI Alpha | Application Bridge、工程产物 | 可视化任务、历史、Plan、审批和专业视图 | 基础与逻辑测试完成，E2E 待补 | Application DTO、系统 WebView | 核心任务完成率、演示稳定性 | 不复制业务逻辑；完整演示 chat/tool/approval/plugin 主链路 | 🟡 `gui/` + 26 个 Node tests + Bridge tests；Windows WebView E2E 待完成 |
| 前端 | Effort 常驻强度滑杆 | Runtime Effort、用户拖动 | 四档预览/提交、失败回滚、Max 紫色动效 | 已完成 | `EffortManager`、GUI Bridge | 切换成功率、失败回滚率 | 无需打开弹窗；拖动只预览；Max 动效支持 reduced-motion | ✅ `effort-control.js` + 4 个 Node tests + Bridge/Core contracts |
| 质量 | 测试与发布门禁 | 源码、测试、依赖 | format/build/vet/test/race/coverage/GUI/release-safety | 持续 | CI、C toolchain、Node 22 | 覆盖率、race、GUI tests、构建平台数 | main/gui 均触发；三平台 build；Linux race；GUI Node/contract；发布包安全 | ✅ `gui` push run 30004410641：六个 job 全绿 |
| 文档 | 状态与事实同步 | HEAD、测试结果、路线决策 | README、打点表、测试报告 | 持续，每迭代 0.5 人日 | CI/人工 Review | 过时陈述数、更新时间 | README 不宣传未接线能力；报告注明提交和日期 | 🟡 本次已更新 |

## 本轮变更 v0.0.2 → v0.0.4

| 新增 | 变更 |
|------|------|
| ✅ Effort low→lite 重命名 | 四级 effort 指令中所有 `low` 改为 `lite` |
| ✅ MaxLoops 翻倍 | medium: 16, high: 50 (原 25), max: 100 (原 50) → 用户进一步调至 20/64/512/1024 |
| ✅ Goal Skill 无限制 | 加载 #goal skill 时 SetMaxLoops(9999)，退栈自动恢复 |
| ✅ Plan 可视化 | `tui/plan.go` 四级 Effort Plan 面板（lite 单行 / medium 打点表 / high 节点树 / max 全框表） |
| ✅ Plan 进度回调 | Seele v0.0.4 `OnNodeDone` — 每节点完成后实时回调到 Service → 更新 PlanState → 发布事件 → TUI 重绘 |
| ✅ Plan per-node 结果 | `plan_run` 返回 JSON 含 `nodes[]` 字段（node_id/kind/status/elapsed） |
| ✅ Skill 用户上下文 | `#skillname 需求` 将 Skill 名称、指令和原始问题作为条目化用户消息发送；不再注入 system prompt |
| ✅ 系统诊断 | `/diag` 命令 — Go 运行时、Plugin、Account、Skill 全部列出 |
| ✅ 配置路径固定 | `<binary-dir>/config/accounts.yaml`，移除 `-c` flag |
| ✅ TUI 周期性刷新 | streaming 期间每 3s 自动重绘 `● receiving N.Ns` |
| ✅ 粘贴折叠 | 3 行以上粘贴折叠为 `[Pasted text #N +M lines]` |
| ✅ 代码审查修复 | tickMsg 接线、PlanState 深拷贝 cloneSnapshot、多诊断实现合并、空 assistant 消息去重、plan_run per-node 输出、panic→log.Fatalf |

## 活动图

```text
[稳定 Application Core + GUI protocol v1]
          │
          ├──→ [强制 Permission Gate] ──→ [seele.yaml 规则生效]  ← manual 模式已接线
          │
          ├──→ [Snapshot 分页/协议版本：已完成] ──→ [JSON-RPC Sidecar] ──→ [远程/IDE 客户端]
          │
          ├──→ [Plugin 可观测状态] ──────┬→ [CAD Plugin 垂直闭环]
          │                              └→ [Dev Plugin 垂直闭环]
          │
          └──→ [Session Resume：已完成] ──→ [跨版本历史兼容/E2E]
```

## 执行顺序建议

1. **P0：事实与安全一致**：Permission Gate seele.yaml 规则强制执行、审批 E2E。
2. **P0：协议地基后续**：冻结 v1 字段兼容规则、补稳定错误码和 sidecar contract。
3. **P1：可并行专业闭环**：CAD Plugin 与 Dev Plugin 可在 Plugin 契约稳定后并行推进。
4. **P1：多前端**：sidecar 完成后启动 Electron，不在 Electron 内复制业务编排。
5. **P2：体验和效率**：Plugin 切换可观测性、TUI 覆盖率、上下文效果度量。
6. **上游条件分支**：Session Resume 等待 Seele 提供安全的历史替换能力，不在 UI 层伪恢复。

建议最大并行度为 3：权限链路、协议链路、专业 Plugin 原型可并行；Electron 必须等待协议链路完成。

## 风险节点与降级路径

| 风险节点 | 失败模式 | 降级路径 |
|----------|----------|----------|
| Permission Gate | Seele 无可注入 middleware | 在 `seelebridge` 增加统一 Tool proxy；接通前明确标注未强制门控 |
| Plugin + MCP | 外部进程启动慢或无法回滚 | 保留旧 Plugin 可用；目标 Plugin 标记 failed；禁止半激活提交 |
| CAD Plugin | FreeCAD 环境重、E2E 不稳定 | 先用 fake executor 固化命令协议，再接 FreeCADCmd |
| Dev Plugin | 与基础 read/write/git Plugin 重叠 | Dev Plugin 只组合工作流、Skill 和质量门禁，不复制底层工具 |
| JSON-RPC | DTO 在开发期频繁变化 | sidecar 上线前冻结 v1；之后只做向后兼容扩展 |
| Electron | UI 先行导致业务重复实现 | Electron 仅消费 sidecar；缺少 API 时先补 core/协议，不在 TS 临时实现业务状态 |
| Session Resume | 历史格式跨版本不兼容或 ReplaceHistory 失败 | 原子保持当前会话，显示恢复失败，不提交半恢复 Snapshot |

## 更新规则

- 每个功能合并时更新"当前状态与证据"；
- 每次发布记录指标基线，不用"已支持"替代可验证完成标准；
- 标记 ✅ 必须同时满足：主链路接入、失败路径处理、自动化验证；
- 专业 Plugin 必须以真实工程任务 E2E 作为完成标准，不能只以 manifest 存在判定完成。
