package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMTeamworkIOOwnershipGate 是 M8 的所有权门禁：teamwork 的 plan 整份
// 替换、审计/绑定只追加与崩溃残尾读取必须落在 store/teamwork；team registry 的
// 读写/删除与规范化必须落在 store/role；根包只允许同身份 alias 与单层转发。
func TestStoreDDDMTeamworkIOOwnershipGate(t *testing.T) {
	root := repoRoot()

	// 1) store/teamwork 必须有 I/O owner（不再是"只有形状与纯判据"）。
	ownerPath := filepath.Join(root, "store", "teamwork", "store.go")
	owner, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatalf("store/teamwork 缺少 I/O owner store.go：%v", err)
	}
	ownerText := string(owner)
	for _, marker := range []string{
		"os.OpenFile",
		"O_APPEND",
		"storeroot.ReadSharedFile",
		"layout.Commit",
		"ModuleID",
	} {
		if !strings.Contains(ownerText, marker) {
			t.Errorf("store/teamwork/store.go 缺少 I/O owner 标记 %q", marker)
		}
	}
	if strings.Contains(ownerText, `"github.com/RedHuang-0622/seelex/store"`) {
		t.Error("store/teamwork 不得 import 根 store（领域包禁止反向依赖）")
	}

	// 2) 根包三个文件不得再持有 IO/规范化实现。
	forbidden := []string{
		"json.Marshal",
		"json.Unmarshal",
		"os.OpenFile",
		"os.MkdirAll",
		"os.ReadFile",
		"storeroot.ReadSharedFile",
		"storeroot.WriteAtomic",
		"sync.Map",
		"sort.SliceStable",
		"normalizeTeamRegistry",
	}
	for _, name := range []string{"teamwork.go", "teamwork_items.go", "team_registry.go"} {
		body, err := os.ReadFile(filepath.Join(root, "store", name))
		if err != nil {
			t.Fatalf("读取根 store/%s：%v", name, err)
		}
		text := string(body)
		for _, marker := range forbidden {
			if strings.Contains(text, marker) {
				t.Errorf("根 store/%s 仍持有实现标记 %q：M8 后只允许 alias 与单层转发", name, marker)
			}
		}
	}

	// 3) 根包必须只剩单层转发（而不是把实现删掉后没人调用）。
	teamworkForward, err := os.ReadFile(filepath.Join(root, "store", "teamwork.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{".WritePlan(", ".ReadPlan(", ".AppendEvent(", ".ReadEvents(", ".JobOutputDir("} {
		if !strings.Contains(string(teamworkForward), call) {
			t.Errorf("根 store/teamwork.go 缺少向 store/teamwork 的单层转发 %q", call)
		}
	}
	itemsForward, err := os.ReadFile(filepath.Join(root, "store", "teamwork_items.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{".AppendBinding(", ".ReadBindings("} {
		if !strings.Contains(string(itemsForward), call) {
			t.Errorf("根 store/teamwork_items.go 缺少向 store/teamwork 的单层转发 %q", call)
		}
	}

	// 4) team registry 的 owner 是 store/role（角色身份/策略；与 M9 的角色会话同子树）。
	registryPath := filepath.Join(root, "store", "role", "registry.go")
	registry, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("team registry 缺少 owner store/role/registry.go：%v", err)
	}
	for _, marker := range []string{"storeroot.WriteAtomic", "sync.Map", "func Normalize("} {
		if !strings.Contains(string(registry), marker) {
			t.Errorf("store/role/registry.go 缺少 registry owner 标记 %q", marker)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "store", "role", "README.md")); err != nil {
		t.Fatalf("store/role 缺少模块 README：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "store", "teamwork", "README.md")); err != nil {
		t.Fatalf("store/teamwork 缺少模块 README：%v", err)
	}
}
