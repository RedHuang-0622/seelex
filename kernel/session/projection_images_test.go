package session_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// TestProjectConversationEventsCarriesUserMessageImages 钉住"用户消息随行的图片
// 跟着行走"：冷恢复/历史窗口回读都只经过这一条投影，图片若在这里丢掉，就会
// 出现"刚发完看得见图、重启/切回来就没了"的两套观感。
func TestProjectConversationEventsCarriesUserMessageImages(t *testing.T) {
	images := []dto.ComposerImage{
		{Ref: "media:aaa", Name: "shot.png", MimeType: "image/png", Bytes: 12, Width: 800, Height: 600},
	}
	messages := ProjectConversationEvents([]protocol.TranscriptEvent{
		{Seq: 4, Role: "user", Content: "看这张图", Images: images},
		{Seq: 5, Role: "assistant", Content: "看到了"},
	}, ConversationProjectionPolicy{})
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if got := messages[0]; len(got.Images) != 1 || got.Images[0].Ref != "media:aaa" ||
		got.Images[0].Name != "shot.png" || got.Images[0].Width != 800 {
		t.Fatalf("用户行的图片 = %#v", got.Images)
	}
	// 助手行不背图（图片只属于用户发的那一条消息）。
	if got := messages[1]; len(got.Images) != 0 {
		t.Fatalf("助手行不该带图片: %#v", got.Images)
	}
}
