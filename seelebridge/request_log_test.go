package seelebridge

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

type recordingFakeCompleter struct{}

func (recordingFakeCompleter) CompleteStream(
	_ context.Context,
	_ []types.Message,
	_ []types.Tool,
	_ func(string),
) (string, string, []types.ToolCall, error) {
	return "ok", "", nil, nil
}

// TestRequestLoggerRecordsOrderedDigests：SEELEX_REQUEST_LOG 门控的中间件
// 按调用顺序写 JSONL（seq 单调），每条记录 role+hash，不写原文。
func TestRequestLoggerRecordsOrderedDigests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	logger, err := openRequestLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	inner := &requestLogStreamCompleter{inner: recordingFakeCompleter{}, log: logger}
	first := []types.Message{{Role: "user", Content: strPtr("hello")}}
	second := []types.Message{
		{Role: "user", Content: strPtr("hello")},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "call-1", Function: types.ToolCallFunction{Name: "get_time", Arguments: `{}`}}}},
	}
	if _, _, _, err := inner.CompleteStream(context.Background(), first, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inner.CompleteStream(context.Background(), second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("request log lines = %d, want 2", len(lines))
	}
	var secondEntry requestLogEntry
	if err := json.Unmarshal([]byte(lines[1]), &secondEntry); err != nil {
		t.Fatal(err)
	}
	if secondEntry.Seq != 2 || len(secondEntry.Messages) != 2 {
		t.Fatalf("second entry = seq %d messages %d, want seq=2 messages=2",
			secondEntry.Seq, len(secondEntry.Messages))
	}
	if secondEntry.Messages[1].Role != "assistant" || len(secondEntry.Messages[1].ToolCalls) != 1 {
		t.Fatalf("tool-call message digest missing: %+v", secondEntry.Messages[1])
	}
	if strings.Contains(lines[1], "hello") || strings.Contains(lines[1], `"arguments":"{}"`) {
		t.Fatalf("request log must not contain raw content/arguments: %s", lines[1])
	}
}

func strPtr(value string) *string {
	return &value
}
