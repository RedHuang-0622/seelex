package e2e

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRoleSessionPortsDoNotLeakStorageTypes 钉住 S27 收口：R2/R4 的角色会话与
// 定时插话端口只用 application/contract 的纯 DTO，存储实现类型（sessionstore.*）
// 不得出现在应用层端口签名与 headless 透传面里。
//
// 语义：AGENTS.md §1「frontend 只能通过 Application API 消费状态」——GUI 与
// application 都不得 import 存储包来表达群聊角色形状；存储类型只允许出现在
// internal/adapters 的映射函数里。
//
// 范围：本用例只覆盖 S27 声明的面（角色会话 / role draft / 顺序 / 定时插话）。
// sessionstore 在其它既有链路（session 路由、goal 审计、compaction 等）的
// 依赖是历史状态，不在本用例内，避免把无关重构捆进这次收口。
func TestRoleSessionPortsDoNotLeakStorageTypes(t *testing.T) {
	root := repoRoot()
	storageImport := "github.com/RedHuang-0622/seelex/sessionstore"

	forbidden := []string{
		filepath.Join("application", "contract", "ports.go"),
		filepath.Join("application", "contract", "dto", "rolesession.go"),
		filepath.Join("application", "core", "role_session.go"),
		filepath.Join("application", "core", "agentteam_service.go"),
		filepath.Join("gui", "headless.go"),
		filepath.Join("gui", "headless_team.go"),
		filepath.Join("gui", "headless_subagent.go"),
	}
	for _, relative := range forbidden {
		path := filepath.Join(root, relative)
		imports := fileImports(t, path)
		if _, leaked := imports[storageImport]; leaked {
			t.Errorf("%s imports %s：R2/R4 端口必须只用 application/contract DTO", relative, storageImport)
		}
	}

	// 反向断言：适配器确实承担了映射职责（收口不是"删掉依赖"，而是"依赖换位置"）。
	adapterImports := fileImports(t, filepath.Join(root, "internal", "adapters", "session_workspace_ports.go"))
	if _, ok := adapterImports[storageImport]; !ok {
		t.Errorf("internal/adapters/session_workspace_ports.go 必须承载 DTO ↔ 存储映射")
	}
}

// fileImports 返回一个 Go 文件的直接 import 路径集合。
func fileImports(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	imports := make(map[string]struct{}, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s import %s: %v", path, spec.Path.Value, err)
		}
		imports[value] = struct{}{}
	}
	if len(imports) == 0 {
		t.Fatalf("%s has no imports; 文件是否被移动/重命名？", path)
	}
	return imports
}

// TestRoleSessionDTOFieldsMatchWireContract 钉住 DTO 的 JSON 形状：真实 API 冒烟
// 与前端都按这些字段名读写，改名会同时打断 headless 巡检与 GUI 渲染。
func TestRoleSessionDTOFieldsMatchWireContract(t *testing.T) {
	sources := map[string]string{
		filepath.Join("application", "contract", "dto", "rolesession.go"): readRepoFile(t, "application", "contract", "dto", "rolesession.go"),
		filepath.Join("application", "contract", "dto", "agentteam.go"):   readRepoFile(t, "application", "contract", "dto", "agentteam.go"),
	}
	required := map[string]map[string][]string{
		filepath.Join("application", "contract", "dto", "rolesession.go"): {
			"RoleRow":              {"role_name", "role_session_id", "round_id", "unit_seq", "message_id"},
			"RoleDraftRow":         {"round_id", "role_name", "role_session_id", "unit_seq", "event"},
			"RoleSnapshot":         {"join_seq_id", "compact_ref", "order_policy", "order_roles", "floor"},
			"RoleWireSnapshot":     {"applied_seq", "prefix_digest", "need_compact", "pending_rows"},
			"ScheduleEventPayload": {"schedule_id", "role_name", "role_session_id", "next_fire_at"},
		},
		// 前端 normalizeAgentTeam/renderAgentTeam 按这些字段名读取：改名会同时
		// 打断 GUI 面板与 headless 巡检的 JSON 形状。
		filepath.Join("application", "contract", "dto", "agentteam.go"): {
			"TeamView":   {"session_id", "team_kind", "order_policy", "order_roles", "members", "scheduled", "configured", "floor_role", "design_notice"},
			"TeamMember": {"role_name", "role_kind", "role_session_id", "order_index", "in_order", "join_policy"},
			"RoleSpec":   {"role_name", "role_kind", "order_priority", "join_policy", "tools_policy"},
		},
	}
	for path, types := range required {
		source := sources[path]
		for typeName, fields := range types {
			if !strings.Contains(source, "type "+typeName+" struct") {
				t.Errorf("%s 缺少类型 %s", path, typeName)
				continue
			}
			for _, field := range fields {
				if !strings.Contains(source, `json:"`+field) {
					t.Errorf("dto 类型 %s 缺少 json 字段 %q（%s）", typeName, field, path)
				}
			}
		}
	}
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{repoRoot()}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(data)
}
