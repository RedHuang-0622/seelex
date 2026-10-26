package adapters

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
)

func TestTeamworkBoardReaderFactoryMatchesBackendReader(t *testing.T) {
	got := NewTeamworkBoardReaderFactory()(nil)
	want := teamwork.NewBoardReader(nil)

	if !reflect.DeepEqual(got.BoardViewFor("session"), want.BoardViewFor("session")) {
		t.Fatal("BoardViewFor differs from backend reader")
	}
	if !reflect.DeepEqual(got.SessionLiveFor("session"), want.SessionLiveFor("session")) {
		t.Fatal("SessionLiveFor differs from backend reader")
	}
	if !reflect.DeepEqual(
		got.SessionLivePageFor("session", 3, 5),
		want.SessionLivePageFor("session", 3, 5),
	) {
		t.Fatal("SessionLivePageFor differs from backend reader")
	}
}
