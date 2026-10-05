package plugin

// 精选目录（curated catalog）：`plugins/curated.yaml` 的读侧与守卫。
//
// 定位：这是**目录**（catalog），不是商店（store）。它是内置 marketplace 清单的
// 本地简化版——外部清单形如 `{name, description, owner:{name}, plugins:[{name,
// source}]}`，用 `name@marketplace` 标识安装、由 `claude plugin validate <dir>`
// 校验；我们只保留「这份发行包里到底带什么、是谁给的、按哪个权限档装配」三件事，
// 不做下载、不做版本解析、不做多源合并。旁车文件放在插件根里**不影响加载器**：
// `Loader.LoadAll` 只认目录（`loader.go:44` 的 `entry.IsDir()`），`curated.yaml`
// 带点号也不匹配 `validPluginName`。
//
// 三条铁律，前两条由本文件变成机器错误（而不是靠人眼评审）：
//
//  1. **只列已落盘插件**：`entries` 与插件根下真实可加载的插件**一一对应**；都落盘的
//     必须都在册，不在册的手写上一条就红。没装的东西只允许出现在 `presets[].pending`，
//     语义是「装配到它必须显式拒绝」——它是路线图，不是可用集合。装配面的两道闸都在
//     本文件：`AssemblePreset` / `AssemblePlugin`，它们对 pending 的拒绝会**点名它是
//     pending 并指向它的来源**，而不是留一句"插件目录不存在"（那时用户既看不出这是
//     路线图里的候选，也不知道该去核谁）。
//
//     `pending` 的每一条还**必须标出证据强度**：`source`（谁给的）+ `verified`（核到
//     什么程度）。当前唯一允许的档是 `listing-only`（只核到"仓库存在 + 官方一句话
//     定位"，**没有**读进任何 skill 正文），因此这些条目一律**不得被读成"已可用"**；
//     转正（从 pending 挪进 `entries`）前必须先实读正文并过**可装载性四问**
//     （`CuratedPromoteQuestions`）。
//  2. **禁止重复 include/exclude、禁止插件自带权限档**：include/exclude 的事实源是
//     `plugins/<name>/plugin.md`，权限档只出现在 `presets[]`（理由见
//     `docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md` 与设计稿契约 5：
//     插件的包可以被覆盖，权限不能由"别人给的包"供货）。本文件用 `yaml.Decoder` 的
//     `KnownFields(true)` 把这条钉成解码错误：`entries` 下冒出一个 `include:` 或
//     `permission_tier:` 就是一个 unknown field。
//  3. **每条 entry 必有 `source`**（kind/url/license/pinned/read_at 五项全填）：
//     「哪个插件是谁给的」必须能一眼读出，否则精选目录退化成一份无出处的名单。

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CuratedFileName 是精选目录文件名（插件根下的旁车数据文件，本身不是 Plugin）。
const CuratedFileName = "curated.yaml"

// CuratedKind 是精选目录的 kind 标记：把它与别的同名 YAML 区分开。
const CuratedKind = "curated-catalog"

// CuratedBaselinePreset 是启动基线装配的名字（必须存在且含 default）。
const CuratedBaselinePreset = "baseline"

// CuratedVerifiedListingOnly 是 `presets[].pending` 候选当前**唯一实读过的证据档**：
// 只核到"仓库存在 + 官方一句话定位"（listing），**没有**读进任何 skill 正文。
// 它是一条路线图标记，不是可用性声明。
const CuratedVerifiedListingOnly = "listing-only"

// CuratedVerifiedFullText 是转正所需的最低证据档：正文实读 + 可装载性四问通过。
// 目前没有任何候选达到它——写在这里是为了让"listing-only → full-text-read"这条
// 晋升路径有名字，而不是靠一句自然语言注释。
const CuratedVerifiedFullText = "full-text-read"

// CuratedSourceCommunityListing 是 pending 候选的 `source.kind`：来源是社区清单/
// 主题页的一次实读，不是官方发行源。用它把"清单上看见的"与"上游发布页"分开。
const CuratedSourceCommunityListing = "community-listing"

// CuratedPromoteQuestions 是转正（pending → entries）前必须逐条回答的**可装载性四问**。
// 它是 pending 那一句话 `promote` 的展开，也是文件注释里那条纪律的机器可引用版本。
const CuratedPromoteQuestions = "① 可装载性：正文能否被 skill.LoadPluginDir 装载（目录/frontmatter/name 合法）；" +
	"② 不带权限面：不携带权限档、hooks 或插件级 MCP 权限（权限只能由 preset 给）；" +
	"③ 许可证：上游 LICENSE 明确且允许再分发；" +
	"④ 成本：装配后每轮进上下文的 name+description 体积可接受"

var curatedVerifiedLevels = map[string]bool{
	CuratedVerifiedListingOnly: true,
	CuratedVerifiedFullText:    true,
}

func curatedVerifiedLevelNames() string {
	return CuratedVerifiedListingOnly + " / " + CuratedVerifiedFullText
}

// curatedPermissionTiers 是 preset 允许声明的权限档，与 `main.go` 的 `-permission`
// 旗标同一组取值（manual/edit/auto/full）。档位是**宿主**给的，不是插件自带的。
var curatedPermissionTiers = map[string]bool{
	"manual": true, "edit": true, "auto": true, "full": true,
}

// CuratedSource 说明一条 entry 的来处：kind 是来源类型（builtin/vendored/…），
// url/license/pinned 是它的身份与版本，read_at 是这份读数的时点。
type CuratedSource struct {
	Kind    string `yaml:"kind"`
	URL     string `yaml:"url"`
	License string `yaml:"license"`
	Pinned  string `yaml:"pinned"`
	ReadAt  string `yaml:"read_at"`
}

// CuratedEntry 是一条已落盘插件的目录条目。**故意没有 include/exclude 字段**
// （事实源在 plugin.md），也没有权限档字段（只属于 preset）。
type CuratedEntry struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Installed   bool          `yaml:"installed"`
	Source      CuratedSource `yaml:"source"`
}

// CuratedPending 是一条**尚未落盘**的候选插件：它只活在路线图里，不是可用集合。
//   - upstream：清单里那条候选的**字面标识**（一般是 `owner/repo`），这是"谁给的"；
//   - source：这次实读的出处（kind/url/license/pinned/read_at 五项必填；kind 目前是
//     `community-listing`，url 指向被实读的清单/主题页）；
//   - verified：我们**核到什么程度**——当前只有 listing-only（只核到仓库存在 +
//     官方一句话定位，没读进正文），故它不得被读成"已可用"；
//   - promote：一句话写清转正还缺什么（四问见 CuratedPromoteQuestions）。
type CuratedPending struct {
	Name     string        `yaml:"name"`
	Upstream string        `yaml:"upstream"`
	Source   CuratedSource `yaml:"source"`
	Verified string        `yaml:"verified"`
	Promote  string        `yaml:"promote"`
}

// CuratedPreset 是一个装配预设：切到哪些插件 + 用哪个权限档。
// pending 列出"这个预设想装、但这台机器/这个发行包里没有"的候选；它们**必须被显式拒绝**
// （`AssemblePreset` / `AssemblePlugin`），而不是让加载器报一句目录不存在。
type CuratedPreset struct {
	Name           string           `yaml:"name"`
	Description    string           `yaml:"description"`
	PermissionTier string           `yaml:"permission_tier"`
	Plugins        []string         `yaml:"plugins"`
	Pending        []CuratedPending `yaml:"pending"`
}

// CuratedCatalog 是精选目录的完整读数。
type CuratedCatalog struct {
	SchemaVersion int             `yaml:"schema_version"`
	Kind          string          `yaml:"kind"`
	ReadAt        string          `yaml:"read_at"`
	Entries       []CuratedEntry  `yaml:"entries"`
	Presets       []CuratedPreset `yaml:"presets"`
}

// Entry 按名字取一条目录条目。
func (c CuratedCatalog) Entry(name string) (CuratedEntry, bool) {
	for _, entry := range c.Entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return CuratedEntry{}, false
}

// Preset 按名字取一个装配预设。
func (c CuratedCatalog) Preset(name string) (CuratedPreset, bool) {
	for _, preset := range c.Presets {
		if preset.Name == name {
			return preset, true
		}
	}
	return CuratedPreset{}, false
}

// PresetForPlugin 回报某个插件挂在哪个 preset 的 `plugins` 下（权限档从这里取）。
func (c CuratedCatalog) PresetForPlugin(name string) (CuratedPreset, bool) {
	for _, preset := range c.Presets {
		if containsString(preset.Plugins, name) {
			return preset, true
		}
	}
	return CuratedPreset{}, false
}

// PendingEntry 跨所有 preset 找一条 pending 候选，并回报它登记在哪个 preset 里。
func (c CuratedCatalog) PendingEntry(name string) (CuratedPending, string, bool) {
	for _, preset := range c.Presets {
		for _, item := range preset.Pending {
			if item.Name == name {
				return item, preset.Name, true
			}
		}
	}
	return CuratedPending{}, "", false
}

// PluginSource 是一个插件「从哪来」的**机读面**：kind 是来源类型
// （builtin / vendored / local / …），url 是它的身份（kind=local 时是 `local:<根>`）。
//
// 它与 SourceSummary 出自**同一处判定**（下面的 ReadSource），所以"哪个插件是谁给的"
// 只有一个答案；本类型只是把那一行摘要拆成两个字段，给运行期视图（runtime.plugins）
// 用，不另建一套来源判定。
type PluginSource struct {
	Kind string
	URL  string
}

// SourceReading 是一次来源读数的完整形态：机读的两个字段 + 可读的一行摘要。
// **它是唯一的判定点**：SourceSummary 与 PluginSource 都从它派生（第二套判定迟早与
// 这一份不一致，届时"谁给的"就会有两个答案）。
type SourceReading struct {
	PluginSource
	Summary string
}

// ReadSource 给出某个插件的来源读数。found=false 表示这个名字既不在 entries 也不在
// 任何 pending 里 —— 此时**什么都不编**：留空而不是给一个默认 kind（"不知道"与
// "它是 builtin"是两回事）。
func (c CuratedCatalog) ReadSource(name string) (SourceReading, bool) {
	if entry, ok := c.Entry(name); ok {
		return SourceReading{
			PluginSource: PluginSource{Kind: entry.Source.Kind, URL: entry.Source.URL},
			Summary: fmt.Sprintf("%s ← %s %s（许可证 %s；固定 %s；实读 %s）",
				entry.Name, entry.Source.Kind, entry.Source.URL, entry.Source.License, entry.Source.Pinned, entry.Source.ReadAt),
		}, true
	}
	if item, fromPreset, ok := c.PendingEntry(name); ok {
		return SourceReading{
			PluginSource: PluginSource{Kind: item.Source.Kind, URL: item.Source.URL},
			Summary: fmt.Sprintf("%s ← %s（%s；已实读 %s；**pending，未落盘**，登记在 preset %q；证据档 %s）",
				item.Name, item.Upstream, item.Source.URL, item.Source.ReadAt, fromPreset, item.Verified),
		}, true
	}
	return SourceReading{}, false
}

// SourceSummary 是"这个插件是谁给的"的一行摘要（读面）。`plugins_list` 之类的回执
// 直接嵌这一行即可，不必各自去解析 curated.yaml。found=false 表示这个名字既不在
// entries 也不在任何 pending 里。
//
// 它不自己判定：正文来自 ReadSource（与机读的 PluginSource 同源同一次判定）。
func (c CuratedCatalog) SourceSummary(name string) (string, bool) {
	reading, ok := c.ReadSource(name)
	if !ok {
		return "", false
	}
	return reading.Summary, true
}

// AssemblyPlan 是一次装配解析的结果：preset 名、权限档、以及**要激活的已落盘插件**。
type AssemblyPlan struct {
	Preset         string   `json:"preset"`
	PermissionTier string   `json:"permission_tier"`
	Plugins        []string `json:"plugins"`
}

// pendingRejection 是"归零不静默"的那一句话：pending 不是可用集合，装配到它必须
// 显式拒绝，且错误里必须同时看到**它是 pending**与**它的来源**——否则用户只看到一句
// "插件目录不存在"，既不知道那是路线图里的候选，也不知道该去核谁。
func pendingRejection(assembling string, item CuratedPending, declaredIn string) error {
	target := item.Name
	if assembling != "" && assembling != item.Name {
		target = fmt.Sprintf("%s（由 %s 引用）", item.Name, assembling)
	}
	return fmt.Errorf(
		"装配被拒绝：%s 是 pending（未落盘，不在可用集合里）——候选来源 %s（%s；许可证 %s；固定 %s；实读 %s），"+
			"登记在 preset %q 的路线图里，证据档 %s。转正前必须：%s",
		target, item.Upstream, item.Source.URL, item.Source.License, item.Source.Pinned,
		item.Source.ReadAt, declaredIn, item.Verified, item.Promote)
}

// AssemblePreset 把 preset 解析成装配计划（要激活哪些已落盘插件 + 用哪个权限档）。
// pending 里的候选一律**不进**计划：preset 一旦引用了它们，装配在这里显式拒绝并点名
// 来源，而不是把失败留给加载器（那时错误里看不到 pending，也看不到出处）。
func (c CuratedCatalog) AssemblePreset(presetName string, installed []string) (AssemblyPlan, error) {
	preset, ok := c.Preset(presetName)
	if !ok {
		return AssemblyPlan{}, fmt.Errorf("装配被拒绝：精选目录里没有 preset %q", presetName)
	}
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}
	declared := make(map[string]CuratedPending, len(preset.Pending))
	for _, item := range preset.Pending {
		declared[item.Name] = item
	}
	plan := AssemblyPlan{Preset: preset.Name, PermissionTier: preset.PermissionTier}
	for _, pluginName := range preset.Plugins {
		if installedSet[pluginName] {
			plan.Plugins = append(plan.Plugins, pluginName)
			continue
		}
		if item, ok := declared[pluginName]; ok {
			return AssemblyPlan{}, pendingRejection(preset.Name, item, preset.Name)
		}
		if item, declaredIn, ok := c.PendingEntry(pluginName); ok {
			return AssemblyPlan{}, pendingRejection(preset.Name, item, declaredIn)
		}
		return AssemblyPlan{}, fmt.Errorf(
			"装配 preset %q 被拒绝：%q 既未落盘、也不在任何 preset 的 pending 里（来源不明，不许静默跳过）",
			preset.Name, pluginName)
	}
	return plan, nil
}

// AssemblePlugin 是"我就要这个插件"的单插件装配解析：已落盘则给出它所在的 preset 与
// 权限档；只存在于 pending 则**显式拒绝并指向来源**（这正是"清单上看见 ≠ 可用"）。
func (c CuratedCatalog) AssemblePlugin(pluginName string, installed []string) (AssemblyPlan, error) {
	for _, name := range installed {
		if name != pluginName {
			continue
		}
		plan := AssemblyPlan{Plugins: []string{pluginName}}
		if preset, ok := c.PresetForPlugin(pluginName); ok {
			plan.Preset = preset.Name
			plan.PermissionTier = preset.PermissionTier
		}
		return plan, nil
	}
	if item, declaredIn, ok := c.PendingEntry(pluginName); ok {
		return AssemblyPlan{}, pendingRejection("", item, declaredIn)
	}
	return AssemblyPlan{}, fmt.Errorf(
		"装配被拒绝：%q 既未落盘、也不在任何 preset 的 pending 里（来源不明）", pluginName)
}

// CuratedRead 是一次"从插件根读精选目录"的完整读数：目录里写了什么（Catalog）+
// **这份目录是从哪读来的**（Path/Roots）+ **读到了没有**（Err）。
//
// 为什么不是只交一个 CuratedCatalog：装配面的拒绝文案要能落到**一页可核的文件**上，
// 并在读不到时说清**找过哪些根**。只报一句"来源不明"，用户既不知道该去核谁，也分不清
// "目录里没有这个名字"与"目录根本没读到"——后者会把路线图候选判成错别字，也会让
// "目录缺失"静默退化成"没有 pending"。
type CuratedRead struct {
	Catalog CuratedCatalog
	// Path 是实读到的 curated.yaml 路径（"实读页"）。Err 非 nil 时，它可能是"存在但
	// 读不动"的那一份；责任链上一个根都没这份文件时为空串。
	Path string
	// Roots 是责任链上按优先级找过的插件根（文案要说清"用哪个根找过"）。
	Roots []string
	// Err 是"没读到"的原因（缺失 / 解析失败 / 校验失败）。**非 nil 不得当空目录**。
	Err error
	// Drift 是"目录 ↔ 这台机器此刻的已装集合"的差异（本机自装插件、退役插件…）。它是
	// **回报**不是错误：目录照样能用于装配判定，启动期也不该因此报一条警告。
	Drift []string
	// Absent 表示责任链上的根**都没有** curated.yaml：自建根/用户树本来可以不带精选
	// 目录，这是合法现场。Err 仍非 nil（装配判定要能说清"没有目录可比对"），但它不是
	// 配置缺陷，不该进启动警告。
	Absent bool
}

// Page 返回"这份读数是从哪里来的"一行（无路径时给找过的根），供拒绝文案与回执引用。
func (read CuratedRead) Page() string {
	if strings.TrimSpace(read.Path) != "" {
		return read.Path
	}
	if len(read.Roots) > 0 {
		return strings.Join(read.Roots, ", ")
	}
	return "(责任链为空)"
}

// Judge 判一个**本进程未定义**的装配名：nil = 放行（目录把它登记为已落盘的 entries），
// 非 nil = 一条说清理由的显式拒绝。
//
// 分支顺序（先"目录读到了没有"，再"它在目录里是什么"，最后"目录里没有它"）：
//  1. **目录没读到** ⇒ 拒，并说清**找过哪些根**、为什么没读到（不得静默当空目录）；
//  2. 在 `entries` 里 ⇒ 拒：目录说它**已落盘**，本进程却没有它 —— 名字漂了/被撤过，
//     这种"失效"要当场说出来，而不是回一句"未定义"让人以为是写错了名字；
//  3. 在 `presets[].pending` 里 ⇒ 拒，且点名 pending + 上游来源 + 实读页 + 证据档；
//  4. 谁都不认识 ⇒ 拒，说清"**不在已装插件、也不在精选目录**"（两边名单都写上）。
//
// installed 是"本进程已定义"的插件名（调用方给，排序稳定），只进文案不打判定：
// "已装"这个事实的判据仍是调用方手里的定义表，本函数不复制它。
func (read CuratedRead) Judge(name string, installed []string) error {
	if read.Err != nil {
		// 目录缺席（自建根/用户树）与目录坏掉（存在却读不动）是两件事：前者是合法现场，
		// 只是"没有目录可比对"；后者是配置缺陷。两者都必须**显式拒绝**（读不到不等于
		// 目录里没有它），但只有后者算启动警告。
		if read.Absent {
			return fmt.Errorf(
				"装配被拒绝：%q 无法判定——责任链上的根都没有 %s（找过: %s；本进程已定义插件: %s）。"+
					"这不是配置缺陷：自建根/用户树可以不带精选目录；但\"目录读不到\"不等于\"目录里没有它\"，"+
					"所以这里显式拒绝，不静默当空目录（否则路线图候选会被读成错别字）",
				name, CuratedFileName, read.Page(), curatedNameList(installed))
		}
		return fmt.Errorf(
			"装配被拒绝：%q 无法判定——精选目录**没读到**（%v；责任链上找过的根: %s）。"+
				"读不到不等于\"目录里没有它\"：不许静默当空目录（否则路线图候选会被读成错别字）",
			name, read.Err, read.Page())
	}
	if entry, ok := read.Catalog.Entry(name); ok {
		return fmt.Errorf(
			"装配被拒绝：%q 在精选目录里登记为**已落盘**（entries，来源 %s %s；实读页 %s），"+
				"但本进程没有定义它——插件被撤过或名字漂了（显式拒绝，不静默忽略）",
			name, entry.Source.Kind, entry.Source.URL, read.Page())
	}
	if item, declaredIn, ok := read.Catalog.PendingEntry(name); ok {
		return fmt.Errorf("%w（精选目录实读页 %s，读于 %s；本进程已定义插件: %s）——显式拒绝，不静默忽略",
			pendingRejection("", item, declaredIn), read.Page(), read.Catalog.ReadAt, curatedNameList(installed))
	}
	return fmt.Errorf(
		"装配被拒绝：%q 未定义——既不在已装插件（%s），也不在精选目录（实读页 %s，读于 %s）的 "+
			"entries / presets[].pending 里；显式拒绝，不静默忽略（错别字？还是忘了登记精选目录？）",
		name, curatedNameList(installed), read.Page(), read.Catalog.ReadAt)
}

// curatedNameList 把插件名清单渲染成一行（空清单必须显式写成"(无)"，不留空档让人猜）。
func curatedNameList(names []string) string {
	if len(names) == 0 {
		return "(无)"
	}
	return strings.Join(names, ", ")
}

// LoadCuratedFromRoot 读 `root/curated.yaml`，并用 root 下**真实可加载**的插件
// 交叉校验（这是"只列已落盘插件"的机器判据：三个已落盘插件少一个都红）。
func LoadCuratedFromRoot(root string) (CuratedCatalog, error) {
	plugins, err := NewLoader(root).LoadAll()
	if err != nil {
		return CuratedCatalog{}, err
	}
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name)
	}
	return LoadCuratedCatalog(root, names)
}

// LoadCuratedCatalog 读取并校验精选目录；installed 是插件根下真实存在的插件名。
// 文件不存在、字段缺失/未知、引用了不存在的插件、pending 里混进已装插件——都是错误。
func LoadCuratedCatalog(root string, installed []string) (CuratedCatalog, error) {
	path := filepath.Join(root, CuratedFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return CuratedCatalog{}, fmt.Errorf("精选目录 %s: %w", path, err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		return CuratedCatalog{}, fmt.Errorf("精选目录 %s: %w", path, err)
	}
	if err := ValidateCuratedCatalog(catalog, installed); err != nil {
		return CuratedCatalog{}, fmt.Errorf("精选目录 %s: %w", path, err)
	}
	return catalog, nil
}

// ParseCuratedCatalog 严格解码精选目录（未知字段即错误，见文件头铁律 2）。
func ParseCuratedCatalog(data []byte) (CuratedCatalog, error) {
	var catalog CuratedCatalog
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&catalog); err != nil {
		return CuratedCatalog{}, fmt.Errorf("解析失败: %w", err)
	}
	return catalog, nil
}

// LoadCuratedForRuntime 读 `root/curated.yaml` 供**运行期装配面**使用：目录**自身**的问题
// （读不动 / 解析失败 / 字段缺失 / 证据档非法）照旧是硬错误，但"目录与这台机器此刻的已装
// 集合不一致"退成**回报**（drift），不是错误。
//
// 为什么必须这么分（2026-10-05 实读）：运行树是使用者的现场——他放个自装插件、删一个不
// 用的插件，都不该让 app 每次启动都报一条配置警告。旧口径下 entries 与已装集合必须一一
// 对应，于是**任何本地改动**都会把这份目录判成"校验失败"，使用者看到的是一句他改不动、
// 也不该由他改的发行侧警告。一一对应仍是**发行守卫**的口径（ValidateCuratedCatalog /
// LoadCuratedCatalog，仓库与交付树的测试都在用它），只是不再拿它去卡运行期。
func LoadCuratedForRuntime(root string, installed []string) (CuratedCatalog, []string, error) {
	path := filepath.Join(root, CuratedFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return CuratedCatalog{}, nil, fmt.Errorf("精选目录 %s: %w", path, err)
	}
	catalog, err := ParseCuratedCatalog(data)
	if err != nil {
		return CuratedCatalog{}, nil, fmt.Errorf("精选目录 %s: %w", path, err)
	}
	if problems := validateCuratedCatalogStructure(catalog); len(problems) > 0 {
		return CuratedCatalog{}, nil, fmt.Errorf("精选目录 %s: 校验失败:\n  - %s", path, strings.Join(problems, "\n  - "))
	}
	return catalog, curatedInstalledDrift(catalog, installed), nil
}

// ValidateCuratedCatalog 校验精选目录与"实际落盘的插件集合"一致，并逐条报告问题
// （一次列全，而不是只报第一条——否则修一轮只能前进一格）。
//
// 这是**发行守卫**的口径（仓库 / 交付树 / preset 装配闸），运行期走 LoadCuratedForRuntime。
func ValidateCuratedCatalog(catalog CuratedCatalog, installed []string) error {
	problems := append(validateCuratedCatalogStructure(catalog), curatedInstalledDrift(catalog, installed)...)
	if len(problems) > 0 {
		return fmt.Errorf("精选目录校验失败:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// validateCuratedCatalogStructure 只校验目录**自身**成立与否（与"这台机器装了什么"无关）：
// schema/kind/read_at、逐条 entry 的字段与来源、逐条 preset 的权限档与 pending 证据档、
// baseline 存在且含 default。这一半是"文件本身错了"，运行期也必须红。
func validateCuratedCatalogStructure(catalog CuratedCatalog) []string {
	problems := make([]string, 0, 8)

	if catalog.SchemaVersion != CurrentSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema_version = %d，want %d", catalog.SchemaVersion, CurrentSchemaVersion))
	}
	if catalog.Kind != CuratedKind {
		problems = append(problems, fmt.Sprintf("kind = %q，want %q", catalog.Kind, CuratedKind))
	}
	if strings.TrimSpace(catalog.ReadAt) == "" {
		problems = append(problems, "read_at 缺失（这份目录是什么时候读的）")
	}
	if len(catalog.Entries) == 0 {
		problems = append(problems, "entries 为空：精选目录一条已落盘插件都没列")
	}

	listed := make(map[string]bool, len(catalog.Entries))
	for index, entry := range catalog.Entries {
		where := fmt.Sprintf("entries[%d]", index)
		if entry.Name != "" {
			where = fmt.Sprintf("entries[%d] (%s)", index, entry.Name)
		}
		if !validPluginName.MatchString(entry.Name) {
			problems = append(problems, fmt.Sprintf("%s: name %q 不是合法插件名", where, entry.Name))
		} else if listed[entry.Name] {
			problems = append(problems, fmt.Sprintf("%s: 重复条目", where))
		} else {
			listed[entry.Name] = true
		}
		if strings.TrimSpace(entry.Description) == "" {
			problems = append(problems, fmt.Sprintf("%s: description 缺失", where))
		}
		if !entry.Installed {
			problems = append(problems, fmt.Sprintf("%s: installed 必须为 true（entries 只列已落盘插件）", where))
		}
		// 逐项（固定顺序，报错可复现）：哪个插件是谁给的必须可读。
		for _, field := range []struct{ name, value string }{
			{"source.kind", entry.Source.Kind},
			{"source.url", entry.Source.URL},
			{"source.license", entry.Source.License},
			{"source.pinned", entry.Source.Pinned},
			{"source.read_at", entry.Source.ReadAt},
		} {
			if strings.TrimSpace(field.value) == "" {
				problems = append(problems, fmt.Sprintf("%s: %s 缺失（哪个插件是谁给的必须可读）", where, field.name))
			}
		}
	}

	if len(catalog.Presets) == 0 {
		problems = append(problems, "presets 为空：没有可装配的权限档组合")
	}
	seenPresets := make(map[string]bool, len(catalog.Presets))
	for index, preset := range catalog.Presets {
		where := fmt.Sprintf("presets[%d]", index)
		if preset.Name != "" {
			where = fmt.Sprintf("presets[%d] (%s)", index, preset.Name)
		}
		if strings.TrimSpace(preset.Name) == "" {
			problems = append(problems, fmt.Sprintf("%s: name 缺失", where))
		} else if seenPresets[preset.Name] {
			problems = append(problems, fmt.Sprintf("%s: 重复 preset", where))
		} else {
			seenPresets[preset.Name] = true
		}
		if strings.TrimSpace(preset.Description) == "" {
			problems = append(problems, fmt.Sprintf("%s: description 缺失", where))
		}
		// 权限档只能由 preset 给（插件不得自带），且必须是产品认识的档位。
		if !curatedPermissionTiers[preset.PermissionTier] {
			problems = append(problems, fmt.Sprintf("%s: permission_tier = %q 不是合法档位（manual/edit/auto/full）", where, preset.PermissionTier))
		}
		if preset.Pending == nil {
			problems = append(problems, fmt.Sprintf("%s: pending 未声明（没有待装插件就写 pending: []）", where))
		}
		if len(preset.Plugins) == 0 {
			problems = append(problems, fmt.Sprintf("%s: plugins 为空", where))
		}
		presetPlugins := make(map[string]bool, len(preset.Plugins))
		for _, name := range preset.Plugins {
			if presetPlugins[name] {
				problems = append(problems, fmt.Sprintf("%s: plugins 重复 %q", where, name))
				continue
			}
			presetPlugins[name] = true
		}
		seenPending := make(map[string]bool, len(preset.Pending))
		for pendingIndex, item := range preset.Pending {
			pendingWhere := fmt.Sprintf("%s pending[%d]", where, pendingIndex)
			if item.Name != "" {
				pendingWhere = fmt.Sprintf("%s pending[%d] (%s)", where, pendingIndex, item.Name)
			}
			switch {
			case !validPluginName.MatchString(item.Name):
				problems = append(problems, fmt.Sprintf("%s: name %q 不是合法插件名（pending 里的名字就是将来落盘的那个插件名）", pendingWhere, item.Name))
			case seenPending[item.Name]:
				problems = append(problems, fmt.Sprintf("%s: pending 重复 %q", where, item.Name))
			case presetPlugins[item.Name]:
				problems = append(problems, fmt.Sprintf("%s: %q 同时在 plugins 与 pending 里", where, item.Name))
			}
			seenPending[item.Name] = true
			// 证据强度必须标死：没有 verified 就没人能区分"清单上看见"与"已核过正文"。
			if !curatedVerifiedLevels[item.Verified] {
				problems = append(problems, fmt.Sprintf(
					"%s: verified = %q 不是已知证据档（%s）——pending 必须标出「核到什么程度」，不许被读成已可用",
					pendingWhere, item.Verified, curatedVerifiedLevelNames()))
			}
			if strings.TrimSpace(item.Promote) == "" {
				problems = append(problems, fmt.Sprintf("%s: promote 缺失（一句话写清转正还缺什么）", pendingWhere))
			}
			if strings.TrimSpace(item.Upstream) == "" {
				problems = append(problems, fmt.Sprintf("%s: upstream 缺失（清单里那条候选的字面标识，一般是 owner/repo）", pendingWhere))
			}
			for _, field := range []struct{ name, value string }{
				{"source.kind", item.Source.Kind},
				{"source.url", item.Source.URL},
				{"source.license", item.Source.License},
				{"source.pinned", item.Source.Pinned},
				{"source.read_at", item.Source.ReadAt},
			} {
				if strings.TrimSpace(field.value) == "" {
					problems = append(problems, fmt.Sprintf("%s: %s 缺失（候选是谁给的必须可读）", pendingWhere, field.name))
				}
			}
		}
	}
	baseline, ok := catalog.Preset(CuratedBaselinePreset)
	if !ok {
		problems = append(problems, fmt.Sprintf("缺少 %q preset（启动基线必须有名字）", CuratedBaselinePreset))
	} else if !containsString(baseline.Plugins, "default") {
		problems = append(problems, fmt.Sprintf("%q preset 必须含 default（启动基线就是 default 插件）", CuratedBaselinePreset))
	}
	return problems
}

// curatedInstalledDrift 报"目录 ↔ 这台机器此刻的已装集合"的差异（两个方向都报，一次列全）：
// 目录里有、这台机器上没有；这台机器上有、目录里没登记；preset 引用了没有的插件；
// 已装插件没有装配路径。
//
// 同一份读数两处用：发行守卫把它当**错误**（三处必须一一对应），运行期把它当**回报**
// （本机自装插件/退役插件都是使用者的合法现场，不该变成启动警告）。
func curatedInstalledDrift(catalog CuratedCatalog, installed []string) []string {
	problems := make([]string, 0, 4)
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}

	listed := make(map[string]bool, len(catalog.Entries))
	for index, entry := range catalog.Entries {
		if entry.Name == "" {
			continue
		}
		where := fmt.Sprintf("entries[%d] (%s)", index, entry.Name)
		if listed[entry.Name] {
			continue
		}
		listed[entry.Name] = true
		if !installedSet[entry.Name] {
			problems = append(problems, fmt.Sprintf("%s: 插件目录不存在（精选目录只能列已落盘插件；未安装的放 presets[].pending）", where))
		}
	}
	// 反向：这台机器上有、目录里没登记（本机自装，或新增插件忘了登记）。
	for _, name := range installed {
		if !listed[name] {
			problems = append(problems, fmt.Sprintf("已落盘插件 %q 不在 entries 里（本机自装，或新增插件忘了登记精选目录）", name))
		}
	}

	covered := make(map[string]bool, len(installed))
	for index, preset := range catalog.Presets {
		where := fmt.Sprintf("presets[%d]", index)
		if preset.Name != "" {
			where = fmt.Sprintf("presets[%d] (%s)", index, preset.Name)
		}
		for _, name := range preset.Plugins {
			if !installedSet[name] {
				problems = append(problems, fmt.Sprintf("%s: plugins 引用了不存在的插件 %q", where, name))
				continue
			}
			covered[name] = true
		}
		for pendingIndex, item := range preset.Pending {
			if installedSet[item.Name] {
				problems = append(problems, fmt.Sprintf("%s pending[%d]: %q 其实已落盘（装上了就请登记进 entries 与 plugins）", where, pendingIndex, item.Name))
			}
		}
	}
	for _, name := range installed {
		if !covered[name] {
			problems = append(problems, fmt.Sprintf("已落盘插件 %q 不属于任何 preset（没有装配路径）", name))
		}
	}
	return problems
}
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
