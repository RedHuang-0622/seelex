package execution

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/view/board"
)

func TestBoardFacadeTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf((*ProjectBoardSink)(nil)).Elem() != reflect.TypeOf((*board.ProjectBoardSink)(nil)).Elem() {
		t.Fatal("ProjectBoardSink is not the Middle identity")
	}
	if reflect.TypeOf((*TeamworkPlanSource)(nil)).Elem() != reflect.TypeOf((*board.TeamworkPlanSource)(nil)).Elem() {
		t.Fatal("TeamworkPlanSource is not the Middle identity")
	}
}
