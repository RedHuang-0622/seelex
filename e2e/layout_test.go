package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/plugin"
	"github.com/RedHuang-0622/seelex/wiring/adapters"
)

var repositoryModules = []string{
	".", ".claude", ".github", "application", "contract",
	"application/core", "backend/engine/react", "backend/engine/input/contextcontrol",
	"backend/engine/context", "backend/engine/input",
	"kernel/state",
	"backend/engine/prompt", "backend/engine/session",
	"middle/view/subagent", "backend/engine/task",
	"middle/view/state", "middle/view/worktable",
	// M5-R5：项目看板的行投影与同步器从 application/core/project_board.go 归位；
	// boardtest 是两侧共用的测试夹具包（无生产调用方）。
	"middle/view/board", "middle/view/board/boardtest",
	// M5-R5c：视图读数面（诊断文本 / 性能指标 / 运行时投影）从 application/core
	// 的三个根文件归位到中间层视图根包（plan §2.4）。
	"middle/view",
	// M5-R5d：错误呈现域（原 application/core/{error_codes,error_presentation}.go）
	// 归位中间层视图子包（plan §2.4）；跨层共享的错误判据（码/provider 失败分类/
	// ReAct 预算哨兵）下沉 contract，中间层与后端都只依赖契约。
	"middle/view/errors",
	// M5-R8a：goal 域（原 application/core/goal，35 文件）归位多代理板块
	// （plan §2.7）。同一份代码换落点，包名与判据不变。
	"backend/multiagent/goal",
	// M5-R8b：agentteam（角色身份 + 写入侧规整，包名 `agentteam` → `team`）与
	// resume（领域无关恢复模板）归位多代理板块（plan §2.7）。
	"backend/multiagent/team", "backend/multiagent/resume",
	"middle/protocol/event", "middle/protocol/model", "application/prompt",
	"application/console", "backend/limit", "backend/limit/accountwindow", "backend/limit/approval",
	"backend/limit/loop",
	"backend/limit/overlay", "backend/limit/policy", "config", "docs",
	"backend/tool/computer",
	"docs/arch", "docs/devlog", "docs/gui", "docs/gui/schemas", "docs/product", "docs/research",
	"docs/test", "e2e", "e2e/scenario", "gui", "gui/frontend", "internal", "wiring/adapters",
	"internal/buildinfo", "internal/frontmatter", "plugins", "plugins/default",
	"plugins/hardware", "plugins/impeccable", "scripts",
	"backend/engine/assemble", "backend/engine/obs", "backend/tool/fork", "backend/tool/io/fs",
	"backend/tool/io/media", "internal/config",
	"backend/workunit",
	"backend/multiagent/teamwork",
	"internal/model", "internal/stream",
	"internal/telemetry", "backend/tool/plan",
	"backend/tool/search", "backend/limit/gate", "backend/tool/task", "backend/tool/websearch", "backend/engine/context/seelectx", "backend/engine/context/seelectx/compactor",
	"backend/engine/context/seelectx/merger", "backend/engine/context/seelectx/provider", "backend/engine/context/seelectx/search", "backend/engine/context/seelectx/snapshot",
	"session", "store", "store/root", "store/schedule", "store/workspace",
	"tui", "tui/splash", "wiring",
	"wiring/config",
	"wiring/registry",
	"wiring/bootstrap",
	// M5-R9：skill 从仓库顶层归位到 engine 板块（plan §2.5）。
	"backend/engine/skill",
	// M5-R9：plugin 归位到 engine 板块；同一目录下的 visibility 子包承载
	// Runtime 的可见性投影缓存（原 backend/engine/plugin，包名 plugin → visibility）。
	"backend/engine/plugin",
	"backend/engine/plugin/visibility",
	// M5-R9：mcpstack 归位到 engine 板块的 mcp 子域（包名不变）。
	"backend/engine/mcp/mcpstack",
	"backend/engine/mcp/mcpstack/config",
	// M5-R2：工具调用读数的内存累计（原 internal/toolrecord）归位到 store 板块。
	"store/record",
	"test",
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
		// 本地草稿目录（`.gitignore` 已忽略：`_scratch/` 是补丁与提交信息的现场，
		// `local/` 是个人工具目录）。它们里面的 .go 不是仓库模块，也不受 README
		// 约定约束；不跳过就会让门禁被草稿文件点亮，把真正的红淹没掉。
		"_scratch": true, "local": true,
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
		// 本地草稿目录（`.gitignore` 已忽略：`_scratch/` 是补丁与提交信息的现场，
		// `local/` 是个人工具目录）。它们里面的 .go 不是仓库模块，也不受 README
		// 约定约束；不跳过就会让门禁被草稿文件点亮，把真正的红淹没掉。
		"_scratch": true, "local": true,
		// vendor/ 是第三方依赖的只读副本：它的包目录既不是仓库模块，也不受
		// AGENTS.md 的模块 README 约定约束（`go mod vendor` 会整体重写它）。
		// 漏掉这一项会让门禁在每一个 vendor 子包上失败，把真正的红淹没掉。
		"vendor": true,
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
	pluginRoot := filepath.Join(root, "plugins")
	plugins, err := plugin.NewLoader(pluginRoot).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	// plugins/ 里除三个插件目录外还躺着 curated.yaml（精选目录，旁车数据文件）。
	// Loader 只认目录，所以"目录里多了一个文件"不该改变插件数量——这一条同时是
	// "零改加载器"的证据。
	if len(plugins) != 3 {
		t.Fatalf("loaded %d plugins, want 3 (default + hardware + impeccable)", len(plugins))
	}
	for _, p := range plugins {
		t.Logf("  plugin=%q skills=%d", p.Name, len(p.Skills))
	}
	// "$plan 属于 default 插件、不是可独立加载的插件"这条钉子在 $plan 退场（规划不再
	// 另设 Skill，见 default 插件口径）后换成当前口径：default 的技能面**与磁盘目录
	// 一一对应**——loader 不许静默跳过某个技能目录，且技能目录不许同时能当插件加载。
	// 判据取"磁盘即事实"，不硬编码技能名（硬编码会随插件增删过期，这正是本条被钉红的
	// 原因：plan 退场后列表还留在断言里）。
	defaultSkills := make(map[string]struct{})
	for _, p := range plugins {
		if p.Name != "default" {
			continue
		}
		for _, skill := range p.Skills {
			defaultSkills[skill.Name] = struct{}{}
		}
	}
	if len(defaultSkills) == 0 {
		t.Fatal("default plugin was not loaded / has no skills")
	}
	entries, err := os.ReadDir(filepath.Join(pluginRoot, "default"))
	if err != nil {
		t.Fatalf("read plugins/default: %v", err)
	}
	onDisk := 0
	loader := plugin.NewLoader(pluginRoot)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, err := os.Stat(filepath.Join(pluginRoot, "default", name, "SKILL.md")); err != nil {
			continue // 只认带 SKILL.md 的技能目录
		}
		onDisk++
		if _, loaded := defaultSkills[name]; !loaded {
			t.Errorf("plugins/default/%s 有 SKILL.md，但 loader 读出的技能集合里没有它（技能被静默跳过？）", name)
		}
		if _, err := loader.Load(name); err == nil {
			t.Errorf("%s 是 default 插件下的技能，不该同时是可独立加载的插件", name)
		}
	}
	if onDisk == 0 {
		t.Fatal("plugins/default 下没扫到任何技能目录——扫描口径失效")
	}
	// 精选目录与**这份 loader 读出的插件集合**交叉校验：只列已落盘插件、少一条即红。
	// （同一份校验也在仓库根的 curated_catalog_test.go 里跑，这里钉住的是交付树视角。）
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name)
	}
	if _, err := plugin.LoadCuratedCatalog(pluginRoot, names); err != nil {
		t.Fatalf("curated catalog: %v", err)
	}
}

// TestShippedCuratedCatalogPendingIsEvidenceTagged 是"归零不静默"在**发行树**上的那一半：
// `plugins/curated.yaml` 的 `presets[].pending` 不许退化成一句没有出处的名单——每条候选
// 必须带 `upstream`（谁给的）、`source`（实读出处与日期）、`verified`（核到什么程度，
// 当前只允许 listing-only）、`promote`（转正还缺什么）；且**装配到它必须被显式拒绝**，
// 拒绝文案要点名 pending 与它的来源。空 `pending` 正是这条守卫的失效形态（我标过的那条）。
func TestShippedCuratedCatalogPendingIsEvidenceTagged(t *testing.T) {
	pluginRoot := filepath.Join(repoRoot(), "plugins")
	catalog, err := plugin.LoadCuratedFromRoot(pluginRoot)
	if err != nil {
		t.Fatalf("curated catalog: %v", err)
	}
	design, ok := catalog.Preset("design")
	if !ok {
		t.Fatal("presets 里必须有 design（前端设计垂直面）")
	}
	if len(design.Pending) == 0 {
		t.Fatal("design 的 pending 是空的：候选没被登记（空路线图正是这条守卫的失效形态）")
	}
	installed := make([]string, 0, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		installed = append(installed, entry.Name)
	}
	for _, item := range design.Pending {
		if item.Verified != plugin.CuratedVerifiedListingOnly {
			t.Errorf("pending %q 的 verified = %q，want %q——只实读过清单层，不得被读成「已可用」",
				item.Name, item.Verified, plugin.CuratedVerifiedListingOnly)
		}
		if item.Source.Kind != plugin.CuratedSourceCommunityListing {
			t.Errorf("pending %q 的 source.kind = %q，want %q", item.Name, item.Source.Kind, plugin.CuratedSourceCommunityListing)
		}
		if strings.TrimSpace(item.Upstream) == "" || strings.TrimSpace(item.Promote) == "" {
			t.Errorf("pending %q 缺 upstream/promote（谁给的 + 转正还缺什么）", item.Name)
		}
		if !strings.HasPrefix(item.Source.URL, "https://github.com/") || strings.TrimSpace(item.Source.ReadAt) == "" {
			t.Errorf("pending %q 的 source 必须带一个可回访的实读页与日期：url=%q read_at=%q",
				item.Name, item.Source.URL, item.Source.ReadAt)
		}
		// 装配这道闸：点名 pending 必须被拒，且错误里要能读到候选与出处。
		if _, err := catalog.AssemblePlugin(item.Name, installed); err == nil {
			t.Errorf("装配到 pending %q 必须被拒绝", item.Name)
		} else if !strings.Contains(err.Error(), "pending") || !strings.Contains(err.Error(), item.Upstream) {
			t.Errorf("拒绝文案必须点名 pending 与来源：%s", err.Error())
		}
	}
	// 阴性对照：design 自己装的是已落盘插件，必须装得起来（否则上面那组可以靠"永远报错"通过）。
	plan, err := catalog.AssemblePreset("design", installed)
	if err != nil {
		t.Fatalf("design 装配失败: %v", err)
	}
	if len(plan.Plugins) != 1 {
		t.Fatalf("preset 仍是全局单选，plan = %#v", plan)
	}
}

// TestPluginsReadmeIndexCarriesSource 钉住最小可见化的落点：`plugins_list` 的回执在
// `main.go`（不在 plugins/**+plugin/** 这块领地），所以「这个插件是谁给的」在这里的
// 可见面是 plugins/README.md 的索引表——每条 entry 的 source.url 必须在表里出现。
func TestPluginsReadmeIndexCarriesSource(t *testing.T) {
	pluginRoot := filepath.Join(repoRoot(), "plugins")
	catalog, err := plugin.LoadCuratedFromRoot(pluginRoot)
	if err != nil {
		t.Fatalf("curated catalog: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(pluginRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for _, entry := range catalog.Entries {
		if !strings.Contains(readme, entry.Source.URL) {
			t.Errorf("plugins/README.md 的索引表没写出 %q 的来源 %q（谁给的必须可读）", entry.Name, entry.Source.URL)
		}
	}
	if summary, ok := catalog.SourceSummary("impeccable"); !ok || summary == "" {
		t.Fatalf("来源读面读不出 impeccable（plugin.CuratedCatalog.SourceSummary 是可见化的机读面）")
	}
	if !strings.Contains(readme, "SourceSummary") {
		t.Error("plugins/README.md 必须指明来源摘要的读面（plugin.CuratedCatalog.SourceSummary），否则可见化只剩人眼")
	}
}
