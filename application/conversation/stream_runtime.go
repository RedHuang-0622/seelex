package conversation

import (
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/event"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
)

// StreamRuntimeDeps supplies session/view state, request lookup, reasoning and
// event publication for one streaming turn.
type StreamRuntimeDeps struct {
	Core                *kernelstate.Core
	Units               *sessionstate.Domain
	View                *viewstate.Coordinator
	Streams             StreamOutputPort
	SessionIDForRequest func(string) string
	RoleRoundFor        func(string) uint64
	EngineHistory       func(string) []contract.EngineMessage
	TakeServedRoute     func(string) (dto.ServedRoute, bool)
	StampServedRoute    func(sessionID, requestID, role, model, provider string)
	PublishSessionEvent func(event.EventKind, uint64, string, string, any) event.Event
}

// StreamRuntime owns visible streaming projection and reasoning/route stamping.
type StreamRuntime struct {
	deps StreamRuntimeDeps
}

// NewStreamRuntime constructs the streaming runtime.
func NewStreamRuntime(deps StreamRuntimeDeps) *StreamRuntime {
	return &StreamRuntime{deps: deps}
}

// AppendDelta consumes one provider chunk and appends its visible portion.
func (runtime *StreamRuntime) AppendDelta(requestID, chunk string) {
	visible := runtime.ConsumeVisibleChunk(requestID, chunk)
	if visible != "" {
		runtime.AppendVisibleDelta(requestID, visible)
	}
}

// NewBatchedDeltaSink creates a bounded visible-output batcher.
func (runtime *StreamRuntime) NewBatchedDeltaSink(requestID string) (StreamBatcher, func(string)) {
	batcher := runtime.deps.Streams.NewBatcher(func(batch []string) {
		runtime.AppendVisibleDelta(requestID, strings.Join(batch, ""))
	}, StreamBatcherOptions{FlushSize: 32, BufferSize: 128, Interval: 40 * time.Millisecond})
	runtime.deps.Core.ViewMu.Lock()
	if sessionID := runtime.deps.SessionIDForRequest(requestID); sessionID != "" {
		if unit := runtime.deps.Units.Unit(sessionID); unit != nil {
			unit.SetBatcher(batcher)
		}
	}
	runtime.deps.Core.ViewMu.Unlock()
	return batcher, func(chunk string) {
		if visible := runtime.ConsumeVisibleChunk(requestID, chunk); visible != "" {
			batcher.OnChunk(visible)
		}
	}
}

// FlushStreamBatcherFor flushes one session's buffered visible text.
func (runtime *StreamRuntime) FlushStreamBatcherFor(sessionID string) {
	if sessionID == "" {
		return
	}
	runtime.deps.Core.ViewMu.RLock()
	var batcher sessionstate.StreamBatcherSink
	if unit := runtime.deps.Units.Unit(sessionID); unit != nil {
		batcher = unit.BatcherSink()
	}
	runtime.deps.Core.ViewMu.RUnlock()
	if batcher != nil {
		_ = batcher.FlushPending()
	}
}

// ConsumeVisibleChunk applies one provider chunk to the owning session.
func (runtime *StreamRuntime) ConsumeVisibleChunk(requestID, chunk string) string {
	sessionID := runtime.deps.SessionIDForRequest(requestID)
	if sessionID != "" && sessionID != runtime.deps.Units.ActiveID() {
		return runtime.consumeVisibleChunkBackground(sessionID, requestID, chunk)
	}
	runtime.deps.Core.ViewMu.Lock()
	defer runtime.deps.Core.ViewMu.Unlock()
	unit := runtime.sessionUnitLocked(sessionID)
	running := unit.ChatState().Running && unit.ChatState().RequestID == requestID
	if !running && runtime.deps.Core.Snapshot.Chat.Running && runtime.deps.Core.Snapshot.Chat.RequestID == requestID {
		running = true
		sessionID = runtime.deps.Core.Snapshot.Session.ID
		unit = runtime.sessionUnitLocked(sessionID)
	}
	if !running {
		return ""
	}
	if stream := unit.StreamSink(); stream == nil || stream.RequestID() != requestID {
		unit.SetStream(runtime.deps.Streams.NewVisibleOutput(requestID))
	}
	return unit.StreamSink().Consume(chunk)
}

func (runtime *StreamRuntime) consumeVisibleChunkBackground(sessionID, requestID, chunk string) string {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		return ""
	}
	chatState := unit.ChatState()
	if !chatState.Running || chatState.RequestID != requestID {
		return ""
	}
	if stream := unit.StreamSink(); stream == nil || stream.RequestID() != requestID {
		unit.SetStream(runtime.deps.Streams.NewVisibleOutput(requestID))
	}
	return unit.StreamSink().Consume(chunk)
}

// AppendVisibleDelta appends one visible chunk to the active or background
// session projection.
func (runtime *StreamRuntime) AppendVisibleDelta(requestID, chunk string) {
	if chunk == "" {
		return
	}
	sessionID := runtime.deps.SessionIDForRequest(requestID)
	if sessionID != "" && sessionID != runtime.deps.Units.ActiveID() {
		runtime.appendVisibleDeltaBackground(sessionID, requestID, chunk)
		return
	}
	runtime.deps.Core.ViewMu.Lock()
	unit := runtime.sessionUnitLocked(sessionID)
	running := unit.ChatState().Running && unit.ChatState().RequestID == requestID
	if !running && runtime.deps.Core.Snapshot.Chat.Running && runtime.deps.Core.Snapshot.Chat.RequestID == requestID {
		running = true
		sessionID = runtime.deps.Core.Snapshot.Session.ID
		unit = runtime.sessionUnitLocked(sessionID)
	}
	if !running {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	if runtime.deps.View.SessionViewBrowsingHistoryLocked(sessionID) {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	messageID := ""
	runtime.deps.View.SessionViewMutateLocked(sessionID, func(view *sessionstate.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			if view.Conversation[index].Role == "assistant" && view.Conversation[index].Tool == nil {
				view.Conversation[index].Content += chunk
				messageID = view.Conversation[index].ID
				break
			}
		}
	})
	runtime.deps.View.MirrorActiveViewLocked()
	revision := runtime.deps.View.BumpLocked()
	runtime.deps.Core.ViewMu.Unlock()
	runtime.deps.PublishSessionEvent(event.EventMessageDelta, revision, requestID, sessionID, event.MessageDelta{MessageID: messageID, Delta: chunk})
}

func (runtime *StreamRuntime) appendVisibleDeltaBackground(sessionID, requestID, chunk string) {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		return
	}
	chatState := unit.ChatState()
	if !chatState.Running || chatState.RequestID != requestID {
		return
	}
	browsing := false
	unit.View.Read(func(view *sessionstate.View) {
		browsing = view.HistoryOffset+viewstate.DurableConversationCount(view.Conversation) < view.TotalMessages
	})
	if browsing {
		return
	}
	var messageID string
	var revision uint64
	unit.View.Mutate(func(view *sessionstate.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			if view.Conversation[index].Role == "assistant" && view.Conversation[index].Tool == nil {
				view.Conversation[index].Content += chunk
				messageID = view.Conversation[index].ID
				break
			}
		}
		view.Revision++
		revision = view.Revision
	})
	if messageID == "" {
		return
	}
	runtime.deps.PublishSessionEvent(event.EventMessageDelta, revision, requestID, sessionID, event.MessageDelta{MessageID: messageID, Delta: chunk})
}

// AttachLatestReasoning attaches engine reasoning steps to visible assistant
// messages without overwriting existing reasoning.
func (runtime *StreamRuntime) AttachLatestReasoning(sessionID, requestID string) {
	history := runtime.deps.EngineHistory(sessionID)
	steps := ReasoningStepsFromHistory(history)
	if len(steps) == 0 {
		return
	}
	runtime.deps.Core.ViewMu.Lock()
	if runtime.deps.View.SessionViewBrowsingHistoryLocked(sessionID) {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	var attached []event.MessageDelta
	runtime.deps.View.SessionViewMutateLocked(sessionID, func(view *sessionstate.View) {
		attached = AttachStepReasoningLocked(view, steps)
		if len(attached) > 0 {
			return
		}
		if delta, ok := AttachLastReasoningLocked(view, LastReasoningFromHistory(history)); ok {
			attached = append(attached, delta)
		}
	})
	if len(attached) == 0 {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	runtime.deps.View.MirrorActiveViewLocked()
	revision := runtime.deps.View.BumpLocked()
	runtime.deps.Core.ViewMu.Unlock()
	for _, delta := range attached {
		if delta.MessageID != "" {
			runtime.deps.PublishSessionEvent(event.EventMessageDelta, revision, requestID, sessionID, delta)
		}
	}
}

// StampServedRoute fills the served route onto the current turn's assistant
// transcript and visible message.
func (runtime *StreamRuntime) StampServedRoute(sessionID, requestID string) {
	route, ok := runtime.deps.TakeServedRoute(sessionID)
	if !ok {
		return
	}
	runtime.deps.StampServedRoute(sessionID, requestID, route.Role, route.Model, route.Provider)
	runtime.deps.Core.ViewMu.Lock()
	if runtime.deps.View.SessionViewBrowsingHistoryLocked(sessionID) {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	var stamped protocol.Message
	found := false
	runtime.deps.View.SessionViewMutateLocked(sessionID, func(view *sessionstate.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			message := &view.Conversation[index]
			if message.Role != "assistant" || message.Tool != nil {
				continue
			}
			if message.ConfigModel != "" {
				return
			}
			message.ConfigRole = route.Role
			message.ConfigModel = route.Model
			message.ConfigProvider = route.Provider
			stamped = *message
			found = true
			return
		}
	})
	if !found {
		runtime.deps.Core.ViewMu.Unlock()
		return
	}
	runtime.deps.View.MirrorActiveViewLocked()
	revision := uint64(0)
	if sessionID == "" || sessionID == runtime.deps.Core.Snapshot.Session.ID {
		revision = runtime.deps.View.BumpLocked()
	}
	runtime.deps.Core.ViewMu.Unlock()
	runtime.deps.PublishSessionEvent(event.EventMessageAdded, revision, requestID, sessionID, stamped)
}

// StreamedAssistantTextLocked returns the current request's accumulated
// visible assistant text. The caller holds Core.ViewMu.
func (runtime *StreamRuntime) StreamedAssistantTextLocked(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if text := runtime.streamedRequestTextLocked(sessionID); text != "" {
		return text
	}
	text := ""
	runtime.deps.View.SessionViewReadLocked(sessionID, func(view *sessionstate.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			message := view.Conversation[index]
			if message.Role != "assistant" || message.Tool != nil {
				continue
			}
			text = message.Content
			return
		}
	})
	return text
}

func (runtime *StreamRuntime) streamedRequestTextLocked(sessionID string) string {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		return ""
	}
	stream := unit.StreamSink()
	if stream == nil {
		return ""
	}
	if texted, ok := stream.(interface{ Text() string }); ok {
		return texted.Text()
	}
	return ""
}

func (runtime *StreamRuntime) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit {
	unit := runtime.deps.Units.Unit(sessionID)
	if unit == nil {
		var err error
		if sessionID == "" {
			unit = sessionstate.NewDraftUnit()
		} else {
			unit, err = sessionstate.NewSessionUnit(sessionID)
		}
		if err == nil {
			runtime.deps.Units.Register(unit)
		}
	}
	return unit
}
