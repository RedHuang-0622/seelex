package view_state_test

import (
	"strings"
	"testing"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
)

func TestToolResultBudgetUsesPreviewDecisionAsSingleJudgement(t *testing.T) {
	tests := []struct {
		name      string
		preview   string
		truncated bool
		wantRef   string
		wantSize  int
	}{
		{name: "truncate", preview: "preview", truncated: true, wantRef: "tr-ref", wantSize: 4},
		{name: "pass-through", preview: "tiny", truncated: false, wantRef: "", wantSize: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var archived string
			budget := view_state.NewToolResultBudget(view_state.ToolResultBudgetDeps{
				Limit: func() int { return 8 },
				Preview: func(content string, limit int) (string, bool) {
					if limit != 8 {
						t.Fatalf("preview limit = %d", limit)
					}
					return test.preview, test.truncated
				},
				Archive: func(name, content string) string {
					archived = name + ":" + content
					return "tr-ref"
				},
			})

			visible, ref, truncated, total := budget.Bound("bash", "tiny")
			if visible != test.preview || ref != test.wantRef || truncated != test.truncated || total != test.wantSize {
				t.Fatalf("Bound = %q,%q,%v,%d", visible, ref, truncated, total)
			}
			if test.truncated {
				if archived != "bash:tiny" {
					t.Fatalf("archived = %q", archived)
				}
			} else if archived != "" {
				t.Fatalf("unexpected archive = %q", archived)
			}
		})
	}
}

func TestToolResultBudgetCountsRunes(t *testing.T) {
	budget := view_state.NewToolResultBudget(view_state.ToolResultBudgetDeps{
		Limit:   func() int { return 100 },
		Preview: func(content string, _ int) (string, bool) { return content, false },
	})
	content := strings.Repeat("汉", 7)

	_, _, _, total := budget.Bound("read_file", content)
	if total != 7 {
		t.Fatalf("total = %d, want 7 runes", total)
	}
}
