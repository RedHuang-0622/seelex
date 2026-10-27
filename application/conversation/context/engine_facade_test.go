package context

import (
	"reflect"
	"testing"

	contextruntime "github.com/RedHuang-0622/seelex/kernel/context"
)

func TestEngineFacadeTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(EngineCoordinator{}) != reflect.TypeOf(contextruntime.Coordinator{}) {
		t.Fatal("EngineCoordinator is not the Backend identity")
	}
	if reflect.TypeOf(EngineDeps{}) != reflect.TypeOf(contextruntime.Deps{}) {
		t.Fatal("EngineDeps is not the Backend identity")
	}
}
