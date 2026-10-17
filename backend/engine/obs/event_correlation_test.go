package obs

// event_correlation_test.go 是 `event_correlation.go` 的域级用例（P21-9：从
// `backend/engine/assemble/events_test.go` 随实现搬来，断言逐字不变），外加一条
// **跨实现一致性**用例：同一份输入分别走 `store` 的两个入口，必须给出同一答案。

import (
	"context"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/store"
)

// TestCorrelateMainSessionIDFillsMissingSession 验证短期事件桥：持久化前为
// 缺失 session_id 的事实轨事件补主会话关联；已有关联的事件不被改写。
func TestCorrelateMainSessionIDFillsMissingSession(t *testing.T) {
	var persisted []frameworkevent.Event
	hook := CorrelateMainSessionID(
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
	if len(persisted) != 1 || store.SessionIDFromEvent(persisted[0]) != "main-session" {
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
	hook := CorrelateMainSessionID(
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

// TestCorrelateMainSessionIDReturnsDownstreamUnchanged 验证"没有可补的东西就不包装"：
// 两格任一为空时原样返回下游钩子（不吞掉、不新造一层壳）。
func TestCorrelateMainSessionIDReturnsDownstreamUnchanged(t *testing.T) {
	if got := CorrelateMainSessionID(func() string { return "s" }, nil); got != nil {
		t.Fatalf("next 为 nil 时应原样返回 nil，得到非 nil")
	}
	// mainSessionID 为 nil = 没有可补的会话读数：返回的钩子必须**原样透传**事件
	//（不补 Location、不吞调用），否则"主会话未就绪"会被悄悄改写成"补了空关联"。
	var forwarded []frameworkevent.Event
	downstream := func(_ context.Context, event frameworkevent.Event) error {
		forwarded = append(forwarded, event)
		return nil
	}
	hook := CorrelateMainSessionID(nil, downstream)
	if hook == nil {
		t.Fatal("mainSessionID 为 nil 时不该丢掉下游钩子")
	}
	event := frameworkevent.Event{ID: "evt-4"}
	if err := hook(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(forwarded) != 1 || forwarded[0].ID != "evt-4" || len(forwarded[0].Locations) != 0 {
		t.Fatalf("forwarded = %#v, want 原样透传", forwarded)
	}
}

// TestSessionIDProjectionIsSingleImplementation 是**跨实现一致性**用例：同一份输入
// （带定位 / 无定位 / 有定位但 session_id 空）分别走 `store.SessionIDFromLocations` 与
// `store.SessionIDFromEvent`，以及经 `UnifiedEventTopic` 的话题映射，三处必须给出同一归属。
// 这三处曾经各有一份逐字副本（assemble 的 eventSessionID、tool/plan 的 sessionIDFromLocations、
// obs 话题映射里的内联循环），本用例是"只剩一份判据"的钉子。
func TestSessionIDProjectionIsSingleImplementation(t *testing.T) {
	cases := []struct {
		name      string
		locations []frameworkevent.Location
		want      string
	}{
		{name: "无定位", locations: nil, want: ""},
		{
			name: "会话级定位",
			locations: []frameworkevent.Location{{
				Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-1"},
			}},
			want: "sess-1",
		},
		{
			name: "进程级定位（session_id 空）",
			locations: []frameworkevent.Location{{
				Kind: "agent.runtime", IDs: map[string]string{"session_id": ""},
			}},
			want: "",
		},
		{
			name: "非 agent.runtime 定位",
			locations: []frameworkevent.Location{{
				Kind: "plugin.runtime", IDs: map[string]string{"session_id": "sess-2"},
			}},
			want: "",
		},
		{
			name: "多条定位取第一条命中的会话",
			locations: []frameworkevent.Location{
				{Kind: "plugin.runtime", IDs: map[string]string{"session_id": "sess-x"}},
				{Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-3"}},
				{Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-4"}},
			},
			want: "sess-3",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			event := frameworkevent.Event{
				Type:      frameworkevent.TypeLifecycle,
				Locations: testCase.locations,
			}
			fromLocations := store.SessionIDFromLocations(testCase.locations)
			fromEvent := store.SessionIDFromEvent(event)
			_, topicSessionID, scoped := UnifiedEventTopic(event)
			if fromLocations != testCase.want {
				t.Fatalf("SessionIDFromLocations = %q, want %q", fromLocations, testCase.want)
			}
			if fromEvent != fromLocations {
				t.Fatalf("SessionIDFromEvent = %q, SessionIDFromLocations = %q（同一判据必须同解）", fromEvent, fromLocations)
			}
			if topicSessionID != fromLocations || scoped != (fromLocations != "") {
				t.Fatalf("UnifiedEventTopic = (%q, %v)，want (%q, %v)", topicSessionID, scoped, fromLocations, fromLocations != "")
			}
		})
	}
}
