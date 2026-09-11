package sessionstore

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// conversationStateV3 是含 plan/execution/projection 重载荷的 v3 会话记录，
// 验证 conversation 模块冷读只解析目标子树的行为契约。
const conversationStateV3 = `{
  "version": 3,
  "id": "session",
  "title": {"value": "Stable title"},
  "plan_stack": [{"id":"plan-1","plan":{"name":"plan","nodes":[{"id":"n1","label":"node"}]}}],
  "conversation": {
    "updated_at": "2026-08-05T00:00:00Z",
    "messages": [
      {"id":"message-1","role":"user","content":"hello","created_at":"2026-08-05T00:00:00Z"},
      {"id":"message-2","role":"assistant","content":"hi","tool":{"id":"call-1","name":"bash","arguments":"{\"cmd\":\"ls\"}","result":"ok","status":"success","duration":120000000},"created_at":"2026-08-05T00:00:01Z"},
      {"id":"message-3","role":"user","content":"world","created_at":"2026-08-05T00:00:02Z"}
    ]
  },
  "execution": {"task":{"status":"completed","summary":"x"},"read_files":[{"path":"a.txt"}]},
  "projection": {"schema_version":1,"session_id":"session","task_id":"task-1","checkpoint":{"version":1}},
  "updated_at": "2026-08-05T00:00:03Z"
}`

// TestConversationRangeFromMessageRows 契约测试：conversation 模块范围读取
// 由 message 事件行派生（窗口切片、总数、越界收敛、limit<=0）。
func TestConversationRangeFromMessageRows(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
	} {
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			key := Key{ProjectID: "project", SessionID: "session"}
			if err := repository.WriteCommit(context.Background(), key, Commit{Events: []Event{
				{Seq: 1, MessageID: "message-1", Role: "user", Content: "hello"},
				{Seq: 2, MessageID: "message-2", Role: "assistant", Content: "hi",
					ToolCalls: []EventToolCall{{ID: "call-1", Name: "bash", Arguments: "{\"cmd\":\"ls\"}"}}},
				{Seq: 3, MessageID: "message-3", Role: "user", Content: "world"},
			}}); err != nil {
				t.Fatal(err)
			}

			// 窗口读：offset=0, limit=2 → 前两条（user 正文 + assistant 正文）；
			// total 为完整消息数：带 tool_call 的行拆成「正文 + 每个调用」，
			// 所以 3 行 message 事件派生 4 条可见消息。
			messages, total, err := repository.ReadConversationRange(context.Background(), key, 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			if total != 4 || len(messages) != 2 || messages[0].ID != "message-1" || messages[1].ID != "message-2" {
				t.Fatalf("window = %#v total=%d, want message-1..message-2 total=4", messages, total)
			}
			if messages[1].Role != "assistant" || messages[1].Content != "hi" || messages[1].Tool != nil {
				t.Fatalf("assistant body = %#v", messages[1])
			}
			// 行内 tool_call 派生一条独立的调用消息（Tool 深拷贝语义、参数保留）。
			calls, _, err := repository.ReadConversationRange(context.Background(), key, 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0].Role != "tool" || calls[0].Tool == nil {
				t.Fatalf("tool call message = %#v", calls)
			}
			if calls[0].Tool.ID != "call-1" || calls[0].Tool.Name != "bash" || calls[0].Tool.Status != "success" ||
				calls[0].Tool.Arguments != `{"cmd":"ls"}` {
				t.Fatalf("tool call = %#v", calls[0].Tool)
			}
			// 尾部窗口：offset=1, limit=10 → 收敛到消息 2..3（含调用消息）。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 1, 10)
			if err != nil {
				t.Fatal(err)
			}
			if total != 4 || len(messages) != 3 || messages[0].ID != "message-2" ||
				messages[1].Tool == nil || messages[2].ID != "message-3" {
				t.Fatalf("tail window = %#v total=%d", messages, total)
			}
			// 越界 offset 收敛到空窗口，不报错。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 5, 2)
			if err != nil {
				t.Fatal(err)
			}
			if total != 4 || len(messages) != 0 {
				t.Fatalf("clamped window = %#v total=%d, want empty total=4", messages, total)
			}
			// limit<=0 返回窗口尾段（与 ReadRange 语义一致）。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if total != 4 || len(messages) != 3 || messages[0].ID != "message-2" {
				t.Fatalf("limit<=0 window = %#v total=%d", messages, total)
			}
		})
	}
}

// TestConversationRangeDerivedFromMessageRows 验证 S20 后 conversation 由
// message 事件行派生（不再读 state.json）。
func TestConversationRangeDerivedFromMessageRows(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), storageSettings{})
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: []Event{
		{Seq: 1, MessageID: "message-1", Role: "user", Content: "a"},
		{Seq: 2, MessageID: "message-2", Role: "user", Content: "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	messages, total, err := repository.ReadConversationRange(context.Background(), key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(messages) != 1 || messages[0].ID != "message-2" || messages[0].Content != "b" {
		t.Fatalf("derived window = %#v total=%d", messages, total)
	}
}

// TestStateChannelRetiredOnJSON 验证 S20：JSON v8 的 state/record 通道停写
// 停读（WriteState 空操作、ReadState not-exist），conversation 改由 message
// 行派生。
func TestStateChannelRetiredOnJSON(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), storageSettings{})
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteState(context.Background(), key, []byte(`{"version":3}`)); err != nil {
		t.Fatalf("WriteState must be a no-op on v8 JSON: %v", err)
	}
	if _, err := repository.ReadState(context.Background(), key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadState err = %v, want fs.ErrNotExist", err)
	}
}

// TestDecodeConversationRangeSkipsNonConversationModules 验证部分解码结果
// 与完整解码一致（工具消息、时间戳完整保留）。
func TestDecodeConversationRangeSkipsNonConversationModules(t *testing.T) {
	messages, total, err := decodeConversationRange([]byte(conversationStateV3), "session", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(messages) != 3 {
		t.Fatalf("decoded = %d/%d messages", len(messages), total)
	}
	createdAt := messages[0].CreatedAt
	if createdAt.IsZero() || createdAt.Location() != time.UTC {
		t.Fatalf("created_at = %v, want non-zero UTC", createdAt)
	}
	want := []string{"message-1", "message-2", "message-3"}
	got := make([]string, len(messages))
	for index := range messages {
		got[index] = messages[index].ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}
