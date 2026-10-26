// Package search owns the application adapter for conversation history search.
// Retrieval, defaults and token-budget clamping remain in the runtime search
// implementation.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ErrEmptyQuery rejects history searches without a query term.
var ErrEmptyQuery = errors.New("search_history: query is required")

// Deps supplies only the runtime search capability needed by this use case.
type Deps struct {
	Available func() bool
	Search    func(context.Context, string, int) (dto.SearchResult, error)
}

// Application validates search requests and shapes the model tool response.
// It does not own the search index, defaults or result limits.
type Application struct {
	deps Deps
}

// New constructs the application from its consumer-defined dependencies.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// SearchHistory returns the authoritative runtime result for a non-empty query.
// A zero limit is forwarded unchanged so the runtime keeps the only default.
func (application *Application) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return dto.SearchResult{}, ErrEmptyQuery
	}
	if application == nil || application.deps.Available == nil || !application.deps.Available() || application.deps.Search == nil {
		return dto.SearchResult{}, errors.New("search_history: runtime is unavailable")
	}
	return application.deps.Search(ctx, query, limit)
}

// SearchHistoryHandler adapts the search_history model tool to SearchHistory.
func (application *Application) SearchHistoryHandler(argsJSON string) (string, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("search_history: invalid JSON: %w", err)
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return "", ErrEmptyQuery
	}
	result, err := application.SearchHistory(context.Background(), input.Query, input.Limit)
	if err != nil {
		return "", fmt.Errorf("search_history: %w", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("search_history: encode result: %w", err)
	}
	return string(encoded), nil
}
