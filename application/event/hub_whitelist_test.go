package event

import (
	"strings"
	"testing"
)

// TestKindWhitelistClassification（G2/M2）：白名单之外的 kind 都是会话类；
// 会话类必填 sid，进程类必空。
func TestKindWhitelistClassification(t *testing.T) {
	for _, sessionKind := range []EventKind{
		EventSnapshotChanged, EventMessageAdded, EventMessageDelta,
		EventToolStarted, EventToolCompleted, EventSubagentChanged,
		EventSubagentToolStarted, EventSubagentToolCompleted,
		EventRuntimeChanged, EventChatChanged, EventWorkTableChanged,
		EventTaskChanged, EventInteractionOpened, EventInteractionClosed,
		EventError,
	} {
		if !KindIsSessionClass(sessionKind) {
			t.Fatalf("kind %q must be session-class", sessionKind)
		}
		if KindIsProcessClass(sessionKind) {
			t.Fatalf("kind %q must not be process-class", sessionKind)
		}
	}
	for _, processKind := range []EventKind{EventResyncRequired, EventExitRequested} {
		if !KindIsProcessClass(processKind) {
			t.Fatalf("kind %q must be process-class", processKind)
		}
		if KindIsSessionClass(processKind) {
			t.Fatalf("kind %q must not be session-class", processKind)
		}
	}
	// 白名单外的新 kind 保守归会话类（宁缺勿滥：发布方被迫显式带 sid）。
	if !KindIsSessionClass("future.kind") {
		t.Fatal("unknown kind must default to session-class")
	}
}

// TestValidateSessionRouting（G2/M2）：会话类空 sid 与进程类带 sid 均为
// 违例；合法组合通过。
func TestValidateSessionRouting(t *testing.T) {
	if err := ValidateSessionRouting(EventMessageAdded, "sess-a"); err != nil {
		t.Fatalf("session kind with sid must pass: %v", err)
	}
	if err := ValidateSessionRouting(EventResyncRequired, ""); err != nil {
		t.Fatalf("process kind without sid must pass: %v", err)
	}
	if err := ValidateSessionRouting(EventMessageAdded, ""); err == nil || !strings.Contains(err.Error(), "requires a session ID") {
		t.Fatalf("session kind without sid must fail, got %v", err)
	}
	if err := ValidateSessionRouting(EventExitRequested, "sess-a"); err == nil || !strings.Contains(err.Error(), "must not carry a session ID") {
		t.Fatalf("process kind with sid must fail, got %v", err)
	}
}
