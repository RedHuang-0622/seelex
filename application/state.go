package application

import "time"

type Snapshot struct {
	Revision       uint64       `json:"revision"`
	Session        SessionState `json:"session"`
	Conversation   []Message    `json:"conversation"`
	Chat           ChatState    `json:"chat"`
	Runtime        RuntimeState `json:"runtime"`
	Interaction    *Interaction `json:"interaction,omitempty"`
	Capabilities   Capabilities `json:"capabilities"`
	HistoryOffset  int          `json:"history_offset"`  // 0 = oldest message at start; >0 = older messages exist in store
	TotalMessages  int          `json:"total_messages"`  // total count in store (0 for live session, set after resume)
	HasMoreHistory bool         `json:"has_more_history"` // true if older messages can be loaded via LoadMoreHistory
}

type SessionState struct {
	ID string `json:"id"`
}
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content,omitempty"`
	Tool      *ToolCall `json:"tool,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type ToolCall struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Status    string        `json:"status"`
	Duration  time.Duration `json:"duration,omitempty"`
}
type ChatState struct {
	Running   bool      `json:"running"`
	RequestID string    `json:"request_id,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}
type RuntimeState struct {
	Model        string      `json:"model"`
	Provider     string      `json:"provider"`
	Account      string      `json:"account,omitempty"`
	Plugin       string      `json:"plugin,omitempty"`
	VisibleTools []Tool      `json:"visible_tools"`
	Skills       []SkillInfo `json:"skills"`
	Tokens       string      `json:"tokens"`
}
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type PluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type AccountInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
}
type SessionInfo struct {
	ID         string    `json:"id"`
	UpdatedAt  time.Time `json:"updated_at"`
	TokenCount int       `json:"token_count"`
}
type Interaction struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Title    string              `json:"title"`
	Question string              `json:"question,omitempty"`
	Risk     string              `json:"risk,omitempty"`
	ToolName string              `json:"tool_name,omitempty"`
	Preview  string              `json:"preview,omitempty"`
	Options  []InteractionOption `json:"options"`
	OpenedAt time.Time           `json:"opened_at"`
	Timeout  time.Duration       `json:"timeout,omitempty"`
}
type InteractionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Style       string `json:"style,omitempty"`
}
type Capabilities struct {
	SessionResume       bool   `json:"session_resume"`
	SessionResumeReason string `json:"session_resume_reason,omitempty"`
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	copySnapshot := snapshot
	copySnapshot.Conversation = append([]Message(nil), snapshot.Conversation...)
	// 标量字段 (HistoryOffset, TotalMessages, HasMoreHistory) 已值拷贝
	for index := range copySnapshot.Conversation {
		if copySnapshot.Conversation[index].Tool != nil {
			tool := *copySnapshot.Conversation[index].Tool
			copySnapshot.Conversation[index].Tool = &tool
		}
	}
	copySnapshot.Runtime.VisibleTools = append([]Tool(nil), snapshot.Runtime.VisibleTools...)
	copySnapshot.Runtime.Skills = append([]SkillInfo(nil), snapshot.Runtime.Skills...)
	if snapshot.Interaction != nil {
		interaction := *snapshot.Interaction
		interaction.Options = append([]InteractionOption(nil), snapshot.Interaction.Options...)
		copySnapshot.Interaction = &interaction
	}
	return copySnapshot
}
