package table

import (
	"context"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/event"
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

// RuntimeDeps supplies the host facts needed by the WorkTable runtime. The
// runtime owns synchronization of Plan/subagent sources, lifecycle consumers
// and WorkTable publication; it does not own Agent execution or persistence.
type RuntimeDeps struct {
	Core                             *kernelstate.Core
	Tasks                            *taskcontext.Coordinator
	View                             *viewstate.Coordinator
	Limits                           func() worktable.Limits
	CloseTerminalSubagentToolRecords func([]dto.SubAgentTreeNode)
	SubAgentTree                     func() []dto.SubAgentTreeNode
	PublishWorkTable                 func(worktable.WorkTableUpdate, protocol.WorkTableEvent)
	PublishSessionEvent              func(event.EventKind, uint64, string, string, any) event.Event
	HandleLifecycleFault             func(any)
	TriggerAsync                     func(map[string]struct{}) int
	TriggerTeamwork                  func(map[string]struct{}) int
	AsyncTriggerEnabled              func() bool
	RuntimeSnapshot                  func() protocol.RuntimeState
	// Continuation 是自动续跑 gate（application/execution.ContinuationApplication）。
	// 缺省 = 这条通道不存在：不建信号源、不扫门，行为与接线前一致。
	Continuation ContinuationGate
}

// Runtime owns the in-memory WorkTable projection state and lifecycle
// consumers. It is intentionally independent from application/assembly.
type Runtime struct {
	deps          RuntimeDeps
	publisher     *Application
	lifecycleStop chan struct{}
	lifecycleOnce sync.Once
	// continuationEvents 是回合休止点 → 续跑门的唤醒信号（缓冲区只为吸收突发：
	// 丢掉一条唤醒不会丢判据，见 continuation.go）。
	continuationEvents chan string
}

// NewRuntime constructs the WorkTable runtime.
func NewRuntime(deps RuntimeDeps) *Runtime {
	var continuationEvents chan string
	if deps.Continuation != nil {
		continuationEvents = make(chan string, 64)
	}
	return &Runtime{
		continuationEvents: continuationEvents,
		deps:               deps,
		publisher: New(Deps{
			SubAgentTree: deps.SubAgentTree,
			Publish:      deps.PublishWorkTable,
		}),
	}
}

// Close stops the latest-wins publisher after flushing its final update.
func (runtime *Runtime) Close() {
	if runtime == nil || runtime.publisher == nil {
		return
	}
	runtime.publisher.Close()
}

// RefreshLocked rebuilds the WorkTable projection while the caller holds the
// shared view write lock. Both inputs must have been sampled outside that lock.
func (runtime *Runtime) RefreshLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
	if runtime == nil || runtime.deps.Core == nil {
		return
	}
	rows, batches := BuildRows(RowInputs{
		Plan:         runtime.deps.Core.Snapshot.Runtime.Plan,
		Tasks:        tasks,
		SubAgentTree: runtime.deps.Core.Snapshot.Runtime.SubAgentTree,
		AsyncRuns:    asyncRuns,
		Limits:       runtime.limits(),
	})
	runtime.deps.Core.Snapshot.Runtime.WorkTable = rows
	runtime.deps.Core.Snapshot.Runtime.WorkTableBatches = batches
}

// Publish sends one immutable table snapshot to the latest-wins publisher.
func (runtime *Runtime) Publish(revision uint64, requestID string, items []protocol.WorkItem, batches []protocol.WorkTableBatch) {
	if runtime == nil || runtime.publisher == nil {
		return
	}
	runtime.publisher.Send(worktable.WorkTableUpdate{
		Revision: revision, RequestID: requestID, Items: items, Batches: batches,
	})
}

// PublishTaskChanged publishes one task.changed increment.
func (runtime *Runtime) PublishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string) {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Events == nil {
		return
	}
	if record.SessionID == "" {
		record.SessionID = sessionID
	}
	item := runtime.WorkItemForRecord(record, sessionID)
	runtime.publishSessionEvent(event.EventTaskChanged, revision, requestID, sessionID, protocol.TaskChangedEvent{TaskID: item.ID, Task: item})
}

// PublishTaskDeltas samples runtime facts outside the lock, rebuilds the
// WorkTable under the lock and publishes the immutable result afterwards.
func (runtime *Runtime) PublishTaskDeltas() {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Runtime == nil {
		return
	}
	tasks := runtime.deps.Core.Deps.Runtime.TaskSnapshot()
	asyncRuns := runtime.AsyncRuns()
	runtime.deps.Core.ViewMu.Lock()
	runtime.RefreshLocked(tasks, asyncRuns)
	revision := runtime.deps.View.BumpLocked()
	requestID := runtime.deps.Core.Snapshot.Chat.RequestID
	items := protocol.CloneWorkItems(runtime.deps.Core.Snapshot.Runtime.WorkTable)
	batches := protocol.CloneWorkTableBatches(runtime.deps.Core.Snapshot.Runtime.WorkTableBatches)
	runtime.deps.Core.ViewMu.Unlock()
	runtime.Publish(revision, requestID, items, batches)
}

// SyncFromSources synchronizes the active session's Plan and subagent tree.
func (runtime *Runtime) SyncFromSources() {
	if runtime == nil || runtime.deps.Core == nil {
		return
	}
	runtime.deps.Core.ViewMu.RLock()
	sessionID := runtime.deps.Core.Snapshot.Session.ID
	runtime.deps.Core.ViewMu.RUnlock()
	runtime.SyncFromSourcesFor(sessionID)
}

// SyncFromSourcesFor projects one session's Plan and owned subagent nodes into
// that session's task scope.
func (runtime *Runtime) SyncFromSourcesFor(sessionID string) {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Runtime == nil {
		return
	}
	runtime.deps.Core.ViewMu.RLock()
	viewSessionID := runtime.deps.Core.Snapshot.Session.ID
	ownerSessionID := sessionID
	if ownerSessionID == "" {
		ownerSessionID = viewSessionID
	}
	plan := runtime.SessionActivePlanLocked(sessionID)
	tree := worktable.CloneSubAgentTreeForSync(runtime.deps.Core.Snapshot.Runtime.SubAgentTree)
	scope := sessionID
	if sessionID == "" || sessionID == viewSessionID {
		scope = ""
	}
	runtime.deps.Core.ViewMu.RUnlock()

	if plan != nil {
		var walk func([]protocol.PlanNode, string)
		walk = func(nodes []protocol.PlanNode, parentID string) {
			for _, node := range nodes {
				runtime.syncPlanNodeTask(scope, node, parentID)
				walk(node.Children, "plan:"+node.ID)
			}
		}
		walk(plan.Nodes, "")
	}
	var walkTree func([]dto.SubAgentTreeNode, string)
	walkTree = func(items []dto.SubAgentTreeNode, parentID string) {
		for _, node := range items {
			if node.ID == "" || node.ID == "main" {
				walkTree(node.Children, parentID)
				continue
			}
			if strings.TrimSpace(node.MainSessionID) != ownerSessionID {
				continue
			}
			runtime.syncSubagentTask(scope, node, parentID)
			walkTree(node.Children, node.ID)
		}
	}
	walkTree(tree, "")
}

// SessionActivePlanLocked returns the active or background session's Plan
// projection while the caller holds the view read lock.
func (runtime *Runtime) SessionActivePlanLocked(sessionID string) *protocol.PlanState {
	if runtime == nil || runtime.deps.Core == nil {
		return nil
	}
	if sessionID == runtime.deps.Core.Snapshot.Session.ID {
		return worktable.ClonePlanForSync(runtime.deps.Core.Snapshot.Runtime.Plan)
	}
	if runtime.deps.Tasks == nil {
		return nil
	}
	return taskcontext.ActivePlanFromStack(
		runtime.deps.Tasks.PlanStackFor(sessionID),
		runtime.deps.Tasks.ActivePlanIDFor(sessionID),
	)
}

func (runtime *Runtime) syncPlanNodeTask(sessionID string, node protocol.PlanNode, parentID string) {
	key := "plan:" + node.ID
	status := worktable.TaskStatusForNode(node.Status)
	identity := dto.ActorIdentity("main", runtime.deps.Core.Deps.Engine.SessionID())
	existing, found, err := runtime.deps.Core.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhasePlan, Task: node.Label, Kind: "plan",
			SourceID: node.ID, Assignee: identity,
		}
		if parentID != "" {
			spec.Dependencies = []string{parentID}
		}
		_, _, _ = runtime.deps.Core.Deps.Runtime.TaskAddFor(sessionID, spec)
		return
	}
	if existing.Status != status {
		_, _ = runtime.deps.Core.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "node:"+node.Status.String())
	}
	if existing.Assignee == "" && identity != "" {
		_, _ = runtime.deps.Core.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

func (runtime *Runtime) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string) {
	key := "subagent:" + node.ID
	status := worktable.TaskStatusForSubagent(node.Status)
	identity := dto.ActorIdentity("subagent", node.SessionID)
	existing, found, err := runtime.deps.Core.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhaseSubagent, Task: node.Goal, Kind: "subagent",
			SourceID: node.ID,
		}
		if parentID != "" {
			spec.Dependencies = []string{"subagent:" + parentID}
		}
		created, _, _ := runtime.deps.Core.Deps.Runtime.TaskAddFor(sessionID, spec)
		_, _ = runtime.deps.Core.Deps.Runtime.TaskSetStatusFor(sessionID, created.ID, status, "subagent:"+node.Status.String())
		if identity != "" {
			_, _ = runtime.deps.Core.Deps.Runtime.TaskAttachParticipant(created.ID, identity)
		}
		return
	}
	if existing.Status != status {
		_, _ = runtime.deps.Core.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "subagent:"+node.Status.String())
	}
	if identity != "" {
		_, _ = runtime.deps.Core.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

// WorkItemForRecord maps one task record and fills Plan adjacency dependencies.
func (runtime *Runtime) WorkItemForRecord(record dto.TaskRecord, sessionID string) protocol.WorkItem {
	if runtime == nil || runtime.deps.Core == nil {
		return protocol.WorkItem{}
	}
	runtime.deps.Core.ViewMu.RLock()
	plan := runtime.SessionActivePlanLocked(sessionID)
	runtime.deps.Core.ViewMu.RUnlock()
	return WorkItemForRecord(record, plan, runtime.limits())
}

// TraceBlock returns the active session's provider-only WorkTable trace block.
func (runtime *Runtime) TraceBlock() string {
	return runtime.TraceBlockFor("")
}

// TraceBlockFor returns one session's unfinished task and async execution
// trace block without entering the shared write lock.
func (runtime *Runtime) TraceBlockFor(sessionID string) string {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Runtime == nil {
		return ""
	}
	runtime.deps.Core.ViewMu.RLock()
	viewID := runtime.deps.Core.Snapshot.Session.ID
	runtime.deps.Core.ViewMu.RUnlock()
	records := runtime.deps.Core.Deps.Runtime.TaskSnapshotFor(viewstate.TaskScope(sessionID, viewID))
	return BuildTraceBlock(TraceInputs{
		SessionID:     sessionID,
		ViewSessionID: viewID,
		Tasks:         records,
		AsyncRuns:     runtime.deps.Core.Deps.Runtime.AsyncRunsSnapshot(),
		TeamworkRuns:  switching.TeamworkJobCompletions(runtime.deps.Core.Deps.Runtime),
		Limits:        runtime.limits(),
	})
}

// RefreshSubagents updates the tree snapshot and synchronizes terminal tool
// records and task sources.
func (runtime *Runtime) RefreshSubagents() {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Engine == nil {
		return
	}
	tree := runtime.deps.Core.Deps.Engine.SubAgentTree()
	runtime.deps.Core.ViewMu.Lock()
	runtime.deps.Core.Snapshot.Runtime.SubAgentTree = tree
	runtime.deps.Core.ViewMu.Unlock()
	if runtime.deps.CloseTerminalSubagentToolRecords != nil {
		runtime.deps.CloseTerminalSubagentToolRecords(tree)
	}
	runtime.RefreshFromSources()
}

// RefreshFromSources synchronizes Plan/subagent sources and publishes the
// resulting WorkTable/task deltas.
func (runtime *Runtime) RefreshFromSources() {
	runtime.SyncFromSources()
	runtime.PublishTaskDeltas()
}

// StartLifecycleConsumers starts the WorkTable signal consumers once.
func (runtime *Runtime) StartLifecycleConsumers() {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Runtime == nil {
		return
	}
	if runtime.lifecycleStop == nil {
		runtime.lifecycleStop = make(chan struct{})
	}
	hostRuntime := runtime.deps.Core.Deps.Runtime
	NewLifecycle(LifecycleDeps{
		SubagentEvents:   hostRuntime.SubagentTreeEvents(),
		TaskChanges:      hostRuntime.TaskChangedChannel(),
		Stop:             runtime.lifecycleStop,
		RefreshSubagents: runtime.RefreshSubagents,
		TaskContext: func() (uint64, string, string) {
			runtime.deps.Core.ViewMu.RLock()
			defer runtime.deps.Core.ViewMu.RUnlock()
			return runtime.deps.Core.Snapshot.Revision, runtime.deps.Core.Snapshot.Chat.RequestID, runtime.deps.Core.Snapshot.Session.ID
		},
		PublishTaskChange: runtime.PublishTaskChanged,
		SafeCall:          runtime.SafeLifecycleCall,
		Async: AsyncConsumerDeps{
			AsyncEvents:    hostRuntime.AsyncRunEvents(),
			TeamworkEvents: switching.TeamworkJobEvents(hostRuntime),
			Refresh:        runtime.RefreshFromSources,
			TriggerAsync: func(triggered map[string]struct{}) int {
				if runtime.deps.AsyncTriggerEnabled != nil && !runtime.deps.AsyncTriggerEnabled() {
					return 0
				}
				if runtime.deps.TriggerAsync == nil {
					return 0
				}
				return runtime.deps.TriggerAsync(triggered)
			},
			TriggerTeamwork: func(triggered map[string]struct{}) int {
				if runtime.deps.AsyncTriggerEnabled != nil && !runtime.deps.AsyncTriggerEnabled() {
					return 0
				}
				if runtime.deps.TriggerTeamwork == nil {
					return 0
				}
				return runtime.deps.TriggerTeamwork(triggered)
			},
			ContinuationEvents: runtime.continuationEvents,
			TriggerContinuation: func(triggered map[string]struct{}, sessionID string) int {
				if runtime.deps.Continuation == nil {
					return 0
				}
				return runtime.deps.Continuation.TriggerGoalContinuations(sessionID, triggered)
			},
		},
	}).Start()
}

// StopLifecycleConsumers closes the lifecycle signals once.
func (runtime *Runtime) StopLifecycleConsumers() {
	if runtime == nil || runtime.lifecycleStop == nil {
		return
	}
	runtime.lifecycleOnce.Do(func() { close(runtime.lifecycleStop) })
}

// SafeLifecycleCall routes consumer panics to the lifecycle fault guard.
func (runtime *Runtime) SafeLifecycleCall(call func()) {
	defer func() {
		if recovered := recover(); recovered != nil && runtime.deps.HandleLifecycleFault != nil {
			runtime.deps.HandleLifecycleFault(recovered)
		}
	}()
	call()
}

// RefreshAfterTodoChange reruns the runtime projection and publishes runtime,
// WorkTable and task increments.
func (runtime *Runtime) RefreshAfterTodoChange() {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.View == nil {
		return
	}
	projection := runtime.deps.View.CollectRuntimeProjection(context.Background())
	runtime.deps.Core.ViewMu.Lock()
	runtime.deps.View.ApplyRuntimeProjectionLocked(projection)
	revision := runtime.deps.View.BumpLocked()
	requestID := runtime.deps.Core.Snapshot.Chat.RequestID
	sessionID := runtime.deps.Core.Snapshot.Session.ID
	items := protocol.CloneWorkItems(runtime.deps.Core.Snapshot.Runtime.WorkTable)
	batches := protocol.CloneWorkTableBatches(runtime.deps.Core.Snapshot.Runtime.WorkTableBatches)
	runtime.deps.Core.ViewMu.Unlock()
	if runtime.deps.RuntimeSnapshot != nil {
		runtime.publishSessionEvent(event.EventRuntimeChanged, revision, requestID, sessionID, runtime.deps.RuntimeSnapshot())
	}
	runtime.Publish(revision, requestID, items, batches)
}

// AsyncRuns returns the host async execution projection.
func (runtime *Runtime) AsyncRuns() []dto.AsyncRunRecord {
	if runtime == nil || runtime.deps.Core == nil || runtime.deps.Core.Deps.Runtime == nil {
		return nil
	}
	return runtime.deps.Core.Deps.Runtime.AsyncRunsSnapshot()
}

func (runtime *Runtime) limits() worktable.Limits {
	if runtime == nil || runtime.deps.Limits == nil {
		return worktable.Limits{}
	}
	return runtime.deps.Limits()
}

func (runtime *Runtime) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event {
	if runtime == nil || runtime.deps.PublishSessionEvent == nil {
		return event.Event{}
	}
	return runtime.deps.PublishSessionEvent(kind, revision, requestID, sessionID, payload)
}
