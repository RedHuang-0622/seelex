package scenario

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type ScriptedEngine struct {
	mu        sync.Mutex
	script    []EngineTurn
	next      int
	history   []application.EngineMessage
	sessionID string
	prompt    string
	maxLoops  int
	tools     ToolLifecycle
	approvals ApprovalRequester
	executor  ToolExecutor
}

func NewScriptedEngine(script []EngineTurn, tools ToolLifecycle, approvals ApprovalRequester) *ScriptedEngine {
	return &ScriptedEngine{
		script: append([]EngineTurn(nil), script...), sessionID: "session-e2e-1",
		tools: tools, approvals: approvals,
	}
}

func (engine *ScriptedEngine) SetSessionID(sessionID string) {
	engine.mu.Lock()
	engine.sessionID = sessionID
	engine.mu.Unlock()
}

func (engine *ScriptedEngine) SetToolExecutor(executor ToolExecutor) {
	engine.mu.Lock()
	engine.executor = executor
	engine.mu.Unlock()
}

func (engine *ScriptedEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	turn, err := engine.takeTurn(input)
	if err != nil {
		return "", err
	}
	var answer strings.Builder
	toolCalls := make([]application.EngineToolCall, 0)
	toolResults := make([]application.EngineMessage, 0)
	toolTurn := 0
	for _, emission := range turn.Emit {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		switch emission.Type {
		case "assistant.delta":
			onChunk(emission.Value)
			answer.WriteString(emission.Value)
		case "tool.call":
			toolTurn++
			execution := ToolExecution{Turn: toolTurn, Name: emission.Name, Arguments: emission.Arguments, Result: emission.Result}
			if engine.tools == nil {
				return "", fmt.Errorf("tool %q emitted without a ToolLifecycle", emission.Name)
			}
			engine.tools.Started(ctx, execution)
			if emission.Approval != nil {
				decision, approvalErr := engine.requestApproval(ctx, emission.Name, *emission.Approval)
				if approvalErr != nil {
					execution.Err = approvalErr
				} else if decision.OptionID != allowOption(*emission.Approval) {
					execution.Err = fmt.Errorf("approval rejected with option %q", decision.OptionID)
				}
			}
			if execution.Err == nil {
				if executor := engine.toolExecutor(); executor != nil {
					execution.Result, execution.Err = executor.Execute(ctx, emission.Name, emission.Arguments)
				}
			}
			if emission.Error != "" {
				execution.Err = errors.New(emission.Error)
			}
			engine.tools.Completed(ctx, execution)
			toolCalls = append(toolCalls, application.EngineToolCall{ID: fmt.Sprintf("script-tool-%d", toolTurn), Name: emission.Name, Arguments: emission.Arguments})
			result := execution.Result
			if execution.Err != nil {
				result = execution.Err.Error()
			}
			toolResults = append(toolResults, application.EngineMessage{Role: "tool", ToolCallID: fmt.Sprintf("script-tool-%d", toolTurn), Name: emission.Name, Content: result, ContentSet: true})
		case "approval.request":
			if _, err := engine.requestApproval(ctx, emission.Name, *emission.Approval); err != nil {
				return "", err
			}
		case "engine.error":
			return "", errors.New(emission.Error)
		default:
			return "", fmt.Errorf("unsupported emission type %q", emission.Type)
		}
	}
	history := []application.EngineMessage{{Role: "user", Content: input, ContentSet: true}}
	history = append(history, application.EngineMessage{Role: "assistant", Content: answer.String(), ContentSet: true, ToolCalls: toolCalls})
	history = append(history, toolResults...)
	engine.mu.Lock()
	engine.history = history
	engine.mu.Unlock()
	return answer.String(), nil
}

func (engine *ScriptedEngine) takeTurn(input string) (EngineTurn, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.next >= len(engine.script) {
		return EngineTurn{}, fmt.Errorf("unexpected user input %q: script exhausted", input)
	}
	turn := engine.script[engine.next]
	if input != turn.OnUser {
		return EngineTurn{}, fmt.Errorf("unexpected user input %q, want %q", input, turn.OnUser)
	}
	engine.next++
	return turn, nil
}

func (engine *ScriptedEngine) requestApproval(ctx context.Context, toolName string, approval ApprovalSpec) (application.ApprovalDecision, error) {
	if engine.approvals == nil {
		return application.ApprovalDecision{}, fmt.Errorf("approval %q emitted without an ApprovalRequester", approval.ID)
	}
	options := make([]application.InteractionOption, 0, len(approval.Options))
	for _, option := range approval.Options {
		options = append(options, application.InteractionOption{
			ID: option.ID, Label: option.Label, Description: option.Description, Style: option.Style,
		})
	}
	return engine.approvals.Request(ctx, application.ApprovalRequest{
		ID: approval.ID, Question: approval.Question, Risk: approval.Risk,
		ToolName: toolName, Preview: approval.Preview, Options: options,
	})
}

func allowOption(approval ApprovalSpec) string {
	if approval.AllowOption != "" {
		return approval.AllowOption
	}
	return "allow"
}

func (engine *ScriptedEngine) History() []application.EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]application.EngineMessage(nil), engine.history...)
}

func (engine *ScriptedEngine) ClearHistory() {
	engine.mu.Lock()
	engine.history = nil
	engine.mu.Unlock()
}

func (engine *ScriptedEngine) ReplaceHistory(sessionID string, history []application.EngineMessage) error {
	engine.mu.Lock()
	engine.sessionID = sessionID
	engine.history = append([]application.EngineMessage(nil), history...)
	engine.mu.Unlock()
	return nil
}

func (engine *ScriptedEngine) SessionID() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.sessionID
}

func (engine *ScriptedEngine) StartSession() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = fmt.Sprintf("session-e2e-%d", engine.next+1)
	engine.history = nil
	return engine.sessionID
}

func (engine *ScriptedEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.prompt = prompt
	engine.mu.Unlock()
}

func (engine *ScriptedEngine) SetMaxLoops(maxLoops int) {
	engine.mu.Lock()
	engine.maxLoops = maxLoops
	engine.mu.Unlock()
}

func (engine *ScriptedEngine) AppendHistory(msg types.Message) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	engine.history = append(engine.history, application.EngineMessage{Role: msg.Role, Content: content})
}

func (*ScriptedEngine) TraceText() string                                      { return "scripted scenario" }
func (*ScriptedEngine) TokenCount() string                                     { return "0" }
func (*ScriptedEngine) NodeSessionConversation(string) ([]types.Message, bool) { return nil, false }
func (*ScriptedEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool) {
	return nil, false
}
func (*ScriptedEngine) NodeToolResult(string, string) (string, bool) { return "", false }
func (*ScriptedEngine) NodeWorktreeInfoFor(string) (seelebridge.NodeWorktreeInfo, bool) {
	return seelebridge.NodeWorktreeInfo{}, false
}
func (*ScriptedEngine) SubAgentTree() []dto.SubAgentTreeNode { return nil }

func (engine *ScriptedEngine) Remaining() int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return len(engine.script) - engine.next
}

func (engine *ScriptedEngine) toolExecutor() ToolExecutor {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.executor
}

var _ application.ChatEngine = (*ScriptedEngine)(nil)
