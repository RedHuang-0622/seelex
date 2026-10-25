// Package sessionaccess defines the shared View/Chat/Recover capability for
// main, subagent, and teammate conversations.
package sessionaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// DefaultPageSize and DefaultRecordRunes are shared defaults for every adapter.
const (
	DefaultPageSize    = 40
	DefaultRecordRunes = 1200
)

// ConversationSession is intentionally limited to View, Chat, and Recover.
// Session directories, switching, approvals, tasks, fork trees, team plans,
// and WorkItem acceptance remain in their owning domains.
type ConversationSession interface {
	View(context.Context, dto.ConversationSessionRef, dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	Chat(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error)
	Recover(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error)
}

// Registry maps the fixed conversation kind to its adapter.
type Registry struct {
	mu       sync.RWMutex
	adapters map[dto.ConversationKind]ConversationSession
}

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[dto.ConversationKind]ConversationSession)}
}

// Register installs one adapter. Nil adapters remove the kind.
func (registry *Registry) Register(kind dto.ConversationKind, adapter ConversationSession) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	if adapter == nil {
		delete(registry.adapters, kind)
	} else {
		registry.adapters[kind] = adapter
	}
	registry.mu.Unlock()
}

// Session resolves and validates one fixed conversation window.
func (registry *Registry) Session(ref dto.ConversationSessionRef) (ConversationSession, error) {
	ref.Kind = dto.ConversationKind(strings.TrimSpace(string(ref.Kind)))
	ref.SessionID = strings.TrimSpace(ref.SessionID)
	ref.WorkItemID = strings.TrimSpace(ref.WorkItemID)
	switch ref.Kind {
	case dto.ConversationMain, dto.ConversationSubagent, dto.ConversationTeammate:
	default:
		return nil, fmt.Errorf("conversation: unknown kind %q", ref.Kind)
	}
	if ref.SessionID == "" {
		return nil, errors.New("conversation: session ID is required")
	}
	if ref.Kind == dto.ConversationTeammate && ref.WorkItemID == "" {
		return nil, errors.New("conversation: teammate window requires a WorkItem; role-only chat is not available")
	}
	if registry == nil {
		return nil, errors.New("conversation: adapter registry is not configured")
	}
	registry.mu.RLock()
	adapter := registry.adapters[ref.Kind]
	registry.mu.RUnlock()
	if adapter == nil {
		return nil, fmt.Errorf("conversation: %s adapter is not configured", ref.Kind)
	}
	return adapter, nil
}

// PaginateRecords is the single record filtering, clipping, and pagination
// implementation used by all adapters and WorkItem execution views.
func PaginateRecords(sessionID string, records []dto.ConversationRecord, request dto.ConversationViewRequest) dto.SessionRecordPage {
	limit := request.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	offset := request.Offset
	if offset < 0 {
		offset = 0
	}
	maxRunes := request.MaxRunes
	if maxRunes <= 0 {
		maxRunes = DefaultRecordRunes
	}
	kindSet := make(map[dto.ConversationRecordKind]struct{}, len(request.Kinds))
	for _, kind := range request.Kinds {
		kindSet[kind] = struct{}{}
	}
	filtered := make([]dto.ConversationRecord, 0, len(records))
	for _, record := range records {
		if len(kindSet) > 0 {
			if _, ok := kindSet[record.Kind]; !ok {
				continue
			}
		}
		filtered = append(filtered, record)
	}
	total := len(filtered)
	page := dto.SessionRecordPage{
		SessionID: strings.TrimSpace(sessionID),
		Offset:    offset,
		Limit:     limit,
		Total:     total,
		Records:   []dto.ConversationRecord{},
	}
	if offset >= total {
		return page
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page.Records = append([]dto.ConversationRecord(nil), filtered[offset:end]...)
	clipped := false
	for index := range page.Records {
		record := &page.Records[index]
		record.Content, record.Truncated = clipRunes(record.Content, maxRunes)
		record.Reasoning, clipped = clipRunes(record.Reasoning, maxRunes)
		record.Truncated = record.Truncated || clipped
		clipped = clipped || record.Truncated
	}
	page.HasMore = offset+len(page.Records) < total
	page.Truncated = clipped || offset > 0
	return page
}

func clipRunes(text string, limit int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= limit {
		return text, false
	}
	return string(runes[:limit]) + "…", true
}
