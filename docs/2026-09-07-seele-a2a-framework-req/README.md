# 2026-09-07 · Seele A2A 基座设计 — 统一基线（README）

> 本目录沉淀 "seelex(产品) × Seele(框架) 的 A2A 治理"设计演进。**唯一权威基线是
> `ds-a2a-protocol.md`（协议 v0.1）+ `ds-a2a-detailed-design.md`（详设 + 前端）**；
> 其余文档是支撑它们的调研/评估档案。改动设计先改这两份；别在档案里开新分支。

日期：2026-09-07 · 状态：协议+详设 = 权威；需求/数学/路线评估 = 档案

---

## 1. 一句话基线

> 产品路线选型 = **双会话不对称 A2A（DS-A2A）**：EXEC（执行 main，会话 a）与 ADVISOR（后台评审，会话 b）
> **各维护独立会话上下文**，b 永不读 a 的全量转录——b 经协议按需接收 a 的事件增量帧，保持
> **前缀稳定 + 只尾部追加** ⇒ provider 前缀缓存命中率高；b 的回合产物结构化回注 a 的受信注入区；
> 生命周期由 CONTROLLER（goal 域 owner）管理，**a 永不因 b 缺席卡死**（B4 铁律）。
> 结论（对比单会话共享方案 A）：**框架必改 ≈ 0–1**，几乎全部可在 seelex 产品层实现。

## 2. 文档状态矩阵

| 文档 | 定位 | 状态 | 正确读法 |
|---|---|---|---|
| `README.md`（本文件） | 统一基线入口/索引 | **维护中** | 先读这里，再按需展开 |
| `ds-a2a-protocol.md` | **协议 v0.1**：信封/帧/时序/一致性/生命周期/错误码 | **权威（当前）** | 实现协议字段以此为准；schema 变更 = 版本化（0.1→0.2…） |
| `ds-a2a-detailed-design.md` | **详设**：运行时组件 + 演示图 + 前端角色会话显示 + A1–A7 验收 | **权威（当前）** | 组件/时序/前端/DTO 以此为准 |
| `requirements.md` | 早期 R1–R8 框架需求单 | **历史档案**（被 DS-A2A 取代，框架必改项收敛为 ≈0） | 只读价值：问题定义、证据索引 |
| `same-session-governance-math.md` | 单会话共享方案 A：数学模型 + Seele 现状 + Diff | **历史档案**（方案 A 未采纳为架构） | §4 Seele 现状 / §7 证据索引仍有效；M 表是选型过程 |
| `dual-session-alternative.md` | 方案 B 对照评估（→ 采纳为 DS-A2A） | **路线决策（ADR）** | B1–B6 不变量/现成设施映射是协议+详设的依据 |

### supersede 关系（勿反着读）
```
requirements.md (R1–R8, 框架大改)
        │  被
        ▼
same-session-governance-math.md (方案 A 建模)      ← 保留 Seele 现状/证据
        │  被
        ▼
dual-session-alternative.md (方案 B 评估, 采纳)    ← B1–B6 出处
        │  收敛为
        ▼
ds-a2a-protocol.md ◄── 权威协议（唯一真相源）
        │  配套
        ▼
ds-a2a-detailed-design.md ◄── 权威详设 + 前端
```

## 3. 统一术语表（本目录内同义词收敛）

| 术语（建议用） | 同义词（旧档中可见） | 含义 |
|---|---|---|
| **EXEC (a)** | mainagent / main / 执行会话 | 执行 agent：用户可见视图、全量转录、唯一 writer |
| **ADVISOR (b)** | TL / techleader / 后台会话 | 评审 agent：独立会话、只读/无工具、后台回合 |
| **CONTROLLER** | supervisor / owner / 运行时 | b 生命周期 owner + 镜像 + 指令总线（详设 §4 组件） |
| **帧 frame** | 事件段 / mirror payload | a→b 同步单位（`ref_seq` 单调 append） |
| **水位 synced_at / ref_seq** | 已应用水位 / 因果屏障 | b 已看到 a 的哪个 seq；回合/裁决带 ref_seq |
| **on_eval** | 事务式同步 / C3 | 回合前一次性 append 落后帧 → 一次 LLM 调用（缓存最优） |
| **受信注入区** | pending 注入 / directivebus | b→a 结构化建议的落点，不进 a 转录主历史 |
| **B4 缺席铁律** | 429 降级 / TL 缺席 | a 永不等待 b；任何 gate 有 deadline，超时按缺席矩阵 |
| **协议事件** | goal 事件 / 审计事件 | 帧/回合/verdict/水位/缺席 → 前端与审计（R6） |

## 4. 决策状态总表

| ID | 决策 | 状态 | 出处 |
|---|---|---|---|
| 路线 | **双会话 DS-A2A**（方案 B）优于单会话共享（方案 A） | ✅ 已定 | `dual-session-alternative.md` |
| 协议 v0.1 | EXEC/ADVISOR/CONTROLLER + 信封 + 帧表 + B1–B6 + 生命周期 | ✅ 基线 | `ds-a2a-protocol.md` |
| B1 | 会话间零共享可变态（不互写对方 Session） | ✅ 基线 | dual §1 / protocol §7 |
| B2 | 镜像单调幂等（seq 严格递增、断点续传） | ✅ 基线 | dual §1 |
| B3 | 指令幂等（corr 去重） | ✅ 基线 | dual §1 |
| B4 | a 永不等待 b；缺席按风险矩阵判负/升人工 | ✅ 基线（铁律） | dual §1 / protocol §8 / design A3 |
| B5 | 全链路有界（回合预算/注入队列/帧体积） | ✅ 基线 | dual §1 |
| B6 | 收敛前不发新回合；reap 有护栏 | ✅ 基线 | dual §1 / protocol §9 |
| C1–C5 | b 上下文缓存命中规则（前缀稳定/只追加/事务/零增重估/跳帧） | ✅ 基线 | protocol §2 / design §5 |
| 终态 | verdict 自动机（done/not_done/rework/escalate）+ ref_seq 门槛 | ✅ 基线 | protocol §5 / design §3 |
| A1–A7 | 验收断言（转录只增/帧幂等/on_eval 事务/缺席收敛/不混转录/可重放/无孤儿） | ✅ 基线（验收清单） | design §7 |
| 组件落点 | `application/core/peersession/`（协议 Go 镜像）+ 复用 seelex 现成件 | ✅ 建议落点 | design §4 |
| R1–R8 | 框架级大改需求单 | ❌ 被取代（框架必改 ≈0） | `requirements.md` |
| 前端 | TUI（bubbletea `Cell.Role` 扩展）/ GUI（wails 泳道） | 🟡 待落地（P2 goal 视图） | design §6 |
| 开放问题 | 见 §6 | 🟡 待拍板 | protocol §12 / design §9 |

## 5. 代码事实基线（贯穿各档的证据）
- seelex：`seelebridge/events_unified.go:85-94,118-140`（seq + QueryRange 增量读）；
  `seelebridge/events.go:32-59`（事件源 session 定位/持久化）；`application/core/service_input.go:25` +
  `chat.go:118,205`（pending 注入挂点）；`application/core/goal/a2a.go:26`（MaxDirectiveQueue=32）；
  `application/contract/dto/projection.go:4`（RuntimeVisibilityProjection）；`tui/state.go:14-20`（Cell.Role）。
- Seele：`session/`（NewSession/Chat/ReActLoop/Reset）；`agent/bridge/registry_runtime.go:28-96`
  （VisibilityPolicy 只读面）；`workplan/runtime/forkexec/`（并发分支隔离，DS-A2A 仅间接复用）；
  GitHub `RedHuang-0622/Seele` == 本地 HEAD `69374b7`，seelex pin v0.1.2。
- 相邻目录：`docs/2026-09-07-goal-domain-techleader/`（goal 域 Part I/II 设计 + MVP；D1–D8 与 gate 语义）、
  `docs/2026-08-07-agile-a2a/`（TL×Programmer 敏捷 A2A 早期设计）。

## 6. 开放问题（待拍板，汇总 protocol §12 + design §9 + dual §6）

| # | 问题 | 选项 | 建议 |
|---|---|---|---|
| Q1 | 镜像粒度 | 全量事件 vs 窗口事件（milestone/tool/压缩摘要）+ TL 重建帧 | 后者（控 token） |
| Q2 | b 跨 goal 复用 | 每 goal 新会话 vs 复用+Reset | 每 goal 新建（干净基线） |
| Q3 | 审计 | 删会话对象、回合/verdict 留事件账本 | 留账本 |
| Q4 | TL 工具面 | 无工具 vs 只读最小集 | 先无工具，跑通再加只读 |
| Q5 | 缺席裁决默认 | low 放行 / high 升人工 | 同审批预筛 |
| Q6 | 抽帧默认集 | `transcript.inc` 是否默认进 b | 默认否（跳帧控量） |
| Q7 | 前端第一落地面 | TUI（bubbletea）先行 vs GUI（wails）先行 | TUI 先行，DTO 共用 |
| Q8 | ADVISOR 面板交互 | 默认折叠+快捷开关 vs 常驻 | 默认折叠 |

## 7. 归档说明（为何保留历史档案）
DS-A2A 设计是一天内的收敛过程：R1–R8（框架大改）→ 方案 A 建模 → 方案 B 评估（采纳）→ 协议 v0.1 →
详设。**档案文档保留演进与证据**（尤其 requirements 的问题定义、math §4 Seele 现状、dual 的现成设施映射），
供日后回溯"为什么这么定"；**不要**从档案反向推断当前需求。改设计 = 改权威两份 + 更新本 README 状态。

## 8. 代码落地记录（2026-09-07 追加，goal 域切片）

goal 域（application/core/goal）已按 DS-A2A 治理重构并冒烟通过（本目录文档随之定稿为权威基线的实现出处）：

| 变更 | 内容 | 验证 |
|---|---|---|
| b 独立上下文 | advisor.go：AdvisorSession（锚点 goal.start + 帧账本 ref_seq 单调 + 自身回合段 + CacheStats），协议 §1/§2 C1-C5 | dsa2a_test.go（帧单调/dup/跳帧/命中回升）+ -race |
| 治理编排 | techleader.go：Supervisor=PeerSessionManager（execSeq a 账本、on_eval 一次性补帧同步、corr 信封 DirectiveBus、惰性 bind、收口 unbind+reap、B4 回合失败缺席） | supervisor_test/headless_tl_test |
| gate | gate.go：终态 ProposeFinish / 审批 PreScreenApproval 走 b 回合 + 缺席默认（429/超时 → escalate 不阻塞） | gate_test + headless 冒烟 TestHeadlessDSA2AAbsentGate |
| headless 契约 | goal_tl_tail 移除（尾窗喂入属旧同会话治理）；goal_tl_snapshot 扩展 b 上下文/cache/落后视图 | headless_tl_test 等 |
| 陈旧治理移除 | 同会话共享（每回合实时重建+尾窗+AppendDirective 写 goal 指令环）停用（AppendDirective 标记 deprecated） | 测试断言 b 不回写 goal 状态 |
| DAG 原型清理 | tmp/a2a-agile-smoke 删除（Plan-DAG TL×Programmer runLoop，难 tracing）；docs/2026-08-07-agile-a2a 标 ARCHIVED；plugins/default/goal/SKILL.md 移除"长任务必须 plan DAG 子代理调度"治理，改为 EXEC 直接执行 + DS-A2A 评审/终态 gate | git + 文档横幅 |

运行冒烟：go test -race ./application/core/goal/ ；go build ./...（均已通过）。
生产接线（P0-wiring：Supervisor 入 seelebridge/session ChatStream、goal 工具族入 isGoalTool、SKILL 与运行时打通）为后续工作，DS-A2A 详设 §8 切片建议保持。
