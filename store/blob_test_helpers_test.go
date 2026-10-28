package store

import (
	"strings"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

func writeBlobForTest(store *storeEngine, key Key, toolName, content string) (storeartifact.Blob, error) {
	return store.artifacts.WriteBlob(layoutScope(key), toolName, content)
}

func readBlobForTest(store *storeEngine, key Key, hash string) (string, error) {
	return store.artifacts.ReadBlob(layoutScope(key), hash)
}

func listBlobHashesForTest(store *storeEngine, key Key) ([]string, error) {
	return store.artifacts.ListBlobHashes(layoutScope(key))
}

func collectBlobsForTest(store *storeEngine, key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	return store.artifacts.CollectBlobs(layoutScope(key), referenced, dryRun)
}

func blobRefOfForTest(ref string) string {
	return strings.TrimPrefix(ref, storeartifact.BlobRefPrefix)
}

func searchIndexPathForTest(store *storeEngine, key Key) string {
	return store.artifacts.SearchPath(layoutScope(key))
}

func rebuildSearchIndexForTest(store *storeEngine, key Key) error {
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

func searchQueryForTest(store *storeEngine, key Key, query string) ([]storeartifact.SearchHit, error) {
	hits, err := store.artifacts.Search(layoutScope(key), query)
	if storeartifact.IsSearchMissing(err) {
		if rebuildErr := rebuildSearchIndexForTest(store, key); rebuildErr != nil {
			return nil, rebuildErr
		}
		return store.artifacts.Search(layoutScope(key), query)
	}
	return hits, err
}
