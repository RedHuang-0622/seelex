package assemble

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	seetelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/compactor"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot"
	seenode "github.com/RedHuang-0622/seelex/backend/tool/node"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
	contextruntime "github.com/RedHuang-0622/seelex/kernel/context"
	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storeproject "github.com/RedHuang-0622/seelex/store/project"
)

func runtimeSessionID(ctx context.Context) string {
	if sessionID := strings.TrimSpace(taskcontext.SessionIDFromContext(ctx)); sessionID != "" {
		return sessionID
	}
	return strings.TrimSpace(seetelemetry.SessionIDFromContext(ctx))
}

type sessionContextStorePort interface {
	Load(context.Context) error
	PromptStackSnapshot() storecontext.PromptStackSnapshot
	CompactStackSnapshot() []storecontext.CompactFrame
	PushCompact(storecontext.CompactFrame) error
	SystemPrompt() string
	GoalStackSnapshot() []storecontext.GoalFrame
	ReplaceGoalStack([]storecontext.GoalFrame) error
	AppendGoalAudit(storecontext.GoalAuditEntry) error
	GoalAuditSnapshot() []storecontext.GoalAuditEntry
}

// AttachSessionContextStore 绑定会话上下文存储（state blob，plan.md §3.7.2）。
// 会话恢复流程接线时由调用方注入（router + sessionID 就绪后）。
func (r *Runtime) AttachSessionContextStore(st sessionContextStorePort, boards ...*storeboard.BoundStore) {
	if bundle := r.activeBundle(); bundle != nil {
		bundle.mu.Lock()
		var boardStore *storeboard.BoundStore
		if len(boards) > 0 {
			boardStore = boards[0]
		}
		bundle.binding.attachContextStore(st, boardStore)
		bundle.mu.Unlock()
	}
}

// SetProjectKnowledgeProvider 注入项目级模块语义提供者（ProjectKnowledge
// 会话前预读；nil 关闭 project 块）。
func (r *Runtime) SetProjectKnowledgeProvider(provider func() *storeproject.ProjectRecord) {
	r.setProjectKnowledge(provider)
}

// compactorInstance 返回跨会话快照压缩器（构造一次，会话间复用）。
func (r *Runtime) compactorInstance() *compactor.Compactor {
	return compactor.NewCompactor()
}

// compressionSnapshot 跨会话快照压缩输入：当前节点/会话的父证据快照
// （compactor 路径；无 → 走 QuickChat 路径）。
func (r *Runtime) compressionSnapshot(_ string) *snapshot.ContextSnapshot {
	return r.node.ParentEvidence()
}

// coverHistoryGap 把滑动窗口与压缩内容之间的真空区轮次压缩为合并帧
// （seelexctx.CoverHistoryGap）：在完整事件流的单元空间里，压缩栈顶 To 与尾窗
// 起点 tailStartUnit 直接对齐，[栈顶To+1, tailStartUnit-1] 即未覆盖区间；它
// 压入会话压缩栈（state blob / 内存兜底），原文经 TurnArchiver 归档
// （read_compressed_turn 可读回）。无真空区 → 无副作用。
func (r *Runtime) coverHistoryGap(ctx context.Context, allEvents []model.TranscriptEvent, tailStartUnit int) error {
	// 判据（"没有压缩基线就不补"）在域包 context_runtime.CoverHistoryGap；本层只把
	// "当前会话的压缩栈 / 轮次归档器 / 会话号 / 传递上限"接成那一跳的读数。
	deps := contextruntime.GapCoverageDeps{
		AllEvents:        allEvents,
		TailStartUnit:    tailStartUnit,
		Stacks:           runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()},
		Turns:            r.getTurnArchiver(),
		SessionID:        r.MainSessionID(),
		FrameCarryTokens: r.limits.ContextFrameCarryTokens,
	}
	if st := r.sessionContextStore(); st != nil {
		deps.Record = st.PromptStackSnapshot
	}
	return contextruntime.CoverHistoryGap(ctx, deps)
}

// mainContextComponents 构造主会话的 ContextComponents（plan.md §3.1 步骤 4）。
// SystemPrompt 置空：会话级提示由 application 经 SetSystemPrompt 注入，
// 保持既有行为；Assembler 的 system provider 在应用层迁移后接管。
func (r *Runtime) mainContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.seelexAssembler(),
		// 工具结果归档上限取 limits 生效值：传 0 会退回出厂默认，
		// max_tool_result_chars 就对回合内归档失效了。
		ToolResultProcessor: seelexctx.NewToolResultProcessor(r.limits.MaxToolResultChars, nil),
		Compressor:          r.seelexCompressor(),
		Controller:          r.seelexController(),
	}
}

// nodeContextComponents 构造节点子代理会话的 ContextComponents
// （bridge.WithSessionComponents 输入）。节点级 PromptBlocks 由
// SeelexAgentNode.Run 注入 ctx（ScopeAssembler 合并），本组件与
// 主会话共享同一套适配器依赖（预算按节点账号限额推导）。
func (r *Runtime) nodeContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.node.Assembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(r.limits.MaxToolResultChars, seenode.ToolResultArchiver{
			ArchiverFor: r.node.ToolResultArchiverFor,
			Shared:      seelexctx.NewInMemoryToolResultArchiver(),
		}),
		Compressor: r.seelexCompressor(),
		Controller: r.nodeController(),
	}
}

// nodeController 构造节点子代理会话的上下文控制器：压缩栈与主会话隔离
// （子代理压缩帧不再写入主会话 SessionContextStore），归档上限与
// processor / 装配层同源于 limits.MaxToolResultChars。
//
// 2026-09-30 起回合内不再压缩对话（见 seelexctx.Handle）：压缩是上下文压缩流程里
// 产出元数据的一步，折完必须由模型按章节写出读后感，只有装配层做得到——它手里握着
// 上一次真实请求的字节前序（system/History/Tools 与 wire 同源），前缀重放才换得来
// 前缀缓存。节点链路拿到的 ev.History 是**装配前**的引擎工作历史，注入摘要器会付
// 全价却拿不到缓存（设计 §9 风险 1），所以节点链路也不折帧：子代理的窗口越线同样
// 交给装配层那条路径。
func (r *Runtime) nodeController() seelectx.ContextController {
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Stacks:             seelexctx.NewMemoryCompactStack(),
		MaxToolResultChars: r.limits.MaxToolResultChars,
	})
}

// PrefixWorld 实现 context_runtime.PrefixWorldPort：报告进入 prefix_key 的
// 世界状态格。项目版本是内容 hash；compact seq 是压缩栈顶覆盖到的事件上界。
// 两者都不含本轮 query，工具纪元由会话 catalog owner 在 M5 接管。
func (r *Runtime) PrefixWorld() contextruntime.PrefixWorld {
	world := contextruntime.PrefixWorld{}
	r.projectMu.RLock()
	provider := r.project
	r.projectMu.RUnlock()
	if provider != nil {
		if record := provider(); record != nil {
			world.ProjectVersion = record.Version
		}
	}
	if store := r.sessionContextStore(); store != nil {
		if frames := store.CompactStackSnapshot(); len(frames) > 0 {
			world.CompactSeq = frames[len(frames)-1].EventTo
		}
	}
	return world
}

// relatedMemoryBlocks 按当前查询从 CompactStack 全部帧选取相关记忆块
// （不止栈顶：超长会话的久远相关段经 seelexctx/memory 词法选取器召回）。
// 无绑定存储 / 空查询 / 无命中 → 不注入（请求内容不变）。
func (r *Runtime) relatedMemoryBlocks(ctx context.Context, query string) []seelectx.PromptBlock {
	return r.relatedMemoryBlocksForContext(ctx, query)
}

func (r *Runtime) relatedMemoryBlocksForContext(ctx context.Context, query string) []seelectx.PromptBlock {
	if sessionID := runtimeSessionID(ctx); sessionID != "" {
		r.contextEpochMu.RLock()
		content := r.memoryEpochBySID[sessionID]
		r.contextEpochMu.RUnlock()
		if content != "" {
			return []seelectx.PromptBlock{{
				Name: "memory",
				Messages: []types.Message{{
					Role: "user", Content: &content,
				}},
			}}
		}
	}
	st := r.sessionContextStore()
	if st == nil {
		return nil
	}
	// 选取与渲染的判据在域包（候选 → 词法选取 → 渲染；无命中就不注入）。
	return contextruntime.RelatedMemoryBlocks(st.CompactStackSnapshot(), query)
}

// FreezeMemoryEpoch selects memory once and returns the exact concatenated
// block bytes that must be persisted for this epoch.
func (r *Runtime) FreezeMemoryEpoch(_ string, query string) string {
	st := r.sessionContextStore()
	if st == nil {
		return ""
	}
	blocks := contextruntime.RelatedMemoryBlocks(st.CompactStackSnapshot(), query)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		for _, message := range block.Messages {
			if message.Content != nil {
				parts = append(parts, *message.Content)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// SetMemoryEpoch publishes a persisted memory snapshot for future assembly.
func (r *Runtime) SetMemoryEpoch(sessionID, content string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.contextEpochMu.Lock()
	if r.memoryEpochBySID == nil {
		r.memoryEpochBySID = make(map[string]string)
	}
	if content == "" {
		delete(r.memoryEpochBySID, sessionID)
	} else {
		r.memoryEpochBySID[sessionID] = content
	}
	r.contextEpochMu.Unlock()
}

// resolvePlaceholder 解析 {{name}} 占位符（当前无内置变量，未知占位符
// 原样保留）。
func (r *Runtime) resolvePlaceholder(name string) (string, error) {
	return "", nil
}

// seelexAssembler 构造 seelex 装配器：栈块（now using = 栈顶）来自
// SessionContextStore（未注入 → 无块）；project 块来自 ProjectKnowledge
// 提供者（未注入 → 无块）；记忆块按当前查询从历史压缩段选取（无存储 →
// 无块）；占位符解析委托 seelexctx。
func (r *Runtime) seelexAssembler() seelectx.RequestAssembler {
	return seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: nil, // 会话级提示由 application 侧注入（迁移后经此渲染）
		ProjectBlock: r.projectBlock,
		PrefixStacks: r.prefixStacks,
		TailStacks:   r.tailStacks,
		Memories:     r.relatedMemoryBlocksForContext,
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			return r.resolvePlaceholder(name)
		}),
	})
}

// seelexCompressor 构造压缩器：短历史快速路径 + QuickChat 结构化 checkpoint
// （共享账号 completer 的隔离调用，无工具、独立 history）。
func (r *Runtime) seelexCompressor() seelectx.Compressor {
	quickChat, err := seelectx.NewQuickChat(r.completer)
	if err != nil {
		quickChat = nil // 装配失败 → 仅短历史/快照路径可用
	}
	return seelexctx.NewCompressor(seelexctx.CompressorOptions{
		QuickChat: quickChat,
		Compactor: r.compactorInstance(),
		SnapshotFor: func(_ context.Context, request seelectx.CompressionRequest) *snapshot.ContextSnapshot {
			return r.compressionSnapshot(request.SessionID)
		},
	})
}

// seelexController 构造控制器：回合内不压缩对话，只做超大工具结果的兜底归档。
//
// 2026-09-30 起压缩整条归装配层（application/assembly/context_runtime，见
// MainCompactionDAG）：压缩是上下文压缩流程里产出元数据的一步，折完必须由模型按
// 章节写出读后感；装配层在压缩那一刻手里握着上一次真实请求的三样原件（coordinator
// 的 systemPrompt/existing/tools），而控制器只拿得到**装配前**的引擎工作历史
// （seelectx.ContextEvent.History = ReActLoop.History()）——拿它当重放素材会付一次
// 全价调用却换不来前缀缓存（设计 §9 风险 1），折出来的帧也因此没有读后感，模型只
// 剩索引、还得 search_history 回读（2026-09-30 现场）。归档上限与 processor /
// 装配层同源于 limits.MaxToolResultChars：全链路只有一份生效值。
func (r *Runtime) seelexController() seelectx.ContextController {
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Stacks:             runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()},
		MaxToolResultChars: r.limits.MaxToolResultChars,
	})
}

// replayInputTokens 返回分片重放的片预算（模型输入侧）：取账号上下文窗口减去
// 固定开销后的保守值。≤0 → 不分片（走原有单次重放，保住前缀缓存）。
//
// 为什么用 ContextWindow 的保守份额而不是硬阈值：分片是"压缩区自身大到一次发不出"
// 的容灾路径，判据应当是"这一次请求能不能装下原始溢出区"，而不是"要不要现在压缩"。
// 取窗口的 3/4 给正文、留 1/4 给 system/tools/指令与估算偏差。
//
// limits.context_compaction_summary.input_tokens > 0 时以配置为准；未配置才走
// 这里的推导——旋钮的零值必须是"沿用既有行为"，否则加了它就静默改掉片预算。
func (r *Runtime) replayInputTokens() int {
	return contextruntime.ReplayInputTokens(r.limits.ContextCompactionSummary.InputTokens, r.ContextWindow())
}

// compactionSummarizer 按开关构造前缀重放厚摘要器（limits.context_compaction_summary）。
//
// 关闭、QuickChat 装配失败、摘要器构造失败三种情况一律返回 **nil**：nil 是
// chapter2Node 的显式判据（`d.opts.Summarizer != nil`），落到本地确定性压缩，
// 既不报错也不静默降级成"发一次没有缓存的调用"。QuickChat 走的是与
// seelexCompressor 同一条构造路径（共享账号 completer 的隔离调用，无工具、
// 独立 history），不第二次装配 completer。
func (r *Runtime) compactionSummarizer() seelexctx.PrefixReplaySummarizer {
	summarizer, _ := r.compactionSummarizerWithNote()
	return summarizer
}

// compactionSummarizerWithNote 是上面那一跳的唯一实现：除了摘要器，还给出
// **为什么没有摘要器**（三种 nil 出口的分别是"开关关闭 / QuickChat 装配失败 /
// 摘要器构造失败"）。三个出口此前都不留痕——windowsgui 构建下 log.Printf 也无处
// 可看，现场只剩一个 `summary_source=local`；这份 note 走进压缩 DAG 的
// SummarizerNote，最终落进帧证据与帧正文，让"模型为什么没被叫到"在帧里自答。
func (r *Runtime) compactionSummarizerWithNote() (seelexctx.PrefixReplaySummarizer, string) {
	// 三条 nil 出口与各自的理由文案都在域包：本层只给"开关"与"主 completer"两格读数。
	return contextruntime.NewCompactionSummarizer(r.completer, r.limits.ContextCompactionSummary.Enabled)
}

// MainCompactionDAG 返回**装配层压缩**（application/assembly/context_runtime，回合
// 开始前那条路径）用的压缩 DAG 执行器。
//
// 与 seelexController 里那份的差别只有一处、但很关键：这份**注入 Summarizer**。
// 装配层在压缩那一刻手里握着上一次真实请求的 system / history / tools 三样原件
// （coordinator.go 的 systemPrompt / existing / tools，全部来自产出该请求的同一
// 条装配路径），因此重放请求能与真实请求共享字节前缀、几乎全命中缓存——详设 §9
// 风险 1 的启用前提在这条路径上是**满足**的。控制器路径拿到的 ev.History 是否
// 与 wire 同源尚未验证，所以那边仍不注入。
//
// SystemPrompt / Tools 与 seelexController 同源（会话上下文存储 + 可见工具），
// 保证两条路径产出的帧形状一致。
func (r *Runtime) MainCompactionDAG() *seelexctx.CompactionDAG {
	summarizer, summarizerNote := r.compactionSummarizerWithNote()
	return seelexctx.NewCompactionDAG(seelexctx.CompactionDAGOptions{
		SessionIDProvider: r.MainSessionID,
		Summarizer:        summarizer,
		SummarizerNote:    summarizerNote,
		SystemPrompt: func() string {
			if st := r.sessionContextStore(); st != nil {
				return st.SystemPrompt()
			}
			return ""
		},
		Tools: func() []types.Tool {
			if r.agt == nil {
				return nil
			}
			return r.agt.VisibleTools(context.Background())
		},
		Chapter2MaxTokens: r.limits.ContextCompactionSummary.Chapter2Tokens,
		FrameCarryTokens:  r.limits.ContextFrameCarryTokens,
		ReplayInputTokens: r.replayInputTokens(),
	})
}

// sessionContextStore 返回绑定的会话上下文存储（nil = 未绑定）。
func (r *Runtime) sessionContextStore() sessionContextStorePort {
	if bundle := r.activeBundle(); bundle != nil {
		return bundle.binding.contextStore()
	}
	return nil
}

// stackBlocks 渲染会话级全部使用栈块（稳定前缀 skill/compact + 动态尾部
// plan/task；now using = 栈顶；未绑定存储 → 无块）。节点子代理分别经
// InheritedBlocks/InheritedTailBlocks 继承两段。
func (r *Runtime) stackBlocks() []seelectx.PromptBlock {
	st := r.sessionContextStore()
	if st == nil {
		return nil
	}
	return seelexctx.RenderStackBlocks(st.PromptStackSnapshot())
}

// prefixStacks 渲染稳定前缀栈块（skill/compact，now using = 栈顶；未绑定
// 存储 → 无块）：主会话装配器在记忆块之后、累积 context 之前注入。
func (r *Runtime) prefixStacks() []seelectx.PromptBlock {
	st := r.sessionContextStore()
	if st == nil {
		return nil
	}
	return seelexctx.RenderStablePrefixBlocks(st.PromptStackSnapshot())
}

// tailStacks 渲染动态尾部栈块（plan/task，now using = 栈顶；未绑定存储 →
// 无块）：主会话装配器在 WorkingHistory 之后、贴近当前输入注入。
func (r *Runtime) tailStacks() []seelectx.PromptBlock {
	st := r.sessionContextStore()
	if st == nil {
		return nil
	}
	return seelexctx.RenderTailBlocks(st.PromptStackSnapshot())
}

// windowPolicy 返回当前窗口策略（NewRuntime 时按配置构造）。
func (r *Runtime) windowPolicy() seelexctx.WindowPolicy {
	r.windowMu.RLock()
	defer r.windowMu.RUnlock()
	return r.window
}

// windowTailBudget 推导主会话 DurableHistory 的读尾预算（判据在域包：窗口 → 读尾的
// tokenBudget/maxUnits 推导与轮数回退）。
func (r *Runtime) windowTailBudget() (tokenBudget, maxUnits int) {
	return contextruntime.WindowTailBudget(r.ContextWindow(), r.windowPolicy())
}
