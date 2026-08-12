package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/adapters"
	"github.com/RedHuang-0622/seelex/plugin"
)

var repositoryModules = []string{
	".", ".claude", ".github", "application", "application/approval", "application/contract",
	"application/core", "application/event", "application/model", "application/prompt",
	"application/search", "application/adapters", "application/console", "config", "docs",
	"docs/arch", "docs/devlog", "docs/gui", "docs/gui/schemas", "docs/product", "docs/research",
	"docs/test", "e2e", "e2e/scenario", "gui", "gui/frontend", "internal", "internal/buildinfo",
	"internal/frontmatter", "mcpstack", "mcpstack/config", "plugin", "plugins", "plugins/default",
	"plugins/freecad", "plugins/git", "plugins/read", "plugins/shell", "plugins/write", "scripts",
	"seelebridge", "seelebridge/fork", "seelebridge/fs", "seelebridge/internal/config",
	"seelebridge/internal/model", "seelebridge/internal/storage", "seelebridge/internal/stream",
	"seelebridge/internal/telemetry", "seelebridge/plan",
	"seelebridge/security", "seelebridge/task", "seelebridge/tools/websearch", "seelexctx", "seelexctx/compactor",
	"seelexctx/merger", "seelexctx/provider", "seelexctx/search", "seelexctx/snapshot",
	"session", "sessionstore", "skill", "tui", "tui/splash", "workspace",
}

var repositoryModuleDocumentation = map[string]string{
	".github": "AUTOMATION.md",
}

func repositoryModuleDocumentationPath(module string) string {
	name := "README.md"
	if configured, ok := repositoryModuleDocumentation[module]; ok {
		name = configured
	}
	return filepath.Join(repoRoot(), module, name)
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
		if actual := adapters.ApprovalAccepted(optionID); actual != expected {
			t.Fatalf("adapters.ApprovalAccepted(%q) = %v, want %v", optionID, actual, expected)
		}
	}
}

func TestRepositoryAgentDocumentationRules(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "AGENTS.md"))
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
		path := repositoryModuleDocumentationPath(module)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("module %q documentation %q: %v", module, path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("module %q documentation %q is empty", module, path)
		}
	}
}

func TestRepositoryModuleReadmeLinks(t *testing.T) {
	pattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, module := range repositoryModules {
		path := repositoryModuleDocumentationPath(module)
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

func TestGitHubAutomationDocumentationDoesNotOverrideRepositoryReadme(t *testing.T) {
	root := repoRoot()
	if path := repositoryModuleDocumentationPath(".github"); path != filepath.Join(root, ".github", "AUTOMATION.md") {
		t.Fatalf(".github documentation path = %q, want %q", path, filepath.Join(root, ".github", "AUTOMATION.md"))
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "README.md")); err == nil {
		t.Fatal(".github/README.md overrides the repository README on the GitHub home page")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect .github/README.md: %v", err)
	}
}

func TestRepositorySkillAndPluginLayouts(t *testing.T) {
	root := repoRoot()
	plugins, err := plugin.NewLoader(filepath.Join(root, "plugins")).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 6 {
		t.Fatalf("loaded %d plugins, want 6 (5 base + freecad)", len(plugins))
	}
	for _, p := range plugins {
		t.Logf("  plugin=%q skills=%d", p.Name, len(p.Skills))
	}
	if _, err := plugin.NewLoader(filepath.Join(root, "plugins")).Load("plan"); err == nil {
		t.Fatal("plan must be a default skill, not an independently loadable plugin")
	}
	for _, p := range plugins {
		if p.Name != "default" {
			continue
		}
		for _, skill := range p.Skills {
			if skill.Name == "plan" {
				return
			}
		}
		t.Fatal("default plugin is missing the plan skill")
	}
	t.Fatal("default plugin was not loaded")
}
