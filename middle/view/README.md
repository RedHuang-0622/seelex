# middle/view

## 生态位

`middle/view` 是中间层的**视图读数面**：把权威快照（`model.Snapshot`）派生成三类
只读写数——诊断文本（`/diag`）、性能指标（GUI 性能追踪钩子）、运行时投影
（可见性投影 + 父证据投影）。主要调用方是 `application/core`（它持有锁、进程级
上限与端口，负责在锁下取数）与门面 `application/application.go`（`RenderDiag`）。

本包是 M5 从 `application/core` 三个根文件归位的（plan §2.4：`diagnostics.go`、
`perf_stats.go`、`runtime_projection.go` → `middle/view/`）：计算与判据原样搬入，
行为零变化；`application/core` 只留"锁下取数 + 转发"的兼容缝，门面签名不变。

## 职责与非职责

- 职责：从快照派生只读写数的**计算与判据**——诊断文本布局、"最近一条可见用户
  目标"的口径与 200 rune 截断、性能指标口径（字符数/截断条数/归档读数），以及
  投影形状的构造。
- 非职责：**取锁与取数**（锁在 `kernel/state` 的 `Core.ViewMu`，进程级上限在
  `backend/limit`，归档读数在任务协调器）；落盘、事件发布、会话状态机；任何
  **写**会话事实的动作。

## 目录结构

| 文件 | 职责 |
|---|---|
| `diagnostics.go` | `RenderDiag`：`/diag` 诊断文本（引擎/插件/账号/技能四段） |
| `metrics.go` | `PerfStats`：GUI 性能追踪钩子的后端数据面（只上报数量/体积） |
| `projection.go` | 运行时投影：`RuntimeVisibility` / `ParentEvidence` / `LatestVisibleUserGoal` |
| `view_test.go` | 本包用例（读数口径、目标取法与截断、投影字段） |

## 核心实现

- `RenderDiag(snapshot)`：纯函数，按 `Runtime` 分段拼文本。`Plan` 段的
  `done/total` 用 `NodeCompleted | NodeSkipped` 两个状态计完成数——**状态词表只有
  一份**（`dto.NodeStatus`，经 `middle/protocol/model` 再导出）。
- `PerfStats(snapshot, historyWindow, toolRefs)`：三个输入都由宿主传入，
  因为它们的来源都不在中间层（进程级上限 / 归档读数 / 锁）。`SnapshotBytes` 是
  **估算**（锁内 `json.Marshal`），不落盘，仅给前端对照 DOM/JS heap。
- `LatestVisibleUserGoal(messages)`：从尾向头找第一条**可见**用户输入——空正文
  跳过、子代理产出标记行（`view_state.SubagentContextMarker` 前缀）跳过；找到后
  按 200 rune 截断并加省略号。
- `RuntimeVisibility` / `ParentEvidence`：投影形状的**唯一构造处**（不可变值，
  宿主在锁内取数、锁外发布）。

## 数据流

```text
引擎/工具/任务域写 Core.Snapshot（kernel/state，持有者：application/core）
        │
        ├── /diag 命令 ────────────► view.RenderDiag(snapshot) ──► 文本通知
        ├── GUI 轮询 PerfStats ────► view.PerfStats(快照, 上限, 归档读数)
        └── 回合尾投影发布 ────────► view.RuntimeVisibility / ParentEvidence
                                          │
                                          └─► contract.RuntimePort.Set*Projection（Runtime 侧只读消费）
```

## 依赖方向

允许依赖：`middle/protocol/model`（快照与行类型）、`middle/view/state`
（`SubagentContextMarker` 这一个共享标记）、`contract/dto`（投影形状）、标准库。

禁止：反向依赖 `application/*`（本包是中间层）；依赖 `backend/*`（进程级上限与
归档读数一律按参数传入——`middle` 不得回指后端板块）；自取锁或持有宿主字段。

## 并发与错误语义

- 本包**无状态、无锁**：所有函数都可并发调用，前提是调用方按自己的锁口径传入
  一致的一份快照（宿主在 `Core.ViewMu` 读锁下取数）。
- `PerfStats` 的 `json.Marshal` 由宿主在锁内调用（既有口径，未改）：它只读快照，
  不阻塞任何写者以外的路径。
- 无错误路径：派生读数失败即"少一项读数"，不影响回合与持久化。

## 扩展方式

- 新增诊断段：在 `RenderDiag` 内加段并同步 `view_test.go` 的断言；**不要**引入
  第二份状态词表（节点完成口径取 `model.Node*`）。
- 新增读数：先问"它是不是从快照派生的纯函数"——是则落在本包，输入按参数传入；
  否则它属于宿主或领域包。
- 改"父证据"口径（目标取法/截断长度）只改 `projection.go` 一处，并把口径变化
  写进[本轮工作包记录](../../docs/2026-10-15-architecture-reshape/m5-record.md)；
  该口径同时是子代理上下文里"父会话在做什么"的唯一来源。

## Review 指南

- 本包有没有出现 `sync`、`Mutex`、`RLock`？有就是越界（锁归宿主）。
- 有没有 import `backend/*` 或 `application/*`？有就是分层回指。
- `PerfStats` 的三处输入是否仍由宿主传入？把 `Limits()` 拉进来会让中间层依赖
  `backend/limit`。
- 节点完成口径是否仍只有 `NodeCompleted | NodeSkipped` 一处？（诊断文本的
  `done/total` 与计划进度必须是同一口径）

## 测试与验证

```text
go test ./middle/view/ -count=1
go test ./application/core/ -run 'PerfStats|RenderDiag|RuntimeProjection' -count=1   # 宿主缝
go test ./e2e/ -count=1                                                              # 模块表/闭包门禁
```

`view_test.go` 覆盖：诊断四段与计划完成计数、性能指标口径（字符数/最大条/截断
条数/归档）、"最近可见用户目标"的跳过与截断、投影字段构造。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### diagnostics.go

- `func RenderDiag(snapshot model.Snapshot) string` — RenderDiag 构建诊断文本。由 /diag 命令调用。

### metrics.go

- `func PerfStats(snapshot model.Snapshot, historyWindow int, toolRefs []model.ToolResultRef) model.PerfStats` — PerfStats 返回 GUI 性能追踪钩子的后端数据面：只上报数量/体积等无内容

### projection.go

- `func RuntimeVisibility(goalSkillActive bool, governance *dto.GoalGovernanceView) dto.RuntimeVisibilityProjection` — RuntimeVisibility 构造「可见性投影」（goal 技能门控 + 当前会话的 goal 治理
- `func ParentEvidence(sessionID string, messages []model.Message, conversationCount int) dto.ParentEvidenceProjection` — ParentEvidence 构造「父证据投影」：会话 ID、最近一条可见用户目标正文、
- `func LatestVisibleUserGoal(messages []model.Message) string` — LatestVisibleUserGoal 返回最后一条**可见**用户输入正文作为"父会话在做什么"的
- `func truncateRuntimeProjectionGoal(content string) string`

### view_test.go

- `func TestRenderDiagReadsRuntimeReadings(t *testing.T)`
- `func TestRenderDiagWithoutRuntimeSections(t *testing.T)`
- `func TestPerfStatsCountsConversationAndArchive(t *testing.T)`
- `func TestLatestVisibleUserGoal(t *testing.T)`
- `func TestRuntimeProjectionConstructors(t *testing.T)`

