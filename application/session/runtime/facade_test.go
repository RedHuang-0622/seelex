package session_runtime

import (
	"reflect"
	"testing"

	sessionruntime "github.com/RedHuang-0622/seelex/kernel/session"
)

func TestSessionRuntimeTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(Coordinator{}) != reflect.TypeOf(sessionruntime.Coordinator{}) {
		t.Fatal("Coordinator is not the Backend identity")
	}
	if reflect.TypeOf(Location{}) != reflect.TypeOf(sessionruntime.Location{}) {
		t.Fatal("Location is not the Backend identity")
	}
}
