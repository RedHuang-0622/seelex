package console

// dependencies.go 声明后端诊断台（console）自己的 consumer-owned 窄端口。
//
// 与 TUI/headless 同一口径：端口按 owner 切分，一个字段对应一个拥有该用例的
// 组件，方法名逐字等于 owner 的方法名（改名只发生在转发层，而转发层正是本次
// 要删除的兼容层）。旧形态里 `backendApplication` 把 Submit/WaitForIdle/Subscribe
// 三件事写在一个接口里，只有兼容门面能同时满足它；拆开后装配表直接把三个
// owner 指针交出来，绑定语句本身就是编译期结构断言。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application"
)

// Submitter 是诊断台唯一的写入口（owner：Conversation.Submission）。
// 控制台刻意只做观察面：所有真实工作仍经一次 Submit 进入 application core。
type Submitter interface {
	Submit(ctx context.Context, text string) error
}

// TurnWaiter 等待全部已接受 chat 收敛（owner：Runtime.Lifecycle）。
type TurnWaiter interface {
	WaitForIdle(ctx context.Context) error
}

// EventReader 订阅全局事件流（owner：Session.View）。
type EventReader interface {
	Subscribe(buffer int) application.Subscription
}

// ViewSnapshot 是项目选择所需的只读视图面（owner：Session.Snapshot）。
// 诊断台只读当前视图的工作区列表，不复制任何视图状态机。
type ViewSnapshot interface {
	Snapshot() application.Snapshot
}

// WorkspaceBinder 是显式项目授权面（owner：Workspace.Switch）。
// 刻意没有隐式 cwd 兜底：用例必须点名要授权哪个项目，scoped 工具才有边界。
type WorkspaceBinder interface {
	CreateWorkspace(name, rootPath, gitRemote string) error
	BindWorkspace(workspaceID string) error
}

// Dependencies 是诊断台事件循环需要的全部应用面：每个字段一个 owner，由
// wiring/bootstrap 装配。字段全部是骨架（未装配即装配错误），诊断台不在运行期
// 探测自己的依赖。项目选择不在这里：它由装配处把两个 owner 显式交给 BindProject，
// 诊断台因此没有"读面和写面各配一份"的第二条通路。
type Dependencies struct {
	Submission Submitter
	Turn       TurnWaiter
	Events     EventReader
}
