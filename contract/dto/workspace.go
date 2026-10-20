package dto

// WorkspaceCreateRequest 是一次批量新建项目中的单个项目草稿。
// RootPath 必须是待登记的项目根；GitRemote 为空时由后端探测 origin。
type WorkspaceCreateRequest struct {
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
}

// WorkspaceCreateResult 是批量新建成功后的一项权威工作区读回值。
type WorkspaceCreateResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
}

// WorkspaceCreateBatchResult 是一次批量新建的结果集合。
// 批量创建不绑定当前会话；绑定仍由 BindWorkspace 单独完成。
type WorkspaceCreateBatchResult struct {
	Workspaces []WorkspaceCreateResult `json:"workspaces"`
}
