// Package dto 承载 application/contract 与域包共享的纯 DTO 类型。
// 本文件：工作区未提交改动（Work Tree Changes）只读元数据 DTO——只含路径/
// 状态/重命名原路径，绝不携带文件内容、diff 补丁或 blob 字节。
package dto

// 改动分类（Kind）取值。它是后端归一化后的展示分类，前端不再重新解释
// porcelain 的 XY 语义（两个字符的组合含义只在一处实现）。
const (
	ChangeModified    = "modified"     // M：内容或模式改动
	ChangeTypeChanged = "type_changed" // T：类型变化（文件 ↔ 符号链接/子模块）
	ChangeAdded       = "added"        // A：新增
	ChangeDeleted     = "deleted"      // D：删除
	ChangeRenamed     = "renamed"      // R：重命名（旧路径在 OldPath）
	ChangeCopied      = "copied"       // C：复制（原路径在 OldPath）
	ChangeConflicted  = "conflicted"   // U/AA/DD 等：未解决冲突
	ChangeUntracked   = "untracked"    // ??：未跟踪
)

// WorkspaceChangeEntry 是一处未提交改动（工作区根基准的只读元数据）。
// Status 是 porcelain v1 两字符 XY 原样（X = 暂存侧、Y = 工作区侧，空格表示
// 该侧无改动）；Index/Worktree 是拆开后的单个字符，便于前端分侧展示。重命名/
// 复制带 OldPath（原路径），其余为空。
type WorkspaceChangeEntry struct {
	Path     string `json:"path"`               // 相对工作区根的路径（/ 分隔）
	OldPath  string `json:"old_path,omitempty"` // 重命名/复制的原路径（相对工作区根）
	Kind     string `json:"kind"`               // 见上方 Change* 常量
	Status   string `json:"status"`             // porcelain v1 的 XY 两字符原样
	Index    string `json:"index"`              // X：暂存侧状态字符（" " 无改动，"?" 未跟踪）
	Worktree string `json:"worktree"`           // Y：工作区侧状态字符
	Staged   bool   `json:"staged"`             // 暂存侧有改动（X 非空格 / 非 "?"）
}

// WorkspaceChangesResult 是一次工作区改动查询结果（GUI 工作区更改面板数据源）。
// Staged/Unstaged/Untracked/Conflicted 与 Total 统计的是**过滤后**的全部条目
// （含被 limit 截断、未出现在 Entries 里的部分）：面板头部说的是工作区状态，
// 不是"当前这屏列了多少行"；列表自身的截断另有 Truncated 提示。
//
// Filtered 是未展示的条目数（命中敏感文件名或落在工作区根之外）——不静默：
// 面板显式说明"另有 N 条未展示"，用户知道面板不是全貌。非 git 仓库或 git
// 不可用时 Error 携带展示文案（非致命，调用方仍返回 Result 而非 Go error）；
// Root 记录查询的工作区根。
type WorkspaceChangesResult struct {
	Entries    []WorkspaceChangeEntry `json:"entries"`
	Branch     string                 `json:"branch,omitempty"` // 当前分支（分离头指针为 HEAD）
	Total      int                    `json:"total"`
	Staged     int                    `json:"staged"`
	Unstaged   int                    `json:"unstaged"`
	Untracked  int                    `json:"untracked"`
	Conflicted int                    `json:"conflicted"`
	Filtered   int                    `json:"filtered,omitempty"`
	Truncated  bool                   `json:"truncated"` // 达到条目上限或解析预算
	Root       string                 `json:"root,omitempty"`
	Error      string                 `json:"error,omitempty"`
}
