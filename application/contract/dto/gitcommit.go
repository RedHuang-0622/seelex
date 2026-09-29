// Package dto 承载 application/contract 与域包共享的纯 DTO 类型。
// 本文件：提交详情（一个提交改了哪些文件）只读元数据 DTO——只含路径/状态/
// 行数统计，绝不携带补丁或 blob 字节；文件内容本身走既有的 dto.FileContent
// （与工作树预览同一形状），区别只在字节来源是 git 对象库而不是工作区磁盘。
package dto

// GitCommitFileEntry 是一个提交里的一处文件改动（只读元数据）。
//
// Kind 复用工作区更改（gitchanges.go）的 Change* 分类——git 的差别语义
// （改名/新增/删除…）只在两处状态字母解释器里各解释一次，前端不重推。
// Status 是 git 名状态原样（"M"/"A"/"D"/"T"/"R062"/"C100"，含相似度分数），
// Letter 是它的单字母形式（前端展示用，R062 → "R"）。
//
// Additions/Deletions 来自 --numstat（改了多少行）。Binary 为真时 git 不给
// 行数（`-`），两个计数保持 0——前端据此显示"二进制"而不是"+0 -0"。
type GitCommitFileEntry struct {
	Path      string `json:"path"`               // 相对工作区根的路径（/ 分隔）
	OldPath   string `json:"old_path,omitempty"` // 重命名/复制的原路径（相对工作区根）
	Kind      string `json:"kind"`               // 见 Change* 常量
	Status    string `json:"status"`             // git 名状态原样（含 R/C 的相似度分数）
	Letter    string `json:"letter"`             // 单字母状态（R062 → "R"）
	Additions int    `json:"additions"`          // 新增行数（Binary 时无意义）
	Deletions int    `json:"deletions"`          // 删除行数（Binary 时无意义）
	Binary    bool   `json:"binary,omitempty"`   // git 不给行数（二进制文件）
}

// GitCommitDetail 是一个提交的完整详情（GUI「提交记录 → 点开某个提交」数据源）。
//
// 头部事实与 GitCommitNode 同形（hash/短 hash/作者/时间/父提交/标题），由同一条
// pretty 格式产出——提交行与提交详情里的"这个提交是谁、什么时候、说了什么"必须
// 是同一份事实。Files 是文件清单（按 git 输出顺序，即路径字母序）。
//
// Total 是**过滤后**的全部文件数（含被 limit 截断、未出现在 Files 里的部分）：
// 详情头部说的是"这个提交改了多少个文件"，不是"本屏列了几行"。Filtered 是未展示
// 的条目数（命中敏感文件名或落在工作区之外的兄弟路径），不静默。
//
// 非 git 仓库 / 提交不存在 / git 不可用时 Error 携带展示文案（非致命，调用方仍
// 返回 Result 而非 Go error）；Root 记录查询的仓库根。
type GitCommitDetail struct {
	Hash      string               `json:"hash"`              // 完整 commit hash
	ShortHash string               `json:"short_hash"`        // 短 hash
	Author    string               `json:"author"`            // 作者名
	Date      string               `json:"date"`              // 短日期（MM-dd HH:mm）
	Parents   []string             `json:"parents,omitempty"` // 父提交 hash（根提交为空）
	Subject   string               `json:"subject"`           // 提交标题（首行）
	Files     []GitCommitFileEntry `json:"files"`
	Total     int                  `json:"total"`
	Filtered  int                  `json:"filtered,omitempty"`
	Truncated bool                 `json:"truncated"` // 达到 limit 截断
	Root      string               `json:"root,omitempty"`
	Error     string               `json:"error,omitempty"`
}
