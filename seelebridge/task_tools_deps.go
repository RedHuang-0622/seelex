package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/task"

// taskToolsDeps 把 Runtime 能力面注入 task 工具族（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) taskToolsDeps() task.Deps {
	return task.Deps{
		RegisterTool:         r.RegisterTool,
		ReplaceTodo:          r.tasks.ReplaceTodo,
		AppendTodo:           r.tasks.AppendTodo,
		SetTodoStatusByIndex: r.tasks.SetTodoStatusByIndex,
		TodoSnapshot:         r.TodoSnapshot,
		TaskAdd:              r.TaskAdd,
		TodoMaxItems:         r.limits.TodoMaxItems,
	}
}
