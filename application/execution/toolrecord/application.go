// Package toolrecord owns the application-side tool outcome ledger.
package toolrecord

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	// DropNoOwner means an execution context did not identify its owner session.
	DropNoOwner = "no_owner_session"
	// DropNoUnit means an outcome had neither a node nor an event identity.
	DropNoUnit = "no_unit_identity"

	subagentUnitPrefix = "subagent:"
	roleUnitPrefix     = "role:"
)

// Deps supplies the host reads needed by the ledger. None of these callbacks
// may depend on toolrecord internals, keeping the dependency direction one-way.
type Deps struct {
	SaveEnabled     func(bool) error
	WritableSession func(string) bool
	ViewSessionID   func() string
}

// SessionView is the read-only ledger state for one session.
type SessionView struct {
	Exists  bool
	Total   int
	Success int
	Failure int
	Tools   []dto.ToolRecordStat
}

// Diagnostics exposes health counters that intentionally stay outside Snapshot.
type Diagnostics struct {
	SessionCount    int
	UnitCount       int
	DroppedReadings map[string]int
}

type unitKey struct {
	sessionID string
	unitID    string
}

// Application owns the per-session accumulators, leaf stages, enable switch,
// snapshot store and last flush error. Callers interact through this single
// state owner; no second ledger state is allowed in Application hosts.
type Application struct {
	mu sync.Mutex

	deps     Deps
	enabled  bool
	flushErr error
	store    contract.ToolRecordSnapshotStore
	byID     map[string]*Accumulator
	loaded   map[string]bool
	stages   map[unitKey]*Stage
	lastSeq  map[unitKey]uint64
	dropped  map[string]int
}

// New constructs an empty ledger.
func New(deps Deps) *Application {
	return &Application{
		deps:    deps,
		byID:    map[string]*Accumulator{},
		loaded:  map[string]bool{},
		stages:  map[unitKey]*Stage{},
		lastSeq: map[unitKey]uint64{},
		dropped: map[string]int{},
	}
}

// SetEnabled persists the user setting before changing the live value.
func (application *Application) SetEnabled(enabled bool) (bool, error) {
	application.mu.Lock()
	save := application.deps.SaveEnabled
	application.mu.Unlock()
	if save != nil {
		if err := save(enabled); err != nil {
			return application.Enabled(), err
		}
	}
	application.mu.Lock()
	application.enabled = enabled
	application.mu.Unlock()
	return enabled, nil
}

// ApplyEnabled changes only the in-memory assembly value.
func (application *Application) ApplyEnabled(enabled bool) {
	application.mu.Lock()
	application.enabled = enabled
	application.mu.Unlock()
}

// Enabled reports the current live switch.
func (application *Application) Enabled() bool {
	application.mu.Lock()
	defer application.mu.Unlock()
	return application.enabled
}

// SetSnapshotStore injects or replaces the session snapshot port.
func (application *Application) SetSnapshotStore(store contract.ToolRecordSnapshotStore) {
	application.mu.Lock()
	application.store = store
	application.mu.Unlock()
}

// Flush drains leaf stages, applies the writable-session gate and writes one
// whole-session snapshot only when the accumulator is dirty.
func (application *Application) Flush(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	application.drainSession(sessionID)

	application.mu.Lock()
	writable := application.deps.WritableSession
	store := application.store
	application.mu.Unlock()
	if writable != nil && !writable(sessionID) {
		return
	}
	if store == nil {
		return
	}
	accumulator := application.findAccumulator(sessionID)
	if accumulator == nil {
		return
	}
	snapshot, dirty := accumulator.Snapshot()
	if !dirty {
		return
	}
	if err := store.SaveToolRecordSnapshot(sessionID, snapshot); err != nil {
		application.noteFlushError(err)
		return
	}
	application.noteFlushError(nil)
}

// FlushAll flushes every session already present in the in-memory ledger.
func (application *Application) FlushAll() {
	application.mu.Lock()
	sessions := make([]string, 0, len(application.byID))
	for sessionID := range application.byID {
		sessions = append(sessions, sessionID)
	}
	application.mu.Unlock()
	for _, sessionID := range sessions {
		application.Flush(sessionID)
	}
}

// Board returns the current-view session projection.
func (application *Application) Board() dto.ToolRecordBoard {
	board := dto.ToolRecordBoard{Enabled: application.Enabled()}
	application.mu.Lock()
	viewSessionID := application.deps.ViewSessionID
	application.mu.Unlock()
	sessionID := ""
	if viewSessionID != nil {
		sessionID = strings.TrimSpace(viewSessionID())
	}
	application.load(sessionID)
	view := application.SessionView(sessionID)
	board.Total, board.Success, board.Failure = view.Total, view.Success, view.Failure
	board.Tools = view.Tools
	application.mu.Lock()
	if application.flushErr != nil {
		board.LedgerError = application.flushErr.Error()
	}
	application.mu.Unlock()
	return board
}

// RecordCommandOutcome records the command-exit-code success criterion.
func (application *Application) RecordCommandOutcome(sessionID, tool string, exitCode int) {
	application.RecordOutcome(sessionID, tool, exitCode != 0, fmt.Sprintf("exit code %d", exitCode))
}

// RecordOutcome records one completed main-agent or command outcome in memory.
func (application *Application) RecordOutcome(sessionID, tool string, failed bool, errorText string) {
	tool = strings.TrimSpace(tool)
	sessionID = strings.TrimSpace(sessionID)
	if tool == "" || sessionID == "" || !application.Enabled() {
		return
	}
	accumulator := application.accumulator(sessionID)
	if accumulator == nil {
		return
	}
	reason := ""
	if failed {
		reason = Reason(errorText)
	}
	accumulator.Record(tool, !failed, reason)
}

// StageUnitOutcome accumulates a leaf result in the execution unit's local stage.
func (application *Application) StageUnitOutcome(sessionID, unitID, tool string, failed bool, errorText string) {
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if !application.Enabled() {
		return
	}
	if sessionID == "" {
		application.noteDropped(DropNoOwner)
		return
	}
	if unitID == "" {
		application.noteDropped(DropNoUnit)
		return
	}
	if strings.TrimSpace(tool) == "" {
		return
	}
	reason := ""
	if failed {
		reason = Reason(errorText)
	}
	application.stage(unitKey{sessionID: sessionID, unitID: unitID}, tool, !failed, reason)
}

// CommitUnit is the single idempotent leaf-to-session commit entry.
func (application *Application) CommitUnit(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat) {
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if sessionID == "" {
		application.noteDropped(DropNoOwner)
		return
	}
	if unitID == "" {
		application.noteDropped(DropNoUnit)
		return
	}
	if len(stats) == 0 || !application.Enabled() {
		return
	}
	if !application.acceptSeq(unitKey{sessionID: sessionID, unitID: unitID}, seq) {
		return
	}
	accumulator := application.accumulator(sessionID)
	if accumulator != nil {
		accumulator.Merge(stats)
	}
}

// CloseUnit synchronously commits one unit's pending stage.
func (application *Application) CloseUnit(sessionID, unitID string) {
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if sessionID == "" || unitID == "" {
		return
	}
	key := unitKey{sessionID: sessionID, unitID: unitID}
	seq, stats := application.take(key)
	if len(stats) == 0 {
		return
	}
	application.CommitUnit(sessionID, unitID, seq, stats)
}

// ReleaseSession reclaims all in-memory ledger state for a released session.
func (application *Application) ReleaseSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	application.mu.Lock()
	delete(application.byID, sessionID)
	delete(application.loaded, sessionID)
	for key := range application.stages {
		if key.sessionID == sessionID {
			delete(application.stages, key)
		}
	}
	for key := range application.lastSeq {
		if key.sessionID == sessionID {
			delete(application.lastSeq, key)
		}
	}
	application.mu.Unlock()
}

// SessionView snapshots one session without mutating lazy-load state.
func (application *Application) SessionView(sessionID string) SessionView {
	sessionID = strings.TrimSpace(sessionID)
	accumulator := application.findAccumulator(sessionID)
	if accumulator == nil {
		return SessionView{}
	}
	total, success, failure := accumulator.Totals()
	return SessionView{
		Exists: true, Total: total, Success: success, Failure: failure, Tools: accumulator.Stats(),
	}
}

// Diagnostics returns dropped-reading and stage counts.
func (application *Application) Diagnostics() Diagnostics {
	application.mu.Lock()
	defer application.mu.Unlock()
	dropped := make(map[string]int, len(application.dropped))
	for reason, count := range application.dropped {
		dropped[reason] = count
	}
	return Diagnostics{SessionCount: len(application.byID), UnitCount: len(application.stages), DroppedReadings: dropped}
}

// NoteDropped records a visibly-dropped reading without fabricating an owner.
func (application *Application) NoteDropped(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	application.noteDropped(reason)
}

// SubagentUnitID derives the stable execution-unit key.
func SubagentUnitID(nodeID, eventID string) string {
	if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
		return subagentUnitPrefix + nodeID
	}
	if eventID = strings.TrimSpace(eventID); eventID != "" {
		return subagentUnitPrefix + eventID
	}
	return ""
}

// RoleUnitID derives the stable role execution-unit key.
func RoleUnitID(roleSessionID, eventID string) string {
	if roleSessionID = strings.TrimSpace(roleSessionID); roleSessionID != "" {
		return roleUnitPrefix + roleSessionID
	}
	if eventID = strings.TrimSpace(eventID); eventID != "" {
		return roleUnitPrefix + eventID
	}
	return ""
}

// CountsStatus reports whether a tool frame is a terminal result frame.
func CountsStatus(status dto.ToolEventStatus) bool {
	return status == dto.ToolEventSuccess || status == dto.ToolEventError
}

// Failed is the single typed-status failure predicate.
func Failed(status dto.ToolEventStatus) bool {
	return status == dto.ToolEventError
}

// Reason maps a raw failure text to the shared reason vocabulary.
func Reason(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case lowered == "":
		return dto.ToolReasonOther
	case containsAny(lowered, "permission", "denied", "unauthorized", "forbidden", "权限", "拒绝授权", "审批"):
		return dto.ToolReasonPermission
	case containsAny(lowered, "timeout", "timed out", "deadline exceeded", "超时", "超预算"):
		return dto.ToolReasonTimeout
	case containsAny(lowered, "拒绝", "refused", "rejected", "not allowed", "cannot", "不能", "不允许"):
		return dto.ToolReasonRefused
	case containsAny(lowered, "not found", "no such file", "cannot find", "does not exist", "unknown handle", "不存在", "找不到", "未知句柄"):
		return dto.ToolReasonNotFound
	case containsAny(lowered, "invalid", "required", "must have", "参数", "不合法", "缺少"):
		return dto.ToolReasonInvalidArgs
	case containsAny(lowered, "conflict", "already exists", "busy", "locked", "冲突", "已存在", "被占用"):
		return dto.ToolReasonConflict
	case containsAny(lowered, "exit code", "退出码"):
		return dto.ToolReasonCommandFailed
	case containsAny(lowered, "connection", "network", "dial", "eof", "account", "missing cli", "网络", "账号", "未装配"):
		return dto.ToolReasonTransport
	default:
		return dto.ToolReasonOther
	}
}

// ErrorText returns the original error text, never the presentation wrapper.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TerminalSubagentStatus reports whether a subagent node must close its stage.
func TerminalSubagentStatus(status dto.SubAgentNodeStatus) bool {
	switch status {
	case dto.SubAgentDone, dto.SubAgentFailed, dto.SubAgentInterrupted:
		return true
	default:
		return false
	}
}

func (application *Application) accumulator(sessionID string) *Accumulator {
	application.mu.Lock()
	defer application.mu.Unlock()
	if existing := application.byID[sessionID]; existing != nil {
		return existing
	}
	created := NewAccumulator()
	application.byID[sessionID] = created
	return created
}

func (application *Application) findAccumulator(sessionID string) *Accumulator {
	application.mu.Lock()
	defer application.mu.Unlock()
	return application.byID[sessionID]
}

func (application *Application) markLoaded(sessionID string) bool {
	application.mu.Lock()
	defer application.mu.Unlock()
	if application.loaded[sessionID] {
		return false
	}
	application.loaded[sessionID] = true
	return true
}

func (application *Application) load(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	application.mu.Lock()
	store := application.store
	application.mu.Unlock()
	if store == nil || !application.markLoaded(sessionID) {
		return
	}
	snapshot, ok, err := store.LoadToolRecordSnapshot(sessionID)
	if err != nil || !ok {
		return
	}
	if accumulator := application.accumulator(sessionID); accumulator != nil {
		accumulator.Load(snapshot)
	}
}

func (application *Application) noteFlushError(err error) {
	application.mu.Lock()
	application.flushErr = err
	application.mu.Unlock()
}

func (application *Application) stage(key unitKey, tool string, ok bool, reason string) {
	application.mu.Lock()
	stage := application.stages[key]
	if stage == nil {
		stage = NewStage(key.unitID, key.sessionID)
		application.stages[key] = stage
	}
	application.mu.Unlock()
	stage.Record(tool, ok, reason)
}

func (application *Application) take(key unitKey) (uint64, []dto.ToolRecordStat) {
	application.mu.Lock()
	stage := application.stages[key]
	application.mu.Unlock()
	if stage == nil {
		return 0, nil
	}
	return stage.TakeForCommit()
}

func (application *Application) acceptSeq(key unitKey, seq uint64) bool {
	application.mu.Lock()
	defer application.mu.Unlock()
	if seq == 0 {
		return true
	}
	if seq <= application.lastSeq[key] {
		return false
	}
	application.lastSeq[key] = seq
	return true
}

func (application *Application) drainSession(sessionID string) {
	application.mu.Lock()
	keys := make([]unitKey, 0, len(application.stages))
	for key := range application.stages {
		if key.sessionID == sessionID {
			keys = append(keys, key)
		}
	}
	application.mu.Unlock()
	for _, key := range keys {
		seq, stats := application.take(key)
		if len(stats) == 0 {
			continue
		}
		application.CommitUnit(key.sessionID, key.unitID, seq, stats)
	}
}

func (application *Application) noteDropped(reason string) {
	application.mu.Lock()
	application.dropped[reason]++
	application.mu.Unlock()
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
