package task_context

import (
	"reflect"
	"testing"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

func TestContextL1RebuildsThenReusesOnlyNewTruth(t *testing.T) {
	coordinator := newContextL1TestCoordinator()
	key := prefixprobe.Key{SystemDigest: "system-v1", RetainedFrom: 0}
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "first"},
		{Seq: 2, Role: "assistant", Content: "answer"},
	}
	first := ContextL1Snapshot{
		Key:          key,
		System:       "system",
		Messages:     TranscriptEventMessages(events),
		RetainedFrom: 0,
		ThroughSeq:   2,
		EventCount:   len(events),
		Tokens:       100,
		TruthTokens:  40,
	}

	stats := coordinator.RecordContextL1("session-a", first)
	if stats.Rebuilt != 1 || stats.Hits != 0 || stats.Last != "miss" {
		t.Fatalf("首次登记应为 rebuilt/miss，实际 %+v", stats)
	}

	events = append(events, model.TranscriptEvent{Seq: 3, Role: "user", Content: "second"})
	resumed, ok := coordinator.ResumeContextL1("session-a", key, events, 0, 63)
	if !ok {
		t.Fatal("键与水位连续时应命中 L1")
	}
	if resumed.ThroughSeq != 3 || resumed.EventCount != 3 {
		t.Fatalf("L1 水位未推进：%+v", resumed)
	}
	gotContents := make([]string, 0, len(resumed.Messages))
	for _, message := range resumed.Messages {
		gotContents = append(gotContents, message.Content)
	}
	if want := []string{"first", "answer", "second"}; !reflect.DeepEqual(gotContents, want) {
		t.Fatalf("L1 增量消息 = %v，want %v", gotContents, want)
	}
	stats = coordinator.ContextL1StatsFor("session-a")
	if stats.Hits != 1 || stats.Last != "hit" || len(stats.LastChanged) != 0 {
		t.Fatalf("命中统计错误：%+v", stats)
	}
}

func TestContextL1ExplicitlyInvalidatesOnKeyChangeAndSeqGap(t *testing.T) {
	coordinator := newContextL1TestCoordinator()
	key := prefixprobe.Key{SystemDigest: "system-v1", RetainedFrom: 0}
	events := []model.TranscriptEvent{{Seq: 1, Role: "user", Content: "first"}}
	coordinator.RecordContextL1("session-a", ContextL1Snapshot{
		Key:          key,
		System:       "system",
		Messages:     TranscriptEventMessages(events),
		RetainedFrom: 0,
		ThroughSeq:   1,
		EventCount:   1,
		Tokens:       50,
		TruthTokens:  20,
	})

	changed := key
	changed.CompactSeq = 9
	changed.MemoryEpoch = 9
	if _, ok := coordinator.ResumeContextL1("session-a", changed, events, 0, 63); ok {
		t.Fatal("压缩纪元变化必须显式失效，不得复用旧 L1")
	}
	if _, ok := coordinator.ResumeContextL1("session-a", key,
		[]model.TranscriptEvent{{Seq: 5, Role: "user", Content: "gap"}}, 0, 63); ok {
		t.Fatal("Seq 水位不连续必须重建，不得把缺口当追加")
	}
}

func TestContextL1ColdRebuildUsesPersistedWireMaterial(t *testing.T) {
	events := []model.TranscriptEvent{{
		Seq: 1, Role: "user", Content: "raw input", ProviderContent: "raw input + environment",
	}}
	messages := TranscriptEventMessages(events)
	if len(messages) != 1 || messages[0].Content != "raw input + environment" {
		t.Fatalf("cold rebuild bypassed persisted wire material: %+v", messages)
	}
}

func TestContextCacheViewReportsL1ReuseVolume(t *testing.T) {
	coordinator := newContextL1TestCoordinator()
	key := prefixprobe.Key{SystemDigest: "system-v1", CompactSeq: 2, ToolsEpoch: 3, RetainedFrom: 4}
	events := []model.TranscriptEvent{{Seq: 1, Role: "user", Content: "first"}}
	coordinator.RecordContextL1("session-a", ContextL1Snapshot{
		Key: key, System: "system", Messages: TranscriptEventMessages(events),
		RetainedFrom: 4, ThroughSeq: 1, EventCount: 1, Tokens: 50, TruthTokens: 20,
	})
	events = append(events, model.TranscriptEvent{Seq: 2, Role: "assistant", Content: "answer"})
	if _, ok := coordinator.ResumeContextL1("session-a", key, events, 4, 0); !ok {
		t.Fatal("expected L1 hit")
	}
	view := coordinator.ContextCacheViewFor("session-a")
	if view == nil || !view.Available || view.Hits != 1 || view.ReusedEvents != 1 ||
		view.ReusedChars <= 0 || view.LastReusedChars != view.ReusedChars ||
		view.PrefixKey.CompactSeq != 2 || view.PrefixKey.RetainedFrom != 4 {
		t.Fatalf("context cache view = %+v", view)
	}
}

func newContextL1TestCoordinator() *Coordinator {
	return NewCoordinator(Deps{
		CurrentSessionID: func() string { return "session-a" },
		IsInternalContent: func(string) bool {
			return false
		},
	})
}
