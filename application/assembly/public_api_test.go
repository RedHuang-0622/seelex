package assembly_test

import (
	"reflect"
	"testing"

	applicationassembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract"
)

func TestPublicConstructorRejectsMissingDependencies(t *testing.T) {
	if _, err := applicationassembly.New(contract.Dependencies{}, applicationassembly.ExternalPorts{}); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestComponentsForNilReturnsEmptyGraph(t *testing.T) {
	if got := applicationassembly.ComponentsFor(nil); !reflect.DeepEqual(got, applicationassembly.Components{}) {
		t.Fatalf("ComponentsFor(nil) = %+v, want zero graph", got)
	}
}
