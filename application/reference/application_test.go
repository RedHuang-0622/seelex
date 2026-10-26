package reference

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestReadToolResultUsesDefaultLimitAndClamp(t *testing.T) {
	var gotLimit int
	application := New(Deps{
		ReferencePageSize:    func() int { return 16 },
		MaxReferencePageSize: func() int { return 8 },
		ResolveAlias:         func(ref string) string { return ref },
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			return model.StoredToolResult{
				ToolResultRef: model.ToolResultRef{Ref: ref},
				Content:       strings.Repeat("x", 32),
			}, true, nil
		},
	})

	raw, err := application.ReadToolResultHandler(`{"result_ref":"tr-a","limit":32}`)
	if err != nil {
		t.Fatal(err)
	}
	var page model.ToolResultPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	gotLimit = len(page.Content)
	if gotLimit != 8 {
		t.Fatalf("content length = %d, want max page size 8", gotLimit)
	}
}

func TestReadToolResultResolvesAliasBeforeStorage(t *testing.T) {
	application := New(Deps{
		ReferencePageSize:    func() int { return 32 },
		MaxReferencePageSize: func() int { return 32 },
		ResolveAlias: func(ref string) string {
			if ref == "result:call-a" {
				return "tr-real"
			}
			return ref
		},
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			if ref != "tr-real" {
				return model.StoredToolResult{}, false, nil
			}
			return model.StoredToolResult{
				ToolResultRef: model.ToolResultRef{Ref: ref},
				Content:       "alias-target",
			}, true, nil
		},
	})

	raw, err := application.ReadToolResultHandler(`{"result_ref":"result:call-a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"result_ref":"tr-real"`) || !strings.Contains(raw, "alias-target") {
		t.Fatalf("unexpected page: %s", raw)
	}
}

func TestReadPlanFiltersCanonicalNodes(t *testing.T) {
	application := New(Deps{
		ActivePlanID: func() string { return "plan-active" },
		PlanFrame: func(planRef string) (string, model.RuntimeState, bool) {
			return `{"id":"plan-active","nodes":{"a":{"id":"a"},"b":{"id":"b"}}}`,
				model.RuntimeState{Plan: &model.PlanState{Name: "plan-active"}}, true
		},
	})

	raw, err := application.ReadPlanHandler(`{"node_ids":["b"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"plan_ref":"plan-active"`) {
		t.Fatalf("missing active plan ref: %s", raw)
	}
	if strings.Contains(raw, `"a":{"id":"a"}`) || !strings.Contains(raw, `"b":{"id":"b"}`) {
		t.Fatalf("unexpected plan node selection: %s", raw)
	}
}
