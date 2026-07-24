# 证据门禁驱动的需求到 Dev 自迭代模块详细设计

> 状态：拟议方案
> 总体架构：[`../architecture.md`](../architecture.md)
> 核心契约：[`evidence-assessment.schema.json`](../schemas/evidence-assessment.schema.json)、[`dev-iteration.schema.json`](../schemas/dev-iteration.schema.json)

## 1. 目标与原则

本模块建立可审计的“资料 → 候选需求 → 架构 → 详细设计 → Dev → E2E → 反馈重开”闭环。LLM 负责生成候选工程对象和原子主张；混合 RAG 为每个主张召回可定位证据；确定性门禁决定自动生成资格；人工处理灰区、冲突、项目新增和豁免。

核心原则：

1. RAG 是工程证据获取机制，不只是 prompt 上下文。
2. 在线阶段不声称计算严格 Recall；使用可计算的 `evidenceReadiness`。真实 Recall 只在离线标注集评估。
3. 检索相关性、证据关系、证据充分度、Agent 自信度和人工状态是不同字段，不能混成一个 confidence。
4. 低分、未命中和冲突条目永不静默删除；它们进入人工队列并保留检索回执。
5. Rerank 只排序候选，不能判定证据是否支持结论。
6. 自动生成只消费通过当前层门禁的对象；待评审对象必须出现在范围报告中。
7. 每层产物以新 generation 发布，反馈只能重开并生成后继版本，不原地覆盖已批准基线。

## 2. 总体流程

```text
原始/客户/法规/历史/平台资料
  → 版本化、分块、内容 hash
  → LLM 候选需求 + 原子主张
  → exact + BM25 + vector + graph 混合检索
  → rerank
  → supports / contradicts / related / insufficient 判定
  → evidenceReadiness 门禁
      ├─ eligible ───────────────► 自动进入需求评审/基线
      ├─ review_required ────────► 人工确认/补证/项目特有确认
      ├─ evidence_insufficient ──► 补证任务，保留候选
      └─ blocked_by_conflict ────► 人工冲突处置
  → 需求基线 generation
  → 架构证据门禁 → 架构生成/分配 → 架构 generation
  → 详设证据门禁 → 详设/验证点生成 → 详设 generation
  → Dev work items → 实现 revision
  → schema/unit/integration/race/security/E2E
  → 反馈分类
      ├─ requirement/evidence gap → 重开需求门禁
      ├─ architecture defect ─────► 重开架构门禁
      ├─ design defect ───────────► 重开详设门禁
      ├─ implementation defect ───► 新 Dev work item
      └─ test/environment defect ─► 新测试任务或人工处理
```

## 3. 模块边界

| 组件 | 职责 | 不负责 |
|---|---|---|
| SourceCatalog | 文档版本、chunk、hash、authority/applicability metadata | 生成需求 |
| CandidateGenerator | 生成结构化候选需求和原子主张 | 决定证据有效性 |
| EvidenceRetriever | exact/BM25/vector/graph 查询与检索回执 | 判断支持/冲突 |
| EvidenceAssessor | 逐 claim 判断 evidence relationship | 修改原始资料 |
| GatePolicy | 计算指标并输出确定性 decision/reason codes | 自动豁免冲突 |
| HumanReviewQueue | 分派人工处置并保存批准人/理由/限制 | 静默提升分数 |
| BaselinePublisher | 发布需求/架构/详设 generation | 原地覆盖基线 |
| DevIterationOrchestrator | 推进阶段、创建 work item、接收验证反馈 | 直接执行任意 shell/代码修改 |
| TraceGraph | 保存 source→claim→requirement→architecture→design→code→test 关系 | 替代各对象的版本库 |

建议接口定义在 `devloop` 调用方：

```go
type EvidenceRetriever interface {
    Retrieve(context.Context, AtomicClaim, RetrievalProfile) (RetrievalReceipt, []EvidenceCandidate, error)
}

type EvidenceAssessor interface {
    Assess(context.Context, AtomicClaim, []EvidenceCandidate) ([]EvidenceBinding, error)
}

type GatePolicy interface {
    Evaluate(context.Context, EvidenceAssessment) (GateDecision, error)
}

type HumanReviewQueue interface {
    Enqueue(context.Context, ReviewItem) error
    Resolve(context.Context, ReviewResolution) error
}

type BaselinePublisher interface {
    Publish(context.Context, BaselineCandidate, ExpectedGenerationID) (GenerationID, error)
}
```

LLM/RAG provider、clock、ID generator、policy、repository 和 review queue 全部构造注入。不得使用全局 embedding client、可变阈值 map 或隐式“当前项目”。

## 4. 证据与指标

### 4.1 原子主张

候选需求先拆成可独立验证的 claim。接口、信号、枚举、单位、时间参数、安全约束和适用范围应分别成为 claim；`mandatory=true` 的 claim 缺证时不能自动通过。

每个 evidence binding 必须保存：

- 稳定 evidence ID；
- source/version/content SHA-256 和精确 locator；
- authority class 与项目/平台/版本适用性；
- 召回方法和各自分数；
- `supports / contradicts / related / insufficient` 关系；
- assessor 理由及模型/规则版本。

### 4.2 Evidence readiness

在线门禁使用以下独立指标：

| 指标 | 含义 |
|---|---|
| `claim_coverage` | mandatory claims 中有有效证据的比例 |
| `source_coverage` | policy 要求的证据类别覆盖度 |
| `support_strength` | 证据对主张的实际支持强度 |
| `authority_score` | 来源是否正式、受控、已批准 |
| `applicability_score` | 项目、平台、车型、版本是否适用 |
| `freshness_score` | 证据版本是否仍有效 |
| `trace_completeness` | 能否定位 source version、hash 和内容位置 |
| contradiction counts | 冲突数量及是否仍有关键冲突 |

`best_vector_score` 和 `best_rerank_score` 只用于诊断，不可单独决定 gate。权重、必选来源和阈值属于版本化 `GatePolicy`，不硬编码在 prompt。

离线另建带标注全集的 benchmark，评估 retrieval Recall@K、relation precision/recall、gate false-pass/false-block 和人工覆写率。在线 readiness 与离线 Recall 必须分别报告。

## 5. 四级证据门禁

推荐初始 policy（可版本化覆盖）：

| Decision | 初始条件 | 自动生成资格 |
|---|---|:---:|
| `eligible` | readiness ≥0.85、claim coverage=1、至少一个支持证据、trace complete、无未解决关键冲突 | 是 |
| `review_required` | 0.60≤readiness<0.85，或适用性/覆盖度待确认 | 否 |
| `evidence_insufficient` | readiness<0.60，或 supporting count=0，或不可定位 | 否 |
| `blocked_by_conflict` | unresolved critical contradictions>0 | 否，分数再高也阻断 |

安全相关需求可要求人工签署后才能从 automatic eligible 转为下游 eligible。门禁必须输出稳定 reason codes，例如 `supporting_evidence_missing`、`claim_coverage_incomplete`、`applicability_uncertain`、`critical_contradiction_open`。

## 6. 人工评审

支持的处置：

- `accept_evidence`：接受当前绑定；
- `add_manual_evidence`：人工指定版本化证据；
- `mark_project_specific`：确认是无历史证据的项目新增需求；
- `accept_with_limitation`：带适用范围/风险限制接受；
- `request_clarification`：退回澄清；
- `reject_candidate`：明确驳回 LLM 候选；
- `resolve_conflict`：记录冲突结论和替代证据；
- `defer`：延期，不进入当前基线。

`mark_project_specific` 不能伪造 supporting evidence。它以人工批准作为资格依据，将条目送入 architecture capability gap 流程，并保留“平台无既有证据”的事实。

所有人工结果必须记录 reviewer、时间、理由、限制、影响范围和被替代 decision。批准过期、source 版本变化或 claim 改写后必须重新评估。

## 7. 不得丢弃低证据条目

`review_required`、`evidence_insufficient` 和 `blocked_by_conflict` 必须继续存在于候选 baseline 和下游范围报告，包含：

- 原始候选与 requirement version；
- 原子主张；
- query digest、retrieval profile、模型版本和时间；
- candidate evidence IDs 和分离的检索分数；
- relationship 判定、gate reason codes；
- required actions 和人工队列 ID。

禁止自动删除、自动标“不适用”、改写成平台已有能力、保留全平台所有架构单元或让架构 Agent 静默忽略。未匹配需求应经过补证/项目特有确认后形成精确 capability gap。

## 8. 架构与详设门禁

### 架构资格

```text
requirement 在有效基线
AND evidence decision ∈ {eligible, human_accepted, project_specific}
AND 无关键冲突
AND evidence binding 未因 source/claim/policy 变化过期
→ architecture_eligible
```

架构 Agent 只接收 eligible requirements；同时收到只读的 `reviewPendingRequirements` 摘要用于范围报告，不能据此生成设计。架构冻结必须列明未分配需求、blocker、责任人和批准的风险例外。

### 详设资格

```text
上游需求资格有效
AND requirement→architecture allocation 已确认
AND 架构 decision 有证据或人工批准
AND interface/schema contract 已冻结
AND 无未处理架构冲突
→ detailed_design_eligible
```

其他单元保留在详设范围清单，不生成伪完整正文；创建人工设计任务，并显示缺失的需求证据、架构证据、分配或接口契约。

## 9. 需求到 Dev 的自迭代

`DevIterationRun` 由 [`dev-iteration.schema.json`](../schemas/dev-iteration.schema.json) 定义。每次 run 固定 source/requirement/architecture/design baselines，禁止在执行中悄悄切换输入。

阶段规则：

1. 每层 gate 通过后发布新的 immutable generation，并把 ID 写入 run。
2. 只有 `design_ready` 且 trace 到 requirement/architecture/design/verification point 完整的对象可生成 Dev work item。
3. Dev Agent 只接收选中的 work item、冻结契约、允许路径和验收标准；不接收 review-pending 条目作为隐式需求。
4. 实现产物绑定 immutable commit/revision；测试报告成为 verification evidence。
5. E2E 失败先分类，再路由到正确层，不能默认全部归因于代码。
6. 重开上游层会使下游 eligibility 过期；受影响对象重新差异分析，不全量无条件重生成。
7. 每次修订发布后继 generation，保留前一代与 evidence/decision diff。
8. 达到 `max_iterations`、出现关键冲突、缺人工授权或连续相同失败时停止自动循环并转人工。

完成条件：所有 mandatory work items 和 verification points 通过；无 open critical feedback；需求到测试 trace 完整；当前 generation/commit 已固定；release gate 明确通过。

## 10. 并发、幂等与一致性

- 一个 iteration run 使用单写 orchestrator/actor；不同项目/run 可有界并行。
- Retrieval 可按 claim 并行，但结果按稳定 claim/evidence ID 归并，排序不影响 gate。
- `(requirement_version_id, claim_digest, retrieval_profile, source_generation_id)` 形成检索幂等键。
- Gate 输入包含 policy version；相同输入必须得到确定性 decision/reason codes。
- 人工 resolution 使用 expected assessment revision，拒绝覆盖新评估。
- Baseline publish 使用 expected generation CAS；冲突后重新读取，不自动合并批准结果。
- E2E feedback 去重键绑定 run、verification、implementation revision 和 failure fingerprint。

## 11. 审计、安全与隐私

审计链必须记录 source/chunk hashes、查询 digest、retrieval/model/profile versions、候选与分数、relationship、policy 输入输出、人工处置、baseline generations、Dev revision、测试证据和反馈路由。

原始客户/法规资料按项目和角色授权；embedding/vector store 继承来源 ACL。日志不记录敏感原文、token 或完整受限 chunk。检索到无权限 evidence 时既不返回内容，也不能通过分数/存在性泄露。

LLM 输出始终是不可信候选；不得用 prompt 指令绕过 policy、人工门禁、PathGuard 或代码审批。自动迭代不得自行提升权限、扩大允许路径、提交到远端或修改基线保留策略。

## 12. E2E 场景

| ID | 场景 | 预期 |
|---|---|---|
| DEVLOOP-E2E-001 | 所有 mandatory claims 有受控支持证据 | 自动进入需求/架构 gate，trace 完整 |
| DEVLOOP-E2E-002 | 高向量分但 evidence relationship=contradicts | `blocked_by_conflict`，不生成架构 |
| DEVLOOP-E2E-003 | 无历史证据的新需求 | 保留并人工 `mark_project_specific`，创建 capability gap |
| DEVLOOP-E2E-004 | 部分 claim 无证据 | `review_required`，范围报告可见，不丢弃 |
| DEVLOOP-E2E-005 | source version/hash 更新 | 旧 assessment 过期，受影响下游 gate 重开 |
| DEVLOOP-E2E-006 | 架构证据通过但 interface 未冻结 | 详设阻断，不生成伪正文 |
| DEVLOOP-E2E-007 | E2E 发现 implementation defect | 创建 Dev work item，不重开需求 |
| DEVLOOP-E2E-008 | E2E 发现 requirement ambiguity | 重开 requirement gate，下游 eligibility 失效 |
| DEVLOOP-E2E-009 | 重复回执/反馈 | 幂等归并，不重复生成 work item |
| DEVLOOP-E2E-010 | 达到迭代上限或连续同类失败 | 自动停止并进入人工 review |

测试使用固定 source corpus、scripted candidate generator/retriever/assessor 和确定性 policy；真实 LLM/RAG 只进入离线 benchmark/nightly，不作为 PR 唯一门禁。

## 13. 计划实现路径

| 层 | 计划目录 | 职责 |
|---|---|---|
| Contracts | `devloop/contracts/` | Assessment、run、trace、reason codes |
| Source/Evidence | `evidence/catalog/`、`evidence/retrieval/` | 版本、chunk、hybrid retrieval、receipt |
| Gate | `evidence/gate/` | relationship、metrics、versioned policy |
| Review | `devloop/review/` | 人工队列、resolution、expiry |
| Orchestration | `devloop/orchestrator/` | 阶段状态机、work item、feedback routing |
| Persistence | `devloop/repository/` | generation/baseline/trace graph adapters |
| Application port | `application/devloop.go` | caller-owned use-case interface |
| GUI | `gui/frontend/dist/devloop-*.js` | 范围、证据、门禁、人工队列、run 状态 |
| E2E | `e2e/devloop/`、`gui/e2e/` | 固定 corpus 与上述 journeys |

物理 package 拆分以稳定变化轴为准。Evidence Retriever 不依赖 GUI/Dev executor；Orchestrator 只依赖 ports，不 import Git/Shell/LLM 具体实现；composition root 负责注入。

## 14. 验收标准

- 每个自动生成需求、架构单元和详设单元都能回溯到 versioned evidence 或明确人工批准。
- 低证据和冲突条目在任何 baseline/range report 中均不丢失。
- 高检索分但冲突证据不能自动通过。
- 项目特有需求能经人工确认进入 capability gap，而不被永久阻断。
- E2E 反馈能精确重开需求/架构/详设/Dev/Test 层，并只失效受影响对象。
- 自动循环有界、可暂停、可恢复、可审计，不原地覆盖已批准 generation。
- Schema、模块 DAG、unit/integration/E2E/race/security 和 generation crash-consistency 门禁全部通过。
