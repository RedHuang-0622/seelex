package session

import (
	"testing"

	sessionruntime "github.com/RedHuang-0622/seelex/application/session/runtime"
)

func TestForkLineageReadsOnlyNarrowPayloadProjection(t *testing.T) {
	application := NewForkLineage(ForkLineageDeps{
		LoadPayload: func(sessionruntime.Location, string) ([]byte, error) {
			return []byte(`{
				"goal_stack": [{"ignored": true}],
				"forked_from": {
					"parent_session_id": "parent",
					"fork_point": {"event_seq": 7}
				},
				"unrelated": {"ignored": true}
			}`), nil
		},
	})
	got := application.Load(sessionruntime.Location{}, "child")
	if got == nil || got.ParentSessionID != "parent" || got.ForkPoint.EventSeq != 7 {
		t.Fatalf("fork lineage = %+v", got)
	}
}

func TestForkLineageRejectsCorruptForkedFrom(t *testing.T) {
	application := NewForkLineage(ForkLineageDeps{
		LoadPayload: func(sessionruntime.Location, string) ([]byte, error) {
			return []byte(`{"forked_from":{"parent_session_id":`), nil
		},
	})
	if got := application.Load(sessionruntime.Location{}, "child"); got != nil {
		t.Fatalf("损坏血缘必须降级为 nil，得到 %+v", got)
	}
}
