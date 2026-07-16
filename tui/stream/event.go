package stream

// Event 是工具调用的事件（对应原 tui.toolEvent）。
type Event struct {
	Kind      string // "start" | "complete"
	Name      string
	ID        string
	Arguments string
	Result    string
	Err       error
}
