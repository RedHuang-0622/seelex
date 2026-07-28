package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
)

var repositoryModules = []string{
	".", ".claude", ".github", "application", "application/approval", "application/contract",
	"application/core", "application/event", "application/model", "application/prompt",
	"application/search", "config", "docs", "docs/arch", "docs/devlog", "docs/gui",
	"docs/gui/schemas", "docs/product", "docs/research", "docs/test", "e2e",
	"e2e/scenario", "gui", "gui/frontend", "internal", "internal/frontmatter",
	"mcpstack", "plugin", "plugins", "plugins/default", "plugins/freecad", "plugins/git",
	"plugins/plan", "plugins/read", "plugins/shell", "plugins/write", "scripts",
	"seelebridge", "seelexctx", "seelexctx/compactor", "seelexctx/merger",
	"seelexctx/provider", "seelexctx/snapshot", "session", "sessionstore", "skill",
	"tui", "tui/splash", "workspace",
}

func TestApprovalAccepted(t *testing.T) {
	tests := map[string]bool{
		"Yes":        true,
		"confirm":    true,
		"No":         false,
		"deny":       false,
		"拒绝":         false,
		"__CANCEL__": false,
	}
	for optionID, expected := range tests {
		if actual := approvalAccepted(optionID); actual != expected {
			t.Fatalf("approvalAccepted(%q) = %v, want %v", optionID, actual, expected)
		}
	}
}

func TestRepositoryAgentDocumentationRules(t *testing.T) {
	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"文档放置规范", "模块 README 必备内容", "Review 清单", "不自动 commit 或 push"} {
		if !strings.Contains(text, required) {
			t.Errorf("AGENTS.md is missing %q", required)
		}
	}
}

func TestRepositoryModulesHaveReadmes(t *testing.T) {
	for _, module := range repositoryModules {
		path := filepath.Join(module, "README.md")
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("module %q README: %v", module, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("module %q README is empty", module)
		}
	}
}

func TestRepositoryModuleReadmeLinks(t *testing.T) {
	pattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, module := range repositoryModules {
		path := filepath.Join(module, "README.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			target := strings.Trim(match[1], "<>")
			target, _, _ = strings.Cut(target, "#")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Join(filepath.Dir(path), filepath.FromSlash(target))
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s links to %q: %v", path, match[1], err)
			}
		}
	}
}

func TestRepositorySkillAndPluginLayouts(t *testing.T) {
	plugins, err := plugin.NewLoader("plugins").LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 7 {
		t.Fatalf("loaded %d plugins, want 7 (6 original + freecad)", len(plugins))
	}
	for _, p := range plugins {
		t.Logf("  plugin=%q skills=%d", p.Name, len(p.Skills))
	}
}
