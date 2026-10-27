package bootstrap

// frontend_ports.go 是 headless 控制面与后端诊断台的装配表：一行一个端口，右值是
// 拥有该用例的组件指针。
//
// 绑定语句本身就是编译期结构断言（左端字段类型是 consumer 接口、右端是具体 owner
// 指针），因此这里不写适配器，也不再需要 `_ headless.X = (*owner)(nil)` 一行。
// 每一行右值都与兼容门面（`application/compat`，M4 删除）转发的字段逐字相同：门面
// 每个方法都转发到同一个 owner 的同名方法，迁移前后判据落在同一处。
//
// 三个前端各自声明自己的 dependencies（tui/、headless/、console/），共同点是只按
// owner 切分；禁止为了复用把三份接口合并回一个 application 级宽接口。

import (
	"github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/console"
	"github.com/RedHuang-0622/seelex/headless"
)

func headlessDependencies(components assembly.Components) headless.Dependencies {
	return headless.Dependencies{
		Events:      components.Session.View,
		Snapshot:    components.Session.Snapshot,
		Turn:        components.Runtime.Lifecycle,
		Submission:  components.Conversation.Submission,
		Draft:       components.Session.DraftEntry,
		Resume:      components.Session.Resume,
		Fork:        components.Session.Fork,
		Interaction: components.Session.Interaction,
		History:     components.Conversation.History,
		Catalog:     components.Session.Query,
		Workspace:   components.Workspace.Switch,
		Policy:      components.Policy.Switch,
		Perf:        components.Admin.Perf,
		ToolRecords: components.Execution.ToolRecords,
		Roles:       components.Policy.RoleSessions,
		Goal:        components.Admin.Goal,
		Subagents:   components.Execution.Subagent,
	}
}

func consoleDependencies(components assembly.Components) console.Dependencies {
	return console.Dependencies{
		Submission: components.Conversation.Submission,
		Turn:       components.Runtime.Lifecycle,
		Events:     components.Session.View,
	}
}
