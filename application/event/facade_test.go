package event

import (
	"reflect"
	"testing"

	kernelevent "github.com/RedHuang-0622/seelex/kernel/event"
)

func TestEventTypesAreKernelIdentity(t *testing.T) {
	if reflect.TypeOf(Event{}) != reflect.TypeOf(kernelevent.Event{}) {
		t.Fatal("Application Event is not the kernel identity")
	}
	if reflect.TypeOf(NewEventHub()) != reflect.TypeOf(kernelevent.NewEventHub()) {
		t.Fatal("Application EventHub is not the kernel identity")
	}
}
