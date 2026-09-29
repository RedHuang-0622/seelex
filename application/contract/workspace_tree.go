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
	// GitLog 返回 root 内最近 limit 条提交（含父提交拓扑；固定 argv、只读、
	// 超时；非 git 仓库以 Result.Error 描述，不返回 Go error）。拓扑交给
	// 前端按泳道渲染分叉，不再下发 `git --graph` 的字符画前缀。
	GitLog(root string, limit int) (dto.GitLogResult, error)
	// GitChanges 返回 root 内未提交改动（暂存/未暂存/未跟踪/冲突；固定 argv、
	// 只读 --no-optional-locks、超时；非 git 仓库以 Result.Error 描述，不返回
	// Go error）。路径以 root 为基准下发（仓库子目录即工作区根时剥掉仓库根
	// 前缀），敏感文件名与工作区之外的路径不展示并计入 Result.Filtered；
	// 只含路径与状态字符，绝不含文件内容或 diff。
	GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error)
	// GitCommitDetail 返回 root 内某个提交改了哪些文件（状态/路径/重命名原路径/
	// ±行数；固定 argv、只读、超时；非 git 仓库或提交不存在以 Result.Error 描述，
	// 不返回 Go error）。hash 必须是十六进制形状——修订表达式不接受，畸形 hash
	// 是调用方传错参数，返回 Go error。清单同样不含补丁或文件内容（文件内容走
	// WorkspaceFilePort.GitCommitFileContent）。
	GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error)
}
