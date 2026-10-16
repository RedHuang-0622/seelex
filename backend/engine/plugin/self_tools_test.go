package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestPluginCreateSkillReloadCycle 覆盖自迭代闭环：
// plugin_create 写盘 → plugins_reload 新增 → skill_create 写盘 →
// plugins_reload 更新（active 插件停用后按新定义重新激活）→ 删除目录后
// plugins_reload 移除。
func TestPluginCreateSkillReloadCycle(t *testing.T) {
	root := t.TempDir()
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(root), tools, mcp, skills)
	if err := m.Load(); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	paths, err := m.Create(CreateSpec{
		Name: "alpha", Description: "self-built plugin",
		Skills: []SkillSpec{{Name: "s1", Description: "skill one", Prompt: "do one"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("created files = %v, want manifest+readme+skill", paths)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", manifestFile)); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}

	report, err := m.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload after create: %v", err)
	}
	if len(report.Added) != 1 || report.Added[0] != "alpha" {
		t.Fatalf("reload added = %v", report.Added)
	}
	if !tools.defined["alpha"] || !skills.published["alpha"] {
		t.Fatalf("alpha not defined/published: tools=%v skills=%v", tools.defined, skills.published)
	}
	alpha, ok := m.All()[0], false
	for _, p := range m.All() {
		if p.Name == "alpha" {
			alpha, ok = p, true
		}
	}
	if !ok || len(alpha.Skills) != 1 || alpha.Skills[0].Name != "s1" {
		t.Fatalf("alpha skills = %+v", alpha.Skills)
	}

	if err := m.Activate(context.Background(), "alpha"); err != nil {
		t.Fatalf("activate alpha: %v", err)
	}
	path, err := m.CreateSkill("alpha", SkillSpec{Name: "s2", Description: "skill two", Prompt: "do two"})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("skill file not written: %v", err)
	}
	report, err = m.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload after skill create: %v", err)
	}
	if len(report.Updated) != 1 || report.Updated[0] != "alpha" {
		t.Fatalf("reload updated = %v", report.Updated)
	}
	if tools.active != "alpha" || skills.active != "alpha" || report.Active != "alpha" {
		t.Fatalf("active plugin not restored: tools=%q skills=%q active=%q", tools.active, skills.active, report.Active)
	}

	if err := os.RemoveAll(filepath.Join(root, "alpha")); err != nil {
		t.Fatal(err)
	}
	report, err = m.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload after remove: %v", err)
	}
	if len(report.Removed) != 1 || report.Removed[0] != "alpha" {
		t.Fatalf("reload removed = %v", report.Removed)
	}
	if tools.defined["alpha"] || tools.active != "" {
		t.Fatalf("alpha still defined/active after removal: %v active=%q", tools.defined, tools.active)
	}
}

// TestPluginReloadRollbackOnDefineFailure 验证 reload 事务性：新增插件
// DefinePlugin 失败时回滚到上一个可用状态（旧插件保持已定义与激活）。
func TestPluginReloadRollbackOnDefineFailure(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, filepath.Join(root, "keep", manifestFile), "---\nname: keep\n---\n# Keep\n")
	tools := &fakeTools{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(root), tools, &fakeMCP{}, skills)
	if err := m.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Activate(context.Background(), "keep"); err != nil {
		t.Fatalf("activate keep: %v", err)
	}

	mustPluginWrite(t, filepath.Join(root, "bad", manifestFile), "---\nname: bad\n---\n# Bad\n")
	tools.defineFail = "bad"
	if _, err := m.Reload(context.Background()); err == nil {
		t.Fatal("reload should fail when define fails")
	}
	if !tools.defined["keep"] || tools.active != "keep" {
		t.Fatalf("rollback lost keep: defined=%v active=%q", tools.defined, tools.active)
	}
	if tools.defined["bad"] {
		t.Fatalf("bad plugin should not be defined after rollback: %v", tools.defined)
	}
	if current, ok := m.Current(); !ok || current.Name != "keep" {
		t.Fatalf("current after rollback = %q ok=%v", current.Name, ok)
	}
}
