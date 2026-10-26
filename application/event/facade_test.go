package event

import (
	"reflect"
	"testing"

	middleevent "github.com/RedHuang-0622/seelex/middle/protocol/event"
)

func TestEventTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(Event{}) != reflect.TypeOf(middleevent.Event{}) {
		t.Fatal("Application Event is not the Middle identity")
	}
	if reflect.TypeOf(NewEventHub()) != reflect.TypeOf(middleevent.NewEventHub()) {
		t.Fatal("Application EventHub is not the Middle identity")
	}
}
