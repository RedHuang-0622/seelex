# 上下文前缀链路（Context Prefix Chain）——多 Agent 任务派发

> 日期：2026-08-30 | 状态：待派发 | 设计定稿见 [docs/arch/context-prefix-chain.md](../../docs/arch/context-prefix-chain.md)

## 总目标

把主会话 provider 上下文从旧链路（有界窗口 + plan/checkpoint 前置、每次重建）改为新链路：

```text
system → project → memory → compact → context(累积已定稿轮次) → plan → task → 当前输入
```

checkpoint 不入 LLM 上下文（正常路径，恢复路径保留）；plan/task 后置且不被压缩；fork 深拷贝新会话且不继承父 todolist。

## 数据结构确认清单（各任务共同依据）

| 数据结构 | 位置 | 说明 |
|---|---|---|
| `SessionContextRecord` | `sessionstore` | 四栈：PlanStack/TaskStack/SkillStack/CompactStack |
| `AssemblerOptions` | `seelexctx/assembler.go` | SystemPrompt/ProjectBlock/StackBlocks/Memories/Blocks/Window |
| `PromptBlock` | `seelexctx` | 投影顺序 system→project→memory→稳定前缀栈(skill/compact)→调用方块→窗口→尾部栈(plan/task) |
| `TokenAudit` | `application/model` | ActualPromptTokens（可加缓存命中观测字段） |
| `SessionRecord.Tasks` | `application/model` | 含 `kind=todo` 的 task（fork 需过滤） |
| `TaskCheckpoint` | `application/core/task_context` | checkpointMessage 数据源 |

## 任务拆分

| # | 任务 | 依赖 | 验收要点 | 状态 |
|---|---|---|---|---|
| A | 装配顺序对齐 + checkpoint 正常路径移除 | 无 | 最终请求顺序 system→project→memory→compact→context→plan→task→输入；正常路径无 checkpointMessage；恢复路径保留 | ✅ 已完成（2026-08-30） |
| B | 累积 context 前缀 + 压缩策略 | A | RetainedSystemHistory 保留稳定前缀+已定稿轮次；达峰才压缩；压缩折叠 compact 栈顶+窗口 | ✅ 已完成（2026-08-30） |
| C | plan/task 后置 + stacks 拆分 | A | compact 前移；plan/task 移到尾部；plan/task 不参与压缩 | ✅ 已完成（2026-08-30） |
| D | fork todolist 过滤 | 无（可并行） | truncateForkRecord 过滤 kind=todo，子会话 todolist 全新 | ✅ 已完成（工作区既有实现，本批回归通过） |
| E | 测试与契约更新 | A/B/C/D | 装配/压缩/fork/快照/事件回归 + -race 全绿 | ⏳ 待执行 |
| F | 文档同步 | A/B/C/D/E | 模块 README 标注实现状态；docs/arch 状态改已实现；根 README 导航 | ✅ 本批执行（设计/详细文档/模块 README + 提交） |

> A/B/C 强耦合（都改最终请求），建议同一 agent 串行或并行后由 E 统一校验。

---

## 任务提示词模板

### 任务 A：装配顺序对齐 + checkpoint 正常路径移除

```text
你是 Seelex 仓库的 Go 工程师。请实现「上下文前缀链路」任务 A：
把主会话 provider 上下文装配顺序改为 system → project → memory → compact →
context(累积已定稿轮次) → plan → task → 当前输入；正常路径不再向 LLM 注入
checkpointMessage（异常恢复路径 provider 504 / history-safety 必须保留）。

依据设计：docs/arch/context-prefix-chain.md
涉及文件（先读再改，含其 README 与测试）：
- application/core/context_runtime/coordinator.go（prepareExecutionContext / fitExecutionHistory / planContextMessageLocked / checkpointContextMessage / TranscriptTailHistory）
- seelexctx/assembler.go（Assemble 投影顺序、AssemblerOptions）
- seelebridge/runtime_context.go（seelexAssembler 接线）
- application/core/history_safety.go（恢复路径只读，不得移除）

要求：
1. 先写/更新契约测试再实现（AGENTS.md 流程）；
2. 不改前端协议；快照/事件字段若变化需同步 docs/gui/schemas 与示例；
3. 保持 project/stacks/memory 的 user 角色消息不进可见会话、不落盘（provider-only 前缀）；
4. go build ./... && go test ./application/core ./seelexctx ./seelebridge -count=1 通过；
5. 更新对应模块 README（实现事实同步），不自动 commit。

完成后报告：改动文件清单、装配顺序的最终代码位置、checkpoint 移出的边界（正常 vs 恢复）、测试结果。
```

### 任务 B：累积 context 前缀 + 压缩策略

```text
你是 Seelex 仓库的 Go 工程师。请实现「上下文前缀链路」任务 B：
把 RetainedSystemHistory 从「仅保留首条 system 消息」扩展为「稳定前缀 +
已定稿完整轮次的 append-only 累积段」；TranscriptTailHistory 从有界窗口（≤4 轮）
改为「达峰才压缩」；压缩时折叠 compact 栈顶 + context 窗口，保留新鲜 compact
帧与窗口剩余内容。

依据设计：docs/arch/context-prefix-chain.md（压缩推演节）
涉及文件：
- application/core/context_runtime/coordinator.go（RetainedSystemHistory / TranscriptTailHistory / fitExecutionHistory）
- application/core/context_runtime/history.go
- seelexctx/compactor 与 memory（只读参考，压缩帧语义不得破坏）

要求：
1. 累积段必须字节稳定（已定稿轮次不重排、不改写）；压缩是唯一使前缀失效的事件；
2. 压缩仍发布 Snapshot.Task.ContextCompactions（公开字段不变）；
3. 先测试后实现；go test ./application/core ./seelexctx -race -count=1 通过；
4. 更新模块 README。
完成后报告：RetainedSystemHistory 新语义、压缩触发与折叠逻辑、token 预算边界、测试结果。
```

### 任务 C：plan/task 后置 + stacks 拆分

```text
你是 Seelex 仓库的 Go 工程师。请实现「上下文前缀链路」任务 C：
拆分 RenderStackBlocks 的四栈渲染——compact 前移到稳定前缀；plan/task 栈顶帧
移到尾部（贴近当前输入）；plan/task 不参与压缩（压缩只折叠 compact 栈顶 +
context 窗口）。

依据设计：docs/arch/context-prefix-chain.md
涉及文件：
- seelexctx/assembler.go（RenderStackBlocks / Assemble 顺序）
- seelebridge/runtime_context.go（stackBlocks / relatedMemoryBlocks）
- application/core/context_runtime/coordinator.go（planMessage 位置）

要求：
1. plan 消息保持紧凑：plan_ref + 当前节点 slice（不得整计划全量入尾部）；
2. 尾部顺序稳定为 plan → task → 当前输入；
3. 先测试后实现；go test ./seelexctx ./application/core ./seelebridge -count=1 通过；
4. 更新模块 README。
完成后报告：新栈块顺序、plan/task 尾部渲染、与任务 A/B 的衔接点、测试结果。
```

### 任务 D：fork todolist 过滤（可并行）

```text
你是 Seelex 仓库的 Go 工程师。请实现「上下文前缀链路」任务 D：
fork 子会话不得继承父会话的 todolist。当前 truncateForkRecord 会把父
SessionRecord.Tasks 整体拷贝（含 kind=todo 的 task）；请过滤 kind=todo，
子会话 todolist 全新（plan/task/checkpoint/tool-results 等其余内容仍按切点拷贝）。

依据设计：docs/arch/context-prefix-chain.md（Fork 节）与 docs/2026-08-30-context-prefix-chain/task-dispatch.md
涉及文件：
- application/core/session_runtime/fork.go（truncateForkRecord / forkTaskRecordsByTime）
- application/core/session_fork.go（调用方语义确认）
- 相关 fork 测试（session_runtime/fork_test.go、application/core/session_fork 相关）

要求：
1. 保留血缘 ForkedFrom 与 plan/checkpoint/tool-results 拷贝；
2. 子会话首次物化后 todolist 为空、可正常添加；
3. 先测试后实现；go test ./application/core ./sessionstore -race -count=1 通过；
4. 更新 application/core/README.md 的 Fork 描述。
完成后报告：过滤实现位置、血缘/其余拷贝不受影响、测试结果。
```

### 任务 E：测试与契约更新

```text
你是 Seelex 仓库的 Go 工程师。请在任务 A/B/C/D 完成后做「上下文前缀链路」任务 E：
全量回归与契约对齐。

覆盖：
1. 装配顺序单测（新顺序 system→project→memory→compact→context→plan→task→输入；
   checkpoint 正常路径不存在）；
2. 压缩推演测试（达峰触发、前缀失效、ContextCompactions 记录）；
3. fork 测试（todolist 隔离、血缘、其余拷贝）；
4. 快照/事件协议回归（docs/gui/schemas + examples 若字段变化需同步）；
5. go test ./... -count=1 -timeout=300s 全绿；关键包 -race。

要求：AGENTS.md 提交约束——保留既有改动、不混文件；README 与实现一致；
不自动 commit。
完成后报告：新增/修改测试清单、全仓测试结果、协议差异说明。
```

### 任务 F：文档同步

```text
你是 Seelex 仓库的文档工程师。请在任务 A-E 完成后做「上下文前缀链路」任务 F：
把设计从「待实现」改为「已实现」，同步所有相关 README。

范围：
- docs/arch/context-prefix-chain.md：状态改已实现，按最终代码修正推演与实现点；
- application/core/README.md、seelexctx/README.md、seelebridge/README.md、
  gui/frontend/README.md：涉及上下文装配/压缩/fork 的段落按实现更新；
- README.md（根）：数据流与机制图导航表补充新链路文档链接；
- 图表（mermaid/字符画）与代码事实一致，不写规划当既成事实。

要求：相对链接存在；git diff --check 通过；不自动 commit。
完成后报告：改动文档清单与关键事实更新点。
```

## 派发顺序建议

1. 先发 **A**（装配顺序骨架），同时可发 **D**（fork 过滤，独立）；
2. A 完成后发 **B** 与 **C**（依赖 A 的顺序定义）；
3. A/B/C/D 完成后发 **E**（回归），最后 **F**（文档）。
