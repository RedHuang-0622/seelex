# 方案 B：双会话复制型 A2A（main ‖ TL 独立会话 + 协议层）对照评估

> **状态：路线决策（ADR，已采纳为 DS-A2A 依据）。** 本文结论（双会话 > 单会话共享；框架必改 ≈0–1；
> B1–B6 不变量）是当前设计的**决策出处**；具体实现与协议以 `ds-a2a-protocol.md` + `ds-a2a-detailed-design.md`
> 为基线（本目录统一基线见 `README.md`）。

> 系列第 3 篇，承接 `requirements.md`（R1–R8）与 `same-session-governance-math.md`（目标模型 + Seele 现状 + Diff）。
> 本文记录用户 2026-09-07 提出的替代路线（下称**方案 B**）评估：**维护两个独立会话**（mainagent 执行会话 ×1、
> techleader 后台会话 ×1），中间经协议层同步"最新聊天记录"，视图只看 main，TL 后台运行；
> 生命周期 = goal 完成即删除 TL 会话；上游 429/连不上 API 也删除 TL 会话（降级）。
> 对比基准 = 前文目标模型（下称**方案 A**：单会话共享 σ，单写者 + 只读 advisor）。状态：评估结论，日期 2026-09-07。

---

## 0. TL;DR（一句话结论）

**方案 B 优于方案 A 作为"并入 Seele"的第一路线**：它把"会话内多角色共享状态"这个框架级难题
翻译成"两会话间一致性 + 协议"，会话边界天然提供单写者隔离，从而**不需要框架发明
会话内多角色原语**（ReadOnlyView / 单写者账本 / advisor 运行时都可省），且与会话边界正交地
复用了 seelex/Seele 已有的**事件流增量读、pending 注入、durable history** 三件现成设施。
代价集中在四处：**转录双份的 token 成本、镜像一致性窗口、TL 会话生命周期收敛、TL 缺席的安全默认**，
后三者可在产品层用不变量收敛，成本可用窗口镜像缓解。

---

## 1. 模型：方案 B 的形式化

```
域（每个 goal 一次）：
  S_main  : 主执行会话（用户可见视图、转录、工具、plan/task 主状态；现有 seelex main 会话）
  S_tl    : TL 后台会话（独立完整 Session + 独立 ReAct 循环；只读工具面；不可见/或 P2 迷你面板）
  P       : 协议层（双向、异步、事件驱动）

P 的两条流（方向/内容/语义都不同，必须区分）：
  [1] 镜像流 mirror   S_main → S_tl
      载体：S_main 事件日志（每事件带单调 seq，events_unified.go: Seq=At.UnixNano()）
      增量：TL 持 lastAppliedSeq；QueryRange(sessionID, fromSeq=last, toSeq=now) 拉新事件
            （seelebridge/events_unified.go:118-140 LoadRange/QueryRange 已是现成基础设施，G7）
      幂等：seq 严格递增应用；重复/乱序由 offset 天然去重
      写入：S_tl 收到的不是裸文本，而是渲染为 TL 上下文的"镜像帧"（受信、只读）
  [2] 指令流 directive  S_tl → S_main
      内容：结构化 envelope{corr_id, verdict|directive|evidence}（TLDirective 已有形状，a2a.go MaxDirectiveQueue=32）
      投递：S_tl 回合结束 → pending 队列 → 由 S_main 下一轮前排空注入
            （application/core/service_input.go:25 injectPendingSubagentContextsFor 是现成挂点，chat.go:118/205 每次聊天前调用）
      幂等：corr_id 去重；注入渲染为受信帧（防 TL 自由文本污染 main 上下文）

水位与触发（S_tl 何时跑回合）：
  synced_at := max seq applied to S_tl
  TL 回合仅在 synced_at ≥ 评估水位时触发（因果屏障：TL 不评估它没看到的 main 状态）
  触发信号 = 事件类型（milestone/tool 打点/context_compacted/approval_asked/terminal_proposal）+ 回合预算

生命周期（S_tl 的 owner = goal 运行时）：
  goal_begin     → spawn S_tl（注入 goal 帧、只读工具面、预算、ctx）
  goal 收敛/abort → stop loop → 排空或丢弃 pending → reap S_tl（删除/按审计策略归档）
  429/上游不可用  → 标记 TL 缺席 → gate 判负/升人工（main 进入 direct 模式继续）→ 可删 S_tl
```

### 关键不变量（方案 B 专属）
| # | 不变量 | 说明 |
|---|---|---|
| B1 | **会话间零共享可变态** | S_main 与 S_tl 从不互写对方会话对象；唯一通道是 [1][2] 两条事件流 ⇒ 无单写者锁问题（隔离靠架构而非纪律） |
| B2 | **镜像单调幂等** | seq 严格递增应用、重复丢弃；断点=lastAppliedSeq 持久化 |
| B3 | **指令幂等** | corr_id 唯一；注入前先去重 |
| B4 | **TL 缺席 = 安全默认** | TL 会话不存在/429/超时 ⇒ 任何等待 TL 的 gate 必须判负或升人工（escalate），main 永不因 TL 缺席而悬挂 |
| B5 | **有界** | TL 回合预算 + 注入队列有界（现 32）+ 镜像窗口有界 |
| B6 | **收敛前不发新回合** | 生命周期停止时先停议程（不再 spawn TL 回合）再 reap；pending 显式丢弃计数并记账 |

---

## 2. 相对方案 A 的 Diff 修订（12 维表 → 逐项标注去向）

> 去向：**[产品层]** = seelex 自建，无需框架改动；**[框架可选增强]** = 若做"自由 A2A 轮子"产品主张才上提；**[框架必改]** = 阻塞。

| 维度（A 表） | 方案 B 后 | 去向 | 理由 |
|---|---|---|---|
| D1 agent 一等抽象 | **取消** | 产品层 | S_tl 只是另一个 `session.Agent`：复用 session 装配（components 模板 = 角色模板）。不必给框架新增 Engine/AgentSpec/Registry |
| D2/D10 类型化信封 + mailbox 公共化 | **降级** | 产品层（进程内） | 两流都走事件流 + pending 队列，不需要框架级 mailbox；seelactor 可继续产品 internal。若做远程 A2A 才需框架 envelope |
| D3 单写者 / advisor 只读视图 | **取消** | 架构天然满足 | 会话边界即隔离：S_tl 只读工具面（registry VisibilityPolicy 注入）+ 不挂任何写工具 = 只读 advisor，无需 ReadOnlyView 原语 |
| D4 回合议程 | **保留** | 产品层 | 触发 = 镜像水位 + 事件类型 + 预算（现 goal supervisor/信号已具雏形，改为异步即可） |
| D5 治理自动机（gate） | **保留 + 补 B4** | 产品层（已实现大半） | goal MVP gate.go 已有终态/审批预筛；补"TL 缺席判负/升人工"路径 |
| D6 生命周期 owner | **保留但更简** | 产品层 | = 会话生命周期管理（spawn/Reset/stop/reap + ctx cancel），框架 session 已有；**新增关键是删除/降级路径（B4/B6）** |
| D7 事件类型扩展 | **降级为可选** | 产品层或框架可选 | seelex 已有事件增量读（seq/QueryRange）；事件 Type 用现有四类 + 产品自扩 payload 即可做镜像；前端若画 TL 泳道再考虑扩事件（R6 降级为 P1.5 可选） |
| D8 合并策略 | **不涉及** | — | TL 不 merge context（不是 worker 并行段）；S_tl 回合产物只以指令流回注，不走 fork-join |
| D9 预算分额 | **保留** | 产品层 | S_main 与 S_tl 各自预算 + 全局封顶，产品记账 |
| D11 远程 A2A | **天然更贴合** | 后置 | 两会话若跨进程，镜像流/指令流经 wire = A2A 协议形状；进程内先行即可平滑迁移 |
| D12 发版/并存 | **不变** | 框架流程 | seelex pin v0.1.2，需升 pin 才能并进新 Seele HEAD |

**修订后的框架必改项 ≈ 0–1**：方案 B 几乎全部可在 seelex 产品层实现（事件增量读、pending
注入、会话装配、只读工具面都已存在）。框架侧唯一"可选增强"是把 [镜像流/指令流样板 + 按会话断点重放] 上提为
通用能力——只有当你坚持"自由的 A2A 轮子进框架"这一产品主张时才需要，且它是**浅层**（协议 + 事件订阅），
不是会话内核改造。

---

## 3. 风险清单与缓解（诚实版）

| # | 风险 | 严重度 | 缓解 |
|---|---|---|---|
| R1 | **转录双份的 token 成本**：S_tl 每次评估重读 main 已花过 token 的上下文 | 中（LLM 账单） | 镜像**窗口化**：只同步增量事件 + TL 上下文重建帧（goal 帧 + tail≤8 + pending≤N，goal MVP 参数可复用），不镜像全量；S_tl 装配自己的 Compressor/ContextController（session 组件支持） |
| R2 | **一致性窗口**：S_tl 看到的 main 状态可能落后/乱序 | 低-中 | seq 单调 + 因果水位（synced_at ≥ 评估水位才触发回合）；TL 回合标注其 synced_at 快照水位，verdict 带基线（gate 只接受 ≥ 当前水位或显式重评估） |
| R3 | **TL 上下文污染 main**：指令流若带 TL 自由文本 | 中 | 结构化 envelope（corr_id/verdict/directive）+ 受信帧渲染；main 不采纳 TL 原始文本，只采纳其结构化 verdict/指令 |
| R4 | **生命周期悬挂/孤儿**：goal 收敛/429 后 TL 仍在跑或 pending 悬挂 | 中 | B6：先停议程 → 排空/丢弃记账 → reap；复用 ctx cancel + session 停止（10s 护栏，参照现有 stopAndReap 语义） |
| R5 | **429 降级后 main 依赖 TL** | 高（必须防） | B4 绝对默认：任何 gate 不因 TL 缺席悬挂——超时/缺席 = 判负或 escalate human，main 进入 direct 模式；把 B4 写成 -race 测试断言 |
| R6 | **TL 会话上下文串味**（跨 goal 复用 S_tl） | 低-中 | 决策 D：每 goal 新 S_tl（干净基线）or 复用但 Reset 到系统 prompt；建议 MVP 每 goal 新建，审计留档另存 |
| R7 | 删除 TL 丢失审计 | 低 | 审计策略（D3 审计开关）把 TL 回合/verdict 以事件留档，删的是会话对象不是账本 |

---

## 4. 与 seelex/Seele 现成设施的映射（证据）

| 需要 | 现成设施（证据） | 缺口 |
|---|---|---|
| S_tl 独立会话 | seelex main 会话装配函数 + `session.NewSession/Chat/NewReActLoop/Reset`（Seele session 包）；role = components 模板（system prompt/工具面/压缩策略不同） | 无（装配参数化） |
| 镜像增量读 | `UnifiedEventReader.LoadRange/QueryRange(fromSeq,toSeq)` by session/node（seelebridge/events_unified.go:118-140）；事件 seq（At.UnixNano，events_unified.go:85-94）；持久 EventStore 双写（seelebridge/events.go） | 需给"跨会话订阅 + lastAppliedSeq 游标"一个小封装（产品层 ~百行） |
| 指令流注入 | `injectPendingSubagentContextsFor(sessionID)`（application/core/service_input.go:25，chat.go:118/205 每轮前调用）；goal pending 队列 MaxDirectiveQueue=32（a2a.go:26） | 无（TLDirective 复用为信封 payload） |
| 只读工具面 | `agent/bridge RegistryRuntime.VisibleTools(ctx)+VisibilityPolicy`（Seele agent/bridge/registry_runtime.go:28-96）——产品给 S_tl 注入只读可见集即可 | 无 |
| 生命周期 | ctx cancel + session 停止（现有会话/子代理回收模式） | 删除/降级封装 + B4/B6 断言 |
| 审计 | 事件账本留档 + sessionstore 记录 | 按 D3 审计策略接事件流 |

---

## 5. 建议与试点（下一步）

**建议**：采纳方案 B 作为并线主线；`requirements.md` 的 R1–R8 按 §2 修订（框架需求降为
"可选浅层增强"：R6 事件类型扩展仅在要做前端 TL 泳道时上提）。上一份设计文档 §5 的 8 条框架原语面
改为 3 条产品层组件：`peersession`（后台会话装配+只读工具面）、`mirror`（增量镜像+游标+水位）、
`directivebus`（指令信封+去重+受信注入）。

**试点 POC（在 seelex goal 域，最快验证路径）**：把现 goal MVP 的同步信箱 TL
替换为"独立后台 TL 会话 + 镜像 + 指令流"：
1. goal_begin 用现 main 装配函数（换 TL system prompt + 只读工具面 + 独立 durable history）spawn S_tl；
2. main 每轮事件 → mirror 增量拉取 → TL 回合（异步、水位触发、回合预算）；
3. TL 回合 verdict/directive → directivebus 去重入队 → main 下一轮受信注入（复用 injectPendingSubagentContextsFor 挂点）；
4. 生命周期：goal 收敛 / abort / 模拟 429 → B6 停议程 → B4 判负/升人工 → reap S_tl；
5. 度量：镜像滞后水位、每回合注入 token、TL 缺席时 main 完成率。
验收 = headless 真 API 场景（goal_begin→干活→TL 拦截纠偏→收口）+ `-race`（B1/B2/B4/B6 断言）+ token 成本对比。

POC 成立后：再决定是否把 mirror/directivebus 的通用形状上提 Seele（可选增强，浅层），
以及前端是否要做 TL 泳道（决定 R6 事件扩展是否值得）。

---

## 6. 待用户拍板（接需求单开放问题）

1. **镜像粒度**：全量转录事件 vs 窗口事件（milestone/tool 打点/压缩摘要）+ TL 自重建帧？建议后者（控 token）。
2. **S_tl 跨 goal 复用**：每 goal 新会话（干净）还是持久+Reset？建议 MVP 每 goal 新建。
3. **审计**：删 S_tl 会话对象但 TL 回合/verdict 留事件账本（建议是）。
4. **TL 工具面**：纯只读工具集 vs 无工具（仅读镜像帧评估）？建议先无工具/只读最小集，跑通再加。
5. **B4 判负默认**：TL 缺席时终态 gate 直接放行（direct）还是升人工？建议：low 风险放行、high 升人工（与审批预筛一致）。

---

## 7. 证据索引
- seelex：`seelebridge/events_unified.go:85-94,118-140`（seq + LoadRange/QueryRange 增量读，G7）；
  `seelebridge/events.go:32-59`（事件源 session_id 定位/持久化）；`application/core/service_input.go:25`
  + `application/core/chat.go:118,205`（pending 注入挂点）；`application/core/goal/a2a.go:26`
  （MaxDirectiveQueue=32）；`seelebridge/session/subagent_context.go`（actor 范式）。
- Seele：`session/`（NewSession/Chat/ReActLoop/Reset/durable history）；`agent/bridge/registry_runtime.go:28-96`
  （VisibilityPolicy 只读面）；`event/`（Type/Status/Recorder/Heartbeat）；`workplan/runtime/forkexec`
  （并发分支隔离，本方案仅间接复用）；GitHub `RedHuang-0622/Seele` HEAD=69374b7，seelex pin v0.1.2。
- 前置：`docs/2026-09-07-seele-a2a-framework-req/{requirements,same-session-governance-math}.md`。
