package reference

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestReadCompressedTurnHandlerLoadsScopedArchiveAndFilters(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: compressedString("alpha")},
		{Role: "assistant", Content: compressedString("beta")},
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	var loadedWorkspaceID, loadedSessionID, loadedRef string
	application := New(Deps{
		ReferencePageSize:    func() int { return 64 },
		MaxReferencePageSize: func() int { return 32 },
		CompressedTurnView: func() (string, string) {
			return "session-1", "workspace-1"
		},
		LoadCompressedTurn: func(workspaceID, sessionID, ref string) (string, error) {
			loadedWorkspaceID, loadedSessionID, loadedRef = workspaceID, sessionID, ref
			return string(encoded), nil
		},
		CompressedTurnPrefix: "compressed:",
	})

	page, err := application.ReadCompressedTurnHandler(`{"segment_id":"segment-1","contains":"beta"}`)
	if err != nil {
		t.Fatal(err)
	}
	if page != "[assistant] beta" {
		t.Fatalf("page = %q, want filtered assistant line", page)
	}
	if loadedWorkspaceID != "workspace-1" || loadedSessionID != "session-1" {
		t.Fatalf("loaded scope = (%q, %q), want (workspace-1, session-1)",
			loadedWorkspaceID, loadedSessionID)
	}
	if loadedRef != "compressed:segment-1" {
		t.Fatalf("loaded ref = %q, want compressed:segment-1", loadedRef)
	}
}

func TestReadCompressedTurnHandlerRejectsInvalidRequestBeforeLoad(t *testing.T) {
	application := New(Deps{
		ReferencePageSize:    func() int { return 8 },
		MaxReferencePageSize: func() int { return 32 },
		LoadCompressedTurn: func(_, _, _ string) (string, error) {
			t.Fatal("storage must not be read for an invalid request")
			return "", nil
		},
	})
	if _, err := application.ReadCompressedTurnHandler(`{"segment_id":"  "}`); err == nil ||
		!strings.Contains(err.Error(), "segment_id is required") {
		t.Fatalf("error = %v, want missing segment_id rejection", err)
	}
	if _, err := application.ReadCompressedTurnHandler(`{"segment_id":"segment-1","offset":-1}`); err == nil ||
		!strings.Contains(err.Error(), "offset must be non-negative") {
		t.Fatalf("error = %v, want negative offset rejection", err)
	}
}

func TestReadCompressedTurnHandlerClampsLimitAndMarksTruncation(t *testing.T) {
	messages := []types.Message{{Role: "user", Content: compressedString("abcdefghij")}}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	application := New(Deps{
		ReferencePageSize:    func() int { return 100 },
		MaxReferencePageSize: func() int { return 4 },
		CompressedTurnView:   func() (string, string) { return "session", "workspace" },
		LoadCompressedTurn: func(_, _, _ string) (string, error) {
			return string(encoded), nil
		},
		CompressedTurnPrefix: "compressed:",
	})
	page, err := application.ReadCompressedTurnHandler(`{"segment_id":"segment-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(page, "[use") || !strings.Contains(page, "truncated") {
		t.Fatalf("page = %q, want clamped truncated page", page)
	}
}

func TestRenderCompressedTurnsPreservesRoleTags(t *testing.T) {
	rendered := renderCompressedTurns([]types.Message{
		{Role: "user", Content: compressedString("question")},
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{Function: types.ToolCallFunction{Name: "read_file"}},
			{Function: types.ToolCallFunction{Name: "write_file"}},
		}},
		{Role: "tool", Name: "read_file", Content: compressedString("result")},
		{Role: "system", Content: compressedString("note")},
	})
	want := "[user] question\n[assistant tools] read_file, write_file\n[tool read_file] result\n[system] note"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func compressedString(value string) *string {
	return &value
}
