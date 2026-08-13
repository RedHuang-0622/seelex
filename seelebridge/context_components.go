// ContextComponents 装配（plan.md §3.1 步骤 4 / §3.5）：主会话与节点会话
// 共享同一套 seelexctx 适配器（Assembler / ToolResultProcessor / Compressor
// / Controller），全部依赖构造注入（窗口策略、预算、归档、会话上下文存储）。
//
// 接线说明：会话级 SessionContextRecord（5 栈）与 ProjectKnowledge 块需要
// router/sessionID 就绪后由会话恢复流程注入（AttachSessionContextStore /
// SetProjectKnowledgeProvider）；未注入时适配器退化为内存态，行为保守
// （窗口外压缩仍工作，压缩帧不落盘）。
package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/session"

	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// runtimeBudgetProvider 把 Runtime 账号限额适配为 seelexctx.BudgetProvider
// （Controller 软/硬阈值输入）。
type runtimeBudgetProvider struct{ runtime *Runtime }

func (p runtimeBudgetProvider) ContextTokens() int   { return p.runtime.ContextWindow() }
func (p runtimeBudgetProvider) MaxOutputTokens() int { return p.runtime.MaxOutputTokens() }

// runtimeCompactStacks 把可选 SessionContextStore 适配为 CompactStackStore：
// 已绑定存储 → 栈操作落盘（state blob）；未绑定 → 内存态（会话内可审计）。
type runtimeCompactStacks struct {
	runtime *Runtime
	memory  seelexctx.CompactStackStore
}

func (s runtimeCompactStacks) Snapshot() sessionstore.SessionContextRecord {
	if store := s.runtime.sessionContextStore(); store != nil {
		return store.Snapshot()
	}
	return s.memory.Snapshot()
}

func (s runtimeCompactStacks) PushCompact(frame sessionstore.CompactFrame) error {
	if store := s.runtime.sessionContextStore(); store != nil {
		return store.PushCompact(frame)
	}
	return s.memory.PushCompact(frame)
}

// mainContextComponents 构造主会话的 ContextComponents（plan.md §3.1 步骤 4）。
// SystemPrompt 置空：会话级提示由 application 经 SetSystemPrompt 注入，
// 保持既有行为；Assembler 的 system provider 在应用层迁移后接管。
func (r *Runtime) mainContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler:           r.seelexAssembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(0, nil),
		Compressor:          r.seelexCompressor(),
		Controller:          r.seelexController(),
	}
}

// nodeContextComponents 构造节点子代理会话的 ContextComponents
// （bridge.WithSessionComponents 输入）。节点级 PromptBlocks 由
// SeelexAgentNode.Run 注入 ctx（nodeScopeAssembler 合并），本组件与
// 主会话共享同一套适配器依赖（预算按节点账号限额推导）。
// 工具结果处理器使用节点归档器（nodeToolResultArchiver）：节点会话的
// result_ref 带 node:<nodeID>: 前缀落节点专属归档，运行中/结束后可经
// Runtime.NodeToolResult 读回（P1 修复 result_ref 读回断链）。
func (r *Runtime) nodeContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.node.Assembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(0, seenode.ToolResultArchiver{
			ArchiverFor: r.node.ToolResultArchiverFor,
			Shared:      seelexctx.NewInMemoryToolResultArchiver(),
		}),
		Compressor: r.seelexCompressor(),
		Controller: r.seelexController(),
	}
}

// seelexAssembler 构造 seelex 装配器：栈块（now using = 栈顶）来自
// SessionContextStore（未注入 → 无块）；project 块来自 ProjectKnowledge
// 提供者（未注入 → 无块）；记忆块按当前查询从历史压缩段选取（无存储 →
// 无块）；占位符解析委托 seelexctx。
func (r *Runtime) seelexAssembler() seelectx.RequestAssembler {
	return seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: nil, // 会话级提示由 application 侧注入（迁移后经此渲染）
		ProjectBlock: r.projectBlock,
		StackBlocks:  r.stackBlocks,
		Memories:     r.relatedMemoryBlocks,
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			return r.resolvePlaceholder(name)
		}),
	})
}

// relatedMemoryBlocks 按当前查询从 CompactStack 全部帧选取相关记忆块
// （不止栈顶：超长会话的久远相关段经 seelexctx/memory 词法选取器召回）。
// 无绑定存储 / 空查询 / 无命中 → 不注入（请求内容不变）。
func (r *Runtime) relatedMemoryBlocks(_ context.Context, query string) []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	record := store.Snapshot()
	if len(record.CompactStack) == 0 {
		return nil
	}
	candidates := make([]memory.Candidate, 0, len(record.CompactStack))
	for _, frame := range record.CompactStack {
		candidates = append(candidates, memory.Candidate{
			SegmentID: frame.SegmentID, Summary: frame.Summary,
			Evidence: frame.Evidence, From: frame.From, To: frame.To,
		})
	}
	opts := memory.DefaultOptions()
	selected := memory.Select(query, candidates, opts)
	if len(selected) == 0 {
		return nil
	}
	if block := memory.RenderMemoryBlock(selected, opts.MaxTokens); block != nil {
		return []seelectx.PromptBlock{*block}
	}
	return nil
}

// coverHistoryGap 把滑动窗口与压缩内容之间的真空区轮次压缩为合并帧
// （seelexctx.CoverHistoryGap）：完整事件流 vs 压缩栈顶 To vs 尾窗装载量
// 三者对齐，未覆盖区间压入会话压缩栈（state blob / 内存兜底），原文经
// TurnArchiver 归档（read_compressed_turn 可读回）。无真空区 → 无副作用。
func (r *Runtime) coverHistoryGap(ctx context.Context, allEvents, tailEvents []sessionstore.Event) error {
	if len(allEvents) == 0 {
		return nil
	}
	stacks := runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()}
	_, err := seelexctx.CoverHistoryGap(ctx, seelexctx.GapCoverageOptions{
		AllEvents:  allEvents,
		TailEvents: tailEvents,
		Record:     stacks.Snapshot(),
		Stacks:     stacks,
		Turns:      r.turnArchiver,
		SessionID:  r.MainSessionID(),
	})
	return err
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

// seelexController 构造控制器：窗口策略来自 RuntimeConfig.WindowConfig
// （DefaultWindowPolicy，plan.md §3.7.3），阈值预算来自账号限额。
func (r *Runtime) seelexController() seelectx.ContextController {
	policy := seelexctx.NewContextWindowPolicy(r.ContextWindow(), r.MaxOutputTokens())
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Policy: policy,
		Window: r.windowPolicy(),
		Budget: runtimeBudgetProvider{runtime: r},
		Stacks: runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()},
		Turns:  r.turnArchiver,
		// 压缩帧 SegmentID 溯源到当前会话：每次压缩动态取值，会话切换后
		// 仍指向正确会话（compact-<sessionID>-<ms>）。
		SessionIDProvider: r.MainSessionID,
	})
}

// windowPolicy 返回当前窗口策略（NewRuntime 时按配置构造）。
func (r *Runtime) windowPolicy() seelexctx.WindowPolicy {
	r.windowMu.RLock()
	defer r.windowMu.RUnlock()
	return r.window
}

// windowTailBudget 从窗口策略推导 Load 的读尾预算（D1，plan.md §9）：
// maxUnits = 窗口轮数（策略推导；输入不足时保守回退 min_rounds）；
// tokenBudget = 账号上下文窗口（上限保护，LoadEventTail 双上限取先到者）。
func (r *Runtime) windowTailBudget() (tokenBudget, maxUnits int) {
	tokenBudget = r.ContextWindow()
	info := seelexctx.ProviderContextInfo{ContextTokens: tokenBudget}
	rounds, _ := r.windowPolicy().WindowRounds(context.Background(), info)
	if rounds <= 0 {
		rounds = 4 // DefaultWindowPolicy 的 min_rounds（输入不足时同样回退）
	}
	return tokenBudget, rounds
}

// stackBlocks 渲染会话级使用栈块（now using = 栈顶；未绑定存储 → 无块）。
func (r *Runtime) stackBlocks() []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	return seelexctx.RenderStackBlocks(store.Snapshot())
}

// projectBlock 渲染项目级模块语义块（ProjectKnowledge，会话前预读缓存；
// 提供者未注入 → 无块）。
func (r *Runtime) projectBlock() *seelectx.PromptBlock {
	r.projectMu.RLock()
	provider := r.projectKnowledge
	r.projectMu.RUnlock()
	if provider == nil {
		return nil
	}
	record := provider()
	if record == nil {
		return nil
	}
	return seelexctx.RenderProjectBlock(*record)
}

// resolvePlaceholder 解析 {{name}} 占位符（当前无内置变量，未知占位符
// 原样保留）。
func (r *Runtime) resolvePlaceholder(name string) (string, error) {
	return "", nil
}

// sessionContextStore 返回绑定的会话上下文存储（nil = 未绑定）。
func (r *Runtime) sessionContextStore() *sessionstore.SessionContextStore {
	r.ctxStoreMu.RLock()
	defer r.ctxStoreMu.RUnlock()
	return r.ctxStore
}

// AttachSessionContextStore 绑定会话上下文存储（state blob，plan.md §3.7.2）。
// 会话恢复流程接线时由调用方注入（router + sessionID 就绪后）。
func (r *Runtime) AttachSessionContextStore(store *sessionstore.SessionContextStore) {
	r.ctxStoreMu.Lock()
	r.ctxStore = store
	r.ctxStoreMu.Unlock()
}

// SetProjectKnowledgeProvider 注入项目级模块语义提供者（ProjectKnowledge
// 会话前预读；nil 关闭 project 块）。
func (r *Runtime) SetProjectKnowledgeProvider(provider func() *sessionstore.ProjectRecord) {
	r.projectMu.Lock()
	r.projectKnowledge = provider
	r.projectMu.Unlock()
}

// compressionSnapshot 跨会话快照压缩输入：当前节点/会话的父证据快照
// （compactor 路径；无 → 走 QuickChat 路径）。
func (r *Runtime) compressionSnapshot(_ string) *snapshot.ContextSnapshot {
	return r.node.ParentEvidence()
}

// ── 编译期检查 ────────────────────────────────────────────────────

var (
	_ seelexctx.BudgetProvider  = runtimeBudgetProvider{}
	_ session.ContextComponents = session.ContextComponents{}
)

// compactor 实例（跨会话快照压缩，NewRuntime 时构造一次）。
func (r *Runtime) compactorInstance() *compactor.Compactor {
	return compactor.NewCompactor()
}
