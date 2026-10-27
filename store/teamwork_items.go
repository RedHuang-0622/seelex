package store

// teamwork_items.go — teamwork 编排面的**第二层**：里程碑里的 Work Item（甘特图节点）
// 与「一个 Work Item 一个 Session + 一个 worktree」的绑定账本的根包兼容出口。
//
// 口径不变：
//
//   - **Milestone 是屏障，Work Item 是调度单位**；
//   - **一个 Work Item 一个 Session + 一个 worktree**：同一个人在同一个里程碑里承担
//     多个 Work Item，每个都拿到自己的会话与工作区，避免上下文与改动互相污染；
//   - **绑定关系落 JSONL（KV 语义）**：`{work_item → session_id + worktree}` 是
//     可追加、可回读的事实流水（只追加、不重写），读侧按 Work Item 取最后一行。
//
// M8 的所有权裁决：绑定账本的落盘实现（路径、追加、崩溃残尾读取）已迁入
// `store/teamwork`（`store/teamwork/store.go` 的 AppendBinding / ReadBindings）。
// 本文件只保留同身份 alias 与单层转发；折叠规则本身在 `store/teamwork/model.go`。

import (
	"context"
	"errors"

	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// Work Item 的状态取值（空 = pending，与里程碑"空 = pending"同口径）。
const (
	TeamworkItemPending = storeteamwork.ItemPending
	TeamworkItemRunning = storeteamwork.ItemRunning
	TeamworkItemReview  = storeteamwork.ItemReview
	TeamworkItemDone    = storeteamwork.ItemDone
	TeamworkItemFailed  = storeteamwork.ItemFailed
)

// TeamworkWorkItem 是 contract/dto.TeamworkWorkItem 的持久化兼容 alias。
type TeamworkWorkItem = storeteamwork.WorkItem

// TeamworkWorkItemStatusOrPending 是 contract/dto 自由函数的兼容入口。
func TeamworkWorkItemStatusOrPending(item TeamworkWorkItem) string {
	return storeteamwork.WorkItemStatusOrPending(item)
}

// TeamworkWorkItemTerminal 是 contract/dto 自由函数的兼容入口。
func TeamworkWorkItemTerminal(item TeamworkWorkItem) bool {
	return storeteamwork.WorkItemTerminal(item)
}

// TeamworkBinding 是「一个 Work Item ↔ 一个 Session + 一个 worktree」的绑定事实。
//
// 它是**可追加的事件流**而不是可变状态：绑定一次记一行，释放时再追加一行
// （Released=true）。读侧按 WorkItem 取最后一行 = 当前绑定；生命周期（team_close
// 之后一并结束）由最后一行是不是 Released 表达，而不是靠"删掉那一行"。
type TeamworkBinding = storeteamwork.Binding

// TeamworkBindings 把绑定流水折成"每个 Work Item 当前的绑定"。
func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding {
	return storeteamwork.Bindings(rows)
}

// AppendTeamworkBinding 追加一行绑定事实（只追加、不重写；崩溃残尾由读侧跳过）。
func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.AppendBinding(layoutScope(key), binding)
}

// ReadTeamworkBindings 读回全部绑定事实（按文件顺序 = 发生顺序）。
func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if repository.layout == nil {
		return nil, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.ReadBindings(layoutScope(key))
}
