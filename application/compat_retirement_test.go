package application_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// retiredCompat*Literal 是兼容门面退场后仍被禁止引用的拼写。这些常量必须用拼接构造：
// 门禁文件自身也是 *.go，而终端验收标准是对下面每一项做全仓 `*.go`/`*.md` rg 并要求
// 返回空——逐字写出会让门禁永远是自己命中的 offender。
const (
	retiredCompatPackageLiteral       = "application" + "/" + "compat"
	retiredCompatAliasLiteral         = "application" + "compat"
	retiredCompatFrontendLiteral      = "compat" + "." + "Frontend"
	retiredApplicationFrontendLiteral = "application" + "." + "Frontend"
	retiredAssemblyFrontendLiteral    = "assembly" + "." + "Frontend"
	retiredFlatPortsLiteral           = "Frontend" + "Ports"
)

// retiredCompatibilityLiterals 是整个退场程序唯一的禁止字面量清单（单一事实源）：
// Go 侧要检查的类型标识符由 retiredCompatibilityTypeNames 从它推导，Markdown 侧的
// 文本引用扫描直接遍历它。同一个判据不允许有第二份清单（AGENTS.md §8 规则 3）。
var retiredCompatibilityLiterals = []string{
	retiredCompatPackageLiteral,
	retiredCompatAliasLiteral,
	retiredCompatFrontendLiteral,
	retiredApplicationFrontendLiteral,
	retiredAssemblyFrontendLiteral,
	retiredFlatPortsLiteral,
}

// historicalRecordMarker 是历史工作包文档的过期标记，也是 Markdown 豁免的唯一依据：
// 它声明「本文提到的包名与类型名只是当时的事实，在当前实现中已不存在」。标记必须落在
// 文档前 historicalMarkerScanLines 行内，并且由门禁反向校验（assertHistoricalRecordMarkers），
// 因此删掉标记既不能让门禁悄悄变绿，也不能让文档悄悄变红而不被点名。
const historicalRecordMarker = "历史记录，不描述当前实现"

// historicalMarkerScanLines 是查找过期标记的行数上限。
const historicalMarkerScanLines = 10

// retiredCompatibilityHistoricalRecords 锚定「哪些文档必须带着过期标记」。
// 它不是豁免白名单——豁免只看标记；这份清单存在的意义是：一旦有人抹掉标记或移走文档，
// 门禁会点名具体文件，而不是让判据静默退化。
var retiredCompatibilityHistoricalRecords = []string{
	"docs/2026-10-25-application-modularization/implementation.md",
	"docs/2026-10-25-application-modularization/remaining-work.md",
	"docs/2026-10-27-application-compat-removal/deletion-list.md",
	"docs/2026-10-27-application-compat-removal/front-review.md",
	"docs/2026-10-27-application-compat-removal/handoff.md",
	"docs/2026-10-27-application-compat-removal/implementation-prompt.md",
	"docs/2026-10-27-application-compat-removal/ledger.md",
	"docs/2026-10-27-application-compat-removal/next-session-prompt.md",
	"docs/2026-10-27-application-compat-removal/plan.md",
}

// nonSourceDirectories 是仓库级扫描必须跳过的目录名：构建产物、版本库、前端依赖、
// 本地缓存与用户会话数据都不属于源码/文档面。Go import 扫描（core_retirement_test.go）
// 与本文件的 Markdown 扫描共用这一份判据，避免各写一份跳过表后互相漂移。
var nonSourceDirectories = map[string]bool{
	".git": true, ".tmp": true, "dist": true, "vendor": true, "_tmp": true,
	"tmp": true, "node_modules": true, ".gocache": true, ".venv": true,
	"_scratch": true, "local": true, ".seelex": true,
}

// isNonSourceDir 判断目录名是否属于扫描盲区。
func isNonSourceDir(name string) bool { return nonSourceDirectories[name] }

// frontendTestConstructorRegistration 是一条「已知的测试专用 `*Frontend` 转发构造器」
// 登记项。登记表不是白名单式的宽容开关，而是一份带死亡条件的债务台账：
// 每一项都要写清旧面、它的新面 owner、删除里程碑和归零命令。登记项被摘掉、
// 或声明本身消失而登记还在，都会让门禁变红。
type frontendTestConstructorRegistration struct {
	// Declaration 是被识别的声明标识，格式「文件名:函数名」。
	Declaration string
	// LegacySurface 是这条测试转发面当前实际在做的事。
	LegacySurface string
	// Successor 是删掉它之后测试应该走的唯一 owner。
	Successor string
	// RemovalMilestone 是它必须消失的里程碑。
	RemovalMilestone string
	// ZeroCommand 是验证它归零的命令（必须能返回 0）。
	ZeroCommand string
}

// maxRegisteredFrontendTestConstructors 是不增长棘轮：登记表只许收缩。删掉一条登记时
// 必须同时下调本常量；想新增登记，就得先解释为什么必须多一份 service→frontend
// 反向构造（review 里会被当作 M4 的欠债而不是合法扩展）。
const maxRegisteredFrontendTestConstructors = 0

// registeredFrontendTestConstructors 已归零：测试直接持有 service.components
// 的 owner 指针，不再从 Service 反向构造 *Frontend。
//
// 豁免条件是「在这张表里」，不是「名字长得像」：新增第二个同类构造器不会自动继承
// 豁免，必须同时补一条登记（旧面 + 新面 owner + 删除里程碑 + 归零命令），否则红。
var registeredFrontendTestConstructors []frontendTestConstructorRegistration

// TestApplicationCompatibilitySurfaceIsRetired is the ratchet for the final
// frontend shape: compatibility packages, compatibility receiver methods and
// test-only forwarding shims must all be gone at the same time. Repository-wide
// Markdown belongs to the same surface, because a document that still names the
// retired facade in present tense is a claim about the current implementation:
// it must be rewritten, or carry the historical-record marker that declares it a
// snapshot of what was true at the time.
func TestApplicationCompatibilitySurfaceIsRetired(t *testing.T) {
	appDir := sourceApplicationDir(t)
	if _, err := os.Stat(filepath.Join(appDir, retiredCompatPackageLiteral)); err == nil {
		t.Errorf("%s still exists; migrate callers to their owning components", retiredCompatPackageLiteral)
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect %s: %v", retiredCompatPackageLiteral, err)
	}

	compatImport := "github.com/RedHuang-0622/seelex/" + retiredCompatPackageLiteral
	retiredTypeNames := retiredCompatibilityTypeNames()
	var compatImports []string
	var forbiddenTypes []string
	err := filepath.WalkDir(appDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		relative, _ := filepath.Rel(appDir, path)
		for _, imported := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if importPath == compatImport {
				compatImports = append(compatImports, filepath.ToSlash(relative))
			}
		}
		if filepath.Dir(path) == filepath.Join(appDir, "assembly") {
			for _, declaration := range file.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.TYPE {
					continue
				}
				for _, specification := range general.Specs {
					spec, ok := specification.(*ast.TypeSpec)
					if !ok {
						continue
					}
					for _, retiredName := range retiredTypeNames {
						if spec.Name.Name == retiredName {
							forbiddenTypes = append(forbiddenTypes, filepath.ToSlash(relative)+":"+spec.Name.Name)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan application: %v", err)
	}
	sort.Strings(compatImports)
	sort.Strings(forbiddenTypes)
	if len(compatImports) > 0 {
		t.Errorf("application still imports the compatibility package:\n%s", strings.Join(compatImports, "\n"))
	}
	if len(forbiddenTypes) > 0 {
		t.Errorf("assembly still defines compatibility frontend types:\n%s", strings.Join(forbiddenTypes, "\n"))
	}

	forbiddenDeclarations := map[string]bool{
		"ViewFace":                 true,
		"CommandFace":              true,
		"PolicyFace":               true,
		"QueueFace":                true,
		"ConversationAPI":          true,
		"SessionAPI":               true,
		"ExecutionQueryAPI":        true,
		"WorkItemQueryAPI":         true,
		"TeammateQueryAPI":         true,
		"SubagentQueryAPI":         true,
		"TranscriptQueryAPI":       true,
		"ExecutionCommandAPI":      true,
		"WorkspaceAPI":             true,
		"GoalAPI":                  true,
		"PolicyAPI":                true,
		"AdminAPI":                 true,
		"RoleAPI":                  true,
		"SubagentAPI":              true,
		"StateAPI":                 true,
		"QueueAPI":                 true,
		"LifecycleAPI":             true,
		"SessionQuery":             true,
		"ConversationHistoryQuery": true,
	}
	entries, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatal(err)
	}
	var legacyDeclarations []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(appDir, entry.Name()), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				spec, ok := specification.(*ast.TypeSpec)
				if ok && forbiddenDeclarations[spec.Name.Name] {
					legacyDeclarations = append(legacyDeclarations, entry.Name()+":"+spec.Name.Name)
				}
			}
		}
	}
	sort.Strings(legacyDeclarations)
	if len(legacyDeclarations) > 0 {
		t.Errorf("application still owns retired wide frontend contracts:\n%s", strings.Join(legacyDeclarations, "\n"))
	}

	// 文档面与代码面同一条判据：全仓 Markdown 里引用已退场门面拼写的行，只有带着
	// 过期标记的历史记录可以豁免。逐节都用 Errorf 而不是 Fatalf，让一次运行就能
	// 报告全部 offender，而不是停在第一个。
	repoRoot := filepath.Dir(appDir)
	assertHistoricalRecordMarkers(t, repoRoot)
	if docOffenders := retiredCompatibilityDocOffenders(t, repoRoot); len(docOffenders) > 0 {
		t.Errorf("仓库 Markdown 仍把已退场兼容门面当作现状描述（未被标记 %q 豁免）:\n%s",
			historicalRecordMarker, strings.Join(docOffenders, "\n"))
	}
	// 反向判据：标记只豁免登记过的历史记录，不能给现行文档自签豁免。
	if adHoc := unregisteredHistoricalMarkedDocuments(t, repoRoot); len(adHoc) > 0 {
		t.Errorf("下列 Markdown 摆了过期标记 %q 却没有登记进历史记录清单（标记不是自签豁免；"+
			"要么把文档登记为历史记录并写清删除里程碑，要么按现行口径改写）:\n%s",
			historicalRecordMarker, strings.Join(adHoc, "\n"))
	}
}

// retiredCompatibilityTypeNames 从禁止字面量清单推导需要在 Go 源码里禁止的类型标识符：
// 取每个拼写最后一个点号右侧的片段，保留合法 Go 标识符并去重。
func retiredCompatibilityTypeNames() []string {
	var names []string
	seen := make(map[string]bool, len(retiredCompatibilityLiterals))
	for _, literal := range retiredCompatibilityLiterals {
		name := literal
		if index := strings.LastIndex(literal, "."); index >= 0 {
			name = literal[index+1:]
		}
		if !isGoIdentifier(name) || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// isGoIdentifier 判断片段能否作为 Go 类型名出现在声明中。
func isGoIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, r := range name {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if index > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// splitDocumentLines 统一 CRLF 并按行切分，使报告的行号与 rg 的口径一致。
func splitDocumentLines(raw []byte) []string {
	return strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
}

// carriesHistoricalRecordMarker 只认「文档开头横幅区」里的过期标记：从第 1 行起逐行看，
// 空行跳过；一旦出现第一条不是引用块（`>` 前缀）的行，横幅区就结束了。这样标记必须是
// 被显著摆出来的声明，而不是正文里顺带复述这个词就能整篇豁免 —— 旧实现只要前
// historicalMarkerScanLines 行里出现该字符串就豁免，一篇讲这条门禁的文档会在正文里
// 引用这句话，从而把自己整篇豁免掉（含后续任何门面拼写）。
func carriesHistoricalRecordMarker(lines []string) bool {
	limit := historicalMarkerScanLines
	if len(lines) < limit {
		limit = len(lines)
	}
	for _, line := range lines[:limit] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ">") {
			return false
		}
		if strings.Contains(trimmed, historicalRecordMarker) {
			return true
		}
	}
	return false
}

// assertHistoricalRecordMarkers 校验登记的每份历史文档确实带着过期标记，并点名失去标记
// （或已被移动、删除）的文件：标记是豁免的唯一来源，它消失时必须有人来解释。
func assertHistoricalRecordMarkers(t *testing.T, root string) {
	t.Helper()
	for _, relative := range retiredCompatibilityHistoricalRecords {
		path := filepath.Join(root, filepath.FromSlash(relative))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("历史文档 %s 读取失败（文档被移动或删除时，必须同步更新锚定清单）: %v", relative, err)
			continue
		}
		if carriesHistoricalRecordMarker(splitDocumentLines(raw)) {
			continue
		}
		t.Errorf("历史文档 %s 的前 %d 行缺少过期标记 %q：它引用的门面拼写只能作为当时的事实保留，"+
			"要么补回标记，要么改写成现行口径；删除标记不会让门禁变绿",
			relative, historicalMarkerScanLines, historicalRecordMarker)
	}
}

// walkMarkdownDocuments 是仓库 Markdown 扫描的唯一遍历实现：跳过 nonSourceDirectories、
// 统一 CRLF、按行切分，再对每篇文档回调「相对路径 + 行」。文档侧有两条判据（禁止拼写、
// 标记豁免必须登记），它们共用这一次遍历，避免各写一份跳过表与行切分后互相漂移；
// 返回值是实际扫到的文档数，用于证明判据不是「因为没扫到所以没红」。
func walkMarkdownDocuments(t *testing.T, root string, visit func(relative string, lines []string)) int {
	t.Helper()
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && isNonSourceDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		scanned++
		visit(filepath.ToSlash(relative), splitDocumentLines(raw))
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 %s 下的 Markdown: %v", root, err)
	}
	t.Logf("Markdown 扫描根 %s：实际读到 %d 篇（跳过目录 %s）", root, scanned, nonSourceDirectoryList())
	return scanned
}

// nonSourceDirectoryList 按字典序输出扫描盲区目录名，供日志与报告使用。
func nonSourceDirectoryList() string {
	names := make([]string, 0, len(nonSourceDirectories))
	for name := range nonSourceDirectories {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// retiredCompatibilityDocOffenders 扫描 root 下所有 Markdown，返回引用了禁止字面量、
// 且文档头没有过期标记的行，格式为「相对路径:行号: 字面量」。豁免只由标记决定，
// 本函数没有任何文件名白名单；扫描盲区由 nonSourceDirectories 统一负责。
func retiredCompatibilityDocOffenders(t *testing.T, root string) []string {
	t.Helper()
	var offenders []string
	walkMarkdownDocuments(t, root, func(relative string, lines []string) {
		if carriesHistoricalRecordMarker(lines) {
			return
		}
		for index, line := range lines {
			for _, literal := range retiredCompatibilityLiterals {
				if strings.Contains(line, literal) {
					offenders = append(offenders, fmt.Sprintf("%s:%d: %s", relative, index+1, literal))
				}
			}
		}
	})
	sort.Strings(offenders)
	return offenders
}

// unregisteredHistoricalMarkedDocuments 是标记豁免的反向判据：过期标记不是自签豁免，
// 只对 retiredCompatibilityHistoricalRecords 里登记过的文档生效。任何没被登记却在开头
// 摆了标记的 Markdown 都会被点名 —— 否则给 application/README.md 加一行标记，就能让它
// 整体从「本文描述现状」的判据里消失。两条方向合起来才闭合：登记文档必须带标记，
// 带标记的文档必须是登记文档。
func unregisteredHistoricalMarkedDocuments(t *testing.T, root string) []string {
	t.Helper()
	registered := make(map[string]bool, len(retiredCompatibilityHistoricalRecords))
	for _, relative := range retiredCompatibilityHistoricalRecords {
		registered[relative] = true
	}
	var offenders []string
	walkMarkdownDocuments(t, root, func(relative string, lines []string) {
		if registered[relative] || !carriesHistoricalRecordMarker(lines) {
			return
		}
		offenders = append(offenders, relative)
	})
	sort.Strings(offenders)
	return offenders
}

// TestRetiredCompatibilityDocScanHonoursHistoricalMarker 是文档判据的非空性证据：
// 在受控临时目录上验证无标记文档逐行点名、横幅区带标记文档整篇豁免、标记落在 10 行之外
// 不生效、标记只出现在正文段落或代码围栏里不生效、扫描盲区目录不参与、标记不能自签豁免
// 未登记文档，以及清干净后扫描为空。判据实现只有 retiredCompatibilityDocOffenders 与
// unregisteredHistoricalMarkedDocuments 两处（共用 walkMarkdownDocuments 这一次遍历），
// 本用例只是它们的驱动，不构成第二份实现。
func TestRetiredCompatibilityDocScanHonoursHistoricalMarker(t *testing.T) {
	root := t.TempDir()
	writeDoc := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeDoc("unmarked.md", "# 现状\n\n"+retiredCompatPackageLiteral+" 仍被引用。\n")
	writeDoc("marked.md", "> "+historicalRecordMarker+"\n\n# 历史\n\n"+retiredFlatPortsLiteral+" 当时存在。\n")
	writeDoc("late-marker.md", strings.Repeat("> 说明\n", historicalMarkerScanLines)+
		"\n> "+historicalRecordMarker+"\n"+retiredCompatAliasLiteral+" 出现在标记之后。\n")
	writeDoc(filepath.Join("dist", "skipped.md"), "产物副本里的 "+retiredApplicationFrontendLiteral+"\n")
	// 标记只被正文复述（不是开头横幅区的引用块）时不得豁免：这是旧实现放行的漏洞。
	writeDoc("mention.md", "# 现状\n\n本文复述标记「"+historicalRecordMarker+"」并引用 "+
		retiredCompatAliasLiteral+"。\n")
	// 标记只出现在代码围栏里时同样不得豁免。
	writeDoc("fenced.md", "# 现状\n\n```\n> "+historicalRecordMarker+"\n```\n\n"+
		retiredFlatPortsLiteral+" 仍在使用。\n")

	offenders := retiredCompatibilityDocOffenders(t, root)
	want := []string{
		"fenced.md:7: " + retiredFlatPortsLiteral,
		"late-marker.md:" + strconv.Itoa(historicalMarkerScanLines+3) + ": " + retiredCompatAliasLiteral,
		"mention.md:3: " + retiredCompatAliasLiteral,
		"unmarked.md:3: " + retiredCompatPackageLiteral,
	}
	if got := strings.Join(offenders, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("Markdown 扫描结果不符（只有开头横幅区的标记能豁免；mention/fenced 必须被点名；dist/ 在盲区外）:\n got:\n%s\nwant:\n%s",
			got, strings.Join(want, "\n"))
	}

	// 只剩横幅区带标记的文档时禁止拼写扫描必须为空：证明豁免不是「永远红的装饰」。
	for _, name := range []string{"unmarked.md", "late-marker.md", "mention.md", "fenced.md",
		filepath.Join("dist", "skipped.md")} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if offenders := retiredCompatibilityDocOffenders(t, root); len(offenders) != 0 {
		t.Fatalf("仅剩带过期标记的文档时不应有 offender，实际:\n%s", strings.Join(offenders, "\n"))
	}

	// 反向判据：marked.md 带了标记但不在历史记录登记表里，必须被点名 —— 标记不是自签豁免。
	if adHoc := unregisteredHistoricalMarkedDocuments(t, root); strings.Join(adHoc, ",") != "marked.md" {
		t.Fatalf("未登记的带标记文档必须被点名，实际:\n%s", strings.Join(adHoc, "\n"))
	}
	// 现行文档（无横幅标记）不进反向判据。
	writeDoc("current.md", "# 现状\n\n"+retiredCompatPackageLiteral+" 的计划。\n")
	if adHoc := unregisteredHistoricalMarkedDocuments(t, root); strings.Join(adHoc, ",") != "marked.md" {
		t.Fatalf("无标记的现行文档不应进入反向判据，实际:\n%s", strings.Join(adHoc, "\n"))
	}
}

// TestAssemblyHasNoTestOnlyForwardingFixtures prevents deleted production
// facades from surviving as shims declared only by tests. Three name-based
// criteria pin the known carriers (legacy / test_alias / frontend_migration),
// and the constructor criterion pins the shape: any test-only `*Frontend`
// forwarding constructor, whether a method or a free function, is the same
// debt and must either be registered with its removal milestone or not exist.
func TestAssemblyHasNoTestOnlyForwardingFixtures(t *testing.T) {
	assemblyDir := filepath.Join(sourceApplicationDir(t), "assembly")
	entries, err := os.ReadDir(assemblyDir)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.Contains(name, "legacy") ||
			strings.Contains(name, "test_alias") ||
			name == "frontend_migration_test.go" {
			fixtures = append(fixtures, name)
		}
	}
	sort.Strings(fixtures)
	if len(fixtures) > 0 {
		t.Errorf("assembly still has test-only compatibility forwarding fixtures:\n%s", strings.Join(fixtures, "\n"))
	}

	// 形状判据：任何以 *Frontend 为返回值的测试专用转发构造器都算同一笔债。
	constructors := scanFrontendTestConstructors(t, assemblyDir)
	if unregistered := unregisteredFrontendTestConstructors(constructors); len(unregistered) > 0 {
		t.Errorf("assembly 有未登记的测试专用 *Frontend 转发构造器（旧面必须登记旧面/新面 owner/"+
			"删除里程碑/归零命令，或直接删除；不允许新增第二份 service→frontend 反向构造）:\n%s",
			strings.Join(unregistered, "\n"))
	}
	if stale := staleFrontendTestConstructorRegistrations(constructors); len(stale) > 0 {
		t.Errorf("下列登记项的声明已经不存在：登记表不能留死条目，删除转发构造器时必须同步摘掉登记:\n%s",
			strings.Join(stale, "\n"))
	}
	// 不增长棘轮 + 登记项完整性：空白登记等于没有死亡条件的白名单。
	if len(registeredFrontendTestConstructors) > maxRegisteredFrontendTestConstructors {
		t.Errorf("测试转发构造器登记表从 %d 涨到 %d 条：登记表只许收缩，"+
			"新增一份 service→frontend 反向构造不是合法扩展",
			maxRegisteredFrontendTestConstructors, len(registeredFrontendTestConstructors))
	}
	for _, registration := range registeredFrontendTestConstructors {
		if registration.Declaration == "" || registration.LegacySurface == "" ||
			registration.Successor == "" || registration.RemovalMilestone == "" ||
			registration.ZeroCommand == "" {
			t.Errorf("登记项 %q 缺少旧面/新面 owner/删除里程碑/归零命令之一：登记项必须自带死亡条件",
				registration.Declaration)
		}
	}
}

// scanFrontendTestConstructors 是「测试专用 *Frontend 转发构造器」判据的唯一实现：
// 逐个解析 assembly 目录下的 *_test.go，收集所有结果里带 *Frontend 的函数（自由函数与
// 方法一视同仁），返回「文件:函数名」的排序列表。判据不看接收者、不看文件命名、不看
// 函数名，只看返回值形状 —— 旧判据只看带接收者的方法，无法覆盖自由函数构造器。
func scanFrontendTestConstructors(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s: %v", dir, err)
	}
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("解析 %s: %v", name, parseErr)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name == nil || function.Type == nil {
				continue
			}
			if !returnsFrontendPointer(function.Type.Results) {
				continue
			}
			found = append(found, name+":"+function.Name.Name)
		}
	}
	sort.Strings(found)
	return found
}

// returnsFrontendPointer 判断结果列表里是否有旧前端指针类型。
func returnsFrontendPointer(results *ast.FieldList) bool {
	if results == nil {
		return false
	}
	for _, field := range results.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		identifier, ok := star.X.(*ast.Ident)
		if ok && identifier.Name == "Frontend" {
			return true
		}
	}
	return false
}

// frontendTestConstructorRegistry 把登记表建成查表结构。
func frontendTestConstructorRegistry() map[string]frontendTestConstructorRegistration {
	registry := make(map[string]frontendTestConstructorRegistration, len(registeredFrontendTestConstructors))
	for _, registration := range registeredFrontendTestConstructors {
		registry[registration.Declaration] = registration
	}
	return registry
}

// unregisteredFrontendTestConstructors 返回找到了但没登记的构造器：这就是「新增第二个
// 同类夹具立刻红」的实现。
func unregisteredFrontendTestConstructors(found []string) []string {
	registry := frontendTestConstructorRegistry()
	var unregistered []string
	for _, declaration := range found {
		if _, ok := registry[declaration]; ok {
			continue
		}
		unregistered = append(unregistered, declaration)
	}
	sort.Strings(unregistered)
	return unregistered
}

// staleFrontendTestConstructorRegistrations 返回登记了却已经找不到声明的条目：
// 登记表只记录仍然存在的旧面，删掉夹具就必须同步摘掉登记，否则豁免会变成永久白名单。
func staleFrontendTestConstructorRegistrations(found []string) []string {
	declared := make(map[string]bool, len(found))
	for _, declaration := range found {
		declared[declaration] = true
	}
	var stale []string
	for _, registration := range registeredFrontendTestConstructors {
		if !declared[registration.Declaration] {
			stale = append(stale, registration.Declaration)
		}
	}
	sort.Strings(stale)
	return stale
}

// TestAssemblyFrontendTestConstructorRatchet 是上面那条形状判据的驱动：判据实现只有
// scanFrontendTestConstructors 加两个差集函数，本用例只负责证明它非空转 ——
// 合成夹具上它能点名自由函数与方法、忽略普通测试辅助函数；真实 assembly 目录上它既能
// 认出登记项（看得见），也不会漏掉任何未登记项（看得见才有意义）。
func TestAssemblyFrontendTestConstructorRatchet(t *testing.T) {
	scratch := t.TempDir()
	writeFixture := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 自由函数（当前夹具的形状）与方法两种写法都必须被识别。
	writeFixture("free_function_test.go", "package assembly\n\nfunc freeFixture(service *Service) *Frontend {\n"+
		"\treturn &Frontend{Service: service}\n}\n")
	writeFixture("receiver_method_test.go", "package assembly\n\ntype fixtureHolder struct{ service *Service }\n\n"+
		"func (holder *fixtureHolder) Frontend() *Frontend {\n\treturn &Frontend{Service: holder.service}\n}\n")
	// 普通测试辅助函数（不产出 *Frontend）不属于这条判据。
	writeFixture("plain_helper_test.go", "package assembly\n\nfunc plainHelper(value int) int {\n\treturn value\n}\n")

	want := []string{"free_function_test.go:freeFixture", "receiver_method_test.go:Frontend"}
	if got := scanFrontendTestConstructors(t, scratch); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("合成夹具上的构造器识别不符:\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// 合成夹具未登记，差集必须点名它们（而不是继承登记表里的豁免）。
	if unregistered := unregisteredFrontendTestConstructors(want); len(unregistered) != len(want) {
		t.Fatalf("未登记的合成夹具应被全部点名，实际只点名 %v", unregistered)
	}

	// 真实仓库：登记项必须被识别到（这就是「判据看得见它」），且不存在未登记同类构造器。
	found := scanFrontendTestConstructors(t, filepath.Join(sourceApplicationDir(t), "assembly"))
	t.Logf("assembly *_test.go 中以 *Frontend 为返回值的构造器：%s", strings.Join(found, ", "))
	if unregistered := unregisteredFrontendTestConstructors(found); len(unregistered) > 0 {
		t.Fatalf("assembly 有未登记的测试专用 *Frontend 转发构造器:\n%s", strings.Join(unregistered, "\n"))
	}
	if stale := staleFrontendTestConstructorRegistrations(found); len(stale) > 0 {
		t.Fatalf("登记项已找不到对应声明:\n%s", strings.Join(stale, "\n"))
	}
	declared := make(map[string]bool, len(found))
	for _, declaration := range found {
		declared[declaration] = true
	}
	for _, registration := range registeredFrontendTestConstructors {
		if !declared[registration.Declaration] {
			t.Fatalf("登记的转发构造器 %s 没有被形状判据识别到：判据又瞎了", registration.Declaration)
		}
		t.Logf("已登记豁免 %s（删除里程碑：%s；归零命令：%s）",
			registration.Declaration, registration.RemovalMilestone, registration.ZeroCommand)
	}
}
