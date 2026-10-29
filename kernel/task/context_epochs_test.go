package task_context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"testing"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

func TestFoldMemoryUpdatesIsAppendOnlyAndDetectsTampering(t *testing.T) {
	updates := []model.MemoryUpdate{
		{Seq: 1, Epoch: 1, Kind: MemoryUpdateAdd, SegmentID: "a", Bytes: "A", Digest: digestBytes("A")},
		{Seq: 2, Epoch: 1, Kind: MemoryUpdateAdd, SegmentID: "b", Bytes: "B", Digest: digestBytes("B")},
		{Seq: 3, Epoch: 2, Kind: MemoryUpdateRetire, SegmentID: "a", Digest: "retire"},
		{Seq: 4, Epoch: 2, Kind: MemoryUpdateAdd, SegmentID: "c", Bytes: "C", Digest: digestBytes("C")},
	}
	got, err := FoldMemoryUpdates(updates, 1)
	if err != nil || got != "AB" {
		t.Fatalf("epoch1=%q err=%v", got, err)
	}
	got, err = FoldMemoryUpdates(updates, 2)
	if err != nil || got != "BC" {
		t.Fatalf("epoch2=%q err=%v", got, err)
	}
	updates[0].Bytes = "tampered"
	if _, err := FoldMemoryUpdates(updates, 2); err == nil {
		t.Fatal("tampered memory bytes must fail digest validation")
	}
}

func TestContextEpochSeqAllocationIsAtomicAcrossMemoryAndTools(t *testing.T) {
	coordinator := NewCoordinator(Deps{Core: &state.Core{}})
	var wg sync.WaitGroup
	for index := 0; index < 100; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			if index%2 == 0 {
				if _, err := coordinator.AppendContextMemoryFor("session-a", 1, MemoryUpdateAdd,
					fmt.Sprintf("m-%d", index), fmt.Sprintf("value-%d", index), "test"); err != nil {
					t.Errorf("memory append %d: %v", index, err)
				}
				return
			}
			sentinel := RenderToolsSentinel(1, []string{"read"}, []string{"read"}, nil)
			if _, err := coordinator.AppendContextToolsFor("session-a", 1, ToolsUpdateRestate,
				[]string{"read"}, sentinel, "test"); err != nil {
				t.Errorf("tools append %d: %v", index, err)
			}
		}()
	}
	wg.Wait()
	state := coordinator.ContextEpochStateFor("session-a")
	seen := make(map[uint64]bool, len(state.MemoryUpdates)+len(state.ToolsUpdates))
	for _, row := range state.MemoryUpdates {
		if row.Seq == 0 || seen[row.Seq] {
			t.Fatalf("duplicate/zero memory seq: %d", row.Seq)
		}
		seen[row.Seq] = true
	}
	for _, row := range state.ToolsUpdates {
		if row.Seq == 0 || seen[row.Seq] {
			t.Fatalf("duplicate/zero tools seq: %d", row.Seq)
		}
		seen[row.Seq] = true
	}
	if len(seen) != 100 {
		t.Fatalf("shared epoch seq allocator produced %d unique rows, want 100", len(seen))
	}
}

func TestToolCatalogEpochOnlyBumpsOnSchemaChange(t *testing.T) {
	coordinator := NewCoordinator(Deps{Core: &state.Core{}})
	catalog := []model.ToolCatalogEntry{{Name: "read", Description: "read file"}, {Name: "bash", Description: "shell"}}
	changed, epoch := coordinator.ReplaceToolCatalogFor("session-a", catalog)
	if !changed || epoch != 1 {
		t.Fatalf("first catalog freeze changed=%t epoch=%d", changed, epoch)
	}
	changed, epoch = coordinator.ReplaceToolCatalogFor("session-a", []model.ToolCatalogEntry{catalog[1], catalog[0]})
	if changed || epoch != 1 {
		t.Fatalf("same catalog must stay frozen, changed=%t epoch=%d", changed, epoch)
	}
	changed, epoch = coordinator.ReplaceToolCatalogFor("session-a", append(catalog, model.ToolCatalogEntry{Name: "write"}))
	if !changed || epoch != 2 {
		t.Fatalf("catalog change bump failed, changed=%t epoch=%d", changed, epoch)
	}
	if got := coordinator.ContextEpochStateFor("session-a").ToolCatalog; !reflect.DeepEqual(got, []model.ToolCatalogEntry{
		{Name: "bash", Description: "shell"},
		{Name: "read", Description: "read file"},
		{Name: "write"},
	}) {
		t.Fatalf("catalog not canonically sorted: %+v", got)
	}
}

func TestToolSentinelRoundTripRejectsTruncation(t *testing.T) {
	rendered := RenderToolsSentinel(7, []string{"bash", "read"}, []string{"bash"}, []string{"⟦old⟧"})
	epoch, err := ParseToolsSentinel(rendered)
	if err != nil || epoch != 7 {
		t.Fatalf("parse epoch=%d err=%v content=%q", epoch, err, rendered)
	}
	if _, err := ParseToolsSentinel(rendered[:len(rendered)-2]); err == nil {
		t.Fatal("truncated sentinel must fail")
	}
	digest := digestBytes(rendered)
	sum := sha256.Sum256([]byte(rendered))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("sentinel digest helper drifted")
	}
}
