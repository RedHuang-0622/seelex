package reference

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestReadToolResultUsesDefaultLimitAndClamp(t *testing.T) {
	var gotLimit int
	application := New(Deps{
		ReferencePageSize:    func() int { return 16 },
		MaxReferencePageSize: func() int { return 8 },
		CallIDAlias:          func(callID string) (string, bool) { return "", false },
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

func TestReadToolResultResolvesWireRefThroughCallIDAlias(t *testing.T) {
	application := New(Deps{
		ReferencePageSize:    func() int { return 32 },
		MaxReferencePageSize: func() int { return 32 },
		CallIDAlias: func(callID string) (string, bool) {
			if callID == "call-a" {
				return "tr-real", true
			}
			return "", false
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

// 只有 wire 形状（"result:<callID>"）才查别名；归档引用与 node 引用一律原样交给存储。
func TestReadToolResultSkipsAliasForNonWireRef(t *testing.T) {
	consulted := false
	application := New(Deps{
		ReferencePageSize:    func() int { return 32 },
		MaxReferencePageSize: func() int { return 32 },
		CallIDAlias: func(callID string) (string, bool) {
			consulted = true
			return "", false
		},
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			return model.StoredToolResult{
				ToolResultRef: model.ToolResultRef{Ref: ref},
				Content:       "direct",
			}, true, nil
		},
	})

	raw, err := application.ReadToolResultHandler(`{"result_ref":"tr-direct"}`)
	if err != nil {
		t.Fatal(err)
	}
	if consulted {
		t.Fatal("CallIDAlias must not be consulted for an archived ref")
	}
	if !strings.Contains(raw, `"result_ref":"tr-direct"`) {
		t.Fatalf("unexpected page: %s", raw)
	}
}

// "result:" 去掉前缀后是空 callID —— 那不是可解析的 wire 引用，同样不查别名。
func TestReadToolResultSkipsAliasForEmptyCallID(t *testing.T) {
	consulted := false
	var resolved string
	application := New(Deps{
		ReferencePageSize:    func() int { return 32 },
		MaxReferencePageSize: func() int { return 32 },
		CallIDAlias: func(callID string) (string, bool) {
			consulted = true
			return "", false
		},
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			resolved = ref
			return model.StoredToolResult{}, false, nil
		},
	})

	if _, err := application.ReadToolResultHandler(`{"result_ref":"result:"}`); err == nil {
		t.Fatal("empty call ID must surface as an unavailable ref")
	}
	if consulted {
		t.Fatal("CallIDAlias must not be consulted when the call ID is empty")
	}
	if resolved != "result:" {
		t.Fatalf("ResolveResult ref = %q, want the untouched wire ref", resolved)
	}
}

// 别名未命中时 wire 引用原样下传，由存储面报告"不可用"，不在装配层改写形状。
func TestReadToolResultKeepsWireRefOnAliasMiss(t *testing.T) {
	var resolved string
	application := New(Deps{
		ReferencePageSize:    func() int { return 32 },
		MaxReferencePageSize: func() int { return 32 },
		CallIDAlias:          func(callID string) (string, bool) { return "", false },
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			resolved = ref
			return model.StoredToolResult{}, false, nil
		},
	})

	if _, err := application.ReadToolResultHandler(`{"result_ref":"result:call-miss"}`); err == nil {
		t.Fatal("an unknown wire ref must be reported as unavailable")
	}
	if resolved != "result:call-miss" {
		t.Fatalf("ResolveResult ref = %q, want the untouched wire ref", resolved)
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
