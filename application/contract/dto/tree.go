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

// GitCommitNode 是 git log 一行提交的结构化元数据（hash/作者/时间/标题；
// 不含 diff、补丁或文件内容）。
type GitCommitNode struct {
	Hash      string `json:"hash"`       // 完整 commit hash
	ShortHash string `json:"short_hash"` // 短 hash（前端复制/展示用）
	Author    string `json:"author"`     // 作者名
	Date      string `json:"date"`       // 短日期（MM-dd HH:mm，由 --date=format 生成）
	Subject   string `json:"subject"`    // 提交标题（首行）
}

// GitLogLine 是 git log --graph 输出的一行：Graph 是拓扑树形字符前缀
// （"* "、"| "、"|\\ " 等，延续线无 Commit）；Commit 非空表示本行是一条提交。
type GitLogLine struct {
	Graph  string         `json:"graph"` // 树形图前缀（等宽渲染，前端 escape）
	Commit *GitCommitNode `json:"commit,omitempty"`
}

// GitLogResult 是 git 提交记录树的完整查询结果（GUI 提交记录树数据源）。
// 非 git 仓库或 git 不可用时 Error 携带展示文案（非致命，调用方仍返回
// Result 而非 Go error）；Root 记录查询的仓库根。
type GitLogResult struct {
	Lines     []GitLogLine    `json:"lines"` // 拓扑行（含延续线，保序）
	Commits   []GitCommitNode `json:"commits"`
	Truncated bool            `json:"truncated"` // 达到 limit 截断
	Root      string          `json:"root,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// FileContent 是工作树文件预览的读取结果（GUI 文件预览数据源）。
// 字节原样以 base64 带回（文本与二进制同一通道；文档/图片类由前端按
// 扩展名分派渲染），绝不携带路径之外的任何文件系统信息。
type FileContent struct {
	Name      string `json:"name"`               // basename
	Path      string `json:"path"`               // 相对工作区根路径（/ 分隔）
	Size      int64  `json:"size"`               // 文件完整字节数
	Base64    string `json:"base64"`             // 原始字节（≤ Limit；base64 编码传输）
	Limit     int64  `json:"limit"`              // 本次读取上限（字节；实际生效值）
	Truncated bool   `json:"truncated"`          // Size > Limit，内容已截断
	TextLike  bool   `json:"text_like"`          // 二进制探测：可安全按文本展示
}
