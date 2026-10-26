package dto

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestGoalWireShapeStable(t *testing.T) {
	record := GoalRecord{
		ID: "g-1", Title: "goal", Statement: "statement",
		Acceptance: []string{"done"}, OutOfScope: []string{"other"},
		Budget:     Budget{MaxLoops: 3, MaxTokens: 100},
		Status:     GoalActive,
		Progress:   []Progress{{At: 1, Kind: ProgressFinding, Content: "found"}},
		Directives: []string{"keep"},
		CreatedAt:  2, UpdatedAt: 3, FinishedAt: 4,
		Meta: map[string]string{"k": "v"},
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"id", "title", "statement", "acceptance", "out_of_scope", "budget",
		"status", "progress", "tl_directives", "created_at", "updated_at",
		"finished_at", "meta",
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Fatalf("GoalRecord wire 缺字段 %q: %s", key, raw)
		}
	}
	if string(fields["status"]) != `"active"` {
		t.Fatalf("status wire = %s, want active", fields["status"])
	}
}

func TestGoalRequestWireShapeStable(t *testing.T) {
	title := "new"
	update, err := json.Marshal(UpdateGoalRequest{
		Title: &title, ProgressKind: ProgressRisk, ProgressContent: "risk",
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(update, &fields); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedKeys(fields), []string{"progress_content", "progress_kind", "title"}) {
		t.Fatalf("UpdateGoalRequest wire fields = %v", sortedKeys(fields))
	}
}

func sortedKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
