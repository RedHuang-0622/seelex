package cold

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

// TaskDeps supplies compaction, transcript and task restore callbacks.
type TaskDeps struct {
	LoadCompactions func(location session_runtime.Location, sessionID string) ([]model.ContextCompaction, bool, error)
	LatestUser      func(messages []model.Message) string
	RetainedFrom    func(transcript []model.TranscriptEvent, compactions []model.ContextCompaction) int
	Restore         func(sessionID string, state task_context.RestoredTaskState)
	Reset           func()
}

// TaskApplication owns cold task/plan/checkpoint restoration.
type TaskApplication struct {
	deps TaskDeps
}

// NewTask constructs the cold-task application.
func NewTask(deps TaskDeps) *TaskApplication {
	return &TaskApplication{deps: deps}
}

// RestoreLocked restores task state under the view lock.
func (application *TaskApplication) RestoreLocked(
	sessionID string,
	location session_runtime.Location,
	record model.SessionRecord,
	transcript []model.TranscriptEvent,
	transcriptRenumbered bool,
	hasRecord bool,
) {
	if !hasRecord {
		application.deps.Reset()
		return
	}
	transcriptSeq := uint64(0)
	if len(transcript) > 0 {
		transcriptSeq = transcript[len(transcript)-1].Seq
	}
	if record.Projection != nil &&
		record.Projection.Checkpoint.CoversEventRange.End > transcriptSeq {
		transcriptSeq = record.Projection.Checkpoint.CoversEventRange.End
	}
	var contextCompactions []model.ContextCompaction
	if storedTask := record.Execution.Task; storedTask != nil {
		contextCompactions = append([]model.ContextCompaction(nil), storedTask.ContextCompactions...)
	}
	if len(contextCompactions) == 0 {
		if records, ok, _ := application.deps.LoadCompactions(location, sessionID); ok {
			contextCompactions = records
		}
	}
	retainedFrom := 0
	if !transcriptRenumbered {
		retainedFrom = application.deps.RetainedFrom(transcript, contextCompactions)
	}
	application.deps.Restore(sessionID, task_context.RestoredTaskState{
		PlanStack:           session_runtime.CloneSessionPlanStack(record.PlanStack),
		ActivePlanID:        record.ActivePlanID,
		Transcript:          transcript,
		TranscriptSeq:       transcriptSeq,
		Checkpoints:         record.Checkpoints,
		ToolResults:         record.ToolResults,
		Projection:          record.Projection,
		FallbackObjective:   application.deps.LatestUser(record.Conversation.Messages),
		ContextCompactions:  contextCompactions,
		ContextRetainedFrom: retainedFrom,
	})
}
