package assemble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/obs"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/view/sessionaccess"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

var _ contract.AgentConversationPort = (*Runtime)(nil)

// ViewAgentConversation implements the execution-side half of the unified
// ConversationSession capability.
func (r *Runtime) ViewAgentConversation(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	records, status, recovery, err := r.agentConversationRecords(ctx, ref)
	if err != nil {
		return dto.SessionRecordPage{}, err
	}
	page := sessionaccess.PaginateRecords(ref.SessionID, records, request)
	page.Status = status
	page.Recovery = recovery
	return page, nil
}

// RecoverAgentConversation reports the shared recovery state without changing
// team plan, WorkItem status, Sessions, or worktrees.
func (r *Runtime) RecoverAgentConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	_, status, state, err := r.agentConversationRecords(ctx, ref)
	if err != nil {
		return dto.SessionRecovery{}, err
	}
	return dto.SessionRecovery{
		State:       state,
		SessionID:   strings.TrimSpace(ref.SessionID),
		Durable:     state != dto.RecoveryLive && state != dto.RecoveryMissing,
		RecoveredAt: time.Now(),
		ResumeNote:  status,
	}, nil
}

// ChatAgentConversation sends one user turn to the exact subagent/teammate
// Session window. It never settles a WorkItem and never accepts/merges work.
func (r *Runtime) ChatAgentConversation(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	if strings.TrimSpace(request.Message) == "" {
		return dto.ChatReceipt{}, errors.New("conversation: message is required")
	}
	if request.WaitMS == 0 {
		request.WaitMS = int(teamwork.TeammateMessageDefaultWait / time.Millisecond)
	}
	if maxWait := int(teamwork.TeammateMessageMaxWait / time.Millisecond); request.WaitMS > maxWait {
		request.WaitMS = maxWait
	}
	switch ref.Kind {
	case dto.ConversationTeammate:
		return r.chatTeammateConversation(ctx, ref, request.Message, request.WaitMS)
	case dto.ConversationSubagent:
		if request.WaitMS > 0 {
			timeout := time.Duration(request.WaitMS) * time.Millisecond
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		reply, err := r.chatSubagentConversation(ctx, ref, request.Message)
		receipt := dto.ChatReceipt{Accepted: err == nil, SessionID: ref.SessionID, Reply: reply, State: dto.RecoveryLive}
		if errors.Is(err, context.DeadlineExceeded) {
			receipt.Accepted, receipt.TimedOut = true, true
			return receipt, nil
		}
		return receipt, err
	default:
		return dto.ChatReceipt{}, fmt.Errorf("conversation: agent adapter does not handle kind %q", ref.Kind)
	}
}

func (r *Runtime) agentConversationRecords(ctx context.Context, ref dto.ConversationSessionRef) ([]dto.ConversationRecord, string, dto.SessionRecoveryState, error) {
	switch ref.Kind {
	case dto.ConversationTeammate:
		return r.teammateConversationRecords(ctx, ref)
	case dto.ConversationSubagent:
		return r.subagentConversationRecords(ref)
	default:
		return nil, "", dto.RecoveryMissing, fmt.Errorf("conversation: agent adapter does not handle kind %q", ref.Kind)
	}
}

func (r *Runtime) teammateConversationRecords(ctx context.Context, ref dto.ConversationSessionRef) ([]dto.ConversationRecord, string, dto.SessionRecoveryState, error) {
	item, err := r.teammateConversationTarget(ctx, ref)
	if err != nil {
		return nil, "", dto.RecoveryMissing, err
	}
	var (
		history  []types.Message
		status   string
		recovery = dto.RecoveryMissing
		record   storeworkunit.Record
		hasRec   bool
	)
	if live, ok := r.teammateLiveSession(ref.SessionID); ok {
		history = live.History
		status = "live"
		recovery = dto.RecoveryLive
	}
	index := r.teamSceneIndex(ref.MainSessionID)
	reader := r.teamUnitReader(ref.MainSessionID, index)
	record, hasRec, err = reader.Record(teamwork.SceneNodeID(item.Role, item.ID))
	if err != nil {
		return nil, "", dto.RecoveryMissing, err
	}
	if recovery != dto.RecoveryLive {
		if hasRec {
			history = record.History
			status = record.Status
			recovery = dto.RecoveryCold
			if workunit.InFlight(record.Status) {
				recovery = dto.RecoveryInterrupted
			}
		}
	}
	records := conversationRecordsFromHistory(history)
	if hasRec && (recovery == dto.RecoveryInterrupted || recovery == dto.RecoveryCold) {
		records = prependRecoveryNote(records, record, workunit.KindTeammate)
	}
	effective := ""
	if hasRec {
		effective = strings.TrimSpace(record.Unit.SystemPrompt)
	}
	if effective == "" {
		effective = r.roleTurnSystemPrompt(item.Role)
	}
	records = ensureSystemRecord(records, effective)
	if hasRec {
		for _, stage := range workunit.ProgressOf(workunit.KindTeammate, record).Stages {
			records = append(records, dto.ConversationRecord{
				ID: "stage:" + stage.Stage, Kind: dto.ConversationRecordStage,
				Content: stage.Stage, Reasoning: stage.Preview,
			})
		}
	}
	return records, status, recovery, nil
}

func (r *Runtime) subagentConversationRecords(ref dto.ConversationSessionRef) ([]dto.ConversationRecord, string, dto.SessionRecoveryState, error) {
	nodeID := strings.TrimSpace(ref.SessionID)
	if nodeID == "" {
		return nil, "", dto.RecoveryMissing, errors.New("conversation: subagent session ID is required")
	}
	history, live := r.subagentSessions.Conversation(nodeID)
	recovery := dto.RecoveryMissing
	status := ""
	if live {
		recovery = dto.RecoveryLive
		status = "live"
	}
	record, hasRecord := r.loadSubagentRecord(ref.MainSessionID, nodeID)
	if recovery != dto.RecoveryLive && hasRecord {
		history = record.History
		status = record.Status
		recovery = dto.RecoveryCold
		if workunit.InFlight(record.Status) {
			recovery = dto.RecoveryInterrupted
		}
	}
	records := conversationRecordsFromHistory(history)
	if hasRecord && recovery != dto.RecoveryLive {
		records = prependRecoveryNote(records, record, workunit.KindSubagent)
	}
	for index, stage := range r.subagentSessions.StageLogs(nodeID) {
		records = append(records, dto.ConversationRecord{
			ID: fmt.Sprintf("stage:%d", index+1), Kind: dto.ConversationRecordStage,
			Content: stage.Stage, Reasoning: stage.Preview,
		})
	}
	return records, status, recovery, nil
}

func (r *Runtime) teammateConversationTarget(ctx context.Context, ref dto.ConversationSessionRef) (storeteamwork.WorkItem, error) {
	mainSessionID := strings.TrimSpace(ref.MainSessionID)
	if mainSessionID == "" {
		return storeteamwork.WorkItem{}, errors.New("conversation: teammate main session ID is required")
	}
	coordinator, err := r.coordinatorForSession(mainSessionID)
	if err != nil {
		return storeteamwork.WorkItem{}, err
	}
	views, err := coordinator.Items(ctx)
	if err != nil {
		return storeteamwork.WorkItem{}, err
	}
	for _, view := range views {
		item := view.Item
		if strings.TrimSpace(item.ID) != strings.TrimSpace(ref.WorkItemID) {
			continue
		}
		if role := strings.TrimSpace(ref.Role); role != "" && role != strings.TrimSpace(item.Role) {
			return storeteamwork.WorkItem{}, fmt.Errorf("conversation: WorkItem %q belongs to role %q, not %q", item.ID, item.Role, role)
		}
		if sessionID := strings.TrimSpace(item.SessionID); sessionID != "" && sessionID != strings.TrimSpace(ref.SessionID) {
			return storeteamwork.WorkItem{}, fmt.Errorf("conversation: WorkItem %q uses session %q, not %q", item.ID, sessionID, ref.SessionID)
		}
		return item, nil
	}
	return storeteamwork.WorkItem{}, fmt.Errorf("conversation: WorkItem %q is not in the team plan", ref.WorkItemID)
}

func (r *Runtime) chatTeammateConversation(ctx context.Context, ref dto.ConversationSessionRef, message string, waitMS int) (dto.ChatReceipt, error) {
	item, err := r.teammateConversationTarget(ctx, ref)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	coordinator, err := r.coordinatorForSession(ref.MainSessionID)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	plan, err := coordinator.Plan(ctx)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	_, ok := teamwork.MemberOf(plan, item.Role)
	if !ok {
		return dto.ChatReceipt{}, fmt.Errorf("conversation: role %q is not in the team plan", item.Role)
	}
	systemPrompt := r.roleTurnSystemPrompt(item.Role)
	record, hasRecord, err := r.teamUnitReader(ref.MainSessionID, r.teamSceneIndex(ref.MainSessionID)).Record(teamwork.SceneNodeID(item.Role, item.ID))
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	if hasRecord && strings.TrimSpace(record.Unit.SystemPrompt) != "" {
		systemPrompt = record.Unit.SystemPrompt
	}
	dispatch, err := coordinator.DeclareTeammateMessage(ctx, ref, message, systemPrompt)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	r.teamworkMu.Lock()
	manager := r.teamworkJobs
	r.teamworkMu.Unlock()
	if manager == nil {
		return dto.ChatReceipt{}, errors.New("conversation: jobs manager is not assembled")
	}
	_, jobRecord, err := manager.Peek(ctx, dispatch.Handle, jobs.FetchBudget{WaitMS: waitMS})
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	receipt := dto.ChatReceipt{
		Accepted:  true,
		SessionID: ref.SessionID,
		State:     dto.RecoveryLive,
	}
	if jobRecord.Running() {
		receipt.TimedOut = true
		receipt.JobHandle = string(dispatch.Handle)
		return receipt, nil
	}
	reply, jobRecord, err := manager.Fetch(ctx, dispatch.Handle, jobs.FetchBudget{WaitMS: -1})
	if err != nil && !errors.Is(err, jobs.ErrRetired) {
		return dto.ChatReceipt{}, err
	}
	receipt.Reply = strings.TrimSpace(reply)
	if jobRecord.State == jobs.StateFailed || jobRecord.State == jobs.StateKilled {
		if strings.TrimSpace(jobRecord.Summary) != "" {
			return receipt, errors.New(jobRecord.Summary)
		}
		return receipt, fmt.Errorf("conversation: teammate chat ended in state %s", jobRecord.State)
	}
	return receipt, nil
}

func (r *Runtime) chatSubagentConversation(ctx context.Context, ref dto.ConversationSessionRef, message string) (string, error) {
	nodeID := strings.TrimSpace(ref.SessionID)
	sess := r.subagentSessions.Session(nodeID)
	if sess == nil {
		if r.subagentSessionFactory == nil {
			return "", errors.New("conversation: subagent session factory is not configured")
		}
		var resume []types.Message
		if record, hasRecord := r.loadSubagentRecord(ref.MainSessionID, nodeID); hasRecord {
			resume = record.History
		}
		created, err := r.subagentSessionFactory.NewWithIDForWithHistory(ref.MainSessionID, nodeID, nil, resume)
		if err != nil {
			return "", err
		}
		sess = created
	}
	chatCtx := obs.WithTelemetrySessionID(ctx, nodeID)
	chatCtx = dto.WithSceneScope(chatCtx, dto.SceneScope{
		SceneID: nodeID, Role: dto.RoleSubAgent, BranchID: nodeID,
	})
	output, err := sess.ChatStream(chatCtx, message, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (r *Runtime) loadSubagentRecord(mainSessionID, nodeID string) (storeworkunit.Record, bool) {
	if r == nil || r.nodeSessionStore == nil {
		return storeworkunit.Record{}, false
	}
	projectID := r.sessionProjectIDFor(mainSessionID)
	if projectID == "" {
		projectID = r.nodeSessionStore.ProjectID()
	}
	record, err := r.nodeSessionStore.Load(projectID, mainSessionID, nodeID)
	if err != nil {
		return storeworkunit.Record{}, false
	}
	return record, true
}

func conversationRecordsFromHistory(history []types.Message) []dto.ConversationRecord {
	records := make([]dto.ConversationRecord, 0, len(history)+1)
	for index, message := range history {
		id := fmt.Sprintf("message-%d", index+1)
		role := strings.TrimSpace(message.Role)
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		switch role {
		case "system":
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordSystem, Role: role, Content: content,
			})
		case "user":
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordUser, Role: role, Content: content,
			})
		case "assistant":
			if strings.TrimSpace(message.ReasoningContent) != "" {
				records = append(records, dto.ConversationRecord{
					ID: id + ":reasoning", Kind: dto.ConversationRecordReasoning,
					Role: role, Reasoning: message.ReasoningContent,
				})
			}
			if strings.TrimSpace(content) != "" {
				records = append(records, dto.ConversationRecord{
					ID: id, Kind: dto.ConversationRecordAssistant, Role: role, Content: content,
				})
			}
			for callIndex, call := range message.ToolCalls {
				records = append(records, dto.ConversationRecord{
					ID: fmt.Sprintf("%s:call-%d", id, callIndex+1), Kind: dto.ConversationRecordToolCall,
					Role: role, Content: call.Function.Arguments, ToolName: call.Function.Name,
					ToolCallID: call.ID, Status: "requested",
				})
			}
		case "tool":
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordToolResult, Role: role, Content: content,
				ToolName: message.Name, ToolCallID: message.ToolCallID, Status: "success",
			})
		}
	}
	return records
}

func ensureSystemRecord(records []dto.ConversationRecord, prompt string) []dto.ConversationRecord {
	if strings.TrimSpace(prompt) == "" {
		return records
	}
	for _, record := range records {
		if record.Kind == dto.ConversationRecordSystem {
			return records
		}
	}
	prefix := []dto.ConversationRecord{{
		ID: "system:effective", Kind: dto.ConversationRecordSystem,
		Role: workunit.RecoveryNoteRole, Content: prompt,
	}}
	return append(prefix, records...)
}

func prependRecoveryNote(records []dto.ConversationRecord, record storeworkunit.Record, kind workunit.Kind) []dto.ConversationRecord {
	note := workunit.RecoveryNote(kind, record)
	if strings.TrimSpace(note) == "" {
		return records
	}
	prefix := []dto.ConversationRecord{{
		ID: "recovery:" + record.NodeID, Kind: dto.ConversationRecordRecoveryNote,
		Role: workunit.RecoveryNoteRole, Content: note,
	}}
	return append(prefix, records...)
}
