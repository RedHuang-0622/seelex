package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCuratedCatalogSideCarIsNotAPlugin 钉住"零改加载器"：curated.yaml 是插件根下的
// **旁车数据文件**，Loader 只认目录（loader.go 的 entry.IsDir()），且 "curated.yaml"
// 带点号不匹配 validPluginName。加了这个文件不该多出任何插件。
func TestCuratedCatalogSideCarIsNotAPlugin(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, filepath.Join(root, "alpha", manifestFile), "---\nschema_version: 1\nname: alpha\ndescription: A\n---\n# Alpha\n")
	mustPluginWrite(t, filepath.Join(root, CuratedFileName), baseCuratedYAML)

	loaded, err := NewLoader(root).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "alpha" {
		t.Fatalf("loaded = %#v, want exactly [alpha]", loaded)
	}
	if _, err := NewLoader(root).Load("curated.yaml"); err == nil {
		t.Fatal("curated.yaml must not be loadable as a plugin")
	}
}

// TestLoadCuratedFromRootRequiresFile 是"文件不存在即红"的那一半。
func TestLoadCuratedFromRootRequiresFile(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, filepath.Join(root, "alpha", manifestFile), "---\nschema_version: 1\nname: alpha\ndescription: A\n---\n# Alpha\n")
	if _, err := LoadCuratedFromRoot(root); err == nil {
		t.Fatal("missing curated.yaml should fail")
	}
}

// TestParseCuratedCatalogRejectsUnknownFields 钉住铁律 2：include/exclude 的事实源是
// plugin.md，权限档只属于 preset —— 精选目录里冒出这些字段必须是解码错误，而不是被
// 悄悄忽略（悄悄忽略就会长出第二份白名单，两份迟早不一致）。
func TestParseCuratedCatalogRejectsUnknownFields(t *testing.T) {
	cases := map[string]string{
		"entry include":  "schema_version: 1\nkind: curated-catalog\nread_at: x\nentries:\n  - name: alpha\n    description: A\n    installed: true\n    include: [bash]\n    source: {kind: builtin, url: u, license: l, pinned: p, read_at: r}\npresets: []\n",
		"entry exclude":  "schema_version: 1\nkind: curated-catalog\nread_at: x\nentries:\n  - name: alpha\n    description: A\n    installed: true\n    exclude: [bash]\n    source: {kind: builtin, url: u, license: l, pinned: p, read_at: r}\npresets: []\n",
		"entry tier":     "schema_version: 1\nkind: curated-catalog\nread_at: x\nentries:\n  - name: alpha\n    description: A\n    installed: true\n    permission_tier: full\n    source: {kind: builtin, url: u, license: l, pinned: p, read_at: r}\npresets: []\n",
		"unknown top":    "schema_version: 1\nkind: curated-catalog\nread_at: x\nmarketplace: https://example.com\nentries: []\npresets: []\n",
		"preset unknown": "schema_version: 1\nkind: curated-catalog\nread_at: x\nentries: []\npresets:\n  - name: baseline\n    description: B\n    permission_tier: manual\n    plugins: [alpha]\n    pending: []\n    owner: someone\n",
	}
	for name, document := range cases {
		if _, err := ParseCuratedCatalog([]byte(document)); err == nil {
			t.Errorf("%s: unknown field must be a decode error", name)
		}
	}
}

// TestValidateCuratedCatalogDiscriminates 是对抗性用例：每一条都会让目录**变红**。
// 没有这一组，"守卫测试"就只是把当前文件抄一遍——文件抄错时它同样绿。
func TestValidateCuratedCatalogDiscriminates(t *testing.T) {
	cases := map[string]struct {
		mutate func(*CuratedCatalog)
		want   string
	}{
		"引用不存在的插件目录": {func(c *CuratedCatalog) { c.Entries[0].Name = "ghost" }, "插件目录不存在"},
		"已落盘插件不在册": {func(c *CuratedCatalog) {
			c.Entries = nil
			c.Presets[0].Plugins = []string{"default", "beta"}
		}, "不在 entries 里"},
		"installed 未声明为 true": {func(c *CuratedCatalog) { c.Entries[0].Installed = false }, "installed 必须为 true"},
		"缺少 source.url":       {func(c *CuratedCatalog) { c.Entries[0].Source.URL = "" }, "source.url 缺失"},
		"缺少 source.pinned":    {func(c *CuratedCatalog) { c.Entries[0].Source.Pinned = "" }, "source.pinned 缺失"},
		"缺少 source.read_at":   {func(c *CuratedCatalog) { c.Entries[0].Source.ReadAt = "" }, "source.read_at 缺失"},
		"缺 description":       {func(c *CuratedCatalog) { c.Entries[0].Description = "" }, "description 缺失"},
		"重复条目":                {func(c *CuratedCatalog) { c.Entries = append(c.Entries, c.Entries[0]) }, "重复条目"},
		"非法插件名":               {func(c *CuratedCatalog) { c.Entries[0].Name = "Bad Name" }, "不是合法插件名"},
		"preset 引用了不存在的插件":    {func(c *CuratedCatalog) { c.Presets[0].Plugins = []string{"default", "ghost"} }, "plugins 引用了不存在的插件"},
		"pending 混进已装插件": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{{Name: "default", Upstream: "x/y", Source: basePending().Source, Verified: CuratedVerifiedListingOnly, Promote: "p"}}
		}, "其实已落盘"},
		"pending 未声明": {func(c *CuratedCatalog) { c.Presets[0].Pending = nil }, "pending 未声明"},
		"pending 缺 verified": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Verified = "" })}
		}, "不是已知证据档"},
		"pending 证据档不可识别": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Verified = "probably-fine" })}
		}, "不是已知证据档"},
		"pending 缺 promote": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Promote = "" })}
		}, "promote 缺失"},
		"pending 缺 upstream": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Upstream = "" })}
		}, "upstream 缺失"},
		"pending 缺 source.url": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Source.URL = "" })}
		}, "source.url 缺失"},
		"pending 名非法": {func(c *CuratedCatalog) {
			c.Presets[0].Pending = []CuratedPending{pendingWithout(func(p *CuratedPending) { p.Name = "owner/repo" })}
		}, "不是合法插件名"},
		"权限档非法":                {func(c *CuratedCatalog) { c.Presets[0].PermissionTier = "admin" }, "不是合法档位"},
		"权限档缺失":                {func(c *CuratedCatalog) { c.Presets[0].PermissionTier = "" }, "不是合法档位"},
		"preset 无 description": {func(c *CuratedCatalog) { c.Presets[0].Description = "" }, "description 缺失"},
		"preset 无 plugins":     {func(c *CuratedCatalog) { c.Presets[0].Plugins = nil }, "plugins 为空"},
		"缺 baseline":           {func(c *CuratedCatalog) { c.Presets[0].Name = "other" }, "缺少 \"baseline\" preset"},
		"baseline 不含 default": {func(c *CuratedCatalog) {
			c.Presets[0].Plugins = []string{}
			c.Presets = append(c.Presets, CuratedPreset{Name: "extra", Description: "E", PermissionTier: "manual", Plugins: []string{"default"}, Pending: []CuratedPending{}})
		}, "必须含 default"},
		"已落盘插件不属于任何 preset":  {func(c *CuratedCatalog) { c.Presets = nil }, "不属于任何 preset"},
		"presets 为空":         {func(c *CuratedCatalog) { c.Presets = []CuratedPreset{} }, "presets 为空"},
		"schema_version 不匹配": {func(c *CuratedCatalog) { c.SchemaVersion = 2 }, "schema_version = 2"},
		"kind 不匹配":           {func(c *CuratedCatalog) { c.Kind = "marketplace" }, "kind ="},
		"read_at 缺失":         {func(c *CuratedCatalog) { c.ReadAt = "" }, "read_at 缺失"},
		"entries 为空":         {func(c *CuratedCatalog) { c.Entries = nil }, "entries 为空"},
	}
	for name, tc := range cases {
		catalog := baseCuratedCatalog()
		tc.mutate(&catalog)
		err := ValidateCuratedCatalog(catalog, []string{"default", "beta"})
		if err == nil {
			t.Errorf("%s: catalog should be rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %q, want substring %q", name, err.Error(), tc.want)
		}
	}
}

// TestValidateCuratedCatalogAcceptsBase 是阴性对照：合法目录必须过（否则上面那组
// 全红用例可以靠"永远报错"通过）。
func TestValidateCuratedCatalogAcceptsBase(t *testing.T) {
	if err := ValidateCuratedCatalog(baseCuratedCatalog(), []string{"default"}); err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
}

// TestLoaderPrimaryRootPrefersExistingRoot 钉住写侧与读侧同源：责任链首根常常是交付树
// 里的路径（本机开发场景不存在），脚手架必须落到第一个**真实存在**的根，否则
// plugin_create 会写进 A 处而 plugins_reload 读 B 处（"创建成功但看不到"）。
func TestLoaderPrimaryRootPrefersExistingRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "exe-plugins")
	if got := NewLoader(missing, real, filepath.Join(root, "cwd")).PrimaryRoot(); got != real {
		t.Fatalf("PrimaryRoot = %q, want %q", got, real)
	}
	if got := NewLoader(missing, filepath.Join(root, "cwd")).PrimaryRoot(); got != missing {
		t.Fatalf("PrimaryRoot without any existing root = %q, want the chain head %q", got, missing)
	}
	if got := NewLoader().PrimaryRoot(); got != "" {
		t.Fatalf("empty loader PrimaryRoot = %q, want empty", got)
	}
}

// TestManagerCreateWritesIntoExistingRoot 是上一条的写侧落地：脚手架真的落到那个根。
func TestManagerCreateWritesIntoExistingRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "exe-plugins")
	m := NewManager(NewLoader(missing, real), &fakeTools{}, &fakeMCP{}, &fakeSkills{})
	if _, err := m.Create(CreateSpec{Name: "alpha", Description: "A"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, "alpha", manifestFile)); err != nil {
		t.Fatalf("plugin_create did not land in the first existing root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(missing, "alpha")); err == nil {
		t.Fatal("plugin_create wrote into a root that does not exist on disk")
	}
	report, err := m.Reload(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(report.Added) != 1 || report.Added[0] != "alpha" {
		t.Fatalf("reload added = %v（写侧与读侧不同源）", report.Added)
	}
}

const baseCuratedYAML = "schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\nentries: []\npresets: []\n"

func baseCuratedCatalog() CuratedCatalog {
	return CuratedCatalog{
		SchemaVersion: CurrentSchemaVersion,
		Kind:          CuratedKind,
		ReadAt:        "2026-10-05",
		Entries: []CuratedEntry{{
			Name: "default", Description: "baseline plugin", Installed: true,
			Source: CuratedSource{Kind: "builtin", URL: "https://example.com/default", License: "MIT", Pinned: "v1", ReadAt: "2026-10-05"},
		}},
		Presets: []CuratedPreset{{
			Name: CuratedBaselinePreset, Description: "baseline", PermissionTier: "manual",
			Plugins: []string{"default"}, Pending: []CuratedPending{},
		}},
	}
}

// basePending 是一条"证据齐备"的 pending 候选：任何一条字段缺失都该让目录变红。
func basePending() CuratedPending {
	return CuratedPending{
		Name:     "frontend-design",
		Upstream: "anthropics/frontend-design",
		Source: CuratedSource{
			Kind:    CuratedSourceCommunityListing,
			URL:     "https://github.com/VoltAgent/awesome-agent-skills",
			License: "未核实（清单未声明）",
			Pinned:  "未固定 commit",
			ReadAt:  "2026-10-05",
		},
		Verified: CuratedVerifiedListingOnly,
		Promote:  "实读正文 + 过可装载性四问",
	}
}

// pendingWithout 在 basePending 上改一个字段，用来逐条证明"证据缺一项就红"。
func pendingWithout(mutate func(*CuratedPending)) CuratedPending {
	item := basePending()
	mutate(&item)
	return item
}

// designCatalog 是一份"设计垂直面"目录：plugins 里是已落盘的 impeccable，
// pending 里是只核到清单那一层的前端候选。
func designCatalog() CuratedCatalog {
	catalog := baseCuratedCatalog()
	catalog.Entries[0].Name = "impeccable"
	catalog.Entries[0].Description = "前端设计纪律"
	catalog.Presets[0].Name = "design"
	catalog.Presets[0].Plugins = []string{"impeccable"}
	catalog.Presets[0].Pending = []CuratedPending{basePending()}
	return catalog
}

// TestCuratedPendingEmptySequenceIsDeclared 钉住 `pending: []` 与"没写 pending"是两件事：
// 前者是"我确认没有待装项"，后者是漏写。二者在 YAML 里必须能被区分开（否则守不住 #3）。
func TestCuratedPendingEmptySequenceIsDeclared(t *testing.T) {
	document := "schema_version: 1\nkind: curated-catalog\nread_at: 2026-10-05\n" +
		"entries:\n  - name: default\n    description: A\n    installed: true\n" +
		"    source: {kind: builtin, url: u, license: l, pinned: p, read_at: r}\n" +
		"presets:\n  - name: baseline\n    description: B\n    permission_tier: manual\n    plugins: [default]\n    pending: []\n"
	catalog, err := ParseCuratedCatalog([]byte(document))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if catalog.Presets[0].Pending == nil {
		t.Fatal("pending: [] 被解码成 nil —— 空路线图与「漏写 pending」将无法区分")
	}
	if err := ValidateCuratedCatalog(catalog, []string{"default"}); err != nil {
		t.Fatalf("空 pending 的合法目录被拒: %v", err)
	}
}

// TestAssemblePresetRejectsPendingAndPointsAtSource 是"归零不静默"的正面用例：
// preset 一旦引用了 pending 候选，装配必须**显式拒绝**，且错误里要能读到
// "pending"、候选的 upstream 与实读出处——不能退化成一句"目录不存在"。
func TestAssemblePresetRejectsPendingAndPointsAtSource(t *testing.T) {
	catalog := designCatalog()
	// 阴性对照先跑：只装已落盘插件的那一份路线图装配得起来。
	if plan, err := catalog.AssemblePreset("design", []string{"impeccable"}); err != nil {
		t.Fatalf("已落盘插件的装配不该被拒: %v", err)
	} else if len(plan.Plugins) != 1 || plan.Plugins[0] != "impeccable" || plan.PermissionTier != "manual" {
		t.Fatalf("plan = %#v", plan)
	}
	// 真缺口：preset 把 pending 候选写在 plugins 里（未来那次"顺手加一条"的写法）。
	catalog.Presets[0].Plugins = []string{"impeccable", "frontend-design"}
	_, err := catalog.AssemblePreset("design", []string{"impeccable"})
	if err == nil {
		t.Fatal("装配到 pending 候选必须被拒绝")
	}
	for _, want := range []string{
		"pending", "frontend-design", "anthropics/frontend-design",
		"https://github.com/VoltAgent/awesome-agent-skills",
		CuratedVerifiedListingOnly, "装配被拒绝",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("拒绝文案缺 %q: %s", want, err.Error())
		}
	}
}

// TestAssemblePluginRejectsPending 是另一条入口：用户/teammate 直接点名要装一个
// "清单上看见的"候选时，同样必须被拒，并且同样要指向来源。
func TestAssemblePluginRejectsPending(t *testing.T) {
	catalog := designCatalog()
	if _, err := catalog.AssemblePlugin("frontend-design", []string{"impeccable"}); err == nil {
		t.Fatal("点名 pending 候选必须被拒绝")
	} else if !strings.Contains(err.Error(), "pending") || !strings.Contains(err.Error(), "anthropics/frontend-design") {
		t.Fatalf("拒绝文案必须点名 pending 与来源: %s", err.Error())
	}
	plan, err := catalog.AssemblePlugin("impeccable", []string{"impeccable"})
	if err != nil {
		t.Fatalf("已落盘插件应可装配: %v", err)
	}
	if plan.Preset != "design" || plan.PermissionTier != "manual" {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := catalog.AssemblePlugin("never-heard-of-it", []string{"impeccable"}); err == nil {
		t.Fatal("来源不明的名字必须被拒绝")
	}
}

// TestAssemblePresetRejectsUnknownNameWithoutSilentSkip 钉住"不许静默跳过"：
// 一个既没落盘、也不在 pending 里的名字必须报错，而不是被当成"没装就跳过"。
func TestAssemblePresetRejectsUnknownNameWithoutSilentSkip(t *testing.T) {
	catalog := designCatalog()
	catalog.Presets[0].Plugins = []string{"impeccable", "ghost-plugin"}
	if _, err := catalog.AssemblePreset("design", []string{"impeccable"}); err == nil {
		t.Fatal("未落盘且不在 pending 的名字必须被拒绝（不许静默跳过）")
	} else if !strings.Contains(err.Error(), "来源不明") {
		t.Fatalf("err = %s", err.Error())
	}
	if _, err := catalog.AssemblePreset("no-such-preset", []string{"impeccable"}); err == nil {
		t.Fatal("不存在的 preset 必须被拒绝")
	}
}

// TestActivateFromCatalogRejectsPendingBeforeSideEffects 是装配面的端到端闸门：
// 拒绝必须发生在**任何副作用之前**（tools/skills 都没动过），不能"先激活一半再报错"。
func TestActivateFromCatalogRejectsPendingBeforeSideEffects(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{"impeccable": {Name: "impeccable"}}

	catalog := designCatalog()
	if err := m.ActivateFromCatalog(context.Background(), catalog, "design"); err != nil {
		t.Fatalf("已落盘插件的 preset 装配失败: %v", err)
	}
	if m.current != "impeccable" || tools.active != "impeccable" {
		t.Fatalf("装配未生效: current=%q tools=%q", m.current, tools.active)
	}

	// 换成"preset 引用了 pending"的那一份目录：必须被拒，且不留下半激活状态。
	catalog.Presets[0].Plugins = []string{"frontend-design"}
	err := m.ActivateFromCatalog(context.Background(), catalog, "design")
	if err == nil {
		t.Fatal("装配到 pending 必须被拒绝")
	}
	if !strings.Contains(err.Error(), "pending") || !strings.Contains(err.Error(), "anthropics/frontend-design") {
		t.Fatalf("拒绝文案必须点名 pending 与来源: %s", err.Error())
	}
	if m.current != "impeccable" || tools.active != "impeccable" || skills.active != "impeccable" {
		t.Fatalf("拒绝发生在副作用之后: current=%q tools=%q skills=%q", m.current, tools.active, skills.active)
	}
}

// TestActivateFromCatalogRejectsMultiPluginPreset 钉住全局单选这条产品口径：
// preset 解析出两个插件时要显式拒绝，而不是悄悄只激活第一个。
func TestActivateFromCatalogRejectsMultiPluginPreset(t *testing.T) {
	tools := &fakeTools{}
	m := NewManager(NewLoader(), tools, &fakeMCP{}, &fakeSkills{})
	m.plugins = map[string]Plugin{"impeccable": {Name: "impeccable"}, "freecad": {Name: "freecad"}}
	catalog := designCatalog()
	catalog.Presets[0].Plugins = []string{"impeccable", "freecad"}
	if err := m.ActivateFromCatalog(context.Background(), catalog, "design"); err == nil {
		t.Fatal("多插件 preset 必须被拒绝（仍是全局单选）")
	} else if !strings.Contains(err.Error(), "全局单选") {
		t.Fatalf("err = %s", err.Error())
	}
	if tools.active != "" {
		t.Fatalf("拒绝后不应有激活: %q", tools.active)
	}
}

// TestReadSourceSharesOneJudgement 钉住"来源判定只有一套"：机读面（ReadSource 给出的
// kind/url）与人读面（SourceSummary 的那一行）必须出自**同一次判定**——否则运行期视图说
// builtin、plugins_list 说别的，用户手里的"谁给的"就有了两个答案。
//
// 同时钉住"缺失来源不编"：未登记的名字两项留空、found=false，而不是给个默认 kind。
func TestReadSourceSharesOneJudgement(t *testing.T) {
	catalog := designCatalog()
	for _, name := range []string{"impeccable", "default", "frontend-design", "nobody"} {
		summary, summaryOK := catalog.SourceSummary(name)
		reading, readingOK := catalog.ReadSource(name)
		if summaryOK != readingOK {
			t.Fatalf("%s: 两个读面判定不一致（summary ok=%v，reading ok=%v）", name, summaryOK, readingOK)
		}
		if !readingOK {
			if reading.Kind != "" || reading.URL != "" || reading.Summary != "" {
				t.Fatalf("%s: 未登记就必须整条留空（不编默认值），得 %+v", name, reading)
			}
			if entry, ok := catalog.Entry(name); ok {
				t.Fatalf("%s: 判定说没登记，entries 里却有它：%+v", name, entry)
			}
			continue
		}
		if reading.Summary != summary {
			t.Fatalf("%s: 同一判定必须给同一行摘要\n summary=%q\n reading=%q", name, summary, reading.Summary)
		}
		if reading.Kind == "" || reading.URL == "" {
			t.Fatalf("%s: 已登记的来源必须给出机读的 kind/url，得 %+v", name, reading)
		}
		if entry, ok := catalog.Entry(name); ok && (reading.Kind != entry.Source.Kind || reading.URL != entry.Source.URL) {
			t.Fatalf("%s: 机读面必须落在 entries 的 source 上：%+v vs %+v", name, reading.PluginSource, entry.Source)
		}
	}
}

// TestSourceSummaryIsReadable 钉住"这个插件是谁给的"这一最小可见化读面：
// entries 读出落盘来源，pending 读出"未落盘 + 出处 + 证据档"，未知名读不出。
func TestSourceSummaryIsReadable(t *testing.T) {
	catalog := designCatalog()
	installed, ok := catalog.SourceSummary("impeccable")
	if !ok {
		t.Fatal("已落盘插件的来源必须可读")
	}
	if !strings.Contains(installed, "impeccable") || !strings.Contains(installed, "https://example.com/default") {
		t.Fatalf("summary = %q", installed)
	}
	pending, ok := catalog.SourceSummary("frontend-design")
	if !ok {
		t.Fatal("pending 候选的来源必须可读")
	}
	for _, want := range []string{"pending", "anthropics/frontend-design", "awesome-agent-skills", CuratedVerifiedListingOnly} {
		if !strings.Contains(pending, want) {
			t.Errorf("pending summary 缺 %q: %q", want, pending)
		}
	}
	if _, ok := catalog.SourceSummary("nobody"); ok {
		t.Fatal("没见过的名字不该编出来源")
	}
}
