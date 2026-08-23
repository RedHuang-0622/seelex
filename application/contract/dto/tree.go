// Package dto 承载 application/contract 与域包共享的纯 DTO 类型。
// 本文件：工作树（Work Tree）只读元数据 DTO——只含路径/名称/类型/大小/
// 计数，绝不携带文件内容。
package dto

// TreeEntry 是工作树的一个节点（目录或文件）。
type TreeEntry struct {
	Name string `json:"name"`           // 条目名（basename）
	Path string `json:"path"`           // 相对工作区根的路径（/ 分隔；root 目录下直接子条目可能为 "dir/file"）
	Type string `json:"type"`           // "dir" | "file"
	Size int64  `json:"size,omitempty"` // 文件字节数（dir 不填；符号链接按 Lstat 大小）
	// Count 是目录的直接文件子条目数（不递归；忽略目录与敏感文件不计入）。
	Count int `json:"count,omitempty"`
}

// TreeListing 是一次目录列表结果（含截断标记）。
type TreeListing struct {
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

// TreeCount 是工作区递归文件/目录统计（有预算上限）。
type TreeCount struct {
	Files     int  `json:"files"`
	Dirs      int  `json:"dirs"`
	Truncated bool `json:"truncated"`
}
