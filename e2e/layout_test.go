package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/plugin"
)

var repositoryModules = []string{
	".", ".claude", ".github", "application", "application/approval", "application/contract",
	"application/core", "application/core/chat", "application/core/context_control",
	"application/core/context_runtime", "application/core/input_router",
	"application/core/internal/limits", "application/core/internal/state",
	"application/core/prompt_layer", "application/core/session_runtime",
	"application/core/subagent_view", "application/core/task_context",
	"application/core/view_state", "application/core/worktable",
	"application/event", "application/model", "application/prompt",
	"application/console", "config", "docs",
	"docs/arch", "docs/devlog", "docs/gui", "docs/gui/schemas", "docs/product", "docs/research",
	"docs/test", "e2e", "e2e/scenario", "gui", "gui/frontend", "internal", "internal/adapters",
	"internal/buildinfo", "internal/frontmatter", "mcpstack", "mcpstack/config", "plugin", "plugins", "plugins/default",
	"plugins/freecad", "scripts",
	"seelebridge", "seelebridge/fork", "seelebridge/fs", "seelebridge/internal/config",
	"seelebridge/internal/model", "seelebridge/internal/stream",
	"seelebridge/internal/telemetry", "seelebridge/plan",
	"seelebridge/search", "seelebridge/security", "seelebridge/task", "seelebridge/tools/websearch", "seelexctx", "seelexctx/compactor",
	"seelexctx/merger", "seelexctx/provider", "seelexctx/search", "seelexctx/snapshot",
	"session", "sessionstore", "skill", "tui", "tui/splash", "workspace",
}

var repositoryModuleDocumentation = map[string]string{
	".github": "AUTOMATION.md",
}

// mermaidDiagramTypes 是校验器认得的图类型首行。erDiagram / classDiagram 用
// { } 表示实体与成员体，括号天然不成对，只做引号检查。
var mermaidDiagramTypes = []string{
	"flowchart", "graph", "sequenceDiagram", "stateDiagram-v2", "stateDiagram",
	"classDiagram", "erDiagram", "journey", "gantt", "pie", "mindmap", "timeline",
	"quadrantChart",
}

func repositoryModuleDocumentationPath(module string) string {
	name := "README.md"
	if configured, ok := repositoryModuleDocumentation[module]; ok {
		name = configured
	}
	return filepath.Join(repoRoot(), module, name)
}

// TestModuleReadmesMermaidBlocksAreStructurallyValid 是 Mermaid 图的结构门禁：
// fence 必须闭合、首行必须是已知图类型、引号与括号（flowchart/state/sequence）
// 必须配平、flowchart 的 subgraph/end 必须成对。它不替代真正的渲染器，但能挡住
// 手写图最常见的语法退化；本地详细报告见 scripts/check_mermaid.py。
func TestModuleReadmesMermaidBlocksAreStructurallyValid(t *testing.T) {
	root := repoRoot()
	blocks := 0
	files := 0
	skipped := map[string]bool{
		".git": true, "dist": true, "_tmp": true, "tmp": true,
		"node_modules": true, ".gocache": true, ".venv": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipped[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(entry.Name(), "README") || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		count := strings.Count(string(data), "```mermaid")
		if count == 0 {
			return nil
		}
		files++
		blocks += count
		for _, problem := range mermaidProblems(string(data)) {
			t.Errorf("%s: %s", filepath.ToSlash(rel), problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocks == 0 {
		t.Fatal("no mermaid blocks were inspected; the walker is not matching README files")
	}
	t.Logf("checked %d mermaid blocks across %d README files", blocks, files)
}

func mermaidProblems(text string) []string {
	lines := strings.Split(text, "\n")
	var problems []string
	for index := 0; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != "```mermaid" {
			continue
		}
		start := index + 1
		cursor := start
		for cursor < len(lines) && strings.TrimSpace(lines[cursor]) != "```" {
			cursor++
		}
		if cursor >= len(lines) {
			problems = append(problems, fmt.Sprintf("line %d: mermaid fence is not closed", start))
			return problems
		}
		problems = append(problems, checkMermaidBlock(start, lines[start:cursor])...)
		index = cursor
	}
	return problems
}

func checkMermaidBlock(start int, block []string) []string {
	var problems []string
	var content []string
	for offset, line := range block {
		if strings.TrimSpace(line) != "" {
			content = append(content, fmt.Sprintf("%d\x00%s", start+offset, line))
		}
	}
	if len(content) == 0 {
		return []string{fmt.Sprintf("line %d: empty mermaid block", start)}
	}
	first := strings.SplitN(content[0], "\x00", 2)[1]
	header := strings.TrimSpace(first)
	known := false
	for _, kind := range mermaidDiagramTypes {
		if strings.HasPrefix(header, kind) {
			known = true
			break
		}
	}
	if !known {
		problems = append(problems, fmt.Sprintf("line %d: unknown diagram type %q", start, header))
	}
	isFlowchart := strings.HasPrefix(header, "flowchart") || strings.HasPrefix(header, "graph")
	checkBrackets := isFlowchart || strings.HasPrefix(header, "stateDiagram") ||
		strings.HasPrefix(header, "sequenceDiagram")
	subgraphs := 0
	for _, entry := range content {
		parts := strings.SplitN(entry, "\x00", 2)
		lineNo, raw := parts[0], parts[1]
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "%%") {
			continue
		}
		if isFlowchart {
			switch {
			case strings.HasPrefix(line, "subgraph"):
				subgraphs++
			case line == "end":
				subgraphs--
				if subgraphs < 0 {
					problems = append(problems, fmt.Sprintf("line %s: unmatched end", lineNo))
					subgraphs = 0
				}
			}
		}
		if strings.Count(line, `"`)%2 != 0 {
			problems = append(problems, fmt.Sprintf("line %s: unbalanced quotes: %q", lineNo, line))
		}
		if checkBrackets {
			for _, pair := range [][2]string{{"[", "]"}, {"(", ")"}, {"{", "}"}} {
				if strings.Count(line, pair[0]) != strings.Count(line, pair[1]) {
					problems = append(problems, fmt.Sprintf("line %s: unbalanced %s%s: %q", lineNo, pair[0], pair[1], line))
				}
			}
		}
		if strings.Contains(line, "<code>") || strings.Contains(line, "</code>") {
			problems = append(problems, fmt.Sprintf("line %s: HTML tag inside a diagram: %q", lineNo, line))
		}
	}
	if subgraphs != 0 {
		problems = append(problems, fmt.Sprintf("line %d: subgraph/end mismatch (delta %d)", start, subgraphs))
	}
	return problems
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

// TestEveryGoPackageDirectoryHasReadme 是 AGENTS.md「模块 README 必备内容」的
// 机械门禁：任何含 .go 文件的目录都必须有非空 README.md。它覆盖手工维护的
// repositoryModules 清单（新增包时不再可能静默漏文档）。
func TestEveryGoPackageDirectoryHasReadme(t *testing.T) {
	root := repoRoot()
	skipped := map[string]bool{
		".git": true, "dist": true, "_tmp": true, "tmp": true,
		"node_modules": true, ".gocache": true, ".venv": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && skipped[entry.Name()] {
			return filepath.SkipDir
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		hasGo := false
		for _, child := range entries {
			if !child.IsDir() && strings.HasSuffix(child.Name(), ".go") {
				hasGo = true
				break
			}
		}
		if !hasGo {
			return nil
		}
		readme := filepath.Join(path, "README.md")
		info, err := os.Stat(readme)
		if err != nil {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("Go package directory %q has no README.md: %v", rel, err)
			return nil
		}
		if info.Size() == 0 {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("Go package directory %q has an empty README.md", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
	if len(plugins) != 2 {
		t.Fatalf("loaded %d plugins, want 2 (default + freecad)", len(plugins))
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
