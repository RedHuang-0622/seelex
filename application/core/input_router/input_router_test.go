package input_router

import (
	"context"
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
