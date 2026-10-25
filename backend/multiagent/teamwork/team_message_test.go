package teamwork

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeTeammateMessageRequestRequiresRoleItemMessage(t *testing.T) {
	if _, err := DecodeTeammateMessageRequest(`{"role":"exec","message":"why"}`); err == nil || !strings.Contains(err.Error(), "item") {
		t.Fatalf("item 必填（WorkItem Session 是唯一窗口），得到 %v", err)
	}
	if _, err := DecodeTeammateMessageRequest(`{"item":"wi-1","message":"why"}`); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("role 必填，得到 %v", err)
	}
	if _, err := DecodeTeammateMessageRequest(`{"role":"exec","item":"wi-1"}`); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("message 必填，得到 %v", err)
	}
}

func TestDecodeTeammateMessageRequestDefaultsAndBoundsWait(t *testing.T) {
	request, err := DecodeTeammateMessageRequest(`{"role":" exec ","item":" wi-1 ","message":" 为什么超时？ "}`)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if request.Role != "exec" || request.Item != "wi-1" {
		t.Fatalf("role/item 必须 trim: %+v", request)
	}
	if request.Message != " 为什么超时？ " {
		t.Fatalf("message 是正文，必须原样保留: %q", request.Message)
	}
	if request.Wait != 30*time.Second {
		t.Fatalf("默认等待 = %s，want 30s", request.Wait)
	}
	tooLong, err := DecodeTeammateMessageRequest(`{"role":"exec","item":"wi-1","message":"x","wait_ms":999999999}`)
	if err != nil {
		t.Fatalf("Decode(too long): %v", err)
	}
	if tooLong.Wait != TeammateMessageMaxWait {
		t.Fatalf("等待必须夹到硬上限 %s，得到 %s", TeammateMessageMaxWait, tooLong.Wait)
	}
}

func TestTeamMessageSchemaNamesTheWorkItemWindow(t *testing.T) {
	schema := TeamMessageSchema()
	raw, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required 形状错误: %#v", schema["required"])
	}
	required := strings.Join(raw, ",")
	for _, field := range []string{"role", "item", "message"} {
		if !strings.Contains(required, field) {
			t.Fatalf("team_message 必填字段缺 %s: %v", field, raw)
		}
	}
	if desc := TeamMessageDescription(); !strings.Contains(desc, "Work Item") || !strings.Contains(desc, "does not") {
		t.Fatalf("描述必须说明 WorkItem 窗口与不改变状态: %q", desc)
	}
}
