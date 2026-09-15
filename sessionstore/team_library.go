// team_library.go：团队库条目的形态、规整与投影，以及**旧项目级布局的只读回退**。
//
// 定位（与 session/team/roles.json 的分工，别混两份事实）：
//   - `session/team/roles.json`（team_registry.go）= **某个会话**当前在编的员工表；
//   - 团队库 = **全局**团队模板库（`<root>/team/library.json`，见 team_global.go）：
//     一支团队 = 角色配置集 + 顺序策略 + gate/compact 策略，可装配到任意会话
//     （装配 = 写该会话的 registry + lifecycle 顺序）。
//
// 形态与 roles.json 一致：整份替换型数据文件，一次提交整份原子替换，替换成功
// 即发布（不需要 head 水位，不写 message、不碰 sequencer）。
//
// 本文件保留两件事：
//  1. 库条目的规整与投影（NormalizeLibraryEntry / SpecOfEntry / EntryFromSpec /
//     EntryFromRegistry）——形态口径与存储布局无关；
//  2. **旧布局只读回退**：老版本团队库落在 `project-<hash>/teams/library.json`
//     （项目级）。全局库缺失时按锚定项目只读读一次，不搬数据、不删旧文件；回退
//     内容在下一次整份写入里自然并入全局（见 team_global.go 文件头）。
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
	"time"
)

// TeamLibrarySchemaVersion 是全局团队库文件（`<root>/team/library.json`）的 schema
// 版本；旧布局 `project-<hash>/teams/library.json` 共用同一版本号。
const TeamLibrarySchemaVersion = 1

// TeamLibraryEntryPhase 是团队库条目的来源标记（用于前端区分内置/自定义）。
const (
	TeamLibraryOriginPreset  = "preset"
	TeamLibraryOriginCurrent = "current-session"
	TeamLibraryOriginCustom  = "custom"
)

// TeamLibraryEntry 是团队库里的一支团队（对应 dto.TeamLibraryEntry 的存储形态）。
type TeamLibraryEntry struct {
	TeamID        string         `json:"team_id"`
	TeamKind      string         `json:"team_kind,omitempty"`
	Name          string         `json:"name,omitempty"`
	OrderPolicy   string         `json:"order_policy,omitempty"`
	OrderRoles    []string       `json:"order_roles,omitempty"`
	Roles         []TeamRoleSpec `json:"roles,omitempty"`
	GatePolicy    string         `json:"gate_policy,omitempty"`
	CompactPolicy string         `json:"compact_policy,omitempty"`
	Origin        string         `json:"origin,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
}

// TeamLibrary 是团队库文件的完整内容（整份替换型，全局粒度）。
// Configured 由读侧推导，不落盘：文件不存在 = 还没有团队库。
type TeamLibrary struct {
	SchemaVersion int                `json:"schema_version"`
	Teams         []TeamLibraryEntry `json:"teams"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	Configured    bool               `json:"-"`
}

// teamLegacyLibraryPath 返回**旧布局**的项目级团队库路径
// （`project-<hash>/teams/library.json`）。只用于读回退，不再有写路径。
func (store *storeEngine) teamLegacyLibraryPath(projectID string) string {
	return filepath.Join(store.root, "project-"+hash(projectID), "teams", "library.json")
}

// normalizeTeamLibraryEntry 校验并规整一条团队库条目：team_id 必填、角色条目
// 复用注册表的口径（同名重复显式报错，不静默丢弃）、顺序表去空去重。
func normalizeTeamLibraryEntry(entry TeamLibraryEntry) (TeamLibraryEntry, error) {
	entry.TeamID = strings.TrimSpace(entry.TeamID)
	if entry.TeamID == "" {
		return TeamLibraryEntry{}, errors.New("session storage: team library team_id is required")
	}
	entry.TeamKind = strings.TrimSpace(entry.TeamKind)
	if entry.TeamKind == "" {
		entry.TeamKind = entry.TeamID
	}
	entry.Name = strings.TrimSpace(entry.Name)
	if entry.Name == "" {
		entry.Name = entry.TeamID
	}
	entry.OrderPolicy = strings.TrimSpace(entry.OrderPolicy)
	entry.GatePolicy = strings.TrimSpace(entry.GatePolicy)
	entry.CompactPolicy = strings.TrimSpace(entry.CompactPolicy)
	entry.Origin = strings.TrimSpace(entry.Origin)

	seen := make(map[string]struct{}, len(entry.Roles))
	roles := make([]TeamRoleSpec, 0, len(entry.Roles))
	for _, role := range entry.Roles {
		role = normalizeTeamRoleSpec(role)
		if role.RoleName == "" {
			return TeamLibraryEntry{}, errors.New("session storage: team library role_name is required")
		}
		if _, ok := seen[role.RoleName]; ok {
			return TeamLibraryEntry{}, fmt.Errorf("session storage: duplicate team library role %q", role.RoleName)
		}
		seen[role.RoleName] = struct{}{}
		roles = append(roles, role)
	}
	entry.Roles = roles
	// 顺序表保留用户给定的次序（不排序），只去空白与重复。
	entry.OrderRoles = normalizeNameList(entry.OrderRoles)
	return entry, nil
}

// normalizeNameList 去空白/去重但**保序**（顺序表不能排序，否则用户编排的发言
// 顺序会被改掉；与 normalizeStringSet 的集合语义区分开）。
func normalizeNameList(values []string) []string {
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
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeTeamLibrary 规整整份团队库：版本校验、条目规整、按 name/team_id 排序
// （保证同一份库写出字节稳定，便于幂等比对）。
func normalizeTeamLibrary(library TeamLibrary) (TeamLibrary, error) {
	if library.SchemaVersion == 0 {
		library.SchemaVersion = TeamLibrarySchemaVersion
	}
	if library.SchemaVersion != TeamLibrarySchemaVersion {
		return TeamLibrary{}, fmt.Errorf("session storage: unsupported team library schema %d", library.SchemaVersion)
	}
	if len(library.Teams) == 0 {
		library.Teams = nil
		return library, nil
	}
	seen := make(map[string]struct{}, len(library.Teams))
	teams := make([]TeamLibraryEntry, 0, len(library.Teams))
	for _, entry := range library.Teams {
		normalized, err := normalizeTeamLibraryEntry(entry)
		if err != nil {
			return TeamLibrary{}, err
		}
		if _, ok := seen[normalized.TeamID]; ok {
			return TeamLibrary{}, fmt.Errorf("session storage: duplicate team library entry %q", normalized.TeamID)
		}
		seen[normalized.TeamID] = struct{}{}
		teams = append(teams, normalized)
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if teams[i].Name != teams[j].Name {
			return teams[i].Name < teams[j].Name
		}
		return teams[i].TeamID < teams[j].TeamID
	})
	library.Teams = teams
	return library, nil
}

// readLegacyTeamLibrary 读**旧布局**的项目级团队库（只读回退用）。文件不存在返回
// Configured=false 的空库（未建库不是错误）；文件损坏显式报错，不返回半份配置。
func (store *storeEngine) readLegacyTeamLibrary(projectID string) (TeamLibrary, error) {
	data, err := os.ReadFile(store.teamLegacyLibraryPath(projectID))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return TeamLibrary{SchemaVersion: TeamLibrarySchemaVersion}, nil
	case err != nil:
		return TeamLibrary{}, err
	}
	var library TeamLibrary
	if err := json.Unmarshal(data, &library); err != nil {
		return TeamLibrary{}, errors.New("session storage: decode team library")
	}
	if library.SchemaVersion == 0 {
		library.SchemaVersion = TeamLibrarySchemaVersion
	}
	library.Configured = true
	return library, nil
}
