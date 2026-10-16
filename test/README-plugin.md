# test/plugin（集成用例分卷）

## 生态位

插件与精选目录契约：目录登记、根解析、自省工具、技能激活、工具声明分封。

覆盖：`plugin_*.go`、`dev_plugin_*.go`、`curated_*.go`、`skill_activate_tool*.go`、`tool_meta_declaration*.go`、`tool_face_prompt*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### curated_catalog_test.go

- `func TestShippedCuratedCatalogMatchesInstalledPlugins(t *testing.T)`
- `func TestPluginsReadmeIndexMatchesCuratedCatalog(t *testing.T)` — TestPluginsReadmeIndexMatchesCuratedCatalog 把 README 的索引表钉在目录上：
- `func readmeIndexNames(t *testing.T, readme string) []string` — readmeIndexNames 解析 `\| Plugin \| 生态位 \| README \|` 表的第一列（反引号里的名字）。
- `func containsValue(values []string, want string) bool`

### dev_plugin_payload_test.go

- `func nonCommentLines(text string) string` — nonCommentLines 去掉整行注释（注释里会引用那条坏写法当反例，不该被判成回归）。
- `func TestDevPluginPayloadRefreshIsWired(t *testing.T)`

### plugin_catalog_registration_test.go

- `func catalogHarness(t *testing.T, pre func(root string)) (string, func(name, args string) (string, error))` — catalogHarness 起一条真链路：临时插件根（seed 插件 + 上面那份 curated.yaml）+ 真
- `func TestPluginsReloadRegistersCreatedPluginInCatalog(t *testing.T)` — TestPluginsReloadRegistersCreatedPluginInCatalog 是用户现场那条链路的正面回归：
- `func TestStartupRegistersPluginAddedOutOfBand(t *testing.T)` — TestStartupRegistersPluginAddedOutOfBand 钉住另一半：不是 plugin_create 造的（手工放置、
- `func catalogHasString(values []string, want string) bool`

### plugin_curated_runtime_test.go

- `func TestResolveCuratedReadReadsFromResolvedRootsAndJudges(t *testing.T)`
- `func TestResolveCuratedReadMissIsExplicitButNotAStartupWarning(t *testing.T)` — TestResolveCuratedReadMissIsExplicitButNotAStartupWarning 钉住 2026-10-05 重排后的口径：
- `func TestResolveCuratedReadToleratesLocalPluginDrift(t *testing.T)` — TestResolveCuratedReadToleratesLocalPluginDrift 钉住"使用者自己改插件不该被自己的 app 警告"：
- `func installedNames(plugins []plugin.Plugin) []string` — installedNames 把加载出来的插件渲染成名字清单（与 config.ResolveCuratedRead 内部同一口径）。
- `func TestPluginRootReadingIsSharedAndReachesTheUIFace(t *testing.T)` — TestPluginRootReadingIsSharedAndReachesTheUIFace 钉住 B 的两半：
- `func curatedFixtureYAML(installed ...string) string`
- `func curatedFixtureYAMLWithRoadmap(installed []string, roadmap string) string` — curatedFixtureYAMLWithRoadmap 生成一份**合法**的精选目录：entries 与 installed 一一对应，
- `func writeCuratedRootPlugin(t *testing.T, root, name string)` — writeCuratedRootPlugin 在根下造一个真实可加载的插件（manifest 三件套：目录 + plugin.md）。
- `func writeCuratedRootFile(t *testing.T, root, body string)`
- `func loadCuratedRootPlugins(t *testing.T, roots ...string) []plugin.Plugin` — loadCuratedRootPlugins 用加载器（生产同一条路）读出这些根下真实可加载的插件。
- `func errorString(text string) error` — errorString 把一段文本包成 error，复用 curatedWantError 的断言口径（文案必须逐个包含）。
- `func (e stringError) Error() string`
- `func curatedWantError(t *testing.T, name string, err error, want ...string)` — curatedWantError 断言一段文案（不是只看 err != nil）：每个子串都必须出现——拒绝必须

### plugin_doc_sigil_test.go

- `func TestPluginDocsDoNotUsePluginSigilForSkills(t *testing.T)`
- `func pluginSkillNames(t *testing.T, pluginDir string) []string` — pluginSkillNames 返回插件目录下的 Skill 名（= 含 SKILL.md 的子目录名）。

### plugin_roots_test.go

- `func TestPluginRootChainOrderAndDedup(t *testing.T)`
- `func TestPluginRootChainWithoutFlagFallsBackToExeThenCwd(t *testing.T)`
- `func TestPluginRootsUsesFlagThenEnvThenCwd(t *testing.T)` — TestPluginRootsUsesFlagThenEnvThenExe 是真实环境下的链序（旗标 / $SEELEX_PLUGINS /
- `func TestRequirePluginsIsExplicitAboutZeroPlugins(t *testing.T)`
- `func TestPluginRootChainIsTheOnlySourceOfLoaderRoots(t *testing.T)` — TestPluginRootChainIsTheOnlySourceOfLoaderRoots 钉住"根解析只有一条路"：全仓生产代码里
- `func indexOf(values []string, want string) int`

### plugin_self_tools_test.go

- `func TestPluginSelfTools(t *testing.T)` — TestPluginSelfTools 验证内置自迭代工具族（plugin_create / plugins_reload /

### skill_activate_tool_test.go

- `func newSkillToolHarness(t *testing.T) (*seelebridge.Runtime, *skill.Registry, *[]string)` — newSkillToolHarness 构造 skill_activate 工具测试台：真实 skill.Registry +
- `func TestSkillActivateToolCandidatesFollowPluginSwitch(t *testing.T)` — TestSkillActivateToolCandidatesFollowPluginSwitch：候选技能列表随插件切换
- `func TestSkillActivateToolValidation(t *testing.T)` — TestSkillActivateToolValidation：空名报错；json 可解析。
- `func TestSkillActivateToolActivateErrorSurfaced(t *testing.T)` — TestSkillActivateToolActivateErrorSurfaced：activate 返回错误 → status=error

### tool_face_prompt_consistency_test.go

- `func TestPromptToolFacesMatchTheRegistrationSurface(t *testing.T)`
- `func hasRetirementMarker(line string) bool`

### tool_meta_declaration_test.go

- `func TestEveryRegisteredToolDeclaresGroups(t *testing.T)` — TestEveryRegisteredToolDeclaresGroups 是打点 K-0 的判据（TC-K0-1）：
- `func scanToolRegistrations(t *testing.T, root string) (map[string]string, []string, int)` — scanToolRegistrations 扫描仓库源码里的工具注册点，返回
- `func constStringsOf(file *ast.File) map[string]string` — constStringsOf 收集文件里的包级字符串常量（`Name = "value"`），供解析
- `func registrationAliases(file *ast.File) map[string]bool` — registrationAliases 收集"注册面的局部别名"：任何被赋值为 `<…>.RegisterTool`
- `func containsRegisterToolSelector(expr ast.Expr) bool`
- `func isRegistrationCall(call *ast.CallExpr, aliases map[string]bool) bool`
- `func literalOrConst(expr ast.Expr, consts map[string]string) (string, bool)` — literalOrConst 解析注册点的第一个实参：字面量、包内常量、或 `包.常量`。
- `func TestRegistrationScannerSeesEveryFamily(t *testing.T)` — TestRegistrationScannerSeesEveryFamily 钉住扫描器本身：漏掉一族工具就等于
