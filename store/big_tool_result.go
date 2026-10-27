package store

// big_tool_result.go is the migration-period adapter for artifact-owned
// oversized text and immutable tool-result storage.

import (
	"strings"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

const blobRefPrefix = storeartifact.BlobRefPrefix

var (
	ErrBigToolResultTooLarge = storeartifact.ErrBigToolResultTooLarge
	ErrBigToolResultQuota    = storeartifact.ErrBigToolResultQuota
)

type toolBlob = storeartifact.Blob

func (store *storeEngine) blobLimits() (softChars int, hardBytes int, quotaBytes int) {
	return store.settings.BlobSoftLimitChars, store.settings.BlobHardLimitBytes, store.settings.BlobSessionQuotaBytes
}

func (store *storeEngine) blobDir(key Key) string {
	return store.artifacts.BlobDir(layoutScope(key))
}

func (store *storeEngine) blobPath(key Key, hash string) string {
	return store.artifacts.BlobPath(layoutScope(key), hash)
}

func (store *storeEngine) writeBlob(key Key, toolName, content string) (toolBlob, error) {
	return store.artifacts.WriteBlob(layoutScope(key), toolName, content)
}

func (store *storeEngine) readBlob(key Key, hash string) (string, error) {
	return store.artifacts.ReadBlob(layoutScope(key), hash)
}

func (store *storeEngine) listBlobHashes(key Key) ([]string, error) {
	return store.artifacts.ListBlobHashes(layoutScope(key))
}

func (store *storeEngine) blobGarbageCollect(key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	return store.artifacts.CollectBlobs(layoutScope(key), referenced, dryRun)
}

func blobRefOf(ref string) string {
	return strings.TrimPrefix(ref, blobRefPrefix)
}
