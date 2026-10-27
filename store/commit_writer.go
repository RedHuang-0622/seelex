package store

// commit_writer.go exposes the three single-aggregate store commits used by the
// application turn unit-of-work. Cross-aggregate ordering is intentionally not
// implemented here.

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
)

// CommitWriter adapts a Router to the application turn commit port.
type CommitWriter struct {
	router *Router
}

// NewCommitWriter constructs a stateless aggregate commit adapter.
func NewCommitWriter(router *Router) *CommitWriter {
	return &CommitWriter{router: router}
}

// WriteArtifacts stores immutable tool results and publishes their refs index.
func (writer *CommitWriter) WriteArtifacts(_ context.Context, projectID, sessionID string, results []ToolResult) error {
	return writer.withRepository(projectID, sessionID, func(repository *jsonRepository, key Key) error {
		return writeArtifactPhase(repository, key, results)
	})
}

// WriteMessages publishes transcript events through the message head.
func (writer *CommitWriter) WriteMessages(_ context.Context, projectID, sessionID string, events []Event) error {
	return writer.withRepository(projectID, sessionID, func(repository *jsonRepository, key Key) error {
		return writeMessagePhase(repository, key, events)
	})
}

// WriteCompactions publishes compaction records through their own head.
func (writer *CommitWriter) WriteCompactions(_ context.Context, projectID, sessionID string, records []json.RawMessage) error {
	return writer.withRepository(projectID, sessionID, func(repository *jsonRepository, key Key) error {
		return writeCompactionPhase(repository, key, records)
	})
}

func (writer *CommitWriter) withRepository(projectID, sessionID string, fn func(*jsonRepository, Key) error) error {
	if writer == nil || writer.router == nil {
		return errors.New("session storage: commit writer is not assembled")
	}
	return writer.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		concrete, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: aggregate commits require JSON repository")
		}
		return fn(concrete, Key{ProjectID: projectID, SessionID: sessionID})
	})
}

func ensureCommitSession(repository *jsonRepository, key Key) error {
	if repository.layout.sessionExists(key) {
		return nil
	}
	_, err := repository.layout.messageCommit(key, "", nil)
	return err
}

func writeArtifactPhase(repository *jsonRepository, key Key, results []ToolResult) error {
	if err := ensureCommitSession(repository, key); err != nil {
		return err
	}
	refs, err := repository.layout.readToolResultRefs(key)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if refs == nil {
		refs = []string{}
	}
	changed := false
	for _, result := range results {
		if err := repository.writeToolResultLocked(key, result); err != nil {
			return err
		}
		if !containsValue(refs, result.Ref) {
			refs = append(refs, result.Ref)
			changed = true
		}
	}
	if changed {
		return repository.layout.publishToolResultRefs(key, refs)
	}
	return nil
}

func writeMessagePhase(repository *jsonRepository, key Key, events []Event) error {
	if err := ensureCommitSession(repository, key); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	_, err := repository.layout.messageCommit(key, "", events)
	return err
}

func writeCompactionPhase(repository *jsonRepository, key Key, records []json.RawMessage) error {
	if err := ensureCommitSession(repository, key); err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	_, _, err := repository.layout.commitCompactionRecords(key, records)
	return err
}
