# Seelex `context` 包改进方案

> 版本: v1.0  
> 创建日期: 2025-12-21  
> 状态: 方案评审中

---

## 目录

1. [前置分析](#前置分析)
2. [plan-design：启发式设计](#plan-design启发式设计)
3. [plan-efficiency：规划式效率](#plan-efficiency规划式效率)
4. [plan-norm：约束式规范](#plan-norm约束式规范)
5. [推荐路线图](#推荐路线图)

---

## 前置分析

### 当前 context 包全景

| 文件 | 内容 | 行数 |
|------|------|:----:|
| `context/bridge.go` | `ContextSnapshot`、`Export`、`ExportWithGoal`、`Import`、`Format` + Builder 方法 | ~250 |

### 已识别的问题

| # | 问题 | 严重程度 |
|:-:|------|:--------:|
| 1 | **Export 太 naive**——仅取首条用户消息作为 Goal，未利用 Seele 的 `tracer.Tree` 提取结构化信息 | 中 |
| 2 | **Import 覆盖已有 prompt**——使用 `SetSystemPrompt()` 直接覆盖，应改为追加/合并 | 高 |
| 3 | **TokenEstimate 字段存在但从未填充**——缺少 token 估算逻辑 | 低 |
| 4 | **缺少上下文压缩**——当历史过长无法处理 | 高 |
| 5 | **缺少双向合并**——子代理结果无法回写父代理 | 中 |
| 6 | **紧耦合 `engine.Engine`**——无接口抽象，不便测试 | 中 |
| 7 | **缺少 ContextProvider 模式**——只能从 Engine 导出 | 中 |
| 8 | **Import 无返回值**——缺少错误反馈 | 低 |
| 9 | **缺少快照验证机制** | 低 |

### Seele 层已有的可复用能力

| 能力 | 来源 | 当前 context 是否利用 |
|------|------|:----:|
| `Engine.History() []types.Message` | engine.Engine | ✅ Export 中用了 |
| `Engine.ExportTrace() *tracer.Tree` | engine.Engine | ❌ **未用** |
| `Engine.Tracer() tracer.Tracer` | engine.Engine | ❌ **未用** |
| `Engine.SetSystemPrompt(prompt)` | engine.Engine | ✅ Import 中用了 |
| `Engine.SessionID() string` | engine.Engine | ✅ 用了 |
| `Engine.ClearHistory()` | engine.Engine | ❌ **未用** |
| `storage.Store.Save/Load` | seelectx/storage | ❌ 未直接引用 |
| `tracer.Tree{ Nodes []Node }` | seelectx/tracer | ❌ **未用** |
| `tracer.Node{ Kind, Attrs, Children }` | seelectx/tracer | ❌ **未用** |

---

## plan-design：启发式设计

### 行业标杆调研

#### 1. Claude Code — Context Providers

Claude Code 的 context 管理有以下模式值得借鉴：

- **Provider 模式**：不同的上下文来源（文件、Git diff、终端输出）都实现同一 Provider 接口
- **Budget 机制**：token 预算驱动——当超出限制时自动压缩/丢弃低优先级上下文
- **结构化注入**：上下文以结构化文本注入，带有明确的标题和分隔符（当前 Format() 已有雏形）

#### 2. LangChain — Memory 体系

- **ConversationSummaryMemory**：当上下文过长时自动生成摘要
- **ConversationBufferWindowMemory**：保留最近 N 轮
- **VectorStoreMemory**：基于语义检索的上下文
- **CombinedMemory**：多种策略组合

#### 3. Continue.dev — Context Provider

- **@file, @folder, @problem** 等标签式上下文引用
- **检索增强（RAG）**：从 embedding 中检索相关内容

---

### 改进方案

#### 方案 A：ContextProvider 接口化（⭐推荐）

```go
// context/provider.go — 核心接口层

// Provider 抽象上下文来源
type Provider interface {
    // Export 从来源导出上下文快照
    Export(ctx context.Context) (*ContextSnapshot, error)
    // Name 提供者名称
    Name() string
}

// Mergable 可合并接口（双向上下文继承）
type Mergable interface {
    // MergeBack 将子代理的结果合并回父代理快照
    MergeBack(child *ContextSnapshot) error
}

// Compactable 可压缩接口
type Compactable interface {
    // Compact 压缩上下文到目标 token 预算内
    Compact(snap *ContextSnapshot, budget int) (*ContextSnapshot, error)
}
```

**三个具象 Provider**：

| Provider | 数据来源 | 实现方案 |
|----------|----------|----------|
| `EngineProvider` | `engine.Engine` | 现有逻辑重构为 Provider 接口 |
| `TraceProvider` | `engine.ExportTrace()` | **新增**：从 `tracer.Tree` 提取决策、发现、进度 |
| `StoreProvider` | `storage.Store` | **新增**：从持久化存储加载 |

**关键创新：TraceProvider**

Seele 的 `tracer.Tree` 包含 `llm_call`、`tool_dispatch` 等 span 节点，每个 span 有 `Attrs`（key-value 元数据）和 `Events`（事件日志）。这些结构化数据可以自动提取为：

```
ContextSnapshot.Decisions    ← 从 tool_dispatch 的结果摘要提取
ContextSnapshot.Findings     ← 从 LLM call 的 reasoning 提取
ContextSnapshot.TokenEstimate ← 从 span attr 中的 token 计数提取
ContextSnapshot.Progress     ← 从完成的 tool_calls 聚合
```

#### 方案 B：Compactor 分层压缩

```
ContextSnapshot (原始)
      │
      ▼
Compactor.Compact(snap, budget)
      │
      ├─ Budget ≥ 500 tokens → 全量快照
      ├─ Budget 200~499      → 摘要模式（压缩 Decisions, Findings 为统计摘要）
      └─ Budget < 200        → 极简模式（仅 Goal + Progress + Escape）
```

#### 方案 C：BidirectionalMerger 双向合并

```
父代理 Export() ───→ ContextSnapshot ───→ 子代理 Import()
                                                    │
                                          子代理完成时
                                                    │
                                              MergeBack()
                                                    │
                                              ▼
                               父代理更新：Findings += 子代理发现
                                         Progress += 子代理产出
                                         Decisions += 子代理决策
```

---

### 架构决策记录 (ADR)

| 决策 | 选择 | 备选 | 理由 |
|------|------|------|------|
| 接口抽象 | 是 | 保持现状 | Testability + 可扩展 Provider |
| TraceProvider | 新增 | 让调用方手动提取 | 减少重复工作，利用已有结构化数据 |
| Import 改为追加 | 是 | 保持覆盖 | 兼容已有 system prompt |
| MergeBack | 接口可选 | 强制要求 | 并非所有场景需要回写 |
| 向后兼容 | 保留旧 API | 废弃 | skills/goal.md 已引用 Export/Import |

---

### 风险清单

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 接口设计不当导致扩展困难 | 高 | 参考行业标杆，预留扩展点 |
| Token 估算偏差大 | 中 | 提供可插拔的计数器接口 |
| 压缩可能丢失关键信息 | 高 | 保留关键决策和发现，压缩历史消息 |
| 迁移成本 | 低 | 保持旧接口可用，渐进式迁移 |

---

## plan-efficiency：规划式效率

### 打点表

| 阶段 | 输入 | 输出 | 预计耗时 | 依赖 | 度量指标 | 完成标准 |
|------|------|------|:--------:|:----:|:--------:|----------|
| P1 定义接口 | 调研方案 | `provider.go`（Provider 接口） | 1 次迭代 | 无 | 接口方法数 ≤4 | 编译通过 |
| P2 重构 EngineProvider | 现有 Export/Import | EngineProvider 实现 | 1 次迭代 | P1 | 功能等价 | 旧测试全通过 |
| P3 实现 TraceProvider | `engine.ExportTrace()` | TraceProvider 实现 | 1 次迭代 | P1 | 提取字段 ≥3 个 | 能从 trace 树提取 Decisions/Findings/TokenEstimate |
| P4 实现 Compactor | 快照 + budget | Compact() 实现 | 1 次迭代 | P1 | 压缩率 ≥50% | 2000 token → ≤500 token 语义保留 |
| P5 实现 MergeBack | 子代理快照 | MergeBack() 实现 | 0.5 次 | P1 | 字段合并正确 | 并行和串行场景均验证 |
| P6 单元测试 | 所有新代码 | 测试覆盖 | 1 次迭代 | P2~P5 | 行覆盖 ≥80% | go test 通过 |
| P7 集成测试 | skills/goal.md 场景 | A2A 上下文承袭 | 0.5 次 | P6 | 端到端场景通过 | Export→Import→MergeBack 全链路 |
| P8 文档更新 | context/README.md | 文档 + 示例 | 0.5 次 | P7 | 读文档可理解 | 发布前核查 |

### 活动图

```
[入口]
   │
   ▼
[P1: 定义接口层] ──── 无依赖，第一个做
   │
   ├──────────────────────────┐
   ▼                          ▼
[P2: EngineProvider]    [P3: TraceProvider]   ← 可并行
   │                          │
   └──────────┬───────────────┘
              ▼
         [P4: Compactor]   ← 依赖 P1 接口，可用 P2 数据测试
              │
              ▼
         [P5: MergeBack]   ← 依赖 P1
              │
              ▼
         [P6: 单元测试]    ← 依赖 P2~P5
              │
              ▼
         [P7: 集成测试]    ← 依赖 P6
              │
              ▼
         [P8: 文档]
              │
              ▼
         [交付]
```

### 各节点详细定义

#### P1: 定义 Provider 接口层

- **context_inherit:**
  - goal: "定义 Context Provider/Mergeable/Compactable 接口"
  - decisions: ["参考 Claude Code Provider 模式", "接口保持极简（≤4 方法）"]
  - constraints: ["Go 1.25+", "不引入新依赖"]
- **files_to_write:** `context/provider.go`
- **expected_output:** 编译通过的 interface 定义 + 接口文档注释
- **retry_on_fail:** 1
- **timeout:** 180s

#### P2: 重构 EngineProvider

- **context_inherit:**
  - goal: "将现有 Export/Import 重构为 EngineProvider"
  - decisions: ["保持 Export/Import 签名不变（向后兼容）", "内部委托给 EngineProvider"]
  - constraints: ["不破坏 skills/goal.md 的调用方"]
- **files_to_modify:** `context/bridge.go`
- **expected_output:** Export/Import 保持相同行为，内部重构
- **retry_on_fail:** 1

#### P3: 实现 TraceProvider

- **context_inherit:**
  - goal: "从 engine.ExportTrace() *tracer.Tree 自动提取上下文"
  - decisions:
    - "从 llm_call span 的 reasoning 提取 Findings"
    - "从 tool_dispatch span 提取 Decisions"
    - "从 span attrs 提取 token 计数"
  - constraints: ["仅读取 tracer.Tree 公开字段", "tree 可能为 nil"]
- **files_to_write:** `context/trace_provider.go`
- **expected_output:** 能从 tracer.Tree 中提取 3+ 字段到 ContextSnapshot
- **retry_on_fail:** 2

#### P4: 实现 Compactor

- **context_inherit:**
  - goal: "基于 token 预算的上下文压缩"
  - decisions: ["三级压缩策略（全量/摘要/极简）", "保留关键决策和发现"]
  - constraints: ["压缩不可逆（不保存被压缩部分）"]
- **files_to_write:** `context/compactor.go`
- **expected_output:** 2000 token 上下文压缩至 ≤500 token 且语义保留
- **retry_on_fail:** 2

#### P5: 实现 MergeBack

- **context_inherit:**
  - goal: "子代理结果回写父代理上下文"
  - decisions: ["Findings/Decisions append 模式", "Progress 替换模式（子代理产出即新进度）"]
  - constraints: ["并发场景用 copy-on-write"]
- **files_to_write:** `context/merger.go`
- **expected_output:** 并行和串行场景均通过验证
- **retry_on_fail:** 1

### 上下文承袭规则

子代理 Import 时的 system prompt 结构（改进后）：

```
原 system prompt（保留）
─────────────────────
[继承上下文]
Goal: ...
Decisions: ...
Findings: ...
Constraints: ...
Progress: ...
Pending: ...
─────────────────────
结束标记
```

---

## plan-norm：约束式规范

### 需求-设计-实现-测试追溯矩阵

| 需求ID | 需求描述 | 设计决策 | 实现文件 | 测试文件 | 状态 |
|--------|----------|----------|----------|----------|:----:|
| REQ-001 | 定义统一的 Context Provider 接口 | DES-001: Provider/Mergeable/Compactable 三接口 | `context/provider.go` | `context/provider_test.go` | 📋 待实现 |
| REQ-002 | 从 Engine 导出上下文 | DES-002: 保持 Export/ExportWithGoal 签名，委托 Provider | `context/engine_provider.go` | `context/engine_provider_test.go` | 📋 待重构 |
| REQ-003 | 从 tracer.Tree 自动提取结构化信息 | DES-003: 遍历 Tree.Nodes，按 Kind 分拣 | `context/trace_provider.go` | `context/trace_provider_test.go` | 📋 待实现 |
| REQ-004 | 上下文快照压缩 | DES-004: 三级压缩策略（全量/摘要/极简） | `context/compactor.go` | `context/compactor_test.go` | 📋 待实现 |
| REQ-005 | 子代理结果回写父代理 | DES-005: MergeBack 接口 + 字段合并策略 | `context/merger.go` | `context/merger_test.go` | 📋 待实现 |
| REQ-006 | 向后兼容现有 API | DES-006: Export/Import 保持相同函数签名 | 同 REQ-002 | 复用现有调用方 | ✅ 保证 |
| REQ-007 | Token 预算管理 | DES-007: TokenEstimate 自动填充 + Compactor 预算驱动 | `context/compactor.go` | `context/compactor_test.go` | 📋 待实现 |
| REQ-008 | 快照验证 | DES-008: Validate() 方法检查必填字段 | `context/snapshot.go` | `context/snapshot_test.go` | 📋 待实现 |

### 变更影响分析

```
变更: context 包接口化 + TraceProvider + Compactor + MergeBack
│
├── 影响模块:
│   ├── context/bridge.go         — 重构为内部委托 EngineProvider（兼容包装）
│   ├── context/provider.go       — 新增
│   ├── context/trace_provider.go — 新增
│   ├── context/compactor.go      — 新增
│   ├── context/merger.go         — 新增
│   ├── context/snapshot.go       — 新增（Snapshot 类型独立文件）
│   ├── tui/commands.go           — 可能需注册 /context 命令
│   └── skills/goal.md            — 文档引用不变（API 保持兼容）
│
├── 影响测试: 无现有测试（context 包当前无测试文件）
│   └── 新增 5 个测试文件
│
├── 风险评估: 低
│   ├── Export/Import 签名不变 → 调用方零修改
│   ├── ContextSnapshot 字段不删 → 序列化兼容
│   └── Format() 逻辑不变 → system prompt 格式不变
│
├── 回滚方案: git revert + 删除新增文件
│
└── 验证计划:
    ├── go build ./context/...
    ├── go test ./context/...
    └── skills/goal.md 中的 A2A 场景手动验证
```

### 审查检查单

| # | 检查项 | 标准 | 状态 |
|:-:|--------|------|:----:|
| 1 | Provider 接口每个方法有 Godoc 注释 | ✅ 要求 | 待编码时检查 |
| 2 | 所有公开类型有示例或说明 | ✅ 要求 | 待编码时检查 |
| 3 | Export/Import 向后兼容 | 旧签名不变，行为不变 | ✅ 保证 |
| 4 | TraceProvider 处理 nil tree | 不 panic，返回空快照 | ✅ 要求 |
| 5 | Compactor 的 budget=0 处理 | 返回空快照或极简模式 | ✅ 要求 |
| 6 | MergeBack 的并发安全 | 字段合并需用 copy-on-write | ✅ 要求 |
| 7 | Validate() 返回具体错误类型 | 非通用 error | ✅ 要求 |
| 8 | token 估算与实际偏差 ≤30% | 精度检查 | ⚠️ 近似估算 |
| 9 | 新代码行覆盖 ≥80% | go test -cover | ⚠️ 目标 |
| 10 | 不需要引入 Seele 外的第三方依赖 | go.mod 不涨 | ✅ 保证 |

### Seele API 直接调用点（零新增依赖）

以下改进完全通过**调用 Seele 已有方法**实现，不需要修改 Seele 核心：

| 改进点 | Seele 调用 | 说明 |
|--------|-----------|------|
| TraceProvider | `eng.ExportTrace() *tracer.Tree` | 已有公开方法 |
| TokenEstimate | 聚合 span 的 token attrs | `tracer.Node.Attrs` 中查找 |
| Decisions 提取 | 从 tool_dispatch span 的事件摘要 | `tracer.Node.Events` 中提取 |
| Findings 提取 | 从 llm_call span 的 reasoning 相关 attr | `tracer.Node.Attrs` 中提取 |
| 快照持久化 | `store.Save(sessionID, snapJSON)` | `storage.Store.Save` 已可存任意 json |

---

## 推荐路线图

### 第 1 步：接口化（最小改动）

- 新增 `context/provider.go` → 定义 3 个接口
- 现有 Export/Import 改为委托
- **耗时**：1 次迭代
- **风险**：极低（API 不变）

### 第 2 步：TraceProvider（最大价值/最小成本）

- 新增 `context/trace_provider.go`
- 调用 `eng.ExportTrace()` 遍历 `tracer.Tree`
- **零新增依赖**
- 自动填充 Decisions、Findings、TokenEstimate

### 第 3 步：Compactor + MergeBack（增强）

- 新增 `context/compactor.go` + `context/merger.go`
- 驱动 token budget 管理
- 实现 A2A 闭环

### 第 4 步：测试 + 文档

- 5 个测试文件
- context 目录 README
- 质量保障
