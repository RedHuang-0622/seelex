package session_test

import (
	"encoding/json"
	"errors"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/store"
)

func TestForkLineageReadsOnlyNarrowPayloadProjection(t *testing.T) {
	application := applicationsession.NewForkLineage(applicationsession.ForkLineageDeps{
		LoadPayload: func(session_runtime.Location, string) ([]byte, error) {
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
	got := application.Load(session_runtime.Location{}, "child")
	if got == nil || got.ParentSessionID != "parent" || got.ForkPoint.EventSeq != 7 {
		t.Fatalf("fork lineage = %+v", got)
	}
}

func TestForkLineageReturnsNilWhenPayloadUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		err     error
	}{
		{name: "error", err: errors.New("read failed")},
		{name: "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lineage := applicationsession.NewForkLineage(applicationsession.ForkLineageDeps{
				LoadPayload: func(session_runtime.Location, string) ([]byte, error) {
					return test.payload, test.err
				},
			})
			if got := lineage.Load(session_runtime.Location{}, "session-a"); got != nil {
				t.Fatalf("Load = %+v, want nil", got)
			}
		})
	}
}

func TestForkLineageRejectsInvalidRecords(t *testing.T) {
	invalidContext := []byte(`{"forked_from":{`)
	missingParent := mustLineagePayload(t, store.SessionContextRecord{
		ForkedFrom: json.RawMessage(`{"event_seq":3}`),
	})
	invalidRef := []byte(`{"forked_from":[]}`)
	for name, payload := range map[string][]byte{
		"invalid-context": invalidContext,
		"missing-parent":  missingParent,
		"invalid-ref":     invalidRef,
	} {
		t.Run(name, func(t *testing.T) {
			lineage := applicationsession.NewForkLineage(applicationsession.ForkLineageDeps{
				LoadPayload: func(session_runtime.Location, string) ([]byte, error) {
					return payload, nil
				},
			})
			if got := lineage.Load(session_runtime.Location{}, "session-a"); got != nil {
				t.Fatalf("Load = %+v, want nil", got)
			}
		})
	}
}

func TestForkLineageDecodesValidReference(t *testing.T) {
	want := model.SessionForkRef{
		ParentSessionID: "parent",
		ForkPoint:       model.ForkPoint{EventSeq: 3},
		SourceEventSeq:  4,
		SourceMessageID: "m4",
	}
	payload := mustLineagePayload(t, store.SessionContextRecord{ForkedFrom: mustJSON(t, want)})
	var gotSessionID string
	lineage := applicationsession.NewForkLineage(applicationsession.ForkLineageDeps{
		LoadPayload: func(_ session_runtime.Location, sessionID string) ([]byte, error) {
			gotSessionID = sessionID
			return payload, nil
		},
	})

	got := lineage.Load(session_runtime.Location{WorkspaceID: "workspace-a"}, "child")
	if gotSessionID != "child" {
		t.Fatalf("session ID = %q", gotSessionID)
	}
	if got == nil || *got != want {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
}

func mustLineagePayload(t *testing.T, record store.SessionContextRecord) []byte {
	t.Helper()
	return mustJSON(t, record)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
