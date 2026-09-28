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

// WorkspaceFileWritePort 是工作区文件写入（「文件详情」面板编辑保存）的 optional
// 端口。与只读的 WorkspaceFilePort **分开声明**：只读宿主（最小装配 / 测试桩）没有
// 写面时，编辑入口必须给出明确错误，而不是让"只读能力"被动升级成写能力。
//
// 实现必须与读面共用同一条可见性边界（相对路径 + containment、忽略目录与敏感文件
// 过滤、目录与符号链接拒绝），并保证写入是原子发布：保存成功即磁盘上的内容就是提交的
// 那一份，调用方读回同一路径即可同步编辑器基线。
type WorkspaceFileWritePort interface {
	// WriteFile 用 content 覆盖 root 内 relPath 文件的全部内容，返回写入后的
	// 相对路径与字节数；非文本文件、越界路径与超上限一律显式失败。
	WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error)
}
