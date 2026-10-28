// Package record owns session-scoped tool-record snapshots.
package record

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const File = "tool-record.jsonl"

type Store struct {
	layout *storelayout.Layout
}

func New(layout *storelayout.Layout) *Store {
	return &Store{layout: layout}
}

func (s *Store) Append(_ context.Context, key dto.SessionKey, payload any) error {
	if err := key.Validate(); err != nil {
		return err
	}
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	path := s.path(key)
	lock := s.layout.Lock(scopeOf(key), "tool_record")
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Store) Read(_ context.Context, key dto.SessionKey) (json.RawMessage, bool, error) {
	if err := key.Validate(); err != nil {
		return nil, false, err
	}
	raw, err := storeroot.ReadSharedFile(s.path(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var last json.RawMessage
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			continue
		}
		last = json.RawMessage(trimmed)
	}
	return last, last != nil, nil
}

func (s *Store) path(key dto.SessionKey) string {
	return filepath.Join(s.layout.SessionRoot(scopeOf(key)), File)
}

func scopeOf(key dto.SessionKey) storelayout.Scope {
	return storelayout.Scope{ProjectID: key.ProjectID, SessionID: key.SessionID}
}
