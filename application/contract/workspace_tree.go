package contract

import (
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// WorkspaceTreePort 是工作区目录树查询的 optional 端口（GUI 工作树数据源）。
// workspace.Repo 实现；Application 通过类型断言启用，未启用时 GUI 显示
// 「不可用」而非报错。root 由 Application 从当前 workspace 提供，客户端
// 只能传相对路径；实现只返回元数据，绝不返回文件内容。
type WorkspaceTreePort interface {
	// ListTree 列出 root 内 relPath 目录的子条目（dir-first 排序、有界）。
	ListTree(root, relPath string, depth int) (dto.TreeListing, error)
	// CountFiles 递归统计 root 内文件/目录数（忽略规则 + 敏感文件除外）。
	CountFiles(root string) (dto.TreeCount, error)
	// GitLog 返回 root 内最近 limit 条提交的 --graph 拓扑行（固定 argv、
	// 只读、超时；非 git 仓库以 Result.Error 描述，不返回 Go error）。
	GitLog(root string, limit int) (dto.GitLogResult, error)
}
