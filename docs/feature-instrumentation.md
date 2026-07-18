# Seelex 功能打点表

> 更新日期：2026-07-18  
> 产品版本：v0.0.2  
> 产品目标：构建可切换专业 Plugin 形态的工科全栈 Agent；CLI/TUI 面向高效工作，Electron 面向毕设、课程项目和成果交付。

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
| 工具强制门控覆盖率 | 0% | 高风险工具 100%，全工具 100% 有决策记录 | tool call 与 permission decision 对账 |
| Chat 事件可恢复率 | EventHub 支持 resync | 断线/背压场景 100% 可由 Snapshot 恢复 | Seq 跳号与 Snapshot 对账测试 |
| Application 覆盖率 | 51.0% | ≥ 75%，关键错误路径 ≥ 90% | `go test -cover` |
| TUI 适配覆盖率 | 9.1% | ≥ 40%，输入/交互/resize 主路径全覆盖 | `go test -cover` |
| 专业 Plugin 垂直闭环 | 0 个 | CAD 1 个、Dev 1 个 | 真实任务从输入到产物的 E2E |
| 多前端协议兼容 | 尚无 sidecar | TUI 与 Electron 共用同一核心和协议版本 | JSON-RPC contract tests |

## 功能打点表

| 阶段 | 功能点 | 输入 | 输出 | 预计工作量 | 依赖 | 度量指标 | 完成标准 | 当前状态与证据 |
|------|--------|------|------|------------|------|----------|----------|----------------|
| 内核 | Headless Application Core | 用户输入、Engine 事件、Interaction 决议 | Snapshot、有序 Event、业务动作 | 已完成 | Seele Engine | application 测试通过率、Event Seq 连续性 | TUI 不持有 Engine/Plugin/Session；核心可无 UI 测试 | ✅ `application/`、`tui.AppController` |
| 内核 | 流式 Chat 生命周期 | Prompt、context cancel、chunk | Message delta、最终历史、错误状态 | 已完成 | Engine ChatStream | 首 token、完成率、取消延迟 | 并发 Chat 被拒绝；取消和错误不遗留 Running 状态 | ✅ 已有无 UI Chat 测试 |
| 内核 | EventHub 与重同步 | Snapshot revision、业务事件 | Seq 单调 Event、`resync.required` | 已完成 | 无 | 丢事件率、resync 成功率 | 订阅缓冲溢出后客户端可拉 Snapshot 恢复一致状态 | ✅ 已有背压测试 |
| 内核 | MCP 调用追溯中间件 | MCP 工具调用（所有 Server） | 可追溯、可回滚的调用记录 | 已完成 | `mcpstack/` 7 个文件 | 记录完整率、查询速度 | 每个 MCP 调用（含熔断事件）自动记录；支持 ByServer/ByTool/Latest 查询 | ✅ `mcpstack/` 18 个测试 |
| 内核 | 熔断事件通道 | 熔断器状态变化 | 异步 event → mcpstack 记录 | 已完成 | Seele breaker + `mcpstack/breaker.go` | 事件不丢失、不阻塞调用链 | 6 个埋点（opened/half_open/closed/recovering/recovered）经 channel→goroutine 写入 trace | ✅ `mcp_integration_test.go` 9 个测试 |
| 内核 | 存储接口解耦 | 写入、读取、列表、删除 | Storage 接口 + FileStore 实现 | 已完成 | `seelectx/storage` | 可替换实现、空路径 no-op | 框架只定义接口不提供默认路径；存储实现可切换 | ✅ `storage_test.go` 兼容测试 |
| 形态系统 | Plugin Manifest | `plugins/*/plugin.md` | Plugin、工具过滤、Prompt、Skill、MCP 配置 | 已完成 | frontmatter loader | 加载成功率、schema 拒绝率 | 非法 schema/名称/路径被拒绝，合法定义可发现 | ✅ Loader 与测试已存在 |
| 形态系统 | Plugin 切换事务 | 当前 Plugin、目标 Plugin | 工具/Skill/MCP 的一致激活状态 | 已完成 | Seele Tool Holder、MCP | 成功率、回滚成功率、耗时 | 任一步失败后恢复旧 Plugin；并发切换串行一致 | ✅ 回滚与并发测试已存在 |
| 形态系统 | Plugin 可观测状态 | 激活请求、阶段变化 | activating/active/failed 状态与耗时 | 2—3 人日 | Plugin Manager、EventHub | 分阶段耗时、失败阶段分布 | UI/日志能定位 attach、tool、skill、detach 哪一步失败 | ⬜ 当前只有最终结果 |
| 形态系统 | CAD Plugin 最小闭环 | 自然语言 CAD 任务 | FreeCAD 模型、命令记录、可验证产物 | 10—20 人日 | Plugin/MCP、FreeCAD executor | 任务成功率、重放一致率、几何校验通过率 | 完成一个零件从描述→建模→导出→重放验证 | ⬜ 设计文档已存在 |
| 形态系统 | Dev Plugin 最小闭环 | Issue/需求、代码仓库 | 方案、代码、测试、Review 报告 | 7—12 人日 | Plugin/Skill、Shell/Git tools | 一次通过率、测试通过率、人工返工次数 | 完成一个真实仓库需求并生成可审查变更 | ⬜ 尚无专用 Plugin |
| Skill | 全局与 Plugin Skill | `SKILL.md`、Plugin 私有 Skill | 可查询、可补全、可激活的 Prompt | 已完成 | Skill Loader/Registry | 加载数、冲突数、激活耗时 | 全局与当前 Plugin Skill 集合一致且可测试 | ✅ 覆盖率 84.4% |
| 安全 | ApprovalBroker | ApprovalRequest、用户选项 | 同步决议、Interaction Event | 已完成 | EventHub、前端 adapter | 超时率、取消率、重复 resolve | 超时/取消/关闭均唤醒等待者，不发生全局请求覆盖 | ✅ Broker 和 TUI resolve 已实现 |
| 安全 | 强制 Permission Gate | Tool call、参数、`seele.yaml` | allow/ask/deny 决策与审计记录 | 5—8 人日 | Seele tool middleware 或 bridge hook | 门控覆盖率、误放行数、决策延迟 | 所有工具执行前必经规则；危险用例无法绕过审批 | ⬜ 当前配置未接线 |
| 会话 | 保存与列表 | Engine History、Session ID | 持久化会话、元数据列表 | 已完成 | Seele Store | 保存成功率、存储耗时 | `/new` 保存当前历史，`/sessions` 可查询 | ✅ 已进入主链路 |
| 会话 | 恢复会话 | Session ID、持久化 History | Engine 与 Snapshot 同步恢复 | 上游 2—4 人日 | Engine history replacement API | 恢复成功率、历史一致率 | 恢复后下一轮 Chat 使用被恢复上下文，UI 与 Engine 完全一致 | ⛔ 当前明确禁用 |
| 上下文 | 压缩、合并与快照 | 长对话、上下文片段 | 受控 Token 上下文、快照 | 已有基础，集成 4—7 人日 | `seelexctx`、Engine | Token 节省率、关键信息保留率 | 长任务不超窗，关键约束在压缩后可回归验证 | 🟡 工具包测试较好，产品链路不足 |
| 前端 | TUI 工作入口 | 键盘、终端尺寸、application Event | CLI/TUI 交互界面 | 已完成，补测 3—5 人日 | Bubble Tea、Application | 输入延迟、渲染错误率、覆盖率 | Chat、补全、审批、Plugin 切换和 resize 有回归测试 | 🟡 主功能可用，覆盖率 9.1% |
| 协议 | Snapshot 分页与版本 | 历史游标、客户端能力 | 分页消息、`protocol_version` | 3—5 人日 | Application DTO | Snapshot 大小、分页延迟、兼容测试数 | 长会话不传全量历史；旧客户端收到可识别错误 | ⬜ Electron 前置条件 |
| 协议 | JSON-RPC/stdio sidecar | RPC request、订阅连接 | response、event notification | 5—8 人日 | 稳定 DTO、协议版本 | RPC 成功率、事件延迟、异常退出率 | Node 测试进程可完成 snapshot→chat→approval→cancel | ⬜ 尚无 `transport/` |
| 前端 | Electron 毕设/交付界面 | sidecar API、工程产物 | 可视化任务、历史、报告和专业视图 | 15—30 人日 | JSON-RPC、至少一个专业 Plugin | 核心任务完成率、演示稳定性 | 不复制业务逻辑；可完整演示 CAD 或 Dev 项目闭环 | ⬜ 后续产品阶段 |
| 质量 | 测试与发布门禁 | 源码、测试、依赖 | build/vet/test/race/coverage 报告 | 2—4 人日 | CI、C toolchain | 覆盖率、race、构建平台数 | 三平台通过；Linux race 通过；报告与 HEAD 同步 | 🟡 build/vet/test 已通过，race 环境未就绪 |
| 文档 | 状态与事实同步 | HEAD、测试结果、路线决策 | README、打点表、测试报告 | 持续，每迭代 0.5 人日 | CI/人工 Review | 过时陈述数、更新时间 | README 不宣传未接线能力；报告注明提交和日期 | 🟡 本次开始收口 |

## 活动图

```text
[稳定 Application Core]
          │
          ├──→ [强制 Permission Gate] ──→ [安全审计与端到端审批]
          │
          ├──→ [Snapshot 分页/协议版本] ──→ [JSON-RPC Sidecar] ──→ [Electron]
          │
          ├──→ [Plugin 可观测状态] ──────┬→ [CAD Plugin 垂直闭环]
          │                              └→ [Dev Plugin 垂直闭环]
          │
          └──→ [Engine History Replace] ──→ [Session Resume]

[测试门禁] 覆盖全部节点，并持续更新本表基线。
```

## 执行顺序建议

1. **P0：事实与安全一致**：Permission Gate、审批 E2E、文档状态同步。
2. **P0：协议地基**：Snapshot 分页、协议版本、稳定错误码。
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
| Session Resume | Engine 历史无法替换 | 保持 capability=false 和明确提示，仅允许历史查看，不伪装恢复 |

## 更新规则

- 每个功能合并时更新“当前状态与证据”；
- 每次发布记录指标基线，不用“已支持”替代可验证完成标准；
- 标记 ✅ 必须同时满足：主链路接入、失败路径处理、自动化验证；
- 专业 Plugin 必须以真实工程任务 E2E 作为完成标准，不能只以 manifest 存在判定完成。
