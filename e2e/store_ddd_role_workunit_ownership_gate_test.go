package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDM9RoleWorkUnitOwnershipGate 是 M9 的所有权门禁。
//
// 判据（对齐 milestones.md §13）：
//  1. `store/workunit` 持有 node/subagent 记录的落盘（文件目录 subagents/、原子写、
//     缺失即 ErrNotFound），而不是继续挂在根 `jsonRepository` 上；
//  2. `store/role` 持有 role draft 的文件形态（只追加 + 崩溃残尾 + 每角色锁）、
//     permission 的落盘，以及角色会话子树的路径与提交身份推导；
//  3. **sync 语义在 role 域里闭环**：先 commit 再删 draft，commit 失败绝不删
//     draft（半失败不许丢证据）；
//  4. 根包只剩单层转发：不得再持有 JSON 编解码、文件锁、排序或路径推导；
//  5. 根 `storeEngine` 上的递归式上帝方法（roleStore/newSubagent 等同型子引擎
//     入口）已删除，改由单一机制工厂 `newSubsessionEngine` 提供。
func TestStoreDDDM9RoleWorkUnitOwnershipGate(t *testing.T) {
	root := repoRoot()
	read := func(parts ...string) string {
		t.Helper()
		path := filepath.Join(append([]string{root}, parts...)...)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s：%v", filepath.Join(parts...), err)
		}
		return string(body)
	}
	assertContains := func(name, text string, markers ...string) {
		t.Helper()
		for _, marker := range markers {
			if !strings.Contains(text, marker) {
				t.Errorf("%s 缺少 owner 标记 %q", name, marker)
			}
		}
	}
	assertAbsent := func(name, text string, markers ...string) {
		t.Helper()
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				t.Errorf("%s 仍持有实现标记 %q：M9 后只允许单层转发", name, marker)
			}
		}
	}
	assertNoRootImport := func(name, text string) {
		t.Helper()
		if strings.Contains(text, `"github.com/RedHuang-0622/seelex/store"`) {
			t.Errorf("%s 不得 import 根 store：领域包禁止反向依赖", name)
		}
	}

	// 1) store/workunit：记录形状 + 落盘 owner。
	record := read("store", "workunit", "record.go")
	assertContains("store/workunit/record.go", record,
		"SchemaVersion = 1",
		`json:"schema_version"`,
		`json:"node_id"`,
		`json:"main_session_id"`,
		"ErrNotFound",
	)
	assertNoRootImport("store/workunit/record.go", record)

	workunitStore := read("store", "workunit", "store.go")
	assertContains("store/workunit/store.go", workunitStore,
		`DirName = "subagents"`,
		"os.MkdirAll",
		"storeroot.WriteAtomic",
		"os.ReadDir",
		"sort.Slice",
		"os.Remove",
		"func FileName(",
	)
	assertNoRootImport("store/workunit/store.go", workunitStore)
	if _, err := os.Stat(filepath.Join(root, "store", "workunit", "README.md")); err != nil {
		t.Fatalf("store/workunit 缺少模块 README：%v", err)
	}

	// 2) 根 node/subagent 记录只剩转发。
	nodeRecord := read("store", "node_session_store.go")
	assertContains("store/node_session_store.go", nodeRecord, "workunit.", ".Write(", ".Read(", ".List(", ".Delete(")
	assertAbsent("store/node_session_store.go", nodeRecord,
		"json.Marshal", "json.Unmarshal", "os.ReadFile", "os.MkdirAll", "storeroot.WriteAtomic", "sort.Slice", "filepath.Join")
	writer := read("store", "unit_record_writer.go")
	assertContains("store/unit_record_writer.go", writer, "workunit.")
	assertAbsent("store/unit_record_writer.go", writer, "errors.New", "NodeSessionStore")

	// 3) store/role：draft（只追加 + 残尾 + 每角色锁 + 排序 + 同步语义）。
	draft := read("store", "role", "draft.go")
	assertContains("store/role/draft.go", draft,
		"storeroot.TruncateCrashTail",
		"storeroot.Hash",
		"sync.Map",
		"sort.SliceStable",
		"func DraftPath(",
		"func AppendDraft(",
		"func ReadDraft(",
		"func DeleteDraft(",
		"func SortDraftRows(",
		"func DraftCommitID(",
		"func SyncDraft(",
		"func RowsToEvents(",
		"sync published but draft cleanup failed",
		"AlreadySynced",
	)
	assertNoRootImport("store/role/draft.go", draft)

	// 4) store/role：permission 落盘 owner。
	permission := read("store", "role", "permission.go")
	assertContains("store/role/permission.go", permission,
		`rolePermissionFileName  = "role-permissions.json"`,
		"storeroot.WriteAtomic",
		"func normalizedRoleVector(",
		"RolePermissionRecord",
	)
	assertNoRootImport("store/role/permission.go", permission)

	// 5) store/role：角色会话子树的路径与提交身份。
	sessionPath := read("store", "role", "session_path.go")
	assertContains("store/role/session_path.go", sessionPath,
		`prefix = "goal_"`,
		`prefix := "role_"`,
		"func SessionRoot(",
		"func SessionCommitID(",
		"func BackupCommitID(",
	)

	// 6) 根 role 文件只剩转发。
	rootRole := read("store", "role_session.go")
	assertAbsent("store/role_session.go", rootRole,
		"os.OpenFile", "sync.Map", "sort.SliceStable", "json.Unmarshal",
		"storeroot.TruncateCrashTail", "storeroot.Hash(", "func roleDraftLock(",
		"func (store *storeEngine) roleStore(",
		"func (store *storeEngine) roleSessionRoot(",
	)
	rootRoleRouter := read("store", "role_session_router.go")
	assertContains("store/role_session_router.go", rootRoleRouter,
		"storerole.AppendDraft(", "storerole.ReadDraft(")
	assertContains("store/role_session.go", rootRole, "storerole.SyncDraft(", "storerole.SessionRoot(")

	rootPermission := read("store", "role_permission.go")
	assertAbsent("store/role_permission.go", rootPermission,
		"os.ReadFile", "json.Unmarshal", "json.Marshal", "storeroot.WriteAtomic",
		"func normalizedRoleVector(", "func (repository *jsonRepository) readRolePermissionLayout(")
	assertContains("store/role_permission.go", rootPermission, "storerole.NewPermissionStore(", "storerole.PermissionStore")

	// 7) 根 storeEngine 不再有递归式子引擎入口；单一机制工厂在位。
	forks := read("store", "fork_store.go")
	assertAbsent("store/fork_store.go", forks,
		"func (store *storeEngine) subagentRoot(",
		"func (store *storeEngine) newSubagent(",
		"newStoreEngine(",
	)
	heads := read("store", "module_heads.go")
	assertContains("store/module_heads.go", heads, "func newSubsessionEngine(")
}
