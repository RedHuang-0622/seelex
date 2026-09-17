package input_router

import (
	"context"
	"strings"
	"testing"
)

type recordingInputRoute struct {
	matches     bool
	dispatches  *int
	dispatchErr error
}

func (route recordingInputRoute) Matches(string) bool { return route.matches }
func (route recordingInputRoute) Dispatch(context.Context, string) error {
	*route.dispatches++
	return route.dispatchErr
}

func TestInputRouterDispatchesFirstMatchingStrategy(t *testing.T) {
	firstCalls := 0
	secondCalls := 0
	router := Router{routes: []Route{
		recordingInputRoute{matches: true, dispatches: &firstCalls},
		recordingInputRoute{matches: true, dispatches: &secondCalls},
	}}

	if err := router.Dispatch(context.Background(), "input"); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("route calls = (%d, %d), want (1, 0)", firstCalls, secondCalls)
	}
}

// 前缀（sigil）契约：`/` 命令、`#` 切换插件、`$` 召回 Skill、`@` 手动召唤团队，
// 其余落到对话。回执记录每个处理闭包收到的载荷，因此"哪个字符归哪条路由"与
// "载荷怎么规整"同时被钉住（权威说明见 docs/gui/modules/shell-and-interactions.md）。
func TestInputRouterSigilOwnership(t *testing.T) {
	seen := map[string]string{}
	router := NewRouter(RouteHandlers{
		Command: func(_ context.Context, input string) error {
			seen["command"] = input
			return nil
		},
		Plugin: func(_ context.Context, name string) error {
			seen["plugin"] = name
			return nil
		},
		Skill: func(_ context.Context, name string, args []string, input string) error {
			seen["skill"] = name + "|" + strings.Join(args, ",") + "|" + input
			return nil
		},
		Team: func(_ context.Context, name string) error {
			seen["team"] = name
			return nil
		},
		Conversation: func(_ context.Context, input string) error {
			seen["conversation"] = input
			return nil
		},
	})

	for _, testCase := range []struct{ input, route, payload string }{
		{"/help", "command", "/help"},
		{"#code", "plugin", "code"},
		{"$review strict", "skill", "review|strict|$review strict"},
		{"@goal-a2a", "team", "goal-a2a"},
		{"@", "team", ""}, // 空名也下发：召唤面据自述可用团队
		// `@<团队> <附言>`：路由**不**按空格切分（团队名可以含空格），整段原样
		// 交给召唤面；"名字 + 附言"的切分在 application/core/input_team.go 完成。
		{"@goal-a2a 这次启动团队主要是看看整个team的工作是否打通。", "team", "goal-a2a 这次启动团队主要是看看整个team的工作是否打通。"},
		{"看看这个 bug", "conversation", "看看这个 bug"},
	} {
		clear(seen)
		if err := router.Dispatch(context.Background(), testCase.input); err != nil {
			t.Fatalf("%q dispatch: %v", testCase.input, err)
		}
		if len(seen) != 1 {
			t.Fatalf("%q 应只命中一条路由，实际 %v", testCase.input, seen)
		}
		if got := seen[testCase.route]; got != testCase.payload {
			t.Fatalf("%q 落到 %q 路由的载荷 = %q, want %q", testCase.input, testCase.route, got, testCase.payload)
		}
	}
}
