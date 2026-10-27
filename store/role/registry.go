// Package role owns persisted role identity and policy: the team registry
// (session/team/roles.json), role sessions and their permission/floor
// lifecycle. It is the single writer of the `<sessionRoot>/team` subtree.
package role

// registry.go — A2A 角色注册表（`session/team/roles.json`）的落盘 owner。
//
// 领域归属证据（M8 迁移裁决）：
//
//  1. **内容是角色身份与策略**：`RoleSpec` 描述"这个会话里有哪些角色、各自用哪个
//     system prompt / 模型策略 / 工具范围 / 顺序优先级"。它不属于 teamwork 计划
//     （计划是"谁、什么顺序"的硬编排），也不属于会话消息/生命周期。
//  2. **物理位置就是本领域子树**：`<sessionRoot>/team/roles.json` 与 M9 的角色会话
//     子树、role draft、permission、floor/message lifecycle 同处 `<sessionRoot>/team`。
//     让 teamwork 包持有它，会把角色身份拆到两个限界上下文。
//  3. **读写语义独立**：整份原子替换、无 head 水位、不写 message、不碰 sequencer
//     ——与 teamwork 的"head + 两条追加流水"是三套机制，不能共用一个 owner。
//
// 状态（A06）：这是 legacy 只读兼容面，当前没有生产调用方；旧会话里的
// `roles.json` 仍必须能读、能诊断。不删文件、不迁移、不停止写入——任何磁盘清理
// 必须另行预警 + 备份 + 用户确认。完整政策见 store/README.md「旧 Agent Team 数据」。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// SchemaVersion 是 session/team/roles.json 的 schema 版本。版本不认识时写入显式
// 报错（不静默降级），读侧照旧；旧文件必须可读。
const SchemaVersion = 1

// RegistryDir / RegistryFile 是物理位置的唯一定义。
const (
	RegistryDir  = "team"
	RegistryFile = "roles.json"
)

// RoleSpec 是角色注册表里的一个角色。空字段一律表示"未配置/继承 preset 默认"，
// 不表示禁用。
type RoleSpec struct {
	RoleName        string   `json:"role_name"`
	RoleKind        string   `json:"role_kind,omitempty"`
	SystemPrompt    string   `json:"system_prompt,omitempty"`
	ModelPolicy     string   `json:"model_policy,omitempty"`
	MirrorPolicy    []string `json:"mirror_policy,omitempty"`
	DirectiveSchema []string `json:"directive_schema,omitempty"`
	OrderPriority   int      `json:"order_priority,omitempty"`
	JoinPolicy      string   `json:"join_policy,omitempty"`
	PresencePolicy  string   `json:"presence_policy,omitempty"`
	ToolsPolicy     string   `json:"tools_policy,omitempty"`
}

// Registry 是 `session/team/roles.json` 的完整内容（整份替换型）。Configured 由
// 读侧推导，不落盘：文件不存在 = 未配置过角色注册表。
type Registry struct {
	SchemaVersion int        `json:"schema_version"`
	TeamID        string     `json:"team_id,omitempty"`
	TeamKind      string     `json:"team_kind,omitempty"`
	OrderPolicy   string     `json:"order_policy,omitempty"`
	Roles         []RoleSpec `json:"roles"`
	UpdatedAt     time.Time  `json:"updated_at,omitempty"`
	Configured    bool       `json:"-"`
}

// Store 是角色注册表持久化面。按路径写锁只串行化同路径的整份替换。
type Store struct {
	layout *storelayout.Layout
	locks  sync.Map // path → *sync.Mutex
}

// New 构造角色注册表持久化面。
func New(layout *storelayout.Layout) *Store {
	return &Store{layout: layout}
}

// Path 返回注册表物理路径。
func (s *Store) Path(scope storelayout.Scope) string {
	return filepath.Join(s.layout.SessionRoot(scope), RegistryDir, RegistryFile)
}

func (s *Store) lock(path string) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// Normalize 校验并规整注册表：空 RoleName 或重复 role_name 显式报错（不静默丢弃，
// 避免角色配置"写了但没生效"）；字符串集合去空排序，保证同一配置写出字节稳定。
func Normalize(registry Registry) (Registry, error) {
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = SchemaVersion
	}
	if registry.SchemaVersion != SchemaVersion {
		return Registry{}, fmt.Errorf("session storage: unsupported team registry schema %d", registry.SchemaVersion)
	}
	registry.TeamID = strings.TrimSpace(registry.TeamID)
	registry.TeamKind = strings.TrimSpace(registry.TeamKind)
	registry.OrderPolicy = strings.TrimSpace(registry.OrderPolicy)
	if len(registry.Roles) == 0 {
		registry.Roles = nil
		return registry, nil
	}
	seen := make(map[string]struct{}, len(registry.Roles))
	roles := make([]RoleSpec, 0, len(registry.Roles))
	for _, spec := range registry.Roles {
		spec = normalizeRoleSpec(spec)
		if spec.RoleName == "" {
			return Registry{}, errors.New("session storage: team registry role_name is required")
		}
		if _, ok := seen[spec.RoleName]; ok {
			return Registry{}, fmt.Errorf("session storage: duplicate team registry role %q", spec.RoleName)
		}
		seen[spec.RoleName] = struct{}{}
		roles = append(roles, spec)
	}
	sort.SliceStable(roles, func(i, j int) bool {
		if roles[i].OrderPriority != roles[j].OrderPriority {
			return roles[i].OrderPriority < roles[j].OrderPriority
		}
		return roles[i].RoleName < roles[j].RoleName
	})
	registry.Roles = roles
	return registry, nil
}

// normalizeRoleSpec 把角色条目规整为可比较形态：role_name 去空白、kind 兜底为
// agent，其余切片去空并排序。
func normalizeRoleSpec(spec RoleSpec) RoleSpec {
	spec.RoleName = strings.TrimSpace(spec.RoleName)
	spec.RoleKind = strings.TrimSpace(spec.RoleKind)
	if spec.RoleKind == "" {
		spec.RoleKind = "agent"
	}
	spec.MirrorPolicy = normalizeStringSet(spec.MirrorPolicy)
	spec.DirectiveSchema = normalizeStringSet(spec.DirectiveSchema)
	return spec
}

func normalizeStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// Read 读注册表。文件不存在返回 Configured=false 的空注册表（未配置不是错误）；
// 文件损坏显式报错，不返回半份配置。
func (s *Store) Read(scope storelayout.Scope) (Registry, error) {
	data, err := os.ReadFile(s.Path(scope))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Registry{SchemaVersion: SchemaVersion}, nil
	case err != nil:
		return Registry{}, err
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, errors.New("session storage: decode team registry")
	}
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = SchemaVersion
	}
	registry.Configured = true
	return registry, nil
}

// Write 整份原子替换注册表；替换成功即发布。
func (s *Store) Write(scope storelayout.Scope, registry Registry) error {
	registry, err := Normalize(registry)
	if err != nil {
		return err
	}
	registry.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	path := s.Path(scope)
	lock := s.lock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return storeroot.WriteAtomic(path, data, 0o600)
}

// Remove 删除角色注册表（团队离场）。文件不存在 = 已经离场（幂等，不是错误）：
// 读面本来就以"文件不存在"表达"本会话没有团队"，删除是它的逆操作——写一份空注册表
// 会让读面报 Configured=true 却一个成员都没有。
func (s *Store) Remove(scope storelayout.Scope) error {
	path := s.Path(scope)
	lock := s.lock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
