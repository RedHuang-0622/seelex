package seelebridge

import (
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/seelebridge/task"
)

// todo_tool.go 只保留 Runtime 公开 facade 与工具注册委托；
// todolist/taskadd 工具实现已迁入 seelebridge/task.Tools（Deps 注入）。

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；
// 主代理每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (r *Runtime) TodoSnapshot() []TodoItem {
	if r == nil || r.tasks == nil {
		return nil
	}
	records := r.tasks.TodoSnapshot()
	items := make([]TodoItem, 0, len(records))
	for _, record := range records {
		items = append(items, taskToTodoItem(record))
	}
	return items
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (r *Runtime) SetTodoStatus(index int, status TodoItemStatus) error {
	if r == nil || r.tasks == nil {
		return errors.New("todolist: unavailable")
	}
	if !validTodoStatus(status) {
		return fmt.Errorf("todolist: invalid status %q (want pending|doing|done)", status)
	}
	_, err := r.tasks.SetTodoStatusByIndex(index, todoToTaskStatus(status))
	return err
}

// validTodoStatus 校验清单项状态（todo 三态）。
func validTodoStatus(status TodoItemStatus) bool {
	return status == TodoItemPending || status == TodoItemDoing || status == TodoItemDone
}

// registerTodoTools 注册 todolist 工具族（委托 task.Tools；RegisterBuiltins 内调用）。
func (r *Runtime) registerTodoTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTodoTools()
}

// registerTaskTools 注册主动任务工具 taskadd（同上委托）。
func (r *Runtime) registerTaskTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTaskTools()
}
