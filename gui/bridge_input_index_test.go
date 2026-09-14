package gui

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// stubInputIndexApplication 是「会话内全量用户输入索引」可选面的测试桩：
// 记录 Bridge 转发的 sessionID 并回放由用例给定的索引。
type stubInputIndexApplication struct {
	*fakeApplication
	sessionID string
	index     model.SessionInputIndex
	err       error
}

func (stub *stubInputIndexApplication) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	stub.sessionID = sessionID
	return stub.index, stub.err
}

// TestBridgeSessionInputIndexForwards SessionInputIndex 只转发一次，
// sessionID 与索引原样回传（右侧索引的唯一数据入口）。
func TestBridgeSessionInputIndexForwards(t *testing.T) {
	t.Parallel()
	stub := &stubInputIndexApplication{
		fakeApplication: newFakeApplication(),
		index: model.SessionInputIndex{
			SessionID:  "sess-a",
			Total:      12,
			InputCount: 6,
			Window:     model.SessionInputWindow{Offset: 8, Count: 4, Total: 12, HasMore: true, WindowSize: 4},
			Items: []model.SessionInputIndexRow{
				{Round: 1, MessageID: "message-1", Offset: 0, Summary: "第一问", Loaded: false},
				{Round: 6, MessageID: "message-11", Offset: 10, Summary: "latest", Loaded: true},
			},
		},
	}
	bridge, err := NewBridge(stub, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	index, err := bridge.SessionInputIndex("sess-a")
	if err != nil {
		t.Fatalf("SessionInputIndex: %v", err)
	}
	if stub.sessionID != "sess-a" {
		t.Fatalf("forwarded sessionID = %q, want sess-a", stub.sessionID)
	}
	if index.InputCount != 6 || len(index.Items) != 2 || index.Items[1].Summary != "latest" {
		t.Fatalf("relayed index = %+v", index)
	}
	if index.Window.WindowSize != 4 || !index.Window.HasMore {
		t.Fatalf("relayed window = %+v", index.Window)
	}
}

// TestBridgeSessionInputIndexUnavailableIsExplicit 未装配该面时返回可展示错误
// （不静默成空索引：右侧轨道宁可报"不可用"，也不假装没有用户输入）。
func TestBridgeSessionInputIndexUnavailableIsExplicit(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridge(newFakeApplication(), Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	index, err := bridge.SessionInputIndex("sess-a")
	if err == nil {
		t.Fatal("SessionInputIndex must fail when the optional surface is not assembled")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want a displayable 'not supported' message", err)
	}
	if index.InputCount != 0 || len(index.Items) != 0 {
		t.Fatalf("unavailable index must be empty, got %+v", index)
	}
}

// TestBridgeSessionInputIndexSurfacesApplicationError application 层错误
// 原样上抛（Bridge 不吞错、不改写）。
func TestBridgeSessionInputIndexSurfacesApplicationError(t *testing.T) {
	t.Parallel()
	cause := errors.New("load session record \"sess-a\": disk on fire")
	stub := &stubInputIndexApplication{fakeApplication: newFakeApplication(), err: cause}
	bridge, err := NewBridge(stub, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if _, err := bridge.SessionInputIndex("sess-a"); !errors.Is(err, cause) {
		t.Fatalf("error = %v, want the original application error", err)
	}
}
