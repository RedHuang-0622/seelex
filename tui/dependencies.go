package tui

import (
	"context"

	"github.com/RedHuang-0622/seelex/application"
)

// dependencies.go — TUI 自己的窄端口与装配面（M2，兼容门面退场）。
//
// 为什么端口按 owner 切：一个端口的形状必须与某个 owner 的方法逐字一致，装配处
// 才能直接交出 owner 指针（结构满足）。按"界面关切"切会把两个 owner 的方法塞进
// 同一个端口，那就又要一份门面把它们缝合起来——正是要删掉的东西。
//
// 为什么方法名跟着 owner 走：`Resolve` 而不是 `ResolveInteraction`、`Board` 而不是
// `ToolRecordBoard`。改名只可能发生在转发层，而转发层没有 owned 判据，属于禁止的
// 第二份实现。

// SnapshotReader 是当前视图会话的快照读数（唯一 owner：session/viewstate 的快照用例）。
type SnapshotReader interface {
	Snapshot() application.Snapshot
}

// EventReader 是视图事件订阅（唯一 owner：session/viewstate 的视图协调器）。
type EventReader interface {
	Subscribe(buffer int) application.Subscription
	// SubscribeSession 订阅指定会话；空 sessionID = 跟随当前视图会话。
	SubscribeSession(sessionID string, buffer int) (application.Subscription, error)
}

// Submitter 把一条输入提交给当前视图会话。
type Submitter interface {
	Submit(ctx context.Context, text string) error
}

// SuggestReader 是输入框补全读数。
type SuggestReader interface {
	Suggestions(input string) []application.Suggestion
}

// InteractionWriter 裁决一条待决交互（同意/拒绝/取消）。
type InteractionWriter interface {
	Resolve(ctx context.Context, interactionID, optionID string) error
}

// TurnStopper 中止正在进行的回合。
type TurnStopper interface {
	CancelChat(requestID string) bool
}

// EffortSwitcher 切换推理档位。owner 的用例不需要 ctx（档位是即时旋钮），
// 因此这里也不带——旧门面上的 ctx 形参从未被读过。
type EffortSwitcher interface {
	SwitchEffort(level string) error
}

// HistoryPager 是可见历史窗口的前后翻页。
type HistoryPager interface {
	LoadMoreHistory(limit int) error
	LoadLatestHistory() error
}

// QueueEditor 是排队输入的编辑面（调换顺序 / 撤回到输入框）。
// 只在回合运行期间生效（队列只承载运行中接受的输入）。
type QueueEditor interface {
	ReorderQueuedInput(sessionID string, from, to int) error
	RecallQueuedInput(sessionID string, index int) (string, error)
}

// ToolRecordReader 是工具记录看板读数（与 GUI「状态 → 工具记录」同源）。
type ToolRecordReader interface {
	Board() application.ToolRecordBoard
}

// Dependencies 是 TUI 需要的全部应用面：每个字段一个 owner，由 wiring/bootstrap 装配。
//
// Queue 与 ToolRecord 允许为 nil：未装配时分别给出明确提示、整块面板退场。
// 这两条出口在旧形态里是运行时类型断言（`model.app.(queueController)`），断言之所以
// 有效只是因为门面恰好没实现；改成显式 nil 判断后，能力有无由装配表说话，界面代码
// 不再探测自己的依赖。
type Dependencies struct {
	Snapshot    SnapshotReader
	Events      EventReader
	Submission  Submitter
	Completion  SuggestReader
	Interaction InteractionWriter
	Lifecycle   TurnStopper
	Effort      EffortSwitcher
	History     HistoryPager
	Queue       QueueEditor
	ToolRecord  ToolRecordReader
}
