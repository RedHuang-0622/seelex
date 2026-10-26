package policy

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit"
)

func TestLimitsFacadeTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(Limits{}) != reflect.TypeOf(limit.Limits{}) {
		t.Fatal("Limits is not the Backend identity")
	}
	if reflect.TypeOf(Knobs{}) != reflect.TypeOf(limit.Knobs{}) {
		t.Fatal("Knobs is not the Backend identity")
	}
}
