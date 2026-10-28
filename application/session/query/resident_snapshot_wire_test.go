package query

import (
	"encoding/json"
	"testing"
)

func TestBuildResidentSnapshotEmptyConversationMarshalsAsArray(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(BuildResidentSnapshot(ResidentSnapshotInput{ProtocolVersion: 1}))
	if err != nil {
		t.Fatalf("marshal resident snapshot: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode resident snapshot: %v", err)
	}
	if got := string(wire["conversation"]); got != "[]" {
		t.Fatalf("empty conversation wire = %s, want [] (GUI protocol requires an array)", got)
	}
}
