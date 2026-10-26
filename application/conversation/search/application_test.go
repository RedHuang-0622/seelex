package search

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestSearchHistoryRejectsEmptyQuery(t *testing.T) {
	application := New(Deps{
		Available: func() bool { return true },
		Search: func(context.Context, string, int) (dto.SearchResult, error) {
			t.Fatal("search must not be called for an empty query")
			return dto.SearchResult{}, nil
		},
	})

	if _, err := application.SearchHistory(context.Background(), "  ", 0); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("empty query error = %v, want ErrEmptyQuery", err)
	}
}

func TestSearchHistoryPreservesQueryAndLimit(t *testing.T) {
	application := New(Deps{
		Available: func() bool { return true },
		Search: func(_ context.Context, query string, limit int) (dto.SearchResult, error) {
			if query != "needle" || limit != 7 {
				t.Fatalf("search call = (%q, %d), want (%q, 7)", query, limit, "needle")
			}
			return dto.SearchResult{Query: query, IndexedFrames: 1}, nil
		},
	})

	result, err := application.SearchHistory(context.Background(), "needle", 7)
	if err != nil || result.Query != "needle" || result.IndexedFrames != 1 {
		t.Fatalf("SearchHistory = %+v, %v", result, err)
	}
}

func TestSearchHistoryHandlerUsesSameApplicationPath(t *testing.T) {
	application := New(Deps{
		Available: func() bool { return true },
		Search: func(_ context.Context, query string, limit int) (dto.SearchResult, error) {
			if query != "needle" || limit != 0 {
				t.Fatalf("search call = (%q, %d), want (%q, 0)", query, limit, "needle")
			}
			return dto.SearchResult{Query: query, IndexedFrames: 2}, nil
		},
	})

	raw, err := application.SearchHistoryHandler(`{"query":" needle "}`)
	if err != nil {
		t.Fatal(err)
	}
	var result dto.SearchResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result.Query != "needle" || result.IndexedFrames != 2 {
		t.Fatalf("handler result = %+v", result)
	}
}

func TestSearchHistoryHandlerWrapsRuntimeFailure(t *testing.T) {
	application := New(Deps{
		Available: func() bool { return true },
		Search: func(context.Context, string, int) (dto.SearchResult, error) {
			return dto.SearchResult{}, errors.New("index offline")
		},
	})

	_, err := application.SearchHistoryHandler(`{"query":"needle"}`)
	if err == nil || !strings.Contains(err.Error(), "index offline") {
		t.Fatalf("handler error = %v", err)
	}
}
