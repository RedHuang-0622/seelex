package contract

// workspace_environment.go —— 项目侧**工作环境表**的窄可选端口。
//
// 与 workspace_connection.go 同一条做法（窄可选端口 + type-assert 探测）：环境表需要
// 密文库与索引写面，没有持久化的宿主只能读不能写，不该逼所有 WorkspacePort 实现
// （含测试 fake）跟着长方法。
//
// 口径（2026-10-24）：工作环境**只挂在项目上**，按环境名分组。环境有端点时
// 执行面为 SSH，无端点时在本地项目根执行；会话只可选择缺省环境。

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// WorkspaceEnvironmentPort 是项目环境表的读写口。
//
// 语义约束（实现方必须满足）：
//   - 读面永不返回口令明文，只给 `PasswordSet` 布尔；
//   - 写面的口令是**三态**（写入 / 清除 / 保持），见 model.WorkspaceEnvironmentChange；
//   - 默认环境必须指向表里存在的名字（空串 = 取消默认）；
//   - 未登记的工作区 ID 返回错误，不静默建空记录。
type WorkspaceEnvironmentPort interface {
	// WorkspaceEnvironments 返回该项目全部环境（按名字升序）与默认环境名。
	WorkspaceEnvironments(id string) ([]model.WorkspaceEnvironmentInfo, string, error)
	// SetWorkspaceEnvironment 写入（或覆盖）一个环境；Delete=true 时删除。
	SetWorkspaceEnvironment(id, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)
	// DeleteWorkspaceEnvironment 删除一个环境（连同它的口令）。
	DeleteWorkspaceEnvironment(id, name string) error
	// SetDefaultWorkspaceEnvironment 指定默认环境（空串 = 取消默认）。
	SetDefaultWorkspaceEnvironment(id, name string) error
}
