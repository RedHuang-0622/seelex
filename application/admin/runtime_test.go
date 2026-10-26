package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

type runtimeProjectionFake struct {
	calls []string
}

func (fake *runtimeProjectionFake) CollectProjection(context.Context) view_state.RuntimeStateProjection {
	fake.calls = append(fake.calls, "collect")
	return view_state.RuntimeStateProjection{}
}
func (fake *runtimeProjectionFake) LockView() { fake.calls = append(fake.calls, "lock") }
func (fake *runtimeProjectionFake) ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection) {
	fake.calls = append(fake.calls, "apply")
}
func (fake *runtimeProjectionFake) BumpLocked() uint64 {
	fake.calls = append(fake.calls, "bump")
	return 11
}
func (fake *runtimeProjectionFake) UnlockView() { fake.calls = append(fake.calls, "unlock") }
func (fake *runtimeProjectionFake) CurrentViewSessionID() string {
	fake.calls = append(fake.calls, "session")
	return "session-a"
}
func (fake *runtimeProjectionFake) RuntimeSnapshot() model.RuntimeState {
	fake.calls = append(fake.calls, "snapshot")
	return model.RuntimeState{Account: "agent-a"}
}
func (fake *runtimeProjectionFake) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState) {
	fake.calls = append(fake.calls, "runtime")
	if revision != 11 || sessionID != "session-a" || runtime.Account != "agent-a" {
		panic("runtime publication changed")
	}
}
func (fake *runtimeProjectionFake) PublishTaskDeltas() {
	fake.calls = append(fake.calls, "tasks")
}

func TestRuntimeApplicationRefreshOrder(t *testing.T) {
	fake := &runtimeProjectionFake{}
	NewRuntime(RuntimeDeps{Projection: fake}).Refresh()
	want := "collect,lock,apply,bump,unlock,session,snapshot,runtime,tasks"
	if got := strings.Join(fake.calls, ","); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}
