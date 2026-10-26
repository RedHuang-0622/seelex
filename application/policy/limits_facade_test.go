package policy

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/kernel/limit"
)

func TestLimitsFacadeTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(Limits{}) != reflect.TypeOf(limit.Limits{}) {
		t.Fatal("Limits is not the Backend identity")
	}
	if reflect.TypeOf(Knobs{}) != reflect.TypeOf(limit.Knobs{}) {
		t.Fatal("Knobs is not the Backend identity")
	}
}

func TestApplySessionReleaseWindowValidatesBeforeMutation(t *testing.T) {
	previous := limit.Get()
	t.Cleanup(func() { limit.Apply(previous) })

	before := limit.Get().ReleaseWindow()
	if _, err := ApplySessionReleaseWindow(0); err == nil {
		t.Fatal("expected invalid zero window")
	}
	if got := limit.Get().ReleaseWindow(); got != before {
		t.Fatalf("invalid apply changed window to %d, want %d", got, before)
	}

	got, err := ApplySessionReleaseWindow(3)
	if err != nil {
		t.Fatalf("ApplySessionReleaseWindow: %v", err)
	}
	if got != 3 || limit.Get().ReleaseWindow() != 3 {
		t.Fatalf("applied window = %d/%d, want 3", got, limit.Get().ReleaseWindow())
	}
}
