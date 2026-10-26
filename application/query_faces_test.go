package application_test

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
)

func TestApplicationQueryFacesStayReadOnlyAndNarrow(t *testing.T) {
	queries := []struct {
		name string
		typ  reflect.Type
		max  int
	}{
		{name: "SessionQuery", typ: reflect.TypeOf((*application.SessionQuery)(nil)).Elem(), max: 8},
		{name: "ConversationHistoryQuery", typ: reflect.TypeOf((*application.ConversationHistoryQuery)(nil)).Elem(), max: 8},
	}
	for _, query := range queries {
		if query.typ.NumMethod() > query.max {
			t.Fatalf("%s 有 %d 个方法，上限 %d", query.name, query.typ.NumMethod(), query.max)
		}
	}
}

func TestApplicationServiceSatisfiesQueryFaces(t *testing.T) {
	var service *application.Service
	frontend := &application.Frontend{Service: service}
	var _ application.SessionQuery = frontend
	var _ application.ConversationHistoryQuery = frontend
}

func TestSessionQueryCoversDurableReadProjections(t *testing.T) {
	query := reflect.TypeOf((*application.SessionQuery)(nil)).Elem()
	want := []string{"GetSessionTranscript", "SessionInputIndex", "SnapshotOf"}
	for _, name := range want {
		if _, ok := query.MethodByName(name); !ok {
			t.Errorf("SessionQuery 缺少只读方法 %s", name)
		}
	}
}
