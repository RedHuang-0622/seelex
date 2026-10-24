package workspace

// environments.go —— **项目侧的工作环境表**。
//
// 口径（2026-10-24 用户定调）：
//   - 环境挂在**项目**上，按名字分组；无端点 = local，有端点 = SSH；
//   - 会话不持有连接信息，只记录首次固定、之后不可切换的环境名；
//   - 适配器按会话绑定注入环境；环境是否存在、地址与 local/ssh 都由本表决定，
//     模型工具入参不暴露环境选择。
//
// 与前一代单条 `Info.SSH` 的关系：
//   - 单条端点仅为旧配置兼容保留；新执行链路按命名环境工作，未固定环境时本地执行，
//     不再自动借用旧端点；
//   - 迁移不自动做（存储层不猜用户想给老端点取什么名字）。
//
// 秘密纪律不变：host/port/user/远端根进 `workspace_index.json`（不含秘密），
// 逐环境口令进 `workspace_secrets.json`（密文库），读出只有 `*Set` 布尔。

import (
	"fmt"
	"sort"
	"strings"
)

// Environment 是项目下**一个命名环境**的配置（非秘密部分）。
//
// 嵌入 SSHEndpoint；空端点表示这个环境就在本地项目根执行，非空才走 SSH。
type Environment struct {
	SSHEndpoint
	// Note 是给人看的说明（"测试机，只读"之类），不参与任何判定。
	Note string `json:"note,omitempty"`
}

// EnvironmentInfo 是**读面**：端点 + 是否已设口令 + 是否默认。
//
// 口令只以布尔出现（`PasswordSet`），与 Connection 同一条纪律。
type EnvironmentInfo struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	RemoteRoot  string `json:"remote_root,omitempty"`
	Note        string `json:"note,omitempty"`
	PasswordSet bool   `json:"password_set"`
	Default     bool   `json:"default"`
}

// environmentSecretSeparator 是密文库键的分隔符。
//
// 用 US（0x1f）而不是 `|` / `:`：工作区 id 与环境名都由用户/配置给出，任何可打印
// 分隔符都可能出现在其中，撞键的后果是"给 dev 存的口令被 prod 读到"。
const environmentSecretSeparator = "\x1f"

// environmentSecretKey 是逐环境口令在密文库里的键。
func environmentSecretKey(workspaceID, name string) string {
	return workspaceID + environmentSecretSeparator + name
}

// normalizeEnvironmentName 归一环境名：trim + 允许字符集 + 长度上限。
//
// 字符集收得比"任意字符串"窄：环境名会被写进配置文件、命令行说明与日志，允许空格
// 或控制字符只会制造粘贴错误。`-` `_` `.` 足够表达 dev / test / staging-cn / prod.eu。
func normalizeEnvironmentName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("workspace: environment name is required")
	}
	if len(trimmed) > 40 {
		return "", fmt.Errorf("workspace: environment name %q is too long (max 40)", trimmed)
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("workspace: environment name %q contains invalid character %q", trimmed, r)
		}
	}
	return trimmed, nil
}

// Environments 返回某工作区的环境表（按名字升序的读面）与默认环境名。
//
// 空环境名 = 没有默认（或表里没有这个名字时也如实返回空串，由调用方决定报错）。
func (r *Repo) Environments(id string) ([]EnvironmentInfo, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.workspaces[id]
	if !ok {
		return nil, "", ErrNotFound
	}
	names := make([]string, 0, len(info.Environments))
	for name := range info.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]EnvironmentInfo, 0, len(names))
	for _, name := range names {
		environment := info.Environments[name]
		out = append(out, EnvironmentInfo{
			Name:        name,
			Host:        environment.Host,
			Port:        environment.Port,
			User:        environment.User,
			RemoteRoot:  environment.RemoteRoot,
			Note:        environment.Note,
			PasswordSet: r.secrets != nil && r.secrets.has(secretKindSSH, environmentSecretKey(id, name)),
			Default:     name == info.DefaultEnv,
		})
	}
	return out, info.DefaultEnv, nil
}

// Environment 读单个环境（含运行时用的端点；口令另走 EnvironmentPassword）。
func (r *Repo) Environment(id, name string) (Environment, bool, error) {
	normalized, err := normalizeEnvironmentName(name)
	if err != nil {
		return Environment{}, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.workspaces[id]
	if !ok {
		return Environment{}, false, ErrNotFound
	}
	environment, ok := info.Environments[normalized]
	return environment, ok, nil
}

// SetEnvironment 写入（或覆盖）一个环境，单参数口令三态与 SetConnection 同款：
//
//	password == nil  → 不改（保留既有口令）
//	password != nil  → 写入；指向空串表示清除
//
// 空端点（host 与 user 都空）不是删除，而是**本地环境**：它让会话可以固定
// dev 这类不经过 SSH 的工作环境。删除必须走显式 DeleteEnvironment。
func (r *Repo) SetEnvironment(id, name string, environment Environment, password *string) (EnvironmentInfo, error) {
	if _, err := r.Get(id); err != nil {
		return EnvironmentInfo{}, err
	}
	normalized, err := normalizeEnvironmentName(name)
	if err != nil {
		return EnvironmentInfo{}, err
	}
	endpoint, err := normalizeEndpoint(environment.SSHEndpoint)
	if err != nil {
		return EnvironmentInfo{}, err
	}
	local := endpoint.Empty()
	if local && password != nil && *password != "" {
		return EnvironmentInfo{}, fmt.Errorf("workspace: local environment %q cannot have an SSH password", normalized)
	}
	note := strings.TrimSpace(environment.Note)

	r.mu.Lock()
	info := r.workspaces[id]
	if info.Environments == nil {
		info.Environments = map[string]Environment{}
	}
	info.Environments[normalized] = Environment{SSHEndpoint: endpoint, Note: note}
	// 第一个环境自动成为默认：否则"配了环境但没默认"会让会话无从选起。
	if info.DefaultEnv == "" {
		info.DefaultEnv = normalized
	}
	r.workspaces[id] = info
	if err := r.saveLocked(); err != nil {
		r.mu.Unlock()
		return EnvironmentInfo{}, err
	}
	r.mu.Unlock()

	// 秘密与索引分开落盘（同 SetConnection 的理由：口令写失败不该回滚非秘密字段）。
	if r.secrets != nil && (local || password != nil) {
		secret := ""
		clear := local
		if password != nil {
			secret = *password
			clear = clear || secret == ""
		}
		if err := r.secrets.put(secretKindSSH, environmentSecretKey(id, normalized), secret, clear); err != nil {
			return EnvironmentInfo{}, err
		}
	}
	return r.environmentInfo(id, normalized)
}

// DeleteEnvironment 删除一个环境（连同它的口令）。
func (r *Repo) DeleteEnvironment(id, name string) error {
	normalized, err := normalizeEnvironmentName(name)
	if err != nil {
		return err
	}
	r.mu.Lock()
	info, ok := r.workspaces[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(info.Environments, normalized)
	if info.DefaultEnv == normalized {
		info.DefaultEnv = ""
	}
	r.workspaces[id] = info
	if err := r.saveLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	if r.secrets != nil {
		return r.secrets.put(secretKindSSH, environmentSecretKey(id, normalized), "", true)
	}
	return nil
}

// SetDefaultEnvironment 指定默认环境（必须在表里；空串 = 取消默认）。
func (r *Repo) SetDefaultEnvironment(id, name string) error {
	trimmed := strings.TrimSpace(name)
	r.mu.Lock()
	info, ok := r.workspaces[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	if trimmed != "" {
		if _, exists := info.Environments[trimmed]; !exists {
			r.mu.Unlock()
			return fmt.Errorf("workspace: environment %q is not configured", trimmed)
		}
	}
	info.DefaultEnv = trimmed
	r.workspaces[id] = info
	err := r.saveLocked()
	r.mu.Unlock()
	return err
}

// EnvironmentPassword 返回某环境的口令明文（**仅供运行时消费**，不进 DTO/日志/快照）。
func (r *Repo) EnvironmentPassword(id, name string) (string, error) {
	normalized, err := normalizeEnvironmentName(name)
	if err != nil {
		return "", err
	}
	if r.secrets == nil {
		return "", nil
	}
	return r.secrets.get(secretKindSSH, environmentSecretKey(id, normalized))
}

// environmentInfo 组装单个环境的读面（调用方已持有 / 未持有锁都安全：走 RLock）。
func (r *Repo) environmentInfo(id, name string) (EnvironmentInfo, error) {
	list, defaultEnv, err := r.Environments(id)
	if err != nil {
		return EnvironmentInfo{}, err
	}
	for _, item := range list {
		if item.Name == name {
			item.Default = name == defaultEnv
			return item, nil
		}
	}
	return EnvironmentInfo{}, ErrNotFound
}
