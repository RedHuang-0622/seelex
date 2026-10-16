package seelexctx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// recordingSummarizer 记录每一次重放请求，用来验证分片链的片数与摘要前向传递。
type recordingSummarizer struct {
	requests []ReplayRequest
	failOn   int // 1 起算的片号；> 0 时该片返回错误
}

func (s *recordingSummarizer) Summarize(_ context.Context, req ReplayRequest) (ReplayResult, error) {
	s.requests = append(s.requests, req)
	if s.failOn > 0 && len(s.requests) == s.failOn {
		return ReplayResult{}, errors.New("chunk failed")
	}
	return ReplayResult{Chapter2: fmt.Sprintf("### 目标 (Goal)\nchunk-%d", len(s.requests))}, nil
}

func replayTestTurns(count int) []types.Message {
	messages := make([]types.Message, 0, count*2)
	for index := 1; index <= count; index++ {
		content := strings.Repeat(fmt.Sprintf("turn %d material ", index), 40)
		answer := "ack " + content
		messages = append(messages,
			types.Message{Role: "user", Content: &content},
			types.Message{Role: "assistant", Content: &answer},
		)
	}
	return messages
}

// TestChunkReplayMessagesSplitsOnProtocolUnits：分片预算 <= 0 或消息为空 → 不分片；
// 否则按协议单元切片，片界不切 message（工具链整体落在一片里）。
func TestChunkReplayMessagesSplitsOnProtocolUnits(t *testing.T) {
	if plan := ChunkReplayMessages(replayTestTurns(2), 0); plan.ChunkCount() != 0 {
		t.Fatalf("预算 <= 0 不应分片，得到 %d 片", plan.ChunkCount())
	}
	if plan := ChunkReplayMessages(nil, 1000); plan.ChunkCount() != 0 {
		t.Fatalf("空消息不应分片，得到 %d 片", plan.ChunkCount())
	}

	messages := replayTestTurns(3)
	plan := ChunkReplayMessages(messages, 200)
	if plan.ChunkCount() < 2 {
		t.Fatalf("小预算应切出多片，得到 %d 片（%v）", plan.ChunkCount(), plan.ChunkTokens)
	}
	total := 0
	for index, chunk := range plan.Chunks {
		if len(chunk) == 0 {
			t.Fatalf("第 %d 片为空", index+1)
		}
		total += len(chunk)
	}
	if total != len(messages) {
		t.Fatalf("分片丢了消息：切出 %d 条，原文 %d 条", total, len(messages))
	}

	// 单单元自身超预算 → 该单元独占一片（不切 message）。
	readArguments, toolBody := `{"path":"a.go"}`, strings.Repeat("body ", 400)
	chain := []types.Message{
		{Role: "user", Content: stringPtr("read the file")},
		{Role: "assistant", ToolCalls: []types.ToolCall{{
			ID: "call-1", Function: types.ToolCallFunction{Name: "read_file", Arguments: readArguments},
		}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: &toolBody},
	}
	chainPlan := ChunkReplayMessages(chain, 10)
	if chainPlan.ChunkCount() != 1 {
		t.Fatalf("工具链整体应落在一片（不切 message），得到 %d 片", chainPlan.ChunkCount())
	}
	if len(chainPlan.Chunks[0]) != len(chain) {
		t.Fatalf("单片丢了工具链消息：%d/%d", len(chainPlan.Chunks[0]), len(chain))
	}
}

// TestSummarizeChunkPlanFailsSafely：缺少摘要器 / 空计划 / 任一片失败都必须整条
// 退出（由调用方回退本地确定性压缩，绝不中断请求）。
func TestSummarizeChunkPlanFailsSafely(t *testing.T) {
	plan := ReplayChunkPlan{Chunks: [][]types.Message{replayTestTurns(1), replayTestTurns(1)}}
	if _, err := SummarizeChunkPlan(context.Background(), nil, ReplayRequest{}, plan); err == nil {
		t.Fatal("缺少摘要器必须报错")
	}
	if _, err := SummarizeChunkPlan(context.Background(), &recordingSummarizer{}, ReplayRequest{}, ReplayChunkPlan{}); err == nil {
		t.Fatal("空计划必须报错")
	}
	failing := &recordingSummarizer{failOn: 2}
	if _, err := SummarizeChunkPlan(context.Background(), failing, ReplayRequest{}, plan); err == nil {
		t.Fatal("片失败必须整条退出（由调用方回退本地压缩）")
	}
	if len(failing.requests) != 2 {
		t.Fatalf("失败前应恰好请求 2 片，得到 %d", len(failing.requests))
	}
}

// TestSummarizeChunkPlanCarriesSummaryForward：逐片重放 + 摘要前向传递——片 1 用固定
// 指令，片 2..k 携带上一片摘要（拼进指令前缀），最终 Chapter 2 = 末片摘要。
func TestSummarizeChunkPlanCarriesSummaryForward(t *testing.T) {
	summarizer := &recordingSummarizer{}
	plan := ReplayChunkPlan{
		Chunks:      [][]types.Message{replayTestTurns(1), replayTestTurns(1), replayTestTurns(1)},
		ChunkTokens: []int{10, 10, 10},
	}
	result, err := SummarizeChunkPlan(context.Background(), summarizer, ReplayRequest{SystemPrompt: "system bytes"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(summarizer.requests) != 3 {
		t.Fatalf("应逐片重放 3 次，得到 %d", len(summarizer.requests))
	}
	first := summarizer.requests[0]
	if first.Instruction != PrefixReplayInstruction {
		t.Fatalf("首片指令应是固定压缩指令，得到 %q", first.Instruction)
	}
	if len(first.History) != len(plan.Chunks[0]) || first.SystemPrompt != "system bytes" {
		t.Fatalf("首片素材不对：system=%q history=%d", first.SystemPrompt, len(first.History))
	}
	for index := 1; index < 3; index++ {
		instruction := summarizer.requests[index].Instruction
		if !strings.Contains(instruction, fmt.Sprintf("chunk-%d", index)) {
			t.Fatalf("第 %d 片指令未携带上一片摘要：%q", index+1, instruction)
		}
		if !strings.HasSuffix(instruction, PrefixReplayInstruction) {
			t.Fatalf("第 %d 片指令必须仍以固定压缩指令收尾（唯一新增尾巴）：%q", index+1, instruction)
		}
	}
	if result.Chapter2 != "### 目标 (Goal)\nchunk-3" {
		t.Fatalf("最终 Chapter 2 应为末片摘要，得到 %q", result.Chapter2)
	}
}

// TestReplayEvidenceOnlyWhenChunked：分片决策的打点落点——未分片（<= 1 片）不留痕，
// 分片时留一条可审计证据。
func TestReplayEvidenceOnlyWhenChunked(t *testing.T) {
	if evidence := ReplayEvidence(ReplayChunkPlan{}); evidence != nil {
		t.Fatalf("未分片不应留痕：%+v", evidence)
	}
	if evidence := ReplayEvidence(ReplayChunkPlan{Chunks: [][]types.Message{replayTestTurns(1)}}); evidence != nil {
		t.Fatalf("单片不应留痕：%+v", evidence)
	}
	evidence := ReplayEvidence(ReplayChunkPlan{Chunks: [][]types.Message{replayTestTurns(1), replayTestTurns(1)}})
	if len(evidence) != 1 || evidence[0].Ref != "replay-chunked:2" {
		t.Fatalf("分片证据形状不对：%+v", evidence)
	}
}

// TestCompactionDAGChunkedReplayWritesFrameFacts：DAG 集成——溢出区超过片预算时走
// 分片重放链，帧标记 summary_source=replay 并留下分片证据（打点）。
func TestCompactionDAGChunkedReplayWritesFrameFacts(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		SessionIDProvider: func() string { return "sess-chunk" },
		Summarizer:        summarizer,
		// 片预算极小 → 必然切多片；History 非空是走重放通道的前提。
		ReplayInputTokens: 120,
	})
	overflow := replayTestTurns(4)
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: overflow, History: overflow, UnitCount: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summarizer.requests) < 2 {
		t.Fatalf("未走分片重放链，重放次数=%d", len(summarizer.requests))
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("分片成功后 summary_source 应为 replay，得到 %q", frame.SummarySource)
	}
	found := false
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, "replay-chunked:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("帧证据里缺少分片重放打点：%+v", frame.Evidence)
	}
	if body := FrameChapter2(frame); !strings.Contains(body, "chunk-") {
		t.Fatalf("Chapter 2 应为末片摘要：\n%s", body)
	}
}

// TestCompactionDAGSingleReplayKeepsPrefixCachePath：片预算足够大时不分片——仍走
// 「system 同字节 + 真实请求 History 原样」的单次重放（唯一能吃前缀缓存的路径）。
func TestCompactionDAGSingleReplayKeepsPrefixCachePath(t *testing.T) {
	summarizer := &recordingSummarizer{}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        summarizer,
		ReplayInputTokens: 1_000_000,
	})
	overflow := replayTestTurns(4)
	frame, err := dag.Execute(context.Background(), CompactionInput{
		Messages: overflow, History: overflow, UnitCount: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summarizer.requests) != 1 {
		t.Fatalf("未超片预算时应只重放一次，得到 %d", len(summarizer.requests))
	}
	if len(summarizer.requests[0].History) != len(overflow) {
		t.Fatalf("单次重放必须用真实请求 History 原样，得到 %d 条", len(summarizer.requests[0].History))
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("summary_source 应为 replay，得到 %q", frame.SummarySource)
	}
	for _, evidence := range frame.Evidence {
		if strings.HasPrefix(evidence.Ref, "replay-chunked:") {
			t.Fatalf("未分片不该留分片证据：%+v", evidence)
		}
	}
}
