# Seelex 产品规划（2026-08 → v1.x）

> 状态：规划草案（基于 2026-08-07 仓库现状与市场调研，见 `docs/research/agent-market-research-2026-08.md`）
> 配套规范：`docs/product/README.md`（PRD/用户旅程/里程碑/验收标准要求）
> 当前基线：Developer Alpha；TUI 为主入口，GUI（Wails）可构建，280 个 Go 文件 / 57.6k 行 / 109 个测试文件 / 三平台 CI（README.md、go.mod）

---

## 1. 产品愿景与一句话定位

**愿景**：让每个开发者拥有一个**可组合、可审计、可自托管的 Coding Agent**——不绑定单一模型、不交出代码与数据、不忍受黑盒。

**一句话定位**：Seelex 是"本地优先、多模型可路由、上下文可逆治理"的开源 Coding Agent Harness——给重视成本、信任与数据主权的开发者，一个介于 Claude Code（绑模型）与自研 agent（什么都自己写）之间的选择。

**差异化三支柱**（与市场对照，依据市场调研第 7 节）：
1. **可逆上下文治理**：压缩帧可读回（`read_compressed_turn` + TurnArchiver），主流压缩不可逆。
2. **多模型账号池路由**：agent/subagent/goalplan 角色账号池 + 分支确定性选路（成本 + 容灾）。
3. **本地优先 + 可审计执行**：ProjectScope/PathGate 审批链 + Snapshot/Event 事件投影，全程可回溯。

---

## 2. 目标用户画像（3 个 Persona）

### P1「成本敏感的个人开发者」—— 王工，28 岁，全栈自由职业
- 痛点：Cursor/Claude Code 订阅贵、绑模型；想用 DeepSeek/Qwen 控成本，又不想放弃 agent 能力。
- 需求：BYOK 多模型、会话可恢复、TUI 快速操作、开源可魔改。
- 对应能力：账号池路由、多后端持久化、TUI、Plugin。

### P2「数据主权优先的团队技术负责人」—— 李工，35 岁，某银行/政务外包团队 TL
- 痛点：代码不能出内网；采购需要审批/审计证据；合规要求可解释。
- 需求：本地自托管、逐操作审批、审计日志、离线可用、私有化部署。
- 对应能力：ProjectScope/PathGate、human-in-the-loop 审批、事件投影、SQLite/Postgres/Redis 后端。

### P3「AI 应用开发者 / 研究型用户」—— 张同学，24 岁，研究生或 AI 创业
- 痛点：要自己搭 agent 流水线，但不想从零写 harness；想复现多 agent/上下文治理论文级设计。
- 需求：清晰的架构边界（ports & adapters）、可替换模块、垂直插件样板（如 freecad）、文档。
- 对应能力：`application/contract` 端口化、seelebridge 隔离、freecad 垂直样板、高密度文档。

---

## 3. 核心使用场景与用户旅程

### 场景 A：TUI 日常任务（P1）
`/new 新建会话 → 输入任务 → 模型 ReAct 执行 → 需要时 plan_load 拆 DAG → 并行 subagent → merge-back → task_complete 收尾 → /resume 恢复上次会话`
验收：单会话可完成"多文件小需求"；窗口超预算时自动压缩且可 `read_compressed_turn` 读回；断电后可恢复。

### 场景 B：企业内网开发（P2）
`本地/内网部署 → 配置 accounts.yaml（内网网关模型）→ 项目作用域限定 → 高敏工具触发 ask 审批 → 事后查事件投影审计`
验收：所有工具调用有事件记录；审批链可解释；无任何出网调用（除模型 API 白名单）。

### 场景 C：垂直领域样板（P3）
`安装 freecad 插件 → 插件事务式切换工具/Skill/MCP → JSON 驱动批量建模 → 导出 STEP/STL → 复用为其它垂直域模板`
验收：一个插件可完整走通"MCP 探索 → batch 兜底 → headless 脚本"降级链（plugins/freecad/README.md）。

---

## 4. MVP 定义（v1.0）

### v1.0 必须（依据当前已实现 + 缺口补齐）
- [x] ReAct 主循环 + Effort 分档（lite/medium/high/max）——已实现
- [x] WorkPlan DAG：plan_load/plan_run、并行 subagent、merge-back——已实现
- [x] 可逆上下文压缩 + result_ref 归档——已实现（差异化核心，需产品化验证）
- [x] ProjectScope + PathGate + allow/ask/deny 审批——已实现
- [x] Plugin/Skill/MCP 事务式切换——已实现
- [x] 多后端持久化（SQLite/Postgres/Redis/JSON）+ 会话恢复——已实现
- [x] TUI 主入口 + GUI（Alpha）——已实现
- [ ] **一键安装/快速开始**：release 二进制 + 中文/英文快速上手（当前需手动编译）
- [ ] **GUI 转 Beta**：会话/计划面板稳定性（docs/gui 已有模块文档，需收口）
- [ ] **账号池 UX**：跨角色路由的可视化与费用归集界面
- [ ] **错误可解释性**：error_codes 体系面向用户呈现（已有 error_presentation.go，需补全）

### v1.0 明确不做
- ❌ IDE 插件（VS Code/JetBrains）——资源不足，避开巨头正面战场
- ❌ 云托管/云端执行环境——违背本地优先定位；不做 Devin 式云 VM
- ❌ 自研模型/调优——模型能力是上游变量，Harness 只做路由与治理
- ❌ 代码库语义索引（embedding 检索）——工程量大，v1.x 再评估（市场调研确认这是 Cursor/Augment 的差异化，也是 Seelex 最大技术短板）
- ❌ 移动端

---

## 5. 里程碑路线图（四阶段）

### 阶段 0：产品化收口（2026 Q4，当前 Alpha → Beta）
- 目标：把"工程证明"变成"可用产品"，建立第一批真实用户
- 关键功能：release 二进制 + 一键启动；GUI Beta 稳定性；快速开始文档；错误码/诊断可读化；账号池费用可视化
- 验收标准：新用户 30 分钟内完成"安装 → 配置 accounts → 跑通一个真实任务"；GUI 无 P0 崩溃；文档覆盖 TUI/GUI 两条路径
- 指标：GitHub stars ≥ 500；真实用户使用（每周活跃会话）≥ 50

### 阶段 1：Beta 与社区验证（2027 Q1）
- 目标：验证差异化叙事（可逆压缩 + 账号池 + 本地可审计），形成种子社区
- 关键功能：Plugin 生态目录（2-3 个垂直样板）；审计报告导出（面向 P2）；AGENTS.md/CLAUDE.md 惯例文件支持；用户级记忆
- 验收标准：至少 3 个外部贡献者提交 PR；1 个企业/团队试用案例产出；垂直插件模板文档
- 指标：stars ≥ 2k；外部 PR 合并 ≥ 10；试用案例 ≥ 3

### 阶段 2：v1.0 发布（2027 H1）
- 目标：MVP 全量交付，形成"可组合 Harness"的品牌
- 关键功能：MVP 清单全部完成；Plugin SDK 文档稳定；A2A 协议实验（跨 agent 互操作，跟进市场调研第 5 节）；可选代码库索引（评估 embedding 检索）
- 验收标准：`go test -race ./...` 全绿；三平台 release 自动化；文档中英双语
- 指标：stars ≥ 5k；周活跃用户 ≥ 1k；企业询盘 ≥ 10

### 阶段 3：v1.x 生态与商业化（2027 H2+）
- 目标：从工具到平台，探索可持续商业模式
- 关键功能：Plugin/Skill/MCP 市场（付费/赞助生态）；团队协作（共享会话/计划/审计）；企业版（SSO/审计报告/私有化部署包）；云端可选服务（云同步/远程执行，作为附加而非默认）
- 验收标准：付费转化 > 1%；至少 3 个垂直行业样板落地
- 指标：ARR 起步；NPS > 40；生态插件数 ≥ 50

---

## 6. 开源与商业化策略

- **开源**：MIT（LICENSE 已如此）；核心 Harness 永远开源；社区化路径：CONTRIBUTING/CODE_OF_CONDUCT 已有，需补"好第一 issue"清单与维护者轮值。
- **生态**：Plugin/Skill/MCP 目录化（plugins/default、plugins/freecad 已示范）；免费提供垂直样板（CAD 等）作为获客钩子。
- **付费点**（全部为"附加服务"，不阉割核心）：
  1. **GUI 高级版**：桌面 GUI 深度功能（当前 GUI 为 Alpha，可作为 Pro 功能）。
  2. **云同步/多机会话**：跨设备续跑（P1 需求）。
  3. **团队协作**：共享 plan/会话/审计 + 角色权限（P2 需求）。
  4. **企业包**：SSO、合规审计报告、私有化部署支持、SLA。
  5. **生态市场分成**：付费 Plugin/MCP server 的渠道费（远期）。
- **品牌叙事**：避开"又一个 AI IDE"，主打"**可组合、可审计、可自托管**"三词；发布即写博客 + 对比表（市场调研第 7 节已有素材）。

---

## 7. 关键成功指标

- **北极星指标**：**每周完成的有效任务数**（任务从开始到 task_complete 的闭环数；比 MAU 更能反映"agent 真的在干活"）。
- 辅助指标：
  - 激活：安装→首个任务完成时长（目标 < 30 min）
  - 留存：次周/次月会话恢复率（resume 使用率）
  - 治理：审批触发率、事件投影完整率（P2 关注）
  - 成本：单任务 token 成本中位数（账号池路由的卖点）
  - 生态：Plugin/Skill 数量、外部贡献 PR 数
  - 工程：`go test -race` 全绿、CI 三平台通过率 100%

---

## 8. 风险与缓解

| 风险 | 证据/来源 | 缓解 |
|---|---|---|
| 无真实用户，产品未验证 | 当前 Developer Alpha，无装机/使用数据（README.md） | 阶段 0 优先做"快速上手 + 真实任务闭环"，建立试用计划 |
| 赛道拥挤、巨头通吃 | 市场调研：Cursor $29.3B 估值、Claude Code 满意度第一、Devin 资本密集 | 避开通用 IDE 正面战场，垂直样板 + 治理/数据主权差异化 |
| 开源洗牌剧烈、维护不可持续 | Roo Code 2026-05 归档、Gemini CLI 退休；单作者 201 commits | 社区化：好第一 issue、维护者制度、赞助（GitHub Sponsors / 企业赞助） |
| 技术债：上下文横切一致性 | ARCHITECTURE_REVIEW.md：context.TODO 5 处、Repository 硬编码 Background()、压缩静默回退 | 阶段 1 专项清理；压缩回退改为显式事件上报 |
| 技术债：mcpstack 双栈与 actor 模型不同源 | ARCHITECTURE_REVIEW.md §1-3 | 维持 Read 路径隔离，阶段 2 评估统一 |
| 上游模型/Seele 依赖变动 | 曾有 `go build ./...` 阻断历史（seele-v2-runtime-architecture.md）；依赖 Seele v0.1.1 | 依赖升级纳入 CI 回归；seelebridge 保持 Anti-Corruption Layer |
| 模型能力窗口被厂商吞掉 | 市场调研：Anthropic/OpenAI 每代改变能力边界 | 主打"治理与路由"而非"生成能力"；支持本地模型（Ollama/LM Studio）作为后路 |
| 无代码库语义索引，长仓库体验弱 | 市场调研：Cursor embedding 索引、Augment Context Engine 是差异化 | 明确不做进 v1.0；v1.x 评估（ProjectKnowledge 可演进为检索底座） |

---

## 9. 证据与依赖

- 现状基线：README.md、go.mod、ARCHITECTURE_REVIEW.md、docs/arch/seele-v2-runtime-architecture.md
- 调研依据：docs/research/agent-market-research-2026-08.md（本文配套）
- 既有研究：docs/research/coding-agent-harness-comparison.md（差距矩阵与路线图）、docs/research/agent-harness-research-report.md
- 产品规范：docs/product/README.md
- 工作示例：docs/2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md（迭代试点计划，可作阶段 0 输入）
