package execution

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

// CompletionDeps supplies the two authoritative job tables and the only
// allowed side effect: submitting an idle session's completion turn.
type CompletionDeps struct {
	AsyncRuns          func() []dto.AsyncRunRecord
	TeamworkRuns       func() []dto.TeamworkJobCompletionRecord
	SessionIdle        func(string) bool
	SubmitConversation func(context.Context, string, string) error
	EvidenceChars      func() int
}

// CompletionApplication scans terminal background jobs and wakes only idle
// sessions. Job registries remain the sole source of job facts.
type CompletionApplication struct {
	deps CompletionDeps
}

// NewCompletion constructs the background-completion application.
func NewCompletion(deps CompletionDeps) *CompletionApplication {
	return &CompletionApplication{deps: deps}
}

// TriggerAsyncCompletions scans asynchronous jobs once and returns the number
// of terminal jobs skipped because their session was busy.
func (application *CompletionApplication) TriggerAsyncCompletions(triggered map[string]struct{}) int {
	pending := 0
	records := application.deps.AsyncRuns()
	present := make(map[string]struct{}, len(records))
	for _, record := range records {
		handle := strings.TrimSpace(record.Handle)
		if handle == "" {
			continue
		}
		present[handle] = struct{}{}
		if !switching.AsyncCompletionTriggers(record.State) {
			continue
		}
		sessionID := strings.TrimSpace(record.SessionID)
		if sessionID == "" {
			continue
		}
		if _, done := triggered[handle]; done {
			continue
		}
		if !application.deps.SessionIdle(sessionID) {
			pending++
			continue
		}
		triggered[handle] = struct{}{}
		if err := application.deps.SubmitConversation(
			context.Background(),
			sessionID,
			application.asyncCompletionPrompt(record),
		); err != nil {
			log.Printf("[async] 后台作业 %s 终态触发对话失败（session=%s）：%v", handle, sessionID, err)
		}
	}
	for handle := range triggered {
		if _, ok := present[handle]; !ok {
			delete(triggered, handle)
		}
	}
	return pending
}

// TriggerTeamworkJobCompletions scans teammate jobs once and returns the number
// skipped because their session was busy.
func (application *CompletionApplication) TriggerTeamworkJobCompletions(triggered map[string]struct{}) int {
	pending := 0
	records := application.deps.TeamworkRuns()
	if len(records) == 0 {
		return 0
	}
	present := make(map[string]struct{}, len(records))
	for _, record := range records {
		handle := strings.TrimSpace(record.Handle)
		if handle == "" {
			continue
		}
		key := switching.TeamworkTriggeredPrefix + handle
		present[key] = struct{}{}
		if !switching.AsyncCompletionTriggers(record.State) {
			continue
		}
		sessionID := strings.TrimSpace(record.SessionID)
		if sessionID == "" {
			continue
		}
		if _, done := triggered[key]; done {
			continue
		}
		if !application.deps.SessionIdle(sessionID) {
			pending++
			continue
		}
		triggered[key] = struct{}{}
		if err := application.deps.SubmitConversation(
			context.Background(),
			sessionID,
			application.teamworkCompletionPrompt(record),
		); err != nil {
			log.Printf("[teamwork] teammate 作业 %s 终态触发对话失败（session=%s）：%v", handle, sessionID, err)
		}
	}
	for key := range triggered {
		if !strings.HasPrefix(key, switching.TeamworkTriggeredPrefix) {
			continue
		}
		if _, ok := present[key]; !ok {
			delete(triggered, key)
		}
	}
	return pending
}

func (application *CompletionApplication) asyncCompletionPrompt(record dto.AsyncRunRecord) string {
	var body strings.Builder
	body.WriteString("后台作业已完成，请取回结果并继续：\n")
	fmt.Fprintf(&body, "- handle: %s（kind=%s）\n", record.Handle, asyncPromptKind(record.Kind))
	fmt.Fprintf(&body, "- 状态: %s · exit=%d\n", record.State, record.ExitCode)
	if title := strings.TrimSpace(record.Description); title != "" {
		fmt.Fprintf(&body, "- 作业: %s\n", worktable.TruncateWorkEvidence(title, 200))
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&body, "- 摘要: %s\n", worktable.TruncateWorkEvidence(summary, application.evidenceChars()))
	}
	fmt.Fprintf(&body, "取回全文用 bg_manage(op=fetch, handle=%q)；确认不需要了用 bg_manage(op=done, handle=%q) 销项。",
		record.Handle, record.Handle)
	return body.String()
}

func (application *CompletionApplication) teamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord) string {
	var body strings.Builder
	body.WriteString("teammate 作业已完成，请收口：\n")
	role := strings.TrimSpace(record.Role)
	item := strings.TrimSpace(record.WorkItem)
	switch {
	case role != "" && item != "":
		fmt.Fprintf(&body, "- teammate: %s（工作项 %s）\n", role, item)
	case role != "":
		fmt.Fprintf(&body, "- teammate: %s\n", role)
	default:
		body.WriteString("- teammate: —\n")
	}
	fmt.Fprintf(&body, "- handle: %s（kind=%s）\n", record.Handle, asyncPromptKind(record.Kind))
	fmt.Fprintf(&body, "- 状态: %s · exit=%d\n", record.State, record.ExitCode)
	if description := strings.TrimSpace(record.Description); description != "" {
		fmt.Fprintf(&body, "- 作业: %s\n", worktable.TruncateWorkEvidence(description, 200))
	}
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		fmt.Fprintf(&body, "- 摘要: %s\n", worktable.TruncateWorkEvidence(summary, application.evidenceChars()))
	}
	fmt.Fprintf(&body,
		"读结论用 team_context（非消费读，不推进游标）；产物正文用 jobs_manage(op=observe|fetch, handle=%q)；"+
			"复核完用 team_accept / team_fail 收口（accept 才会打开下游依赖闸门）。",
		record.Handle)
	return body.String()
}

func (application *CompletionApplication) evidenceChars() int {
	if application == nil || application.deps.EvidenceChars == nil {
		return 0
	}
	return application.deps.EvidenceChars()
}

func asyncPromptKind(kind string) string {
	if trimmed := strings.TrimSpace(kind); trimmed != "" {
		return trimmed
	}
	return "process"
}
