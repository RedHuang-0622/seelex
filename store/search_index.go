package store

// search_index.go adapts session projection reads to the artifact-owned search
// index. Message/compact semantics stay here; index construction/query lives in
// store/artifact.

import (
	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

type searchHit = storeartifact.SearchHit
type searchIndex = storeartifact.SearchIndex

func (store *storeEngine) searchIndexPath(key Key) string {
	return store.artifacts.SearchPath(layoutScope(key))
}

func (store *storeEngine) rebuildSearchIndex(key Key) error {
	head, err := store.readMessageHead(key)
	if err != nil {
		return err
	}
	rows, err := store.readRows(key, 1, 0)
	if err != nil {
		return err
	}
	searchRows := make([]storeartifact.SearchRow, 0, len(rows))
	for _, row := range rows {
		calls := make([]storeartifact.SearchToolCall, 0, len(row.ToolCalls))
		for _, call := range row.ToolCalls {
			calls = append(calls, storeartifact.SearchToolCall{Name: call.Name, Arguments: call.Arguments})
		}
		searchRows = append(searchRows, storeartifact.SearchRow{
			Seq: row.Seq, Content: row.Content, Name: row.Name,
			ReasoningContent: row.ReasoningContent, ToolCalls: calls,
		})
	}
	summary := ""
	if head.WatermarkSeq > 0 {
		compact, compactErr := store.readCompactHead(key)
		if compactErr == nil && compact.LatestFrame != nil {
			summary = compact.LatestFrame.Summary
		}
	}
	return store.artifacts.PutSearch(layoutScope(key), searchRows, head.LastSeq, head.WatermarkSeq, summary)
}

func (store *storeEngine) searchQuery(key Key, query string) ([]searchHit, error) {
	hits, err := store.artifacts.Search(layoutScope(key), query)
	if storeartifact.IsSearchMissing(err) {
		if rebuildErr := store.rebuildSearchIndex(key); rebuildErr != nil {
			return nil, rebuildErr
		}
		return store.artifacts.Search(layoutScope(key), query)
	}
	return hits, err
}
