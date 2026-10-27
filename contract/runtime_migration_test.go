package contract_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// runtimeAssemblyInventory 冻结 backend/engine/assemble 每个生产文件的迁移归属。
// 新增文件必须先在这里声明它最终属于 runtime 生命周期、能力适配、Application
// process manager 还是 WorkUnit/Teamwork 用例，防止 assemble 继续无界生长。
var runtimeAssemblyInventory = map[string]string{
	"events_unified.go":                 "wiring/runtime",
	"review_files.go":                   "wiring/registry",
	"role_host_session.go":              "application/conversation",
	"runtime.go":                        "wiring/runtime",
	"runtime_account.go":                "wiring/runtime",
	"runtime_agent_conversation.go":     "application/conversation",
	"runtime_async.go":                  "wiring/runtime",
	"runtime_bundle.go":                 "wiring/runtime",
	"runtime_compaction_index.go":       "wiring/runtime",
	"runtime_computer.go":               "wiring/runtime",
	"runtime_context.go":                "wiring/runtime",
	"runtime_deps.go":                   "wiring/runtime",
	"runtime_history.go":                "wiring/runtime",
	"runtime_live.go":                   "wiring/runtime",
	"runtime_mcp.go":                    "wiring/runtime",
	"runtime_node.go":                   "wiring/runtime",
	"runtime_plugin.go":                 "wiring/runtime",
	"runtime_plugin_capabilities.go":    "wiring/runtime",
	"runtime_project_board.go":          "wiring/runtime",
	"runtime_role_plugins.go":           "application/conversation",
	"runtime_role_prompt.go":            "application/conversation",
	"runtime_role_turn.go":              "application/conversation",
	"runtime_role_user_spaces.go":       "application/conversation",
	"runtime_schedule.go":               "wiring/runtime",
	"runtime_session.go":                "application/session",
	"runtime_session_release.go":        "application/session",
	"runtime_subagent.go":               "application/subagent",
	"runtime_subagent_recovery.go":      "application/subagent",
	"runtime_subagent_resume.go":        "application/subagent",
	"runtime_subagent_session.go":       "application/subagent",
	"runtime_task.go":                   "wiring/runtime",
	"runtime_teammate_inspection.go":    "application/teamwork",
	"runtime_teammate_session_live.go":  "application/teamwork",
	"runtime_teamwork.go":               "application/teamwork",
	"runtime_teamwork_board.go":         "application/teamwork",
	"runtime_teamwork_board_archive.go": "application/teamwork",
	"runtime_teamwork_board_close.go":   "application/teamwork",
	"runtime_teamwork_context.go":       "application/teamwork",
	"runtime_teamwork_curated.go":       "application/teamwork",
	"runtime_teamwork_items.go":         "application/teamwork",
	"runtime_teamwork_jobs.go":          "application/teamwork",
	"runtime_teamwork_scene.go":         "application/teamwork",
	"runtime_teamwork_scope.go":         "application/teamwork",
	"runtime_tools.go":                  "wiring/runtime",
	"runtime_transport.go":              "wiring/runtime",
	"runtime_unit_recovery.go":          "application/subagent",
	"runtime_visibility.go":             "wiring/runtime",
	"runtime_workitem_execution.go":     "application/teamwork",
	"strings.go":                        "wiring/runtime",
	"teamwork_job_namer.go":             "backend/multiagent/teamwork",
	"teamwork_job_signals.go":           "wiring/runtime",
	"workunit_assembly.go":              "application/teamwork/workunit",
	"workunit_node.go":                  "application/teamwork/workunit",
	"workunit_team.go":                  "application/teamwork/workunit",
	"workunit_team_records.go":          "application/teamwork/workunit",
}

func TestBM005RuntimeAssemblyProductionFilesAreClassified(t *testing.T) {
	root := repositoryRoot(t)
	var unexpected []string
	for _, relative := range productionGoFiles(t, root) {
		if !strings.HasPrefix(relative, "backend/engine/assemble/") {
			continue
		}
		name := strings.TrimPrefix(relative, "backend/engine/assemble/")
		target, ok := runtimeAssemblyInventory[name]
		if !ok || strings.TrimSpace(target) == "" {
			unexpected = append(unexpected, name)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Fatalf("assemble 生产文件未登记迁移归属：\n%s", strings.Join(unexpected, "\n"))
	}
}

func TestBM005DomainPackagesDoNotImportRuntimeAssembly(t *testing.T) {
	root := repositoryRoot(t)
	edges := productionImportEdges(t, root)
	var violations []string
	for _, edge := range append(append([]string(nil), edges.backend...), edges.backendMiddle...) {
		parts := strings.Split(edge, " -> ")
		if len(parts) != 2 || parts[0] == "backend/engine/assemble" {
			continue
		}
		if parts[1] == "backend/engine/assemble" || parts[1] == "wiring/runtime" {
			violations = append(violations, edge)
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("领域包反向 import 组合根：\n%s", strings.Join(violations, "\n"))
	}
}

func TestBM005ReviewFilesRegistrationIsOwnedByWiring(t *testing.T) {
	root := repositoryRoot(t)
	read := func(relative string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		return string(body)
	}

	domain := read("backend/tool/review/show_files.go")
	registry := read("wiring/registry/registry.go")
	compat := read("backend/engine/assemble/review_files.go")
	if !strings.Contains(domain, "func RegisterShowFilesTool(") {
		t.Error("backend/tool/review must own the single ToolRegistrar registration helper")
	}
	// 本用例锁的是「注册归 wiring、端口来自 owner 组件」，不是「端口来自兼容门面」：
	// application/compat 已登记退场（docs/2026-10-27-application-compat-removal D1），
	// 字面量因此钉在 owner 指针，并额外禁止门面重新出现在注册面。
	if !strings.Contains(registry, "review.RegisterShowFilesTool(runtime, appassembly.ComponentsFor(app).Admin.Review)") {
		t.Error("wiring/registry must call review.RegisterShowFilesTool with the review owner component")
	}
	if strings.Contains(registry, "applicationcompat.New(app)") {
		t.Error("wiring/registry must not take the show_files port from the compat facade")
	}
	if strings.Contains(registry, "assemble.RegisterReviewFilesTool(runtime, app)") {
		t.Error("wiring/registry still routes show_files registration through the composition package")
	}
	if !strings.Contains(compat, "review.RegisterShowFilesTool(runtime, port)") {
		t.Error("the legacy assemble path must be a one-line forward to review.RegisterShowFilesTool")
	}
}
