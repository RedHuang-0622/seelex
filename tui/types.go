// ── 共享类型定义 ──────────────────────────────────────────────

package tui

// ── 内部事件 ───────────────────────────────────────────────────

type toolEvent struct {
	kind      string // "start" | "complete"
	name      string
	id        string
	arguments string
	result    string
	err       error
}

type streamChunk struct {
	text string
	done bool
	err  error
	tool *toolEvent
}

// ── 命令消息 ───────────────────────────────────────────────────

type messageView struct {
	role    string
	content string
	extra   string
}

// ── 审批 ───────────────────────────────────────────────────────

type promptRequest struct {
	question string
	choices  []string
	ch       chan string
}

// ── 选择器 ─────────────────────────────────────────────────────

type selectState int

const (
	selNone selectState = iota
	selSession
	selAccount
	selModel
)

type selectItem struct {
	id    string
	label string
	desc  string
}

// Keep types alive
var _ = promptRequest{}
