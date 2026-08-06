// seelexAssembler 是 seelex 的 RequestAssembler（plan.md §3.5 / 架构文档 4.8.4）。
//
// 每次模型请求的投影顺序：
//
//	system prompt（effort/skill 动态生成）→ project 块（会话前预读）→
//	栈块（plan/task/skill/compact，now using = 栈顶）→ 记忆块（按当前查询
//	从历史压缩段选取的相关记忆，可选）→ 调用方静态块（plan authority /
//	task checkpoint / evidence）→ WorkingHistory（窗口轮次）
//
// 系统提示与 PromptBlocks 只进入模型请求，不写入 durable history（新不变量）。
// 占位符（{{plan}}/{{skill}}）经 PlaceholderRequestAssembler 解析，只作用于块内消息。
package seelexctx

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// AssemblerOptions 装配器依赖（全部可省略；nil 的提供者被跳过）。
type AssemblerOptions struct {
	// SystemPrompt 返回会话级基础提示（effort/skill/任务摘要动态生成）。
	// 对应 ContextComponents.SystemPrompt 置空、由装配器自行渲染。
	SystemPrompt func() string

	// ProjectBlock 返回项目级模块语义块（ProjectKnowledge，会话前预读缓存；
	// nil 表示不注入）。
	ProjectBlock func() *seelectx.PromptBlock

	// StackBlocks 返回会话级使用栈块（plan/task/skill/compact，栈顶 = now using）。
	StackBlocks func() []seelectx.PromptBlock

	// Window 返回滑动窗口轮次（WorkingHistory 源；nil 时使用调用方传入的
	// WorkingHistory）。读取失败保守回退调用方历史，不让请求失败。
	Window func(ctx context.Context) ([]types.Message, error)

	// Memories 可选：按当前请求从历史压缩段选取相关记忆块（nil 不注入）。
	// query 取 WorkingHistory 中最后一条非控制块 user 消息；返回 nil/空 →
	// 不渲染记忆块。见 seelexctx/memory 的 Select/RenderMemoryBlock。
	Memories func(ctx context.Context, query string) []seelectx.PromptBlock

	// Resolver 解析 {{name}} 占位符；nil 时 PlaceholderRequestAssembler 报错。
	Resolver seelectx.PlaceholderResolver
}

// seelexAssembler 实现 seelectx.RequestAssembler。
type seelexAssembler struct {
	options AssemblerOptions
}

// NewAssembler 构造 seelex 装配器。
func NewAssembler(options AssemblerOptions) seelectx.RequestAssembler {
	return seelexAssembler{options: options}
}

// Assemble 按投影顺序组装一次模型请求。
func (a seelexAssembler) Assemble(ctx context.Context, request seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
	blocks := make([]seelectx.PromptBlock, 0, len(request.Blocks)+4)

	// 1. system prompt（effort/skill 动态生成，永不持久化）。
	if prompt := a.systemPrompt(); prompt != "" {
		blocks = append(blocks, seelectx.PromptBlock{
			Name:     "system",
			Messages: []types.Message{{Role: "system", Content: &prompt}},
		})
	}
	// 2. project 块（项目级模块语义，会话前预读）。
	if project := a.projectBlock(ctx); project != nil {
		blocks = append(blocks, *project)
	}
	// 3. 栈块（now using = 栈顶）。
	blocks = append(blocks, a.stackBlocks()...)

	// 4. 记忆块（按当前查询从历史压缩段选取；超长会话的相关记忆注入）。
	if a.options.Memories != nil {
		blocks = append(blocks, a.options.Memories(ctx, lastUserQuery(request.WorkingHistory))...)
	}

	// 5. 调用方静态块（plan authority / task checkpoint / evidence）。
	blocks = append(blocks, request.Blocks...)

	// 5. WorkingHistory = 滑动窗口轮次。
	history := request.WorkingHistory
	if a.options.Window != nil {
		if windowed, err := a.options.Window(ctx); err == nil && windowed != nil {
			history = windowed
		}
		// 窗口读取失败保守回退调用方历史（请求仍可进行，与 3.7.3 的
		// "出错时保守回退"同风格）。
	}

	if a.options.Resolver == nil {
		// 无占位符解析器 → 直接委托默认装配器（保序拷贝）。
		return seelectx.DefaultRequestAssembler{}.Assemble(ctx, seelectx.AssemblyRequest{
			Blocks:         blocks,
			WorkingHistory: history,
			Tools:          request.Tools,
		})
	}
	return seelectx.PlaceholderRequestAssembler{Resolver: a.options.Resolver}.
		Assemble(ctx, seelectx.AssemblyRequest{
			Blocks:         blocks,
			WorkingHistory: history,
			Tools:          request.Tools,
		})
}

func (a seelexAssembler) systemPrompt() string {
	if a.options.SystemPrompt == nil {
		return ""
	}
	return a.options.SystemPrompt()
}

func (a seelexAssembler) projectBlock(ctx context.Context) *seelectx.PromptBlock {
	if a.options.ProjectBlock == nil {
		return nil
	}
	return a.options.ProjectBlock()
}

func (a seelexAssembler) stackBlocks() []seelectx.PromptBlock {
	if a.options.StackBlocks == nil {
		return nil
	}
	return a.options.StackBlocks()
}

// lastUserQuery 提取 WorkingHistory 中最后一条非控制块 user 消息作为
// 记忆选取查询（窗口外记忆按它判断相关性；无 → 空串不选取）。
func lastUserQuery(history []types.Message) string {
	for index := len(history) - 1; index >= 0; index-- {
		message := history[index]
		if message.Role != "user" || message.Content == nil {
			continue
		}
		if isStackContextMarker(message) {
			continue
		}
		return *message.Content
	}
	return ""
}

// ── 栈块渲染（now using = 栈顶，plan.md §3.7.2）────────────────────────

// RenderStackBlocks 把 SessionContextRecord 渲染为 PromptBlock 列表：
// plan/task/skill/compact 各栈只渲染栈顶一帧（当前使用中的模块），
// 空栈不渲染。帧内容结构化（JSON），供模型与 UI 读取当前上下文。
func RenderStackBlocks(record sessionstore.SessionContextRecord) []seelectx.PromptBlock {
	blocks := make([]seelectx.PromptBlock, 0, 4)

	if len(record.PlanStack) > 0 {
		top := record.PlanStack[len(record.PlanStack)-1]
		blocks = append(blocks, renderStackBlock("plan", "当前计划 (now using plan)", map[string]any{
			"plan_id": top.PlanID, "title": top.Title, "status": top.Status,
			"nodes": top.Nodes,
		}))
	}
	if len(record.TaskStack) > 0 {
		top := record.TaskStack[len(record.TaskStack)-1]
		blocks = append(blocks, renderStackBlock("task", "当前任务 (now using task)", map[string]any{
			"task_id": top.TaskID, "objective": top.Objective, "status": top.Status,
			"evidence": top.Evidence,
		}))
	}
	if len(record.SkillStack) > 0 {
		top := record.SkillStack[len(record.SkillStack)-1]
		blocks = append(blocks, renderStackBlock("skill", "当前技能 (now using skill)", map[string]any{
			"skill_id": top.SkillID, "name": top.Name,
		}))
	}
	if len(record.CompactStack) > 0 {
		top := record.CompactStack[len(record.CompactStack)-1]
		blocks = append(blocks, renderStackBlock("compact", "压缩上下文 (now using compact context)", map[string]any{
			"segment_id": top.SegmentID, "from": top.From, "to": top.To,
			"summary": top.Summary, "evidence": top.Evidence,
		}))
	}
	return blocks
}

func renderStackBlock(name, title string, payload map[string]any) seelectx.PromptBlock {
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte("{}")
	}
	content := "## " + title + "\n" + string(encoded)
	return seelectx.PromptBlock{
		Name:     name,
		Messages: []types.Message{{Role: "user", Content: &content}},
	}
}

// RenderProjectBlock 渲染项目级模块语义块（ProjectKnowledge，plan.md
// §3.7.1 / 架构文档 4.8.4：会话开始预读，内容 hash 版本化）。无模块时
// 返回 nil（不渲染空块）。
func RenderProjectBlock(record sessionstore.ProjectRecord) *seelectx.PromptBlock {
	if record.Version == "" && len(record.Modules) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.WriteString("## 项目模块语义 (Project Modules)\n")
	if record.Version != "" {
		builder.WriteString("> 版本: ")
		builder.WriteString(record.Version)
		builder.WriteByte('\n')
	}
	for _, module := range record.Modules {
		builder.WriteString("- **")
		builder.WriteString(module.Name)
		builder.WriteString("**")
		if module.Summary != "" {
			builder.WriteString(": ")
			builder.WriteString(module.Summary)
		}
		if module.Path != "" {
			builder.WriteString(" (")
			builder.WriteString(module.Path)
			builder.WriteString(")")
		}
		builder.WriteByte('\n')
		for _, doc := range module.Docs {
			builder.WriteString("  - 文档: ")
			builder.WriteString(doc)
			builder.WriteByte('\n')
		}
	}
	content := builder.String()
	return &seelectx.PromptBlock{
		Name:     "project",
		Messages: []types.Message{{Role: "user", Content: &content}},
	}
}

// isStackContextMarker 判断消息是否为 seelex 上下文控制块（压缩帧/检查点），
// 这类消息不参与轮次单元切分，也不得被渲染为普通对话内容。
func isStackContextMarker(message types.Message) bool {
	if message.Role != "user" || message.Content == nil {
		return false
	}
	return strings.HasPrefix(*message.Content, compactContextMarker) ||
		strings.HasPrefix(*message.Content, checkpointMarker)
}
