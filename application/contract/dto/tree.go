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

// GitCommitNode 是 git log 一行提交的结构化元数据（hash/作者/时间/标题/
// 父提交；不含 diff、补丁或文件内容）。
type GitCommitNode struct {
	Hash      string   `json:"hash"`              // 完整 commit hash
	ShortHash string   `json:"short_hash"`        // 短 hash（前端复制/展示用）
	Author    string   `json:"author"`            // 作者名
	Date      string   `json:"date"`              // 短日期（MM-dd HH:mm，由 --date=format 生成）
	Parents   []string `json:"parents,omitempty"` // 父提交 hash（顺序同 git；根提交为空）
	Subject   string   `json:"subject"`           // 提交标题（首行）
}

// GitLogResult 是 git 提交记录树的完整查询结果（GUI 提交记录树数据源）。
// Commits 按 git 拓扑序（新 → 旧）排列，父提交关系在 Parents 里——前端按
// 泳道算法渲染分叉，不再解析 `git --graph` 的字符画前缀。非 git 仓库或 git
// 不可用时 Error 携带展示文案（非致命，调用方仍返回 Result 而非 Go error）；
// Root 记录查询的仓库根。
type GitLogResult struct {
	Commits   []GitCommitNode `json:"commits"`
	Truncated bool            `json:"truncated"` // 达到 limit 截断
	Root      string          `json:"root,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// FileContent 是工作树文件预览的读取结果（GUI 文件预览数据源）。
// 字节原样以 base64 带回（文本与二进制同一通道；文档/图片类由前端按
// 扩展名分派渲染），绝不携带路径之外的任何文件系统信息。
type FileContent struct {
	Name      string `json:"name"`      // basename
	Path      string `json:"path"`      // 相对工作区根路径（/ 分隔）
	Size      int64  `json:"size"`      // 文件完整字节数
	Base64    string `json:"base64"`    // 原始字节（≤ Limit；base64 编码传输）
	Limit     int64  `json:"limit"`     // 本次读取上限（字节；实际生效值）
	Truncated bool   `json:"truncated"` // Size > Limit，内容已截断
	TextLike  bool   `json:"text_like"` // 二进制探测：可安全按文本展示
}

// FileWriteResult 是工作树文件写入（文件详情编辑保存）的结果。
// 只报告"落到哪个相对路径、写了多少字节"：写入是原子发布（同目录临时文件 +
// rename），因此保存成功即"磁盘上的内容就是这一份"，调用方随后按同一路径读回
// 就能把编辑器基线同步到实际文件——这是编辑面唯一的成功判据，不靠回执自证。
type FileWriteResult struct {
	Path  string `json:"path"`  // 相对工作区根路径（/ 分隔）
	Size  int64  `json:"size"`  // 写入后的文件字节数
	Limit int64  `json:"limit"` // 生效的单次写入上限（字节）
}
