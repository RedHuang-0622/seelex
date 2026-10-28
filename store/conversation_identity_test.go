package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

func TestConversationUsesProtocolIdentity(t *testing.T) {
	var event Event = protocol.TranscriptEvent{Seq: 1, Role: "user"}
	message := protocol.Message{ID: "message-1"}
	toolCall := protocol.ToolCall{ID: "call-1", Status: dto.ToolEventSuccess}
	message.Tool = &toolCall
	if message.Tool.Status != dto.ToolEventSuccess {
		t.Fatalf("tool status = %q, want %q", message.Tool.Status, dto.ToolEventSuccess)
	}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded protocol.Message
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != message.ID || decoded.Tool == nil || decoded.Tool.ID != message.Tool.ID ||
		decoded.Tool.Status != dto.ToolEventSuccess {
		t.Fatalf("round-trip message = %#v", decoded)
	}
	if event.Seq != 1 || event.Role != "user" {
		t.Fatalf("event identity = %#v", event)
	}
}

func TestStateChannelRetiredOnJSON(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), storerootSettingsForTest())
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

func storerootSettingsForTest() storeroot.Settings {
	return storeroot.Settings{}
}
