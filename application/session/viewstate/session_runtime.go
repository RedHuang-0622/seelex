package view_state

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// ProjectDraftWorkspaceLocked injects the active draft's workspace binding
// into the visible session-workspace projection.
func (c *Coordinator) ProjectDraftWorkspaceLocked(snapshot *protocol.Snapshot) {
	if c == nil || snapshot == nil || c.draftView == nil {
		return
	}
	draftID, draftWorkspaceID := c.draftView()
	if draftID == "" {
		return
	}
	workspaceID := ""
	switch {
	case snapshot.Session.Draft && snapshot.Session.ID == draftID:
		if snapshot.CurrentWorkspace != nil {
			workspaceID = snapshot.CurrentWorkspace.ID
		}
	case draftWorkspaceID != "":
		workspaceID = draftWorkspaceID
	}
	if workspaceID == "" {
		return
	}
	if snapshot.SessionWorkspaces == nil {
		snapshot.SessionWorkspaces = make(map[string]string, 1)
	}
	snapshot.SessionWorkspaces[draftID] = workspaceID
}

// WithLiveSessionRowsLocked appends running/queued/awaiting sessions that are
// not yet persisted into the directory projection.
func (c *Coordinator) WithLiveSessionRowsLocked(rows []protocol.SessionInfo) []protocol.SessionInfo {
	if c == nil || c.units == nil {
		return rows
	}
	ids := c.units.UnitIDs()
	if len(ids) == 0 {
		return rows
	}
	draftID := ""
	if c.draftView != nil {
		draftID, _ = c.draftView()
	}
	listed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		listed[row.ID] = struct{}{}
	}
	live := make([]protocol.SessionInfo, 0, len(ids))
	for _, sessionID := range ids {
		if sessionID == "" || sessionID == draftID {
			continue
		}
		if _, ok := listed[sessionID]; ok {
			continue
		}
		switch c.SessionStatusLocked(sessionID) {
		case protocol.SessionStatusRunning, protocol.SessionStatusQueued, protocol.SessionStatusAwaitingApproval:
		default:
			continue
		}
		row := protocol.SessionInfo{ID: sessionID, UpdatedAt: time.Now()}
		if c.titleForSession != nil {
			row.Name = c.titleForSession(sessionID)
		}
		live = append(live, row)
	}
	if len(live) == 0 {
		return rows
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return append(live, rows...)
}

// EnrichDirectoryRowsLocked overlays session status, approval count and
// residency onto directory rows.
func (c *Coordinator) EnrichDirectoryRowsLocked(rows []protocol.SessionInfo) []protocol.SessionInfo {
	if c == nil {
		return rows
	}
	draftID := ""
	if c.draftView != nil {
		draftID, _ = c.draftView()
	}
	if draftID != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Status == protocol.SessionStatusDraft && row.ID != draftID {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
	}
	for index := range rows {
		if draftID != "" && rows[index].ID == draftID {
			rows[index].Status = protocol.SessionStatusDraft
			continue
		}
		sessionID := rows[index].ID
		rows[index].Status = c.SessionStatusLocked(sessionID)
		if unit := c.units.Unit(sessionID); unit != nil {
			rows[index].ApprovalCount = unit.PendingApprovalCount()
			rows[index].Resident = unit.Resident()
		}
	}
	return rows
}

// SessionStatusLocked returns the visible status of one session.
func (c *Coordinator) SessionStatusLocked(sessionID string) protocol.SessionStatus {
	if sessionID == "" {
		return protocol.SessionStatusDraft
	}
	if c.IsRestoringLocked(sessionID) {
		return protocol.SessionStatusRestoring
	}
	unit := c.units.Unit(sessionID)
	if unit == nil {
		return protocol.SessionStatusIdle
	}
	if unit.PendingApprovalCount() > 0 {
		return protocol.SessionStatusAwaitingApproval
	}
	runtime := unit.ChatState()
	if runtime.Running {
		return protocol.SessionStatusRunning
	}
	if len(unit.PendingRequests()) > 0 || runtime.QueuedCount > 0 {
		return protocol.SessionStatusQueued
	}
	return protocol.SessionStatusIdle
}

// IsRestoringLocked reports whether a session is in the cold-restore shell.
func (c *Coordinator) IsRestoringLocked(sessionID string) bool {
	if c == nil {
		return false
	}
	_, ok := c.restoring[sessionID]
	return ok
}

// SetRestoringLocked marks a session as entering cold restore.
func (c *Coordinator) SetRestoringLocked(sessionID string) {
	if c.restoring == nil {
		c.restoring = make(map[string]struct{})
	}
	c.restoring[sessionID] = struct{}{}
}

// ClearRestoringLocked removes the restore shell and wakes waiters.
func (c *Coordinator) ClearRestoringLocked(sessionID string) {
	delete(c.restoring, sessionID)
	c.signalRestoreLocked()
}

func (c *Coordinator) signalRestoreLocked() {
	if c.restoreSig == nil {
		c.restoreSig = make(chan struct{})
		return
	}
	close(c.restoreSig)
	c.restoreSig = make(chan struct{})
}

// RestoreSignalLocked returns the current restore-change signal.
func (c *Coordinator) RestoreSignalLocked() <-chan struct{} {
	if c.restoreSig == nil {
		c.restoreSig = make(chan struct{})
	}
	return c.restoreSig
}

func (c *Coordinator) nextViewEpochLocked() uint64 {
	c.viewEpoch++
	return c.viewEpoch
}

// NextViewEpochLocked advances the view activation clock.
func (c *Coordinator) NextViewEpochLocked() uint64 {
	return c.nextViewEpochLocked()
}

// MarkSessionViewIntentLocked records a new per-session activation intent.
func (c *Coordinator) MarkSessionViewIntentLocked(sessionID string) uint64 {
	if c.sessionViewIntent == nil {
		c.sessionViewIntent = make(map[string]uint64)
	}
	intent := c.nextViewEpochLocked()
	c.sessionViewIntent[sessionID] = intent
	return intent
}

// MarkSessionViewIntent records activation intent while taking the view lock.
func (c *Coordinator) MarkSessionViewIntent(sessionID string) {
	c.ViewMu.Lock()
	c.MarkSessionViewIntentLocked(sessionID)
	c.ViewMu.Unlock()
}

// SessionViewIntentLocked returns the latest activation intent for a session.
func (c *Coordinator) SessionViewIntentLocked(sessionID string) uint64 {
	return c.sessionViewIntent[sessionID]
}

// RefreshRuntimeProjectionForSession samples outside the lock and applies the
// projection under it when a session is activated.
func (c *Coordinator) RefreshRuntimeProjectionForSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	projection := c.CollectRuntimeProjectionFor(context.Background(), sessionID)
	c.ViewMu.Lock()
	c.ApplyRuntimeProjectionForLocked(sessionID, projection)
	c.ViewMu.Unlock()
}

// AppendAssistantPlaceholderAfterToolLocked appends one empty assistant row
// after a tool result unless the tail already is one.
func (c *Coordinator) AppendAssistantPlaceholderAfterToolLocked(sessionID string) *protocol.Message {
	emptyAssistantTail := false
	c.SessionViewReadLocked(sessionID, func(view *session.View) {
		if n := len(view.Conversation); n > 0 {
			last := view.Conversation[n-1]
			emptyAssistantTail = last.Role == "assistant" && last.Content == "" && last.Tool == nil
		}
	})
	if emptyAssistantTail {
		return nil
	}
	return c.AppendMessageWithOriginLockedFor(sessionID, "assistant", "", nil, protocol.MessageOrigin{
		RoleName: protocol.RoleNameMain, RoleSessionID: sessionID,
		RoundID: c.tasks.RoleRoundFor(sessionID),
	})
}

// SessionViewEmptyLocked reports whether a session has no visible messages.
func (c *Coordinator) SessionViewEmptyLocked(sessionID string) bool {
	unit := c.units.Unit(sessionID)
	if unit == nil {
		return true
	}
	empty := true
	unit.View.Read(func(view *session.View) {
		empty = len(view.Conversation) == 0
	})
	return empty
}

// RecordReadFileForSessionLocked records/refreshes one read-file reference.
func (c *Coordinator) RecordReadFileForSessionLocked(sessionID, arguments string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return
	}
	now := time.Now()
	c.SessionViewMutateLocked(sessionID, func(view *session.View) {
		for index := range view.ReadFiles {
			if view.ReadFiles[index].Path == input.Path {
				view.ReadFiles[index].ReadAt = now
				return
			}
		}
		view.ReadFiles = append(view.ReadFiles, protocol.ReadFileRef{Path: input.Path, ReadAt: now})
	})
	c.MirrorActiveViewLocked()
}
