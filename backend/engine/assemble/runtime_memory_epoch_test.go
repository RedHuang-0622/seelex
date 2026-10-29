package assemble

import (
	"context"
	"testing"

	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
)

func TestMemoryEpochKeepsBytesIndependentOfQuery(t *testing.T) {
	runtime := &Runtime{}
	runtime.SetMemoryEpoch("session-a", "frozen-memory")
	ctx := taskcontext.WithSessionID(context.Background(), "session-a")

	first := runtime.relatedMemoryBlocksForContext(ctx, "query one")
	second := runtime.relatedMemoryBlocksForContext(ctx, "query two")
	if len(first) != 1 || len(second) != 1 || len(first[0].Messages) != 1 || len(second[0].Messages) != 1 {
		t.Fatalf("frozen memory block shape changed: first=%+v second=%+v", first, second)
	}
	if first[0].Messages[0].Content == nil || second[0].Messages[0].Content == nil ||
		*first[0].Messages[0].Content != "frozen-memory" || *second[0].Messages[0].Content != "frozen-memory" {
		t.Fatalf("memory selection reran within one epoch: first=%+v second=%+v", first, second)
	}
	runtime.SetMemoryEpoch("session-a", "next-epoch")
	next := runtime.relatedMemoryBlocksForContext(ctx, "query three")
	if got := *next[0].Messages[0].Content; got != "next-epoch" {
		t.Fatalf("epoch roll did not replace frozen bytes: %q", got)
	}
}

func TestFrozenToolCatalogIsReadBySessionContext(t *testing.T) {
	runtime := &Runtime{}
	runtime.SetToolCatalogForSession("subagent-1", 1, []Tool{
		{Name: "read", Description: "read files"},
		{Name: "bash", Description: "shell"},
	})
	ctx := taskcontext.WithSessionID(context.Background(), "subagent-1")
	got := runtime.VisibleTools(ctx)
	if len(got) != 2 || got[0].Name != "read" || got[1].Name != "bash" {
		t.Fatalf("frozen tool catalog not returned: %+v", got)
	}
}
