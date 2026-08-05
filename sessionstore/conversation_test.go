package sessionstore

import (
	"context"
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

// TestConversationRangeAcrossLocalBackends 契约测试：conversation 模块范围
// 读取在 JSON/SQLite 后端语义一致（窗口切片、总数、越界收敛、limit<=0）。
func TestConversationRangeAcrossLocalBackends(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
		{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")},
	} {
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			key := Key{ProjectID: "project", SessionID: "session"}
			if err := repository.WriteState(context.Background(), key, []byte(conversationStateV3)); err != nil {
				t.Fatal(err)
			}

			// 窗口读：offset=0, limit=2 → 前两条；total 为完整消息数。
			messages, total, err := repository.ReadConversationRange(context.Background(), key, 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			if total != 3 || len(messages) != 2 || messages[0].ID != "message-1" || messages[1].ID != "message-2" {
				t.Fatalf("window = %#v total=%d, want message-1..message-2 total=3", messages, total)
			}
			// 工具消息映射完整（Tool 深拷贝语义）。
			if messages[1].Tool == nil || messages[1].Tool.ID != "call-1" || messages[1].Tool.Name != "bash" || messages[1].Tool.Status != "success" || messages[1].Tool.Duration != 120*time.Millisecond {
				t.Fatalf("tool message = %#v", messages[1].Tool)
			}
			// 尾部窗口：offset=1, limit=10 → 收敛到消息 2-3。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 1, 10)
			if err != nil {
				t.Fatal(err)
			}
			if total != 3 || len(messages) != 2 || messages[0].ID != "message-2" || messages[1].ID != "message-3" {
				t.Fatalf("tail window = %#v total=%d", messages, total)
			}
			// 越界 offset 收敛到空窗口，不报错。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 5, 2)
			if err != nil {
				t.Fatal(err)
			}
			if total != 3 || len(messages) != 0 {
				t.Fatalf("clamped window = %#v total=%d, want empty total=3", messages, total)
			}
			// limit<=0 返回窗口尾段（与 ReadRange 语义一致）。
			messages, total, err = repository.ReadConversationRange(context.Background(), key, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if total != 3 || len(messages) != 2 || messages[0].ID != "message-2" {
				t.Fatalf("limit<=0 window = %#v total=%d", messages, total)
			}
		})
	}
}

// TestConversationRangeLegacyArchiveShape 验证 v1 SessionArchive 布局
// （conversation 直接是数组）仍可读（v1 fallback）。
func TestConversationRangeLegacyArchiveShape(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	payload := []byte(`{"version":1,"name":"legacy","conversation":[{"id":"message-1","role":"user","content":"a"},{"id":"message-2","role":"user","content":"b"}]}`)
	if err := repository.WriteState(context.Background(), key, payload); err != nil {
		t.Fatal(err)
	}
	messages, total, err := repository.ReadConversationRange(context.Background(), key, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(messages) != 1 || messages[0].ID != "message-2" || messages[0].Content != "b" {
		t.Fatalf("legacy window = %#v total=%d", messages, total)
	}
}

// TestConversationRangeDegradesExplicitly 验证损坏/不兼容的 conversation
// 模块显式报错，不静默成空历史（interfaces.md 降级矩阵）。
func TestConversationRangeDegradesExplicitly(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	for name, payload := range map[string][]byte{
		"corrupt":          []byte(`{"version":3,"id":"session","conversation":`),
		"version-mismatch": []byte(`{"version":3,"id":"other-session","conversation":{"messages":[]}}`),
		"unsupported":      []byte(`{"version":9,"conversation":{"messages":[]}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := repository.WriteState(context.Background(), key, payload); err != nil {
				t.Fatal(err)
			}
			messages, total, err := repository.ReadConversationRange(context.Background(), key, 0, 5)
			if err == nil {
				t.Fatalf("corrupt conversation returned messages=%#v total=%d, want explicit error", messages, total)
			}
		})
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
