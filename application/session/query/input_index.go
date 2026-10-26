package query

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// InputIndexDeps supplies durable reads and lock-protected view access for the
// full user-input index. The durable range read remains outside ViewMu.
type InputIndexDeps struct {
	ViewMu        *sync.RWMutex
	Locate        func(sessionID string) session_runtime.Location
	LoadRange     func(workspaceID, sessionID string) (messages []model.Message, total int, available bool, err error)
	LoadRecord    func(location session_runtime.Location, sessionID string) (model.SessionRecord, bool, error)
	Record        func(record model.SessionRecord) []model.Message
	ReadUnit      func(sessionID string, read func(*sessionstate.View))
	BuildItems    func(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow
	SummaryLimit  func() int
	HistoryWindow func() int
	DurableCount  func([]model.Message) int
	UserText      func(model.Message) string
}

// InputIndexApplication owns full-session user-input index composition.
type InputIndexApplication struct {
	deps InputIndexDeps
}

// NewInputIndex constructs the read-only input index application.
func NewInputIndex(deps InputIndexDeps) *InputIndexApplication {
	return &InputIndexApplication{deps: deps}
}

// Read returns the bounded index for every user input in the session while
// marking only rows in the currently loaded window.
func (application *InputIndexApplication) Read(sessionID string) (model.SessionInputIndex, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionInputIndex{}, errors.New("session ID is required")
	}
	if application == nil || application.deps.ViewMu == nil {
		return model.SessionInputIndex{}, errors.New("session input index is not available")
	}
	location := session_runtime.Location{}
	if application.deps.Locate != nil {
		location = application.deps.Locate(sessionID)
	}
	conversation, total, err := application.readConversation(location, sessionID)
	if err != nil {
		return model.SessionInputIndex{}, err
	}
	window, loaded := application.readLoadedWindow(sessionID, total)
	items := []model.SessionInputIndexRow(nil)
	if application.deps.BuildItems != nil {
		limit := 0
		if application.deps.SummaryLimit != nil {
			limit = application.deps.SummaryLimit()
		}
		userText := application.deps.UserText
		if userText == nil {
			userText = func(model.Message) string { return "" }
		}
		items = application.deps.BuildItems(conversation, loaded, limit, userText)
	}
	return model.SessionInputIndex{
		SessionID:  sessionID,
		Total:      total,
		InputCount: len(items),
		Window:     window,
		Items:      items,
	}, nil
}

// readConversation prefers durable message rows and falls back to the record's
// visible conversation for legacy layouts. Both paths stay outside ViewMu.
func (application *InputIndexApplication) readConversation(location session_runtime.Location, sessionID string) ([]model.Message, int, error) {
	var rangeErr error
	if application.deps.LoadRange != nil {
		messages, total, available, err := application.deps.LoadRange(location.WorkspaceID, sessionID)
		if available && err == nil {
			return messages, total, nil
		}
		if available {
			rangeErr = err
		}
	}
	if application.deps.LoadRecord == nil {
		if rangeErr != nil && !errors.Is(rangeErr, fs.ErrNotExist) {
			return nil, 0, fmt.Errorf("load session conversation %q: %w", sessionID, rangeErr)
		}
		return nil, 0, nil
	}
	record, ok, err := application.deps.LoadRecord(location, sessionID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("load session record %q: %w", sessionID, err)
	}
	if !ok || record.ID != sessionID || application.deps.Record == nil {
		if rangeErr != nil && !errors.Is(rangeErr, fs.ErrNotExist) {
			return nil, 0, fmt.Errorf("load session conversation %q: %w", sessionID, rangeErr)
		}
		return nil, 0, nil
	}
	conversation := application.deps.Record(record)
	return conversation, len(conversation), nil
}

// readLoadedWindow takes only the shared read lock around in-memory view facts.
func (application *InputIndexApplication) readLoadedWindow(sessionID string, total int) (model.SessionInputWindow, []string) {
	windowSize := 0
	if application.deps.HistoryWindow != nil {
		windowSize = application.deps.HistoryWindow()
	}
	window := model.SessionInputWindow{WindowSize: windowSize, Total: total}
	loaded := []string{}
	if application.deps.ReadUnit != nil {
		application.deps.ViewMu.RLock()
		application.deps.ReadUnit(sessionID, func(view *sessionstate.View) {
			window.Offset = view.HistoryOffset
			if application.deps.DurableCount != nil {
				window.Count = application.deps.DurableCount(view.Conversation)
			}
			window.HasMore = view.HasMoreHistory
			if view.TotalMessages > 0 {
				window.Total = view.TotalMessages
			}
			if view.ConversationWindow > 0 {
				window.WindowSize = view.ConversationWindow
			}
			userText := application.deps.UserText
			if userText == nil {
				return
			}
			for _, message := range view.Conversation {
				if text := userText(message); text != "" {
					loaded = append(loaded, text)
				}
			}
		})
		application.deps.ViewMu.RUnlock()
	}
	if window.WindowSize <= 0 {
		window.WindowSize = windowSize
	}
	if window.Total <= 0 {
		window.Total = total
	}
	if window.Count <= 0 {
		window.Count = len(loaded)
	}
	return window, loaded
}
