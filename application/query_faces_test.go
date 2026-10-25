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
	var _ application.SessionQuery = service
	var _ application.ConversationHistoryQuery = service
}
