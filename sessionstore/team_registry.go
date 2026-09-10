// team_registry.go：A2A 角色注册表（agent-team 工厂与角色管理设置面的持久化）。
//
// 定位（my_design §2.0 整份替换型数据文件；长期边界见
// docs/arch/a2a-agent-team-factory.md §2/§7）：
//   - 文件 = `session/team/roles.json`，一次提交整份原子替换；
//     替换成功即发布（不需要 head 水位），不写 message、不碰 sequencer；
//   - 内容 = TeamSpec 里的角色注册表部分（RoleSpec 清单 + team_kind + 顺序策略），
//     `order_policy`/`order_roles` 仍以 lifecycle head 为运行时权威，本文件只做
//     角色配置的持久事实；两者由应用层在同一次装配里一起写，避免出现第二份顺序事实；
//   - 角色身份是 metadata：`role_name` 只用于展示/归属/装配，provider role 仍只有
//     system/user/assistant/tool。
package sessionstore

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
)

// TeamRegistrySchemaVersion 是 session/team/roles.json 的 schema 版本。
const TeamRegistrySchemaVersion = 1

// TeamRoleSpec 是角色注册表里的一个角色（对应 RoleSpec，见 arch 稿 §2.1）。
// 空字段一律表示“未配置/继承 preset 默认”，不表示禁用。
type TeamRoleSpec struct {
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

// TeamRegistry 是 `session/team/roles.json` 的完整内容（整份替换型）。
// Configured 由读侧推导，不落盘：文件不存在 = 未配置过角色注册表。
type TeamRegistry struct {
	SchemaVersion int            `json:"schema_version"`
	TeamID        string         `json:"team_id,omitempty"`
	TeamKind      string         `json:"team_kind,omitempty"`
	OrderPolicy   string         `json:"order_policy,omitempty"`
	Roles         []TeamRoleSpec `json:"roles"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	Configured    bool           `json:"-"`
}

// teamRegistryLocks 是 `session/team/roles.json` 的按路径写锁（同 roleDraftLocks
// 形态：整份替换型文件只需串行化同路径的读改写，不需要模块 head 锁）。
var teamRegistryLocks sync.Map

func teamRegistryLock(path string) *sync.Mutex {
	value, _ := teamRegistryLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (store *storeEngine) teamRegistryPath(key Key) string {
	return filepath.Join(store.sessionRoot(key), "team", "roles.json")
}

// normalizeTeamRoleSpec 把角色条目规整为可比较形态：role_name 去空白、kind 兜底
// 为 agent，其余切片去空并排序（保证同一配置写出字节稳定、便于幂等比对）。
func normalizeTeamRoleSpec(spec TeamRoleSpec) TeamRoleSpec {
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

// normalizeTeamRegistry 校验并规整注册表；空 RoleName 或缺 role_name 的重复条目
// 显式报错（不静默丢弃，避免角色配置“写了但没生效”）。
func normalizeTeamRegistry(registry TeamRegistry) (TeamRegistry, error) {
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = TeamRegistrySchemaVersion
	}
	if registry.SchemaVersion != TeamRegistrySchemaVersion {
		return TeamRegistry{}, fmt.Errorf("session storage: unsupported team registry schema %d", registry.SchemaVersion)
	}
	registry.TeamID = strings.TrimSpace(registry.TeamID)
	registry.TeamKind = strings.TrimSpace(registry.TeamKind)
	registry.OrderPolicy = strings.TrimSpace(registry.OrderPolicy)
	if len(registry.Roles) == 0 {
		registry.Roles = nil
		return registry, nil
	}
	seen := make(map[string]struct{}, len(registry.Roles))
	roles := make([]TeamRoleSpec, 0, len(registry.Roles))
	for _, spec := range registry.Roles {
		spec = normalizeTeamRoleSpec(spec)
		if spec.RoleName == "" {
			return TeamRegistry{}, errors.New("session storage: team registry role_name is required")
		}
		if _, ok := seen[spec.RoleName]; ok {
			return TeamRegistry{}, fmt.Errorf("session storage: duplicate team registry role %q", spec.RoleName)
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

// readTeamRegistry 读注册表。文件不存在返回 Configured=false 的空注册表（未配置
// 不是错误）；文件损坏显式报错，不返回半份配置。
func (store *storeEngine) readTeamRegistry(key Key) (TeamRegistry, error) {
	data, err := os.ReadFile(store.teamRegistryPath(key))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return TeamRegistry{SchemaVersion: TeamRegistrySchemaVersion}, nil
	case err != nil:
		return TeamRegistry{}, err
	}
	var registry TeamRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return TeamRegistry{}, errors.New("session storage: decode team registry")
	}
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = TeamRegistrySchemaVersion
	}
	registry.Configured = true
	return registry, nil
}

// writeTeamRegistry 整份原子替换注册表；替换成功即发布。
func (store *storeEngine) writeTeamRegistry(key Key, registry TeamRegistry) error {
	registry, err := normalizeTeamRegistry(registry)
	if err != nil {
		return err
	}
	registry.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	path := store.teamRegistryPath(key)
	lock := teamRegistryLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// ReadTeamRegistryWorkspace 读主会话的角色注册表（项目作用域显式传入，禁止回退
// 活跃写作用域）。
func (router *Router) ReadTeamRegistryWorkspace(projectID, sessionID string) (TeamRegistry, error) {
	var registry TeamRegistry
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team registry requires session layout")
		}
		var err error
		registry, err = layout.layout.readTeamRegistry(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		return err
	})
	return registry, err
}

// WriteTeamRegistryWorkspace 整份写入主会话的角色注册表。
func (router *Router) WriteTeamRegistryWorkspace(projectID, sessionID string, registry TeamRegistry) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team registry requires session layout")
		}
		return layout.layout.writeTeamRegistry(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)}, registry)
	})
}

// EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false（不报错），
// 供 AgentTeamFactory 重复装配同一条 TeamSpec 时不产生第二个会话。
func (router *Router) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	created := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: strings.TrimSpace(mainSessionID)}
		roleStore, roleKey := layout.layout.roleStore(mainKey, roleName, roleSessionID)
		if roleStore.sessionExists(roleKey) {
			return nil
		}
		if _, _, err := layout.layout.createRoleSession(mainKey, roleName, roleSessionID, joinSeq); err != nil {
			// 并发装配时另一方可能已经建好：再查一次，存在即视为成功。
			if roleStore.sessionExists(roleKey) {
				return nil
			}
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// ReadLifecycleOrderWorkspace 读群聊顺序策略（lifecycle head 是运行时权威）。
// 空值 = 尚未编排过顺序，不是错误。
func (router *Router) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error) {
	var policy string
	var roles []string
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: lifecycle order requires session layout")
		}
		head, err := layout.layout.readLifecycleHead(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		if err != nil {
			return err
		}
		policy = head.OrderPolicy
		roles = append([]string(nil), head.OrderRoles...)
		return nil
	})
	return policy, roles, err
}
