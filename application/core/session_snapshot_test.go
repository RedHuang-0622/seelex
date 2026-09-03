package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSessionSnapshotTransportShape（G3）：SessionSnapshot 是传输完备的会话
// 粒度制品——JSON 不含进程级目录/工作区/能力清单字段，capabilities 显式声明
// session_snapshot；ProcessSnapshot 反之不含会话字段。协议版本保持原值。
func TestSessionSnapshotTransportShape(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionID := service.Snapshot().Session.ID
	snapshot, err := service.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(active): %v", err)
	}
	if !snapshot.Capabilities.SessionSnapshot || !snapshot.Capabilities.SessionResume {
		t.Fatalf("session snapshot capabilities = %+v", snapshot.Capabilities)
	}
	if snapshot.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version changed: %d", snapshot.ProtocolVersion)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	for _, forbidden := range []string{`"sessions"`, `"workspaces"`, `"session_workspaces"`, `"model"`, `"provider"`, `"accounts"`, `"visible_tools"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("SessionSnapshot must not carry process field %s: %s", forbidden, raw)
		}
	}
	if !strings.Contains(raw, `"session_snapshot":true`) {
		t.Fatalf("SessionSnapshot must declare capabilities.session_snapshot: %s", raw)
	}
}

// TestProcessSnapshotTransportShape（G3）：ProcessSnapshot 承载进程级目录与
// 能力清单，不含会话对话/聊天运行态。
func TestProcessSnapshotTransportShape(t *testing.T) {
	process := ProcessSnapshot{
		ProtocolVersion: ProtocolVersion,
		Revision:        1,
		Sessions:        []SessionInfo{{ID: "sess-a", Status: SessionStatusIdle}},
		Capabilities:    Capabilities{SessionResume: true},
		Runtime: ProcessRuntime{
			Model:    "test-model",
			Provider: "test-provider",
		},
	}
	encoded, err := json.Marshal(process)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	for _, forbidden := range []string{`"conversation"`, `"chat"`, `"message"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("ProcessSnapshot must not carry session field %s: %s", forbidden, raw)
		}
	}
	if !strings.Contains(raw, `"model"`) || !strings.Contains(raw, `"sessions"`) {
		t.Fatalf("ProcessSnapshot must carry process originals: %s", raw)
	}
}
