# 压缩 DAG 实施记录（第一批：原子先行 1-4）

> 日期：2026-09-06
> 状态：第一批已实现并测试通过；检索 DAG（5-6）与参数冒烟（7）为后续步骤。
> 契约与设计：见同目录 [design.md](design.md)。

## 本批范围（对应详设 §7 实施顺序 1-4）

1. **契约与纯函数**
   - `sessionstore/session_context.go`：`CompactFrame` 新增超上下文索引
     （`request_from/to`）、链锚字段（`prev_segment_id`/`prev_request_from/to`/
     `prev_summary_one_line`）与来源标记（`summary_source`/`anchor_source`），
     全部 `omitempty`，旧记录向后兼容（有专门解码测试）。
   - `PushCompact` 追加详设 §3.4 不变量：request 首尾同空/同非空且非倒置；
     `prev_segment_id` 首帧为空、非首帧必须与栈顶 `SegmentID` 一致；
     `prev_request_from/to` 与栈顶 request 索引一致。
   - `seelexctx/frame.go`：两章节 Summary 拼装/截取（`FrameChapter1/2`）、
     Chapter 2 DSH 小节骨架与本地折叠（`LocalChapter2`）、一句话摘要
     （`OneLineSummary`）、锚点质量标记、ChatQueue request 覆盖标签
     （controller/gap 共用）。
2. **Chapter 2 生成器**：`seelexctx/replay.go` 定义 `PrefixReplaySummarizer`
   协议（`ReplayRequest`/`ReplayResult`）与固定压缩指令，QuickChat 实现
   （字节顺序 = system（可选）→ History 原样 → 指令尾巴），返回正文归一化；
   失败语义为结构化错误，由调用方走本地折叠。
3. **Chapter 1 锚点生成器**：`RenderAnchorChapter` + `FrameAnchorSource`
   （ok/degraded），一句话由本地确定性提取（rune 截断）。
4. **压缩 DAG 执行器**
   - `seelexctx/dag.go`：规范 codec 文档与详设 §4.1 一致；节点按 purpose
     装配（select_range → chapter1_anchor/chapter2_thick → merge_frame →
     publish_stack 适配点），经 `codec.Import` + `runner.Run` 执行。
   - 本批以串行调度跑通（详设 §7 第 4 步"先串行跑通，再接 fork"）；
     装配失败或节点未开始 → 同图节点串行化兜底。
   - Chapter 2：注入 Summarizer 且有 History → 前缀重放（一次重试），
     失败/无素材 → 本地折叠（`summary_source=local`）；Chapter 1 失败 →
     锚点降级（`anchor_source=degraded`）。
   - 控制器 `compressWindowOutsideWith` 注入 DAG 时走图取帧；去重提前到
     DAG 前（避免无谓重放模型调用）+ 保留后置去重兜底；阈值/窗口/归档/
     PushCompact 语义不变。真空区 `buildGapFrame` 同步产出两章节 + 链字段。
   - 装配器 compact 栈块只渲染栈顶帧 Chapter 2 视图（`summary_chapter2`）。
5. **seelebridge 接线**：主/节点控制器注入 `CompactionDAG`；`Summarizer`
   暂不注入（启用前提 = 字节级装配出口快照固化，见 design.md §9 风险 1），
   Chapter 2 恒本地折叠；`SystemPrompt`/`Tools` 提供者已就位。

## 验证

```text
go build ./...
go test ./sessionstore/... ./seelexctx/... -count=1 -timeout=300s
go test ./seelebridge/ -count=1 -run 'TestNodeScope|TestSubAgentTreeContextProjection|TestRuntimeSession|TestDurable|TestGap|TestCover'
gofmt -l <改动文件>
```

新增测试文件：`sessionstore/session_context_test.go` 扩展、
`seelexctx/frame_test.go`、`seelexctx/replay_test.go`、`seelexctx/dag_test.go`；
控制器合并语义测试更新为链锚契约（不再断言整段递归复制前驱摘要）。

## 边界与适配说明

- `publish_stack` 目前是适配点节点（只校验 merge 输出存在）：实际
  `PushCompact` 仍留在 controller/gap（去重/原文归档/审计语义不变，
  与 design.md §4.6"成功取帧"表述一致）。
- request 覆盖标签：消息路径无真实 requestID，使用 ChatQueue 单元 1 基
  标签 `chat-N`；真空区事件流携带 `TaskID` 时优先用真实 requestID。
- 本地折叠的合并帧在 Current Work 中内嵌上一帧 Chapter 2 正文（保持栈顶
  自足的薄摘要近似），不再递归复制上一帧 Chapter 1 锚点。

## 后续步骤（未实现）

- 详设 §7 第 5-6 步：`search_compact_stack`/`search_current_history` 原子
  化 + 检索 DAG 编排（帧 request 索引消费点）。
- 详设 §7 第 7 步：后缀窗口默认值（`ContextMaxUnits`/`window.min_rounds`）
  与缓存冒烟（usage 归因需真实 API）。
- fork 并发：接 `workplan/sugar/fork`（章节级并行），与主会话请求并发
  的限流/串行兜底按冒烟数据验证。
- 前缀重放生产启用：在真实请求装配出口固化
  `SystemPrompt/History/Tools` 快照后再注入 Summarizer。
