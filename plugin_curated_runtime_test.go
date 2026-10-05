package main

// 精选目录**进运行期**的守卫（A：从声明面变成装配面）与根读数**进 UI 面**的守卫（B）。
//
// 复核实读背景（2026-10-05）：`plugin.LoadCuratedFromRoot` / `ActivateFromCatalog` 当时
// 只出现在测试里 —— 真走 team_plan 时，一个 pending 名字落到的是 `validateMemberPlugins`
// 的"插件 %q 未定义"，文案里既没有 pending 也没有来源；`main.go` 的 `startupWarnings` 是
// 唯一进 UI 面的通路，而根读数（logPluginRoots）只写终端日志。
//
// 本文件钉四件事：
//  1. `resolveCuratedRead` 真的从**已解析的插件根**读（first-wins）、并与本次真正加载出来的
//     插件集合交叉校验；
//  2. 目录**读不到**时出声（Err 非空 + 说清找过哪些根），判决函数对每个未定义名显式拒绝
//     ——不得静默当空目录；
//  3. 判决文案三种分支各自可判别（pending + 上游来源 + 实读页 / 两边都不在 / 目录说它已落盘）；
//  4. 根读数（加载了几个、从哪个根）与 logPluginRoots 是**同一份字节**，且进得了 UI 面。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
)

// ── A：运行期读精选目录 ─────────────────────────────────────────────────────

func TestResolveCuratedReadReadsFromResolvedRootsAndJudges(t *testing.T) {
	root := t.TempDir()
	writeCuratedRootPlugin(t, root, "default")
	writeCuratedRootPlugin(t, root, "docs")
	writeCuratedRootFile(t, root, curatedFixtureYAML("default", "docs"))

	loaded := loadCuratedRootPlugins(t, root)
	read := resolveCuratedRead([]string{root}, loaded)
	if read.Err != nil {
		t.Fatalf("合法目录必须读得出来：%v", read.Err)
	}
	wantPath := filepath.Join(root, plugin.CuratedFileName)
	if read.Path != wantPath {
		t.Fatalf("实读页 = %q，want %q（拒绝文案要能落到一页可核的文件上）", read.Path, wantPath)
	}
	if read.Catalog.ReadAt == "" {
		t.Fatal("读数里没有 read_at：这份目录是什么时候读的必须可读")
	}
	if _, _, ok := read.Catalog.PendingEntry(curatedFixtureRoadmap); !ok {
		t.Fatalf("目录里的 pending %q 没被读出来——判决函数就无从点名来源", curatedFixtureRoadmap)
	}

	judge := curatedAssemblyJudge(read)
	installed := []string{"default", "docs"}

	// ① pending（路线图）：拒，且点名 pending + 上游来源 + 实读页。
	err := judge(curatedFixtureRoadmap, installed)
	if err == nil {
		t.Fatalf("装配到 pending %q 必须被拒绝", curatedFixtureRoadmap)
	}
	curatedWantError(t, "pending 候选", err,
		"pending", curatedFixtureRoadmap, curatedFixtureUpstream, curatedFixtureListing,
		plugin.CuratedVerifiedListingOnly, wantPath, "listing")
	if !strings.Contains(err.Error(), read.Catalog.ReadAt) {
		t.Fatalf("错误文案 %q 必须带目录的读数时点（%q）", err.Error(), read.Catalog.ReadAt)
	}

	// ② 谁都不认识：拒，且说清"不在已装插件、也不在精选目录"，两边名单都写上。
	err = judge("ghost", installed)
	if err == nil {
		t.Fatal("不认识的名字必须被拒绝")
	}
	curatedWantError(t, "未知名", err, "未定义", "不在已装插件", "也不在精选目录", "default, docs", wantPath, "显式拒绝")

	// ③ 目录说它已落盘、本进程却没有它：这是"名字漂了/被撤过"，不是错别字。
	err = judge("docs", []string{"default"})
	if err == nil {
		t.Fatal("目录 entries 里有、本进程没有的名字必须被拒绝")
	}
	curatedWantError(t, "entries 有但进程没有", err, "entries", "已落盘", "docs", wantPath)
}

// TestResolveCuratedReadMissIsExplicitButNotAStartupWarning 钉住 2026-10-05 重排后的口径：
// 责任链上一个根都没有 curated.yaml ⇒ **装配面照样显式拒绝**（不得静默当空目录），但这是
// 合法现场（自建根/用户树可以不带精选目录），**不进启动警告**——使用者自己装的插件不该让
// app 每次启动都背一条与他无关的发行侧警告。
//
// 反过来，"文件存在却读不动"是配置缺陷：Absent 为假 ⇒ 调用方照旧出声（终端 + UI 启动警告）。
func TestResolveCuratedReadMissIsExplicitButNotAStartupWarning(t *testing.T) {
	empty := t.TempDir()
	other := t.TempDir()
	roots := []string{empty, other}

	read := resolveCuratedRead(roots, nil)
	if read.Err == nil {
		t.Fatal("责任链上一个根都没有 curated.yaml 时必须留 Err（静默当空目录会把 pending 判成错别字）")
	}
	if !read.Absent {
		t.Fatal("一个根都没有 ⇒ Absent 必须为真（调用方据此只写终端一行，不进启动警告）")
	}
	if len(read.Drift) != 0 {
		t.Errorf("目录都没读到，不该有漂移读数: %v", read.Drift)
	}
	judge := curatedAssemblyJudge(read)
	err := judge(curatedFixtureRoadmap, []string{"default"})
	if err == nil {
		t.Fatal("目录缺席时的装配必须被拒绝（不得静默当空目录）")
	}
	curatedWantError(t, "目录缺席", err, plugin.CuratedFileName, empty, other, "显式拒绝", "静默当空目录", "default")

	// 解析失败（字段缺失）算"存在却读不动"：Absent 为假，且要点名那一份坏文件。
	broken := t.TempDir()
	writeCuratedRootFile(t, broken, "schema_version: 1\nkind: curated-catalog\n")
	read = resolveCuratedRead([]string{broken}, nil)
	if read.Err == nil {
		t.Fatal("坏目录必须留 Err")
	}
	if read.Absent {
		t.Fatal("文件存在却读不动 ≠ 目录缺席：Absent 必须为假（这一档要进启动警告）")
	}
	if read.Path != filepath.Join(broken, plugin.CuratedFileName) {
		t.Fatalf("坏目录也要点名实读页，得 %q", read.Path)
	}
	curatedWantError(t, "目录解析失败", curatedAssemblyJudge(read)(curatedFixtureRoadmap, nil),
		"没读到", plugin.CuratedFileName)
}

// TestResolveCuratedReadToleratesLocalPluginDrift 钉住"使用者自己改插件不该被自己的 app 警告"：
// 运行树里多出本机自装插件（entries 里没登记它）、目录里登记了一个这台机器上没有的插件，
// 都**不是错误**——目录照样读得出来、pending 照样能点名来源，差异只留在 Drift 里供终端回报。
//
// 同时钉住另一半：一一对应仍是**发行守卫**的口径（ValidateCuratedCatalog 照旧红），
// 运行期的宽容不许把那条守卫悄悄放掉。
func TestResolveCuratedReadToleratesLocalPluginDrift(t *testing.T) {
	root := t.TempDir()
	writeCuratedRootPlugin(t, root, "default")
	writeCuratedRootPlugin(t, root, "local-tool") // 本机自装：不在 curated.yaml 的 entries 里
	writeCuratedRootFile(t, root, curatedFixtureYAML("default", "ghost"))

	installed := loadCuratedRootPlugins(t, root)
	read := resolveCuratedRead([]string{root}, installed)
	if read.Err != nil {
		t.Fatalf("本机自装/退役插件只是差异、不是错误，目录必须读得出来：%v", read.Err)
	}
	if read.Absent {
		t.Fatal("目录读到了就不该是 Absent")
	}
	drift := strings.Join(read.Drift, "\n")
	for _, want := range []string{"local-tool", "不在 entries 里", "ghost", "插件目录不存在"} {
		if !strings.Contains(drift, want) {
			t.Errorf("漂移读数必须点出 %q（两个方向都要报），得: %s", want, drift)
		}
	}
	if _, _, ok := read.Catalog.PendingEntry(curatedFixtureRoadmap); !ok {
		t.Fatalf("目录读到之后 pending %q 必须照样可用（判定的信息面不许被降级）", curatedFixtureRoadmap)
	}
	if err := plugin.ValidateCuratedCatalog(read.Catalog, installedNames(installed)); err == nil {
		t.Fatal("发行守卫口径必须仍然红：entries 与已装集合一一对应")
	}
}

// installedNames 把加载出来的插件渲染成名字清单（与 resolveCuratedRead 内部同一口径）。
func installedNames(plugins []plugin.Plugin) []string {
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name)
	}
	return names
}

// ── B：根读数进 UI 面 ──────────────────────────────────────────────────────

// TestPluginRootReadingIsSharedAndReachesTheUIFace 钉住 B 的两半：
//  1. 根读数写明"每个根供了几个 + 每个插件来自哪个根"，且 logPluginRoots **返回同一份字节**
//     （两处各生成一次就会出现两份对不上的读数）；
//  2. 这条读数确实进 UI 面（run() 的启动通知），不是只写终端日志。
func TestPluginRootReadingIsSharedAndReachesTheUIFace(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeCuratedRootPlugin(t, first, "alpha")
	writeCuratedRootPlugin(t, second, "beta")
	loaded := loadCuratedRootPlugins(t, first, second)
	if len(loaded) != 2 {
		t.Fatalf("夹具应加载出 2 个插件，得 %d", len(loaded))
	}

	report := logPluginRoots([]string{first, second}, loaded)
	curatedWantError(t, "根读数", errorString(report),
		first+"=1", second+"=1", "共加载 2 个插件", "alpha←", "beta←")

	// 多根 first-wins：同一个插件名在两个根里都有时，只有链上先出现的那个根算供上它
	// （后一个根的那一份是死代码，读数不该把它算成"这个根供了 1 个"）。
	writeCuratedRootPlugin(t, second, "alpha")
	loaded = loadCuratedRootPlugins(t, first, second)
	if len(loaded) != 2 {
		t.Fatalf("同名插件去重后应仍是 2 个，得 %d", len(loaded))
	}
	report = logPluginRoots([]string{first, second}, loaded)
	curatedWantError(t, "first-wins 根读数", errorString(report),
		first+"=1", second+"=1", "共加载 2 个插件", "alpha←"+filepath.Join(first, "alpha"))
	if strings.Contains(report, "alpha←"+filepath.Join(second, "alpha")) {
		t.Fatalf("first-wins 下读数只能报链首那份的来源：%q", report)
	}

	// 第二半：这条读数在 UI 面上的落点（main.go 的启动通知通路）。
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "pluginStartup.RootReading") {
		t.Fatal("main.go 没有把根读数带出 initPluginSystem（它进不了 UI 面，就只能是终端日志）")
	}
	if !strings.Contains(text, `app.AddNotice("插件: " + pluginStartup.RootReading)`) {
		t.Fatal("根读数必须进 UI 面（app.AddNotice）——只写 log.Printf 的话，GUI 里根本看不到")
	}
	if !strings.Contains(text, "pluginRootReport(roots, loaded)") {
		t.Fatal("根读数必须只有一处生成（pluginRootReport），终端与 UI 两处引用同一份字节")
	}
}

// ── 夹具 ───────────────────────────────────────────────────────────────────

const (
	curatedFixtureRoadmap  = "next-cad"
	curatedFixtureUpstream = "acme/next-cad"
	curatedFixtureListing  = "https://example.com/awesome-skills"
)

func curatedFixtureYAML(installed ...string) string {
	return curatedFixtureYAMLWithRoadmap(installed, curatedFixtureRoadmap)
}

// curatedFixtureYAMLWithRoadmap 生成一份**合法**的精选目录：entries 与 installed 一一对应，
// baseline 含 default，pending 带证据档（pending 的合法性由 plugin 包的校验机保证）。
func curatedFixtureYAMLWithRoadmap(installed []string, roadmap string) string {
	var builder strings.Builder
	builder.WriteString("schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\nentries:\n")
	for _, name := range installed {
		builder.WriteString("  - name: " + name + "\n    description: " + name + " plugin\n    installed: true\n")
		builder.WriteString("    source:\n      kind: builtin\n      url: https://example.com/" + name +
			"\n      license: MIT\n      pinned: v1\n      read_at: 2026-10-05\n")
	}
	builder.WriteString("presets:\n  - name: baseline\n    description: baseline\n    permission_tier: manual\n" +
		"    plugins: [default]\n    pending: []\n")
	builder.WriteString("  - name: cad\n    description: cad\n    permission_tier: manual\n    plugins: [" +
		strings.Join(installed, ", ") + "]\n    pending:\n")
	builder.WriteString("      - name: " + roadmap + "\n        upstream: " + curatedFixtureUpstream + "\n")
	builder.WriteString("        source:\n          kind: community-listing\n          url: " + curatedFixtureListing +
		"\n          license: MIT\n          pinned: listing\n          read_at: 2026-10-05\n")
	builder.WriteString("        verified: " + plugin.CuratedVerifiedListingOnly + "\n")
	builder.WriteString("        promote: 实读正文 + 过可装载性四问\n")
	return builder.String()
}

// writeCuratedRootPlugin 在根下造一个真实可加载的插件（manifest 三件套：目录 + plugin.md）。
func writeCuratedRootPlugin(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nschema_version: 1\nname: " + name + "\ndescription: " + name + " plugin\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCuratedRootFile(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, plugin.CuratedFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// loadCuratedRootPlugins 用加载器（生产同一条路）读出这些根下真实可加载的插件。
func loadCuratedRootPlugins(t *testing.T, roots ...string) []plugin.Plugin {
	t.Helper()
	loaded, err := plugin.NewLoader(roots...).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// errorString 把一段文本包成 error，复用 curatedWantError 的断言口径（文案必须逐个包含）。
func errorString(text string) error { return stringError(text) }

type stringError string

func (e stringError) Error() string { return string(e) }

// curatedWantError 断言一段文案（不是只看 err != nil）：每个子串都必须出现——拒绝必须
// 说清理由，不靠上层猜。
func curatedWantError(t *testing.T, name string, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：期望一条错误，得到 nil", name)
	}
	message := err.Error()
	for _, substring := range want {
		if !strings.Contains(message, substring) {
			t.Fatalf("%s：文案 %q 必须包含 %q", name, message, substring)
		}
	}
}
