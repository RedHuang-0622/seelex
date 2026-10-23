package workspace

// connection.go —— 项目粒度的**连接配置**：SSH 端点与 git 凭据。
//
// 两条硬口径：
//
//  1. **非秘密与秘密分开存**。host/port/user/git 用户名进 workspace_index.json
//     （人读、可分享、不含秘密）；口令进同目录的 workspace_secrets.json，值是
//     **密文**（Windows 走 DPAPI）。索引文件因此可以随便看，不会漏出口令。
//  2. **口令只进不出**。读面（Connection / 快照 / RPC 返回）只有 `*Set` 布尔；
//     明文只有运行时消费面（SSHPassword / GitPassword）能取，且不进 JSON。
//
// 与 docs/arch/ssh-tool.md §7 的凭据纪律一致：不进 argv、不进项目目录、不进日志。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SSHEndpoint 是项目级 SSH 端点的**非秘密**部分。
type SSHEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

// Empty 报告端点是否为空（空端点 = 该项目没有配 SSH）。
func (e SSHEndpoint) Empty() bool {
	return strings.TrimSpace(e.Host) == "" && strings.TrimSpace(e.User) == ""
}

// GitAccess 是项目级 git 远端访问的**非秘密**部分。
//
// 口令走密文库；Username 进索引（仓库地址、账号名都不是秘密，口令才是）。
type GitAccess struct {
	Username string `json:"username"`
	// Kind 是访问方式："https" | "ssh"；空 = 未声明（前端可留空）。
	Kind string `json:"kind,omitempty"`
}

// Empty 报告 git 访问配置是否为空。
func (g GitAccess) Empty() bool {
	return strings.TrimSpace(g.Username) == "" && strings.TrimSpace(g.Kind) == ""
}

// Connection 是**读面**：秘密只以 `*Set` 布尔出现，因此可以安全地进 DTO/快照。
type Connection struct {
	SSH     *SSHEndpoint `json:"ssh,omitempty"`
	Git     *GitAccess   `json:"git,omitempty"`
	SSHPass bool         `json:"ssh_password_set"`
	GitPass bool         `json:"git_password_set"`
}

// ConnectionUpdate 是**写面**：口令用指针表达三态。
//
//	SSHPassword == nil  → 不改（保留既有口令）
//	SSHPassword != nil  → 写入；指向空串表示清除
//
// 这条三态是必须的：UI 回填时拿不到旧口令，若用空串表示"不改"，就永远清不掉；
// 若用空串表示"清除"，一保存就会把口令抹掉。
type ConnectionUpdate struct {
	SSH         *SSHEndpoint
	SSHPassword *string
	Git         *GitAccess
	GitPassword *string
}

// secretsSnapshot 是 workspace_secrets.json 的盘上形态。
type secretsSnapshot struct {
	Version int               `json:"version"`
	SSH     map[string]string `json:"ssh,omitempty"`
	Git     map[string]string `json:"git,omitempty"`
}

// secretStore 是密文库：与索引同目录，独立文件，独立锁。
type secretStore struct {
	mu     sync.Mutex
	path   string // 空 = 不落盘（仅内存，测试/无持久化模式）
	loaded bool
	snap   secretsSnapshot
}

func newSecretStore(indexPath string) *secretStore {
	store := &secretStore{}
	if indexPath != "" {
		store.path = filepath.Join(filepath.Dir(indexPath), "workspace_secrets.json")
	}
	return store
}

// loadLocked 首次访问时读盘；文件不存在视为空库（首次使用是常态）。
func (s *secretStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	s.snap = secretsSnapshot{Version: 1}
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("workspace: read secrets: %w", err)
	}
	var snap secretsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return fmt.Errorf("workspace: parse secrets: %w", err)
	}
	if snap.SSH == nil {
		snap.SSH = map[string]string{}
	}
	if snap.Git == nil {
		snap.Git = map[string]string{}
	}
	s.snap = snap
	return nil
}

// saveLocked 原子落盘（同目录临时文件 + rename），权限 0600。
func (s *secretStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.snap, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: encode secrets: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("workspace: create secrets dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("workspace: write secrets: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("workspace: commit secrets: %w", err)
	}
	return nil
}

// put 写入/清除某一类口令（clear=true 时删除该项）。
func (s *secretStore) put(kind, workspaceID string, plain string, clear bool) error {
	if err := s.loadLocked(); err != nil {
		return err
	}
	table := s.snap.SSH
	if kind == "git" {
		table = s.snap.Git
	}
	if table == nil {
		table = map[string]string{}
		if kind == "git" {
			s.snap.Git = table
		} else {
			s.snap.SSH = table
		}
	}
	if clear {
		delete(table, workspaceID)
		return s.saveLocked()
	}
	sealed, err := sealSecret(plain)
	if err != nil {
		return err
	}
	table[workspaceID] = sealed
	return s.saveLocked()
}

// get 取出某一类口令明文（仅运行时消费面调用）。
func (s *secretStore) get(kind, workspaceID string) (string, error) {
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	table := s.snap.SSH
	if kind == "git" {
		table = s.snap.Git
	}
	sealed, ok := table[workspaceID]
	if !ok || sealed == "" {
		return "", nil
	}
	return openSecret(sealed)
}

// has 报告是否存有该项口令。
func (s *secretStore) has(kind, workspaceID string) bool {
	if err := s.loadLocked(); err != nil {
		return false
	}
	table := s.snap.SSH
	if kind == "git" {
		table = s.snap.Git
	}
	return table[workspaceID] != ""
}

// dropAll 删除某工作区的全部口令（删项目时调用，避免孤儿密文留在盘上）。
func (s *secretStore) dropAll(workspaceID string) error {
	if err := s.loadLocked(); err != nil {
		return err
	}
	_, hadSSH := s.snap.SSH[workspaceID]
	_, hadGit := s.snap.Git[workspaceID]
	if !hadSSH && !hadGit {
		return nil
	}
	delete(s.snap.SSH, workspaceID)
	delete(s.snap.Git, workspaceID)
	return s.saveLocked()
}

const (
	secretKindSSH = "ssh"
	secretKindGit = "git"
)

// ── Repo 上的连接配置读写面 ─────────────────────────────────────────────

// Connection 返回某工作区的连接配置（秘密只以布尔出现）。
func (r *Repo) Connection(id string) (Connection, error) {
	if _, err := r.Get(id); err != nil {
		return Connection{}, err
	}
	r.mu.RLock()
	info := r.workspaces[id]
	r.mu.RUnlock()

	conn := Connection{}
	if info.SSH != nil && !info.SSH.Empty() {
		endpoint := *info.SSH
		conn.SSH = &endpoint
	}
	if info.Git != nil && !info.Git.Empty() {
		access := *info.Git
		conn.Git = &access
	}
	if r.secrets != nil {
		conn.SSHPass = r.secrets.has(secretKindSSH, id)
		conn.GitPass = r.secrets.has(secretKindGit, id)
	}
	return conn, nil
}

// SetConnection 写入某工作区的连接配置，并返回权威读回值。
//
// 校验只做"能立刻判断的错"：端口范围、host 必填。语义校验（能不能连上）不在这里
// ——那是探针的职责，存储层不该假装会连。
func (r *Repo) SetConnection(id string, update ConnectionUpdate) (Connection, error) {
	if _, err := r.Get(id); err != nil {
		return Connection{}, err
	}
	if update.SSH != nil {
		normalized, err := normalizeEndpoint(*update.SSH)
		if err != nil {
			return Connection{}, err
		}
		update.SSH = &normalized
	}
	if update.Git != nil {
		git := *update.Git
		git.Username = strings.TrimSpace(git.Username)
		git.Kind = strings.TrimSpace(git.Kind)
		if git.Kind != "" && git.Kind != "https" && git.Kind != "ssh" {
			return Connection{}, fmt.Errorf("workspace: git kind must be https or ssh, got %q", git.Kind)
		}
		update.Git = &git
	}

	r.mu.Lock()
	info := r.workspaces[id]
	// 指针是浅拷贝：先复制值再挂回去，避免调用方后续改动穿透到库里。
	if update.SSH != nil {
		endpoint := *update.SSH
		if endpoint.Empty() {
			info.SSH = nil
		} else {
			info.SSH = &endpoint
		}
	}
	if update.Git != nil {
		access := *update.Git
		if access.Empty() {
			info.Git = nil
		} else {
			info.Git = &access
		}
	}
	r.workspaces[id] = info
	if err := r.saveLocked(); err != nil {
		return Connection{}, err
	}
	r.mu.Unlock()

	// 秘密在索引之外单独落盘：口令写失败不应该把已经写好的非秘密字段回滚掉
	// （那会让用户"填了 host 也丢了"），所以这两步分开报错。
	if r.secrets != nil {
		if update.SSHPassword != nil {
			if err := r.secrets.put(secretKindSSH, id, *update.SSHPassword, *update.SSHPassword == ""); err != nil {
				return Connection{}, err
			}
		}
		if update.GitPassword != nil {
			if err := r.secrets.put(secretKindGit, id, *update.GitPassword, *update.GitPassword == ""); err != nil {
				return Connection{}, err
			}
		}
	}
	return r.Connection(id)
}

// SSHPassword 返回 SSH 口令明文（**仅供运行时消费**，不得进 DTO/日志/快照）。
func (r *Repo) SSHPassword(id string) (string, error) {
	if r.secrets == nil {
		return "", nil
	}
	return r.secrets.get(secretKindSSH, id)
}

// GitPassword 返回 git 口令/令牌明文（**仅供运行时消费**）。
func (r *Repo) GitPassword(id string) (string, error) {
	if r.secrets == nil {
		return "", nil
	}
	return r.secrets.get(secretKindGit, id)
}

// normalizeEndpoint 归一 SSH 端点：trim、默认端口 22、端口范围校验。
func normalizeEndpoint(endpoint SSHEndpoint) (SSHEndpoint, error) {
	out := SSHEndpoint{
		Host: strings.TrimSpace(endpoint.Host),
		User: strings.TrimSpace(endpoint.User),
		Port: endpoint.Port,
	}
	if out.Empty() {
		return SSHEndpoint{}, nil
	}
	if out.Host == "" {
		return SSHEndpoint{}, fmt.Errorf("workspace: ssh host is required when user is set")
	}
	if out.User == "" {
		return SSHEndpoint{}, fmt.Errorf("workspace: ssh user is required when host is set")
	}
	if out.Port == 0 {
		out.Port = 22
	}
	if out.Port < 1 || out.Port > 65535 {
		return SSHEndpoint{}, fmt.Errorf("workspace: ssh port out of range: %d", out.Port)
	}
	return out, nil
}
