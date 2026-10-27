package store

// teamwork_items.go — teamwork 编排面的**第二层**：里程碑里的 Work Item（甘特图节点）
// 与「一个 Work Item 一个 Session + 一个 worktree」的绑定账本。
//
// 为什么要有这一层（2026-10-03 重构）：
//
//   - **Milestone 是屏障，Work Item 是调度单位**。阶段（stages）只回答"哪个角色先上"，
//     回答不了"这一步具体做什么、谁做、做完的判据是什么"；而 leader 排活、人看板、
//     依赖约束都要求工作是一个**有名字、有目标、有归属、有依赖**的实体。
//   - **一个 Work Item 一个 Session + 一个 worktree**：工作拆分之后专项专做，
//     隔离粒度从"人"下沉到"这一件事"。同一个人在同一个里程碑里承担多个 Work Item，
//     每个都拿到自己的会话与工作区——否则两次工作的上下文与改动会互相污染。
//   - **绑定关系落 JSONL（KV 语义）**：`{work_item → session_id + worktree}` 是
//     可追加、可回读的事实，生命周期与 Session 一致（team_close 之后一并结束）。
//     它不进计划 head：计划是"谁、什么顺序"的整份替换型内容，而绑定是**发生过的
//     事实流水**（与 events.jsonl 同一口径：只追加、不重写）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// teamworkBindingFile 是 Work Item ↔ Session/worktree 绑定账本的文件名（追加、不重写）。
const teamworkBindingFile = "worktrees.jsonl"

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
//
// 折叠规则**只看最后一行**：绑定与释放都是追加，最后一行说了算（KV 语义）。
// 已释放的项不出现在返回值里——"没有绑定"就是"没有在跑的隔离"。
func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding {
	return storeteamwork.Bindings(rows)
}

// teamworkBindingsPath 返回绑定账本路径。
func (store *storeEngine) teamworkBindingsPath(key Key) string {
	return filepath.Join(store.teamworkDir(key), teamworkBindingFile)
}

// AppendTeamworkBinding 追加一行绑定事实（只追加、不重写；崩溃残尾由读侧跳过）。
func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	if strings.TrimSpace(binding.WorkItem) == "" {
		return errors.New("teamwork: binding.work_item is required")
	}
	if binding.At == 0 {
		binding.At = time.Now().UTC().Unix()
	}
	line, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("teamwork: encode binding: %w", err)
	}
	store := repository.layout
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	dir := store.teamworkDir(key)
	lock := store.mu(key, moduleTeamwork)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, teamworkBindingFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("teamwork: open binding log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("teamwork: append binding log: %w", err)
	}
	return nil
}

// ReadTeamworkBindings 读回全部绑定事实（按文件顺序 = 发生顺序）。
func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if repository.layout == nil {
		return nil, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	file, err := storeroot.ReadSharedFile(repository.layout.teamworkBindingsPath(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []TeamworkBinding{}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(file), "\n")
	bindings := make([]TeamworkBinding, 0, len(lines))
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var binding TeamworkBinding
		if err := json.Unmarshal([]byte(trimmed), &binding); err != nil {
			if index == len(lines)-1 {
				continue // 崩溃残尾
			}
			return nil, fmt.Errorf("teamwork: decode binding row %d: %w", index, err)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}
