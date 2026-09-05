package contract

import (
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// WorkspaceFilePort 是工作区文件读取的 optional 端口（GUI 文件预览数据源）。
// workspace.Repo 实现；Application 通过类型断言启用，未启用时 GUI 提示
// 「暂不支持预览」而非报错。root 由 Application 从当前 workspace 提供，
// 客户端只能传相对路径；实现保证 containment、敏感文件/忽略目录过滤、
// 符号链接拒绝与大小上限，只按需读取受控字节，不暴露任意文件。
type WorkspaceFilePort interface {
	// ReadFile 读取 root 内 relPath 文件的前 limit 字节（limit ≤ 0 用实现
	// 默认上限；超出实现硬上限时钳制并如实报告 Limit/Truncated）。
	ReadFile(root, relPath string, limit int64) (dto.FileContent, error)
}
