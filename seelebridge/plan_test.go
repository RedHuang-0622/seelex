package seelebridge

import (
	"reflect"
	"testing"
)

func TestAdjacencyToEdgesStableOrder(t *testing.T) {
	edges := AdjacencyToEdges(map[string][]string{
		"build": {"test", "lint"},
		"start": {"build"},
	})
	want := []PlanEdge{{From: "build", To: "test"}, {From: "build", To: "lint"}, {From: "start", To: "build"}}
	if !reflect.DeepEqual(edges, want) {
		t.Fatalf("edges = %#v, want %#v", edges, want)
	}
}

func TestTopoSortRespectsCrossDependency(t *testing.T) {
	order := TopoSort("entry", map[string][]string{
		"entry": {"build", "test"},
		"test":  {"build"},
	}, map[string]struct{}{"entry": {}, "build": {}, "test": {}})
	want := []string{"entry", "test", "build"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestDetectCycle(t *testing.T) {
	if err := DetectCycle(map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil {
		t.Fatal("DetectCycle returned nil for a cycle")
	}
}
