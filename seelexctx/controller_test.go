package seelexctx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// heavyTokenCounter 放大 token 估算（len×100），让测试用小历史即可跨过
// 软/硬阈值（注入 TokenCounter 契约；独立实现避免值接收者嵌入陷阱）。
type heavyTokenCounter struct{}

func (heavyTokenCounter) Name() string { return "heavy-test-v1" }

func (heavyTokenCounter) CountText(value string) int { return len(value) * 100 }

func (c heavyTokenCounter) CountMessage(message types.Message) int {
	tokens := 4 + c.CountText(message.Role) + c.CountText(messageContent(message)) + c.CountText(message.ReasoningContent)
	if message.ToolCallID != "" {
		tokens += 2 + c.CountText(message.ToolCallID)
	}
	if message.Name != "" {
		tokens += 2 + c.CountText(message.Name)
	}
	for _, call := range message.ToolCalls {
		tokens += 8 + c.CountText(call.ID) + c.CountText(call.Function.Name) + c.CountText(call.Function.Arguments)
	}
	return tokens
}

func (c heavyTokenCounter) CountHistory(history []types.Message) int {
	total := 0
	for _, message := range history {
		total += c.CountMessage(message)
	}
	return total
}

// fixedWindowPolicy 固定窗口轮数 N（测试注入 WindowPolicy）。
type fixedWindowPolicy struct{ rounds int }

func (p fixedWindowPolicy) WindowRounds(context.Context, ProviderContextInfo) (int, error) {
	return p.rounds, nil
}

func textMessage(role, content string) types.Message {
	return types.Message{Role: role, Content: &content}
}

// roundHistory 构造 rounds 轮完整协议单元（user + assistant 文本）。
func roundHistory(rounds int) []types.Message {
	var history []types.Message
	for i := 0; i < rounds; i++ {
		history = append(history,
			textMessage("user", fmt.Sprintf("round-%d-user-input-xxxxxxxxxx", i)),
			textMessage("assistant", fmt.Sprintf("round-%d-assistant-reply-yyyyyyyy", i)),
		)
	}
	return history
}

// controllerTestLimits 是控制器**机制**用例的显式阈值注入：这组用例验证窗口
// 溢出/压缩/归档/去重本身，不验证出厂默认档。默认档口径（2026-09-26 起
// 95/98/80）由 limits_test.go 钉住——机制用例显式给出自己假设的比例，默认档
// 调整不会把机制用例带红，也不会让"配置被消费"的断言变成对默认值的隐式依赖。
func controllerTestLimits() Limits {
	return Limits{
		ContextSafetyReserveDivisor: 8,
		ContextSoftPercent:          75,
		ContextHardPercent:          90,
		ContextTargetPercent:        60,
		ContextSingleItemPercent:    50,
	}
}

// newController 构造注入压缩栈的控制器。
//
// 2026-09-30 起控制器只剩"超大工具结果兜底归档"一件事（压缩整条归装配层）：
// 注入面因此只有归档器与压缩栈——栈顶帧的 From 是"溢出起点"的去重基准
// （见 chatUnits / compactedUnitBase），窗口外轮次的划分要接着上次压缩的落点算。
func newController(stacks CompactStackStore) *seelexContextController {
	return &seelexContextController{opts: ControllerOptions{Stacks: stacks}}
}

// recordStack 是测试用 CompactStackStore：Snapshot 返回预置的完整会话
// 记录（含任务/计划栈帧），PushCompact 追加压缩帧。
type recordStack struct {
	mu     sync.Mutex
	record sessionstore.SessionContextRecord
}

func (s *recordStack) Snapshot() sessionstore.SessionContextRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.record
	record.CompactStack = append([]sessionstore.CompactFrame(nil), s.record.CompactStack...)
	return record
}

func (s *recordStack) PushCompact(frame sessionstore.CompactFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record.CompactStack = append(s.record.CompactStack, frame)
	return nil
}

func TestControllerPlanTailMessageDoesNotFormCompressionUnit(t *testing.T) {
	controller := newController(NewMemoryCompactStack())
	history := []types.Message{
		textMessage("user", ActivePlanContextMarker+"\n"+`{"plan_ref":"p1"}`),
		textMessage("user", "真实问题"),
		textMessage("assistant", "回答"),
		textMessage("user", "下一个"),
		textMessage("assistant", "完成"),
	}
	units := controller.chatUnits(history)
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2 (plan tail message must not form a unit)", len(units))
	}
	for _, unit := range units {
		if strings.HasPrefix(*unit.messages[0].Content, ActivePlanContextMarker) {
			t.Fatalf("plan tail message entered compression units: %+v", units)
		}
	}
}

func stringPtr(value string) *string { return &value }

// recordingTurnArchiver 是 TurnArchiver 测试替身：记录归档调用。
type recordingTurnArchiver struct {
	segmentIDs []string
	messageN   []int
}

func (a *recordingTurnArchiver) StoreTurn(_ context.Context, segmentID string, messages []types.Message) (string, error) {
	a.segmentIDs = append(a.segmentIDs, segmentID)
	a.messageN = append(a.messageN, len(messages))
	return "compressed:" + segmentID, nil
}
