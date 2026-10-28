package contract_test

import (
	"bufio"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/RedHuang-0622/seelex/"

// backendEdgeBudget freezes every direct production import edge below backend.
// New edges require an explicit reason here; the migration must remove edges,
// not exchange them for another unrestricted dependency.
var backendEdgeBudget = map[string]string{
	"backend/engine/assemble -> backend/engine/account":                                     "装配根能力接线",
	"backend/engine/assemble -> backend/engine/context/seelectx":                            "装配根能力接线",
	"backend/engine/assemble -> backend/engine/context/seelectx/compactor":                  "装配根能力接线",
	"backend/engine/assemble -> backend/engine/context/seelectx/provider":                   "装配根能力接线",
	"backend/engine/assemble -> backend/engine/context/seelectx/search":                     "装配根能力接线",
	"backend/engine/assemble -> backend/engine/context/seelectx/snapshot":                   "装配根能力接线",
	"backend/engine/assemble -> backend/engine/mcp":                                         "装配根能力接线",
	"backend/engine/assemble -> backend/engine/mcp/mcpstack":                                "装配根能力接线",
	"backend/engine/assemble -> backend/engine/obs":                                         "装配根能力接线",
	"backend/engine/assemble -> backend/engine/plugin/visibility":                           "装配根能力接线",
	"backend/engine/assemble -> backend/engine/schedule":                                    "装配根能力接线",
	"backend/engine/assemble -> backend/engine/skill":                                       "装配根能力接线",
	"backend/engine/assemble -> backend/limit/gate":                                         "装配根能力接线",
	"backend/engine/assemble -> backend/limit/policy":                                       "装配根能力接线",
	"backend/engine/assemble -> backend/multiagent/agentsession":                            "装配根能力接线",
	"backend/engine/assemble -> backend/multiagent/resume":                                  "装配根能力接线",
	"backend/engine/assemble -> backend/multiagent/subagent":                                "装配根能力接线",
	"backend/engine/assemble -> backend/multiagent/team":                                    "装配根能力接线",
	"backend/engine/assemble -> backend/multiagent/teamwork":                                "装配根能力接线",
	"backend/engine/assemble -> backend/tool/computer":                                      "装配根能力接线",
	"backend/engine/assemble -> backend/tool/fork":                                          "装配根能力接线",
	"backend/engine/assemble -> backend/tool/io/fs":                                         "装配根能力接线",
	"backend/engine/assemble -> backend/tool/io/media":                                      "装配根能力接线",
	"backend/engine/assemble -> backend/tool/jobs":                                          "装配根能力接线",
	"backend/engine/assemble -> backend/tool/node":                                          "装配根能力接线",
	"backend/engine/assemble -> backend/tool/registry":                                      "装配根能力接线",
	"backend/engine/assemble -> backend/tool/review":                                        "装配根能力接线",
	"backend/engine/assemble -> backend/tool/ssh":                                           "装配根能力接线",
	"backend/engine/assemble -> backend/tool/task":                                          "装配根能力接线",
	"backend/engine/assemble -> backend/tool/worktree":                                      "装配根能力接线",
	"backend/engine/assemble -> backend/workunit":                                           "装配根能力接线",
	"backend/engine/context/seelectx -> backend/engine/context/seelectx/compactor":          "上下文字包协作",
	"backend/engine/context/seelectx -> backend/engine/context/seelectx/provider":           "上下文字包协作",
	"backend/engine/context/seelectx -> backend/engine/context/seelectx/snapshot":           "上下文字包协作",
	"backend/engine/context/seelectx -> backend/engine/context/seelectx/tokens":             "上下文字包协作",
	"backend/engine/context/seelectx/compactor -> backend/engine/context/seelectx/snapshot": "压缩快照",
	"backend/engine/context/seelectx/compactor -> backend/engine/context/seelectx/tokens":   "压缩预算",
	"backend/engine/context/seelectx/memory -> backend/engine/context/seelectx/tokens":      "记忆预算",
	"backend/engine/context/seelectx/merger -> backend/engine/context/seelectx/snapshot":    "合并快照",
	"backend/engine/context/seelectx/provider -> backend/engine/context/seelectx/snapshot":  "快照适配",
	"backend/engine/context/seelectx/search -> backend/engine/context/seelectx/memory":      "相关记忆检索",
	"backend/engine/context/seelectx/search -> backend/engine/context/seelectx/tokens":      "检索预算",
	"backend/engine/mcp -> backend/engine/mcp/mcpstack":                                     "MCP 栈适配",
	"backend/engine/mcp/mcpstack -> backend/engine/context/seelectx/snapshot":               "MCP 快照",
	"backend/engine/mcp/mcpstack -> backend/engine/context/seelectx/tokens":                 "MCP 预算",
	"backend/engine/plugin -> backend/engine/skill":                                         "插件技能装配",
	"backend/engine/schedule -> backend/limit/gate":                                         "定时命令安全门",
	"backend/multiagent/subagent -> backend/engine/context/seelectx":                        "子代理上下文适配",
	"backend/multiagent/subagent -> backend/engine/context/seelectx/merger":                 "父证据合并",
	"backend/multiagent/subagent -> backend/engine/context/seelectx/provider":               "子代理追踪",
	"backend/multiagent/subagent -> backend/engine/context/seelectx/snapshot":               "子代理快照",
	"backend/multiagent/subagent -> backend/multiagent/agentsession":                        "独立会话工厂",
	"backend/multiagent/subagent -> backend/multiagent/resume":                              "恢复模板",
	"backend/multiagent/subagent -> backend/tool/fork":                                      "子代理派发形状",
	"backend/multiagent/subagent -> backend/workunit":                                       "执行单元收口",
	"backend/multiagent/team -> backend/engine/plugin/visibility":                           "角色插件可见性",
	"backend/multiagent/teamwork -> backend/workunit":                                       "工作项生命周期",
	"backend/tool/computer/mcp -> backend/tool/computer":                                    "电脑工具 MCP 适配",
	"backend/tool/fork -> backend/tool/node":                                                "节点执行",
	"backend/tool/fork -> backend/tool/task":                                                "子代理任务登记",
	"backend/tool/io/media -> backend/tool/computer":                                        "媒体电脑能力",
	"backend/tool/io/media -> backend/tool/io/imageattach":                                  "媒体附件",
	"backend/tool/jobs -> backend/tool/registry":                                            "后台作业投影",
	"backend/tool/node -> backend/engine/context/seelectx":                                  "节点上下文装配",
	"backend/tool/node -> backend/engine/context/seelectx/provider":                         "节点追踪",
	"backend/tool/node -> backend/engine/context/seelectx/snapshot":                         "节点快照",
	"backend/tool/node -> backend/engine/skill":                                             "节点技能匹配",
	"backend/tool/node -> backend/tool/worktree":                                            "节点工作树",
	"backend/tool/node -> backend/workunit":                                                 "节点执行单元",
	"backend/tool/registry -> backend/limit/gate":                                           "工具安全门",
	"backend/tool/registry -> backend/limit/policy":                                         "工具权限策略",
	"backend/tool/registry -> backend/tool/io/fs":                                           "文件工具适配",
	"backend/tool/registry -> backend/tool/ssh":                                             "SSH 工具适配",
	"backend/tool/registry -> backend/workunit":                                             "工具执行单元",
	"backend/tool/ssh/sshprobe -> backend/tool/ssh":                                         "SSH 探针",
	"backend/tool/websearch -> backend/tool/search":                                         "网络搜索适配",
	"backend/tool/worktree -> backend/limit/gate":                                           "工作树安全门",
	"backend/tool/worktree -> backend/workunit":                                             "工作树执行单元",
}

var backendMiddleBudget = map[string]string{
	"backend/engine/assemble -> middle/view/sessionaccess": "BM-005 装配迁出",
}

var middleBackendBudget = map[string]string{}

var backendApplicationBudget = map[string]string{}

// oversizedFileBudget freezes the current sizes of legacy files above the
// repository's 500-line guideline. Existing files may shrink but never grow;
// newly introduced files are held to the 500-line budget without exception.
var oversizedFileBudget = map[string]int{
	"contract/ports.go":                                  629,
	"contract/protocol/state.go":                         1233,
	"application/session/viewstate/coordinator.go":       738,
	"backend/tool/worktree/worktree_manager.go":          1153,
	"backend/tool/task/task.go":                          795,
	"application/workspace/switch/workspace.go":          574,
	"backend/tool/registry/job_tools.go":                 527,
	"backend/tool/registry/async_exec.go":                1069,
	"backend/tool/computer/uia_windows.go":               692,
	"backend/tool/registry/router.go":                    878,
	"backend/limit/policy/policy.go":                     545,
	"backend/tool/registry/registry_state.go":            678,
	"backend/tool/registry/permission_policy.go":         725,
	"backend/tool/computer/mcp/main.go":                  894,
	"backend/engine/task/task_service.go":                512,
	"backend/engine/task/task_context_state.go":          1888,
	"backend/engine/task/ledger.go":                      532,
	"backend/engine/task/coordinator.go":                 790,
	"kernel/limit/limits.go":                             779,
	"backend/engine/schedule/schedule.go":                949,
	"backend/engine/session/coordinator.go":              547,
	"backend/engine/session/archive.go":                  664,
	"backend/engine/session/fork.go":                     611,
	"backend/engine/plugin/curated.go":                   692,
	"backend/multiagent/teamwork/items.go":               1295,
	"backend/multiagent/teamwork/coordinator.go":         660,
	"backend/multiagent/subagent/subagent_sessions.go":   870,
	"backend/multiagent/subagent/subagent_tree.go":       651,
	"backend/engine/context/coordinator.go":              1745,
	"backend/engine/context/seelectx/dag.go":             627,
	"backend/engine/assemble/runtime.go":                 1024,
	"backend/engine/assemble/runtime_role_turn.go":       783,
	"backend/engine/assemble/runtime_subagent_resume.go": 516,
	"backend/engine/assemble/runtime_teamwork.go":        899,
}

func TestBM001DependencyBudgetIsFrozen(t *testing.T) {
	root := repositoryRoot(t)
	edges := productionImportEdges(t, root)
	assertEdgeBudget(t, "backend internal", edges.backend, backendEdgeBudget)
	assertEdgeBudget(t, "backend -> middle", edges.backendMiddle, backendMiddleBudget)
	assertEdgeBudget(t, "middle -> backend", edges.middleBackend, middleBackendBudget)
	assertEdgeBudget(t, "backend -> application", edges.backendApplication, backendApplicationBudget)
}

func TestBM003AndBM004TargetEdgesAreZero(t *testing.T) {
	root := repositoryRoot(t)
	edges := productionImportEdges(t, root)
	if len(edges.middleBackend) != 0 {
		t.Errorf("BM-003: middle must not import backend; remaining:\n%s", formatEdges(edges.middleBackend))
	}
	if len(edges.backendApplication) != 0 {
		t.Errorf("BM-004: backend must not import application; remaining:\n%s", formatEdges(edges.backendApplication))
	}
}

func TestBM002BackendOnlyKeepsRuntimeIntegrationToMiddle(t *testing.T) {
	root := repositoryRoot(t)
	edges := productionImportEdges(t, root)
	want := map[string]string{
		"backend/engine/assemble -> middle/view/sessionaccess": "BM-005 runtime/sessionaccess query wiring",
	}
	assertEdgeBudget(t, "backend -> middle after BM-002", edges.backendMiddle, want)
}

func TestBM001FileSizeBudget(t *testing.T) {
	root := repositoryRoot(t)
	files := productionGoFiles(t, root)
	for _, relative := range files {
		lines := lineCount(t, filepath.Join(root, filepath.FromSlash(relative)))
		limit, frozen := oversizedFileBudget[relative]
		if !frozen {
			limit = 500
		}
		if lines > limit {
			t.Errorf("%s has %d lines; budget is %d (shrink or split before growing)", relative, lines, limit)
		}
	}
}

type importEdges struct {
	backend            []string
	backendMiddle      []string
	middleBackend      []string
	backendApplication []string
}

func productionImportEdges(t *testing.T, root string) importEdges {
	t.Helper()
	var result importEdges
	fset := token.NewFileSet()
	for _, relative := range productionGoFiles(t, root) {
		filename := filepath.Join(root, filepath.FromSlash(relative))
		parsed, err := parser.ParseFile(fset, filename, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		from := strings.TrimPrefix(filepath.ToSlash(filepath.Dir(relative)), "./")
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", relative, err)
			}
			if !strings.HasPrefix(importPath, modulePath+"backend/") &&
				!strings.HasPrefix(importPath, modulePath+"middle/") &&
				!strings.HasPrefix(importPath, modulePath+"application/") {
				continue
			}
			to := strings.TrimPrefix(importPath, modulePath)
			edge := from + " -> " + to
			switch {
			case strings.HasPrefix(from, "backend/") && strings.HasPrefix(to, "backend/"):
				result.backend = append(result.backend, edge)
			case strings.HasPrefix(from, "backend/") && strings.HasPrefix(to, "middle/"):
				result.backendMiddle = append(result.backendMiddle, edge)
			case strings.HasPrefix(from, "middle/") && strings.HasPrefix(to, "backend/"):
				result.middleBackend = append(result.middleBackend, edge)
			case strings.HasPrefix(from, "backend/") && strings.HasPrefix(to, "application/"):
				result.backendApplication = append(result.backendApplication, edge)
			}
		}
	}
	result.backend = sortedUnique(result.backend)
	result.backendMiddle = sortedUnique(result.backendMiddle)
	result.middleBackend = sortedUnique(result.middleBackend)
	result.backendApplication = sortedUnique(result.backendApplication)
	return result
}

func assertEdgeBudget(t *testing.T, label string, got []string, budget map[string]string) {
	t.Helper()
	gotSet := make(map[string]struct{}, len(got))
	for _, edge := range got {
		gotSet[edge] = struct{}{}
	}
	var added, removed []string
	for edge := range gotSet {
		if _, ok := budget[edge]; !ok {
			added = append(added, edge)
		}
	}
	for edge := range budget {
		if _, ok := gotSet[edge]; !ok {
			removed = append(removed, edge)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 0 {
		t.Errorf("%s gained unapproved edges:\n%s", label, formatEdges(added))
	}
	if len(removed) > 0 {
		t.Errorf("%s budget is stale (shrink it to lock the improvement):\n%s", label, formatEdges(removed))
	}
}

func productionGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, top := range []string{"backend", "middle", "contract"} {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	sort.Strings(files)
	return files
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Dir(filepath.Dir(filename))
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func formatEdges(edges []string) string {
	if len(edges) == 0 {
		return "(none)"
	}
	return strings.Join(edges, "\n")
}
