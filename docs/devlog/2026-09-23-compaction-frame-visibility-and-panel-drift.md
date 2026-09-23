# 回合之间压缩必须留记录 + 压缩帧正文可回读（前后端可见面）（2026-09-23 第 2 轮）

> 触发：用户在另一个会话里执行压缩后拿到回执
> 「可变 transcript 已折叠为有界 checkpoint 帧并按会话落盘（引擎历史已换成压缩形态）；但该回合的任务执行已收尾，
> 压缩记录只在执行中产生，故本次不留记录。原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn /
> search_history 回读。」并指出两个现象：**状态里看不到压缩帧**、**当前会话的压缩帧像是占位符**。
>
> 续接到上一份 devlog（`2026-09-23-compact-manual-policy-and-skill-sigil-drift.md`）：那一轮修的是"显式压缩被阈值挡住"
> 与"没写记录被当成没压缩"；这一轮修的是它留下的两个尾巴——**记录门槛只在 Running**、**帧正文从不进前端**。

## 1. 根因（全部可复现，不是猜测）

### 1.1 `folded_without_record` 不是偶发：它与最自然的用法正面冲突

链路：`/compact` 是**命令**，不走 chat 流、不建执行纪元（`input_router` → `submitCommand`），因此回合结束后
内存里仍留着上一回合的 `TaskExecutionState`（`st.taskExecution` 只在"冷恢复且无 projection"时清空，
`task_context_state.go` 的 `restoreTaskProjectionLocked`）→ 纪元门 `state.RequestID == requestID` 通过 → 强制折叠
真的发生（引擎历史替换、checkpoint 落盘、`ContextVersion++`）→ 但记录门槛

```go
// application/core/task_context/task_context_state.go（改前）
if state == nil || state.RequestID != requestID || state.Status != StatusRunning {
    return false
}
```

把这次压缩拒之门外 → `CompactFoldedUnrecorded` → 用户看到的那句"不留记录"。

**结论**：四种分类里，「回合之间手动压缩」恰好是唯一必然被拒的组合，而它是最常见的用法。这不是 bug 的偶发，
是口径写死的必然。

### 1.2 两个可见面因此都是空的

- 记录为空 → `renderContextCompactions([])` 直接返回 `""`（右栏状态页整块 `hidden`）；轨迹页压缩轨是条件渲染
  （`marks.length ? renderCompressionLane(marks) : ""`），没记录就整条不画、连"压缩 ×N"提示都没有。
- 即使有记录也只有一句**硬编码英文占位句**（`context-summary.js` 的
  `Task checkpoint retained; details can be re-read when needed.`）：没有区间、没有来源、不可展开。用户说的
  "压缩帧像占位符"就是它。

### 1.3 帧正文根本没有前端通路

| 帧的形态 | 存哪 | 前端可见 |
|---|---|---|
| `model.TaskCheckpoint`（有界 checkpoint） | 会话记录 `Checkpoints` + projection | 否（快照里没有字段） |
| `AutonomousCompactionMessage`（自主压缩帧正文） | 只在内存 engine history（动态尾部，回合后剔除） | 否 |
| 引擎侧 compact frame（`seelexctx` → `PushCompact`） | 会话记录 `CompactStack` | 仅经左栏「历史检索」 |
| `model.ContextCompaction`（公开记录） | 会话记录 `Execution.Task` | 是（但只有 version/reason/计数） |

并且**应用侧折叠不写引擎压缩栈**（`PushCompact` 只有引擎侧调用点），所以左栏检索也捡不到应用侧折叠出来的帧。

### 1.4 轨迹只有刻度，没有分界

压缩轨只画一个 `#N` 小段，没有"以上被折掉了"的分界表达；详情只给元数据 + 一句"可经
`read_compressed_turn` / `search_history` 读回"——而那是**模型侧工具**，前端点不开。

### 1.5 命令/工具在面板里平铺，没有可执行性区分

`/` 面板是"全量入口（命令 + 工具 + Skill）"，但提交路径只执行命令与 Skill。工具候选与命令候选外观完全一样，
于是用户照着面板打 `/compact_context` 只得一句"未知命令"（上一轮已加提示，但面板本身没标）。
另外兼容别名工具（`todolist_*`、`taskadd`、`switch_mode`，注册在 `main.go` 与 `seelebridge/task/tools.go`）
与主名各占一行，同一条能力看起来是两个工具。

## 2. 改法

### 2.1 记录门槛：Running **或** 显式要求（F1）

- `model.ContextCompaction` 增 `Origin`（`auto` / `explicit` / `explicit_after_turn`）与
  `CompactionOrigin*` 常量、`ExplicitCompactionOrigin` 判定；
- `_RecordContextCompactionLocked` 放宽为：`state.Status != StatusRunning` 时，只有显式来源才放行——
  **自动路径（软/硬阈值）保持原口径**，避免把上一回合的收尾状态误标成"该回合压缩过"；
- 折叠处按当时状态写来源：`options.forceCompact && status != Running` → `explicit_after_turn`；
- 快照面补一条兜底：快照里还没有任务面时按内存状态重建（`_TaskStateFor`），否则记录只活在内存、前端依旧空白；
  任务面已存在但属于别的请求时不覆盖（那是另一个会话的在飞回合）。

回执统一走 `compactionRecordNote`（`/compact` 命令与 `compact_context` 工具同句）：

```
已压缩上下文：v12（上下文预算），被压区间 消息 message-1..message-103（事件 1..6），
装配后估算 20480 tokens（判据量 129409），来源 explicit_after_turn；帧正文 ref tr-xxxx（状态页「上下文压缩」条目可展开查看）。
原始轮次仍在会话存储里，可用 read_tool_result / read_compressed_turn / search_history 回读细节。
```

`folded_without_record` 的文案改为如实说"这次是自动路径 + 回合已收尾"，不再泛泛说"记录只在执行中产生"。

### 2.2 帧正文可回读（F2）

折叠那一刻渲染 `compactionFrameBody`（新函数，`context_runtime/coordinator.go`），内容分三段、互不混写：

```
<!-- seelex:context-checkpoint-frame:v1 -->
# Context checkpoint frame v12
reason: context_budget · origin: explicit_after_turn · at: …
folded: 消息 message-1..message-103（事件 1..6）（这段被折出 provider 历史；原文仍在会话存储里，可按区间回读）
tokens: compared 129409 → assembled 20480 (soft 118962 / hard 142754)
injected: no —— 本次走保留窗口路径：provider 历史 = 稳定 system 前缀 + 保留窗口 + plan，未注入下面的证据摘要

## Task evidence checkpoint
<ContextSummary：objective/plan/证据/工具结果>

## Plan tail (kept in provider history)
<plan 尾部>
```

`injected` 这一行是刻意的：普通显式压缩走"保留窗口"路径，**并没有**把证据摘要注入 provider 历史
（`tryFitExecutionHistory` 不接受 summary，只有自主压缩的 `compressExecutionHistory` 才注入）。把两者写成
同一句话就等于告诉用户"模型看得到这份摘要"——那是假的。

正文写进既有会话内容存储（`StoreToolResultForLocked`，内容寻址 ref），记录里只带
`frame_ref/frame_bytes/frame_tokens`；**不加新 Bridge**，前端复用已有的
`Bridge.ToolResultContent(ref, offset, limit)` 分页读回。

### 2.3 右栏条目：删掉占位句，展开看正文（F3）

`context-summary.js` 重写：版本 + 原因（补上 `context_budget_autonomous`）+ 来源 + 被压区间 + 估算 + 时间；
有 `frame_ref` 就提供「查看帧正文」按钮，展开后**复用轨迹详情同一容器与分页组件**
（`.axis-detail` / `.axis-detail-text` / `data-compact-frame-load`），不再自造第二套面板与第二套分页交互。
没有正文可读时如实写"本次没有可回读正文"。

### 2.4 轨迹分界虚线（F4，用户明确要求）

新增「分界」轨（`renderCompressionCutRow`）：每个压缩点一条竖向虚线 + 标注
`以上 消息 message-1..message-103（事件 1..6）已被折叠`，虚线向上穿过所有轨（超出部分由
`.context-axis-track` 的 `overflow: hidden` 裁剪），悬停给出"折了什么 / 原文在哪 / 谁要的"。
**只画锚定在本页的刻度**：`offPage` 的刻度是钳在页边界的占位，位置本身不真实，给它画线就是画假线
（点击它仍然跳页）。详情里的入口也换成前端自己的分页读回，不再把用户指向模型侧工具。

### 2.5 面板口径统一（F5）

- `Suggestion` 增 `Executable`：命令/Skill/Plugin/团队 = true，**工具 = false**；前端在工具行加「模型侧」标记
  （`title` 说明"由模型调用、经权限门，不能从输入框直接执行"）；
- 兼容别名工具折叠进主名那一行（`toolAliases` 表 + 主名说明追加"（兼容别名 …）"）；**主名不可见时别名保留成行**
  ——绝不隐藏仍然可用的能力；
- 措辞统一：`application/application.go` 与 `application/core/input_router/router.go` 的前缀说明改成
  "`/` 全量入口（命令 + 工具 + Skill；仅命令/Skill 可直接执行，工具由模型调用）"，与 `completion.go`、`/help`、
  前端文档对齐（此前两处仍叫"命令面板"，是漂移）。

### 2.6 展示口径单一事实源

新增 `gui/frontend/dist/compaction-format.js`（纯函数、零依赖）：原因/来源标签、区间渲染、分界标注；
右栏条目与轨迹压缩轨都从这里取（此前两边各写一份，且区间写法已经不同）。轨迹模块头部注释同步声明这唯一依赖。

## 3. 验证

- `go build ./...`、`go vet ./application/core/` 通过；
- `go test ./application/core/` 通过，其中新增/改写：
  - `TestCompactManualAfterTurnRecordsExplicitOrigin`（回合后显式压缩 → 记录 + `explicit_after_turn` + 帧 ref + 进快照）；
  - `TestCompactAfterTurnSurfacesRecordWithoutTaskFace`（无任务面时也能进快照）；
  - `TestAutoCompactionAfterTurnKeepsRecordGate`（自动路径收尾后**仍不**补记，且断言夹具真的折叠了）；
  - `TestCompactionFrameBodyIsReadableByRef`（`frame_ref` 能经 `ToolResultContent` 读回正文，体量一致）；
  - `context_runtime/compaction_frame_test.go`（帧正文如实区分 injected=yes/no、无证据/无区间不编造）；
  - `completion_test.go`（工具候选 `Executable=false`、别名折叠、主名缺失时别名保留）。
- 前端 `node --test dist/*.test.mjs`：432 passed / 0 failed，其中新增压缩区间、分界虚线、帧正文分页、
  钳位刻度不画线、右栏展开读回等用例。
- GUI 冒烟：**未在本轮执行**（运行中的 GUI 是旧构建，需要重开进程才会加载新的 `dist/*.js` 与 `styles.css`）。

## 4. 遗留与已知边界

1. 帧正文是"折叠那一刻"的快照，不是 provider 历史本身；`injected` 行明确区分两者。
2. 普通显式压缩（保留窗口路径）不把证据摘要注入 provider 历史——这是既有行为，本轮只是**如实展示**，
   没有改变它。若要改变（把摘要也注入窗口路径），属于 provider 字节变化，需单独评估前缀缓存影响。
3. `frame_ref` 走会话内容存储；fork 的可达性裁剪（`reachableToolResultRefs`）以事件/记录/帧为输入，
   压缩记录的 `frame_ref` 是否被计入——**已确认并已修**（同批，见
   `2026-09-23-compaction-progress-visible-from-first-instant.md` §5）：压缩记录的 `FrameRef`
   现随继承的记录一起进入子会话可达集合（`session_runtime/fork.go`），钉子
   `TestForkSessionKeepsCompactionFrameRefReachable`；同时把口径写进代码——这条规则只描述
   **会话分叉 ForkSession**，**子代理派发 ForkSubagent** 落 `subagent_<hash>/` 子树、复用主会话
   `big_tool_result`、无独立 refs 索引，没有"按 ref 裁剪"这一层（`sessionstore/fork_store.go` 头部
   + `docs/arch/README.md` 分支链路）。子代理侧的端到端读回仍无用例（本轮只声明，不构造）。
4. TUI 的建议面尚未显示「模型侧」标记（`Suggestion.Executable` 已在契约里，TUI 可自行消费）。
5. 前端测试仍以纯函数为主；DOM 交互（展开/分页/虚线点击）靠人工冒烟。
