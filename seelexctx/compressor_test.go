package seelexctx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// fakeQuickChat 记录调用次数并返回固定摘要（QuickChat 契约）。
type fakeQuickChat struct {
	calls    int
	response string
}

func (f *fakeQuickChat) Complete(_ context.Context, _ seelectx.QuickChatRequest) (types.Message, error) {
	f.calls++
	return types.Message{Role: "assistant", Content: &f.response}, nil
}

// bigHistory 构造 len 条超长消息（≥ 最小压缩阈值）。
func bigHistory(count, chars int) []types.Message {
	content := strings.Repeat("压缩历史内容", chars)
	history := make([]types.Message, count)
	for index := range history {
		history[index] = textMessage("user", content)
	}
	return history
}

func TestCompressorShortHistoryFastPath(t *testing.T) {
	// 短历史快速路径：低于阈值直接返回，不调用 QuickChat。
	chat := &fakeQuickChat{response: "不应调用"}
	compressor := NewCompressor(CompressorOptions{
		QuickChat:      chat,
		ShortThreshold: 6,
	})
	result, err := compressor.Compress(context.Background(), seelectx.CompressionRequest{
		History: bigHistory(3, 10), MaxTokens: 100, Query: "q",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("short history must return unchanged, got %d messages", len(result.Messages))
	}
	if chat.calls != 0 {
		t.Fatalf("short history must not invoke QuickChat, calls=%d", chat.calls)
	}
}

func TestCompressorQuickChatCheckpoint(t *testing.T) {
	chat := &fakeQuickChat{response: "结构化摘要：完成迁移目标"}
	compressor := NewCompressor(CompressorOptions{
		QuickChat:      chat,
		ShortThreshold: 2,
		MinMessages:    2,
		MinTokens:      1,
		MaxDepth:       2,
	})
	result, err := compressor.Compress(context.Background(), seelectx.CompressionRequest{
		History: bigHistory(3, 30), MaxTokens: 50, Query: "压缩查询",
	})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls == 0 {
		t.Fatal("long history must invoke QuickChat checkpoint")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("checkpoint result messages = %d, want 1 summary", len(result.Messages))
	}
	if !strings.Contains(*result.Messages[0].Content, "结构化摘要") {
		t.Fatalf("summary content = %q", *result.Messages[0].Content)
	}
}

func TestCompressorSnapshotPath(t *testing.T) {
	// 跨会话承袭：有快照 → compactor 按预算压缩（不调用 QuickChat）。
	chat := &fakeQuickChat{response: "不应调用"}
	compressor := NewCompressor(CompressorOptions{
		QuickChat:      chat,
		ShortThreshold: 2,
		Compactor:      compactor.NewCompactor(),
		SnapshotFor: func(_ context.Context, _ seelectx.CompressionRequest) *snapshot.ContextSnapshot {
			return &snapshot.ContextSnapshot{
				SourceSessionID: "parent", ExportedAt: time.Now(),
				Goal: "父目标", Findings: []string{"发现1", "发现2", "发现3"},
			}
		},
	})
	result, err := compressor.Compress(context.Background(), seelectx.CompressionRequest{
		History: bigHistory(4, 30), MaxTokens: 2000, Query: "q",
	})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 {
		t.Fatalf("snapshot path must not invoke QuickChat, calls=%d", chat.calls)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("snapshot compression must render one summary message, got %d", len(result.Messages))
	}
	if !strings.Contains(*result.Messages[0].Content, "父目标") {
		t.Fatalf("snapshot summary must preserve goal: %q", *result.Messages[0].Content)
	}
}

func TestCompressorNoQuickChatFailsLongHistory(t *testing.T) {
	compressor := NewCompressor(CompressorOptions{ShortThreshold: 2})
	_, err := compressor.Compress(context.Background(), seelectx.CompressionRequest{
		History: bigHistory(4, 30), MaxTokens: 50,
	})
	if err == nil {
		t.Fatal("long history without QuickChat must fail explicitly")
	}
}
