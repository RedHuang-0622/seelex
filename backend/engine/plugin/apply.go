package plugin

import (
	"errors"
	"fmt"
	"reflect"
)

// Step 是事务的一步：Do 执行正向操作；失败时按逆序执行已成功步骤的 Undo
// （可为 nil 表示无需回滚）。
type Step struct {
	Name string
	Do   func() error
	Undo func() error
}

// Transaction 顺序执行 steps：任一步 Do 失败时，按逆序回滚所有已成功步骤
// 的 Undo，并聚合正向错误与回滚错误返回。全部成功返回 nil。
// 它统一了插件域里"准备新态 → 失败逆序回滚 → 恢复旧态"的写法
// （Load 注册回滚 / Activate/Deactivate 的 prepare-restore 回滚）。
func Transaction(steps ...Step) error {
	var result error
	for index, step := range steps {
		if err := step.Do(); err != nil {
			result = fmt.Errorf("%s: %w", step.Name, err)
			for rollbackIndex := index - 1; rollbackIndex >= 0; rollbackIndex-- {
				if steps[rollbackIndex].Undo == nil {
					continue
				}
				if undoErr := steps[rollbackIndex].Undo(); undoErr != nil {
					result = errors.Join(
						result,
						fmt.Errorf("rollback %s: %w", steps[rollbackIndex].Name, undoErr),
					)
				}
			}
			return result
		}
	}
	return nil
}

// StateDiff 是两份插件定义快照的差异摘要（热更新审计面）。
type StateDiff struct {
	Added   []string
	Removed []string
	Updated []string
}

// DiffState 对比 current/next 插件定义：
//   - 新增 = 只存在于 next；
//   - 删除 = 只存在于 current；
//   - 修改 = 同名且定义不等（reflect.DeepEqual）。
//
// Added/Updated 保持 next 的稳定顺序；Removed 无顺序保证。
func DiffState(current, next map[string]Plugin) StateDiff {
	var diff StateDiff
	for name, plugin := range next {
		old, ok := current[name]
		if !ok {
			diff.Added = append(diff.Added, name)
			continue
		}
		if !reflect.DeepEqual(old, plugin) {
			diff.Updated = append(diff.Updated, name)
		}
	}
	for name := range current {
		if _, ok := next[name]; !ok {
			diff.Removed = append(diff.Removed, name)
		}
	}
	return diff
}
