//go:build !extended

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestL1HotPathDoesNotReconvertGrowingHistory(t *testing.T) {
	for _, rounds := range []int{10, 50, 200} {
		rounds := rounds
		t.Run(fmt.Sprintf("rounds_%d", rounds), func(t *testing.T) {
			harness := newPrefixProbeHarness(t, 1_000_000)
			harness.startStream()
			for index := 0; index < rounds; index++ {
				harness.service.ViewMu.Lock()
				harness.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
					TaskID: prefixProbeRequestID, Role: "user", Content: fmt.Sprintf("question-%d", index),
				})
				harness.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
					TaskID: prefixProbeRequestID, Role: "assistant", Content: fmt.Sprintf("answer-%d", index),
				})
				harness.service.ViewMu.Unlock()
			}

			harness.beginTurn("seed L1")
			before, ok := harness.service.components.context.PrefixL1StatsFor(harness.session)
			if !ok || before.Rebuilt == 0 {
				t.Fatalf("seed assembly did not rebuild: %+v", before)
			}
			start := time.Now()
			for index := 0; index < 20; index++ {
				if _, err := harness.service.components.context.PrepareExecutionContextFor(
					harness.session, prefixProbeRequestID, "seed L1",
				); err != nil {
					t.Fatalf("hot assembly %d: %v", index, err)
				}
			}
			elapsed := time.Since(start)
			after, _ := harness.service.components.context.PrefixL1StatsFor(harness.session)
			if after.Hits-before.Hits != 20 {
				t.Fatalf("hot path did not hit every time: before=%+v after=%+v", before, after)
			}
			if after.ReusedEvents != before.ReusedEvents {
				t.Fatalf("no new truth events should be appended, reused delta moved %d→%d",
					before.ReusedEvents, after.ReusedEvents)
			}
			if after.RebuiltMessages != before.RebuiltMessages {
				t.Fatalf("hot path reconverted history: rebuilt messages %d→%d",
					before.RebuiltMessages, after.RebuiltMessages)
			}
			t.Logf("rounds=%d hot_calls=20 elapsed=%s rebuilt_messages=%d hits=%d",
				rounds, elapsed, after.RebuiltMessages, after.Hits)
		})
	}
}
