package policy

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit/tier"
)

func TestTierFacadeTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(TierDeps{}) != reflect.TypeOf(tier.Deps{}) {
		t.Fatal("TierDeps is not the Backend identity")
	}
	if reflect.TypeOf(TierGate(nil)) != reflect.TypeOf(tier.Gate(nil)) {
		t.Fatal("TierGate is not the Backend identity")
	}
}
