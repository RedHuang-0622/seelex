package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/workspace"
)

func TestEngineMessageRoundTripPreservesResumeContext(t *testing.T) {
	t.Parallel()
	empty := ""
	toolResult := "done"
	original := []seelebridge.Message{
		{
			Role: "assistant", ReasoningContent: "reasoning", Content: &empty,
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "read", Arguments: `{"path":"README.md"}`},
			}},
		},
		{Role: "tool", Content: &toolResult, ToolCallID: "call-1", Name: "read"},
	}

	restored := restoreMessages(adaptMessages(original))
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored history differs\n got: %#v\nwant: %#v", restored, original)
	}
}

func TestWorkspacePortUsesRootBasenameAndUniqueIDs(t *testing.T) {
	parentA, parentB := t.TempDir(), t.TempDir()
	rootA, rootB := filepath.Join(parentA, "shared-name"), filepath.Join(parentB, "shared-name")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	port := workspacePort{repo: workspace.NewRepo()}
	first, err := port.Create("custom label", rootA, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := port.Create("custom label", rootB, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "shared-name" || second.Name != "shared-name" {
		t.Fatalf("workspace names = %q, %q; want root basename", first.Name, second.Name)
	}
	if first.ID == second.ID {
		t.Fatalf("duplicate display names must not collide on ID %q", first.ID)
	}
	got, err := port.Get(first.ID)
	if err != nil || got.ID != first.ID || got.Name != "shared-name" {
		t.Fatalf("workspace lookup changed identity: got=%#v err=%v", got, err)
	}
}
