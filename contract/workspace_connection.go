package contract

// workspace_connection.go —— 项目级连接配置的**窄可选端口**。
//
// 为什么不并进 WorkspacePort：那条端口是所有存储实现（含测试 fake）都必须满足的
// 最小面，往里加方法论会逼所有 fake 跟着改，且"连接配置"本身是可缺能力的
// （没有密文库的宿主只能读不能写）。因此按仓库既有做法（窄可选端口）单开一条，
// 由实现方按需满足，消费方 type-assert 探测：`store, ok := port.(WorkspaceConnectionPort)`。

import model "github.com/RedHuang-0622/seelex/contract/protocol"

// WorkspaceConnectionPort 是项目级 SSH/git 连接配置的读写口。
//
// 语义约束（实现方必须满足）：
//   - 读面永不返回口令明文，只给 `*PasswordSet` 布尔；
//   - 写面里口令是**三态**（写入 / 清除 / 保持），见 model.WorkspaceConnectionChange；
//   - 未登记的工作区 ID 返回错误，不静默建空记录。
type WorkspaceConnectionPort interface {
	WorkspaceConnection(id string) (model.WorkspaceConnection, error)
	SetWorkspaceConnection(id string, change model.WorkspaceConnectionChange) (model.WorkspaceConnection, error)
}
