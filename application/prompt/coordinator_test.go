package prompt

import (
	"reflect"
	"testing"

	promptlayer "github.com/RedHuang-0622/seelex/kernel/prompt"
)

func TestCoordinatorIsEngineIdentity(t *testing.T) {
	if reflect.TypeOf(Coordinator{}) != reflect.TypeOf(promptlayer.Coordinator{}) {
		t.Fatal("Application prompt Coordinator is not the Engine identity")
	}
}
