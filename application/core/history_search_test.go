package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
)

// searchRuntime 是支持检索断言的 fakeRuntime 包装（记录查询参数）。
type searchRuntime struct {
	*fakeRuntime
	query string
	limit int
}

func (runtime *searchRuntime) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error) {
	runtime.query = query
	runtime.limit = limit
	return runtime.fakeRuntime.SearchHistory(context.Background(), query, limit)
}

func TestSearchHistoryHandlerReturnsStructuredHits(t *testing.T) {
	runtime := &searchRuntime{fakeRuntime: &fakeRuntime{
		searchResult: seelexctxsearch.Result{
			Query: "数据库优化", TotalUnits: 5, IndexedFrames: 2,
			Hits: []seelexctxsearch.Hit{{SegmentID: "compact-a", From: 0, To: 2, Score: 2,
				Records: []seelexctxsearch.ChatRecord{{Role: "user", Content: "聊聊数据库索引"}}}},
		},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	output, err := service.SearchHistoryHandler(context.Background(), `{"query": " 数据库优化 "}`)
	if err != nil {
		t.Fatalf("SearchHistoryHandler: %v", err)
	}
	if runtime.query != "数据库优化" {
		t.Fatalf("runtime query = %q, want 数据库优化（trim 后）", runtime.query)
	}
	if runtime.limit != seelexctxsearch.DefaultLimit {
		t.Fatalf("runtime limit = %d, want DefaultLimit %d", runtime.limit, seelexctxsearch.DefaultLimit)
	}
	var decoded seelexctxsearch.Result
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("handler output is not valid JSON: %v", err)
	}
	if len(decoded.Hits) != 1 || decoded.Hits[0].SegmentID != "compact-a" || decoded.Hits[0].Records[0].Content != "聊聊数据库索引" {
		t.Fatalf("decoded hits = %+v, want 权威命中原样返回", decoded.Hits)
	}
}

func TestSearchHistoryHandlerRejectsEmptyQuery(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if _, err := service.SearchHistoryHandler(context.Background(), `{"query": "  "}`); !errors.Is(err, ErrEmptySearchQuery) {
		t.Fatalf("empty query error = %v, want ErrEmptySearchQuery", err)
	}
}

func TestSearchHistoryHandlerPropagatesRuntimeError(t *testing.T) {
	runtime := &fakeRuntime{searchErr: errors.New("事件库未装配")}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	if _, err := service.SearchHistoryHandler(context.Background(), `{"query": "历史"}`); err == nil || err.Error() != "search_history: 事件库未装配" {
		t.Fatalf("runtime error = %v, want wrapped propagation", err)
	}
}

func TestSearchHistoryRejectsEmptyQueryAtServiceBoundary(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if _, err := service.SearchHistory(context.Background(), "", 5); !errors.Is(err, ErrEmptySearchQuery) {
		t.Fatalf("Service.SearchHistory empty query error = %v, want ErrEmptySearchQuery", err)
	}
}
