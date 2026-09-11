package sessionstore

// 工具结果的「视图呈现 / provider wire 原文」分离（研究文档 §8 发现 2）：
//
// 记录侧必须保存**已发出的字节**。工具失败时 wire 上是框架合成的原始错误
// JSON（`{"error": %q}`，63 B），而视图/轨迹读的是应用分类呈现文本
// （presentToolError，181 B）；超限结果同理只差归档引用。provider 投影出口
// （eventsToMessages / 会话 wire 装配）必须取 ProviderContent，否则下一轮从
// 记录重投影时这条 tool 消息被改写、provider 前缀缓存自该点起全部失效。
//
// 运行：go test ./sessionstore -run ProviderContent -v -count=1

import (
	"context"
	"strings"
	"testing"
	"time"
)

const (
	providerContentWire      = `{"error": "project scope: no project is bound to this session"}`
	providerContentPresented = "【模块：工具执行｜方法：handleToolComplete(read_file)】\n该工具未能完成本次操作。\n当前进度已保留；请检查任务条件后重试或调整下一步。"
)

func providerContentToolRows() []Event {
	return []Event{
		{Seq: 1, MessageID: "m1", Role: "user", Kind: EventKindUserInput, Content: "读一下文件", TokenCount: 4, CreatedAt: time.Now().UTC()},
		{Seq: 2, MessageID: "m2", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x"}`}}, TokenCount: 4, CreatedAt: time.Now().UTC()},
		{Seq: 3, MessageID: "m3", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "call-1", Name: "read_file",
			Content: providerContentPresented, ProviderContent: providerContentWire, TokenCount: 4, CreatedAt: time.Now().UTC()},
		{Seq: 4, MessageID: "m4", Role: "assistant", Kind: EventKindLLM, Content: "第一轮完成", TokenCount: 4, CreatedAt: time.Now().UTC()},
	}
}

// TestProviderContentPersistsAcrossReload 覆盖「重启恢复」：ProviderContent 必须
// 随事件行落盘并原样读回（否则恢复后重投影又回落到呈现文本）。
func TestProviderContentPersistsAcrossReload(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, providerContentToolRows())

	rows, err := store.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	tool := providerContentToolRow(t, rows)
	if tool.Content != providerContentPresented || tool.ProviderContent != providerContentWire {
		t.Fatalf("reloaded tool row content=%q provider_content=%q, want 呈现/原文分离",
			tool.Content, tool.ProviderContent)
	}
}

// TestProviderContentDrivesProviderProjections 覆盖两个 provider 投影出口都取
// wire 原文：事件行 →（R2 wire 装配 / eventsToMessages）。
func TestProviderContentDrivesProviderProjections(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, providerContentToolRows())

	// 出口 1：R2 wire 装配（会话存储装配 API）。
	assembled := runWireAssembly(t, store, key, nil, 200_000, 3)
	if got := providerContentToolWireContent(t, assembled, "call-1"); got != providerContentWire {
		t.Fatalf("assembleWire tool content = %q, want wire 原文 %q", got, providerContentWire)
	}

	// 出口 2：eventsToMessages（DurableHistory 冷载 / Read 派生）。
	rows, err := store.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	messages := rowsToProviderMessages(rows)
	found := false
	for _, message := range messages {
		if message.Role != "tool" || message.ToolCallID != "call-1" {
			continue
		}
		found = true
		if message.Content == nil || *message.Content != providerContentWire {
			t.Fatalf("eventsToMessages tool content = %v, want wire 原文 %q", message.Content, providerContentWire)
		}
	}
	if !found {
		t.Fatalf("eventsToMessages dropped the tool row: %+v", messages)
	}
}

// TestProviderContentFallsBackToContent 钉住兜底语义：未设置 ProviderContent 的
// 事件（绝大多数行）投影时仍取 Content——分离字段不改变既有行为。
func TestProviderContentFallsBackToContent(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		{Seq: 1, MessageID: "m1", Role: "user", Kind: EventKindUserInput, Content: "问题", TokenCount: 2, CreatedAt: time.Now().UTC()},
		{Seq: 2, MessageID: "m2", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "call-1", Name: "read_file", Arguments: "{}"}}, TokenCount: 2, CreatedAt: time.Now().UTC()},
		{Seq: 3, MessageID: "m3", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "call-1", Name: "read_file", Content: "OK\n", TokenCount: 2, CreatedAt: time.Now().UTC()},
	}
	commitRoundRows(t, store, key, rows)

	assembled := runWireAssembly(t, store, key, nil, 200_000, 3)
	if got := providerContentToolWireContent(t, assembled, "call-1"); got != "OK\n" {
		t.Fatalf("assembleWire tool content = %q, want Content 兜底 %q", got, "OK\n")
	}
}

// TestDurableHistoryLoadUsesProviderContent 覆盖 DurableHistory.Load 的滑动窗口
// 分支（生产真实投影出口：每轮收尾清空工作视图，下一轮从这里冷载）。
func TestDurableHistoryLoadUsesProviderContent(t *testing.T) {
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-provider-content")
	history.SetTailBudget(100_000, 10)
	if err := router.SaveCommit("session-provider-content", Commit{Events: providerContentToolRows()}); err != nil {
		t.Fatal(err)
	}
	loaded, err := history.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range loaded {
		if message.Role != "tool" || message.ToolCallID != "call-1" {
			continue
		}
		if message.Content == nil || *message.Content != providerContentWire {
			t.Fatalf("loaded tool content = %v, want wire 原文 %q", message.Content, providerContentWire)
		}
		if strings.Contains(*message.Content, "【模块：") {
			t.Fatalf("呈现文本进入了 provider 投影: %q", *message.Content)
		}
		return
	}
	t.Fatalf("loaded history missing tool row: %+v", loaded)
}

func providerContentToolRow(t *testing.T, rows []Event) Event {
	t.Helper()
	for _, row := range rows {
		if row.Role == "tool" && row.ToolCallID == "call-1" {
			return row
		}
	}
	t.Fatalf("rows missing tool call-1: %+v", rows)
	return Event{}
}

func providerContentToolWireContent(t *testing.T, assembled wireResult, toolCallID string) string {
	t.Helper()
	for _, message := range assembled.Messages {
		if message.Role == wireRoleTool && message.ToolCallID == toolCallID {
			return message.Content
		}
	}
	t.Fatalf("assembled wire missing tool %s: %+v", toolCallID, assembled.Messages)
	return ""
}
