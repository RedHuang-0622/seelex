# test/release（集成用例分卷）

## 生态位

发布与交付契约：版本号、默认档位、构建布局、图标资源、快照校验与启动种子。

覆盖：`release_*.go`、`build_layout*.go`、`icon_resource*.go`、`boot_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### boot_prompt_test.go

- `func TestPromptDirChainShape(t *testing.T)`
- `func TestResolvePromptDirSeedsDefaultsThenReadsThem(t *testing.T)`
- `func TestResolvePromptDirPrefersCWDCopy(t *testing.T)`
- `func TestResolvePromptDirMissingWithoutSeedRoot(t *testing.T)`

### boot_seed_test.go

- `func executableDir(t *testing.T) string` — executableDir 返回当前测试二进制的目录（等价于"包自己的目录"）：
- `func TestEnsureConfigFileSeedsDefaultThenReadsIt(t *testing.T)`
- `func TestEnsureConfigFilePrefersCWDConfig(t *testing.T)`
- `func TestEnsureConfigFileKeepsLegacyRootFallback(t *testing.T)`
- `func TestResolveAutoGetJobsDirFollowsChain(t *testing.T)` — TestResolveToolDirFollowsChain：仓库里放着脚本时命中它；没放时（CI 等
- `func TestResolveToolDirSeedsSkeletonWithoutRegistering(t *testing.T)` — TestResolveToolDirSeedsSkeletonWithoutRegistering：工具目录两处都没有时初始化
- `func TestResolveToolDirPrefersCWDCopy(t *testing.T)`

### build_layout_test.go

- `func readRepoFile(t *testing.T, rel string) string`
- `func TestBuildLayoutSingleSource(t *testing.T)`
- `func TestBrandLogoShippedPerDeliveryFolder(t *testing.T)` — TestBrandLogoShippedPerDeliveryFolder 钉住"品牌图每个交付文件夹各存一份"。
- `func TestNoLegacyBuildDirsInSources(t *testing.T)`

### icon_resource_test.go

- `func TestWindowsIconResourcePipeline(t *testing.T)`

### release_test.go

- `func TestParseFrontendMode(t *testing.T)`
- `func TestParsePermissionMode(t *testing.T)`
- `func TestReleaseVersionIsNotStale(t *testing.T)`
- `func TestSafeFlagDefaults(t *testing.T)`
- `func TestDefaultManualRulesAllowSafeControlPlaneTools(t *testing.T)`
- `func (runtime *permissionRuntimeRecorder) SetPermissionConfig( config toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler, )`
- `func (runtime *permissionRuntimeRecorder) SetFullAccess(on bool)`
- `func (runtime *permissionRuntimeRecorder) SetPermissionTierFor(_ string, tier string) error`
- `func TestFullAccessStartupKeepsManualPermissionBaseline(t *testing.T)`
