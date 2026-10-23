package dto

// workspace.go —— 项目登记的输入/输出 DTO。
//
// 连接配置（SSH / git）的口令是**单向字段**：只出现在请求里，永不回读。
// 读面只有 `*_password_set` 布尔——前端据此显示"已保存/未设置"，不需要也拿不到明文。
// 口径出处：docs/arch/ssh-tool.md §7（凭据不进返回值、不进日志、不进项目目录）。

// WorkspaceSSHInput 是新建项目时填的 SSH 端点。
type WorkspaceSSHInput struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	User string `json:"user"`
	// Password 只在写入方向存在；空 = 该项目不存口令（改用 agent/key）。
	Password string `json:"password,omitempty"`
	// RemoteRoot 是远端工作目录（= 远端项目根，对端 POSIX 绝对路径）。
	// 空 = 未知：远端面上路径门就只能把"本地根也容不下的绝对路径"全算成越界候选。
	RemoteRoot string `json:"remote_root,omitempty"`
}

// WorkspaceGitInput 是新建项目时填的 git 远端凭据。
type WorkspaceGitInput struct {
	Username string `json:"username"`
	// Kind 是访问方式："https" | "ssh"；空 = 未声明。
	Kind string `json:"kind,omitempty"`
	// Password 是 HTTPS 访问用的口令/令牌；单向字段。
	Password string `json:"password,omitempty"`
}

// WorkspaceCreateRequest 是一次批量新建项目中的单个项目草稿。
// RootPath 必须是待登记的项目根；GitRemote 为空时由后端探测 origin。
type WorkspaceCreateRequest struct {
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
	// SSH / Git 为空 = 该项目不配连接（等价于旧行为）。
	SSH *WorkspaceSSHInput `json:"ssh,omitempty"`
	Git *WorkspaceGitInput `json:"git,omitempty"`
}

// WorkspaceSSHSummary 是 SSH 端点的读面（无口令）。
type WorkspaceSSHSummary struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	// RemoteRoot 是远端工作目录（无口令，可回读；空 = 未配置）。
	RemoteRoot string `json:"remote_root,omitempty"`
}

// WorkspaceGitSummary 是 git 访问配置的读面（无口令）。
type WorkspaceGitSummary struct {
	Username string `json:"username"`
	Kind     string `json:"kind,omitempty"`
}

// WorkspaceCreateResult 是批量新建成功后的一项权威工作区读回值。
type WorkspaceCreateResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
	// 连接配置读回：只有摘要 + "有没有口令"，没有明文。
	SSH            *WorkspaceSSHSummary `json:"ssh,omitempty"`
	Git            *WorkspaceGitSummary `json:"git,omitempty"`
	SSHPasswordSet bool                 `json:"ssh_password_set,omitempty"`
	GitPasswordSet bool                 `json:"git_password_set,omitempty"`
	// ConnectionError 是"项目建成了但连接配置没写成功"的**非致命**说明：
	// 建项目是主操作，连接配置是附带项，不能因为后者把前者回滚成"什么都没建成"。
	ConnectionError string `json:"connection_error,omitempty"`
}

// WorkspaceCreateBatchResult 是一次批量新建的结果集合。
// 批量创建不绑定当前会话；绑定仍由 BindWorkspace 单独完成。
type WorkspaceCreateBatchResult struct {
	Workspaces []WorkspaceCreateResult `json:"workspaces"`
}

// WorkspaceConnectionInput 是**更新**已有项目连接配置的请求。
//
// 口令三态（与 model.WorkspaceConnectionChange 同语义）：
//
//	SSHPassword 非空        → 写入/轮换
//	ClearSSHPassword = true → 清除
//	都不给                  → 保持原值（**默认**，因为 UI 回填拿不到旧口令）
type WorkspaceConnectionInput struct {
	SSHHost string `json:"ssh_host"`
	SSHPort int    `json:"ssh_port"`
	SSHUser string `json:"ssh_user"`
	// SSHRemoteRoot 是远端工作目录（对端 POSIX 绝对路径；空 = 未知/未配置）。
	// 它是路径门在远端面上的取根口径：根内路径不需要 ssh.userspace。
	SSHRemoteRoot string `json:"ssh_remote_root,omitempty"`

	SSHPassword      string `json:"ssh_password,omitempty"`
	ClearSSHPassword bool   `json:"clear_ssh_password,omitempty"`

	GitUsername string `json:"git_username"`
	GitKind     string `json:"git_kind,omitempty"`

	GitPassword      string `json:"git_password,omitempty"`
	ClearGitPassword bool   `json:"clear_git_password,omitempty"`
}

// WorkspaceConnectionResult 是连接配置的权威读回值。
type WorkspaceConnectionResult struct {
	WorkspaceID    string               `json:"workspace_id"`
	SSH            *WorkspaceSSHSummary `json:"ssh,omitempty"`
	Git            *WorkspaceGitSummary `json:"git,omitempty"`
	SSHPasswordSet bool                 `json:"ssh_password_set"`
	GitPasswordSet bool                 `json:"git_password_set"`
}
