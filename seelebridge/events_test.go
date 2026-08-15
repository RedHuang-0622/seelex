package seelebridge

import (
	"context"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// TestCorrelateMainSessionIDFillsMissingSession 验证短期事件桥：持久化前为
// 缺失 session_id 的事实轨事件补主会话关联；已有关联的事件不被改写。
func TestCorrelateMainSessionIDFillsMissingSession(t *testing.T) {
	var persisted []frameworkevent.Event
	hook := correlateMainSessionID(
		func() string { return "main-session" },
		func(_ context.Context, event frameworkevent.Event) error {
			persisted = append(persisted, event)
			return nil
		},
	)

	// 缺失 session_id → 补主会话 agent.runtime Location。
	if err := hook(context.Background(), frameworkevent.Event{ID: "evt-1"}); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || eventSessionID(persisted[0]) != "main-session" {
		t.Fatalf("persisted = %#v, want main-session correlation", persisted)
	}

	// 已有 session_id → 保持原样，不重复追加 Location。
	child := frameworkevent.Event{
		ID: "evt-2",
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": "child-session"},
		}},
	}
	if err := hook(context.Background(), child); err != nil {
		t.Fatal(err)
	}
	if got := persisted[1].Locations; len(got) != 1 || got[0].IDs["session_id"] != "child-session" {
		t.Fatalf("existing session location mutated: %#v", got)
	}
}

// TestCorrelateMainSessionIDSkipsEmptyMainSession 验证主会话未就绪时保持
// 事件原样透传（不注入空 session_id）。
func TestCorrelateMainSessionIDSkipsEmptyMainSession(t *testing.T) {
	var persisted []frameworkevent.Event
	hook := correlateMainSessionID(
		func() string { return "" },
		func(_ context.Context, event frameworkevent.Event) error {
			persisted = append(persisted, event)
			return nil
		},
	)
	if err := hook(context.Background(), frameworkevent.Event{ID: "evt-3"}); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || len(persisted[0].Locations) != 0 {
		t.Fatalf("persisted = %#v, want unchanged event without location", persisted)
	}
}
