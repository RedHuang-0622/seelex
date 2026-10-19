package contract

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// WorkspaceGitReposPort 是工作区内 git 仓库发现的 optional 端口（GUI 仓库
// 标签栏数据源）。workspace.Repo 实现；Application 通过类型断言启用，未启用
// 时前端显示「不可用」而不是报错。
//
// 与 WorkspaceTreePort **分开声明**：往既有端口里加方法会打断所有既有实现与
// 测试假体（它们只关心目录树/提交日志），而仓库发现是可缺省能力。
//
// root 由 Application 从当前 workspace 提供（客户端不能指定绝对路径）；实现
// 只在 root 之内扫描，只返回相对路径与分支名，绝不返回文件内容。
type WorkspaceGitReposPort interface {
	// GitRepos 扫描 root 内的 git 仓库（BFS，有界）。目录内含 `.git`（目录或
	// 文件都算）即为仓库；仓库内部继续下钻（跳过 `.git` 自身）。剪枝复用
	// 目录树的忽略表 + 隐藏目录。limit <= 0 用实现默认值并按实现上限钳制。
	// 深度/目录/条目预算任一触顶即 Truncated=true 如实上报；读不了的目录跳过
	// 继续。
	GitRepos(root string, limit int) (dto.GitReposResult, error)
}
