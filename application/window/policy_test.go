package window

import (
	"reflect"
	"testing"

	contextcontrol "github.com/RedHuang-0622/seelex/backend/engine/input/contextcontrol"
)

func TestPolicyTypesAreEngineIdentity(t *testing.T) {
	if reflect.TypeOf(WindowConfig{}) != reflect.TypeOf(contextcontrol.WindowConfig{}) {
		t.Fatal("Application WindowConfig is not the Engine identity")
	}
	if got, want := DefaultWindowConfig(), contextcontrol.DefaultWindowConfig(); got != want {
		t.Fatalf("DefaultWindowConfig = %+v, want %+v", got, want)
	}
}
