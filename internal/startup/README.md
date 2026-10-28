# Startup：启动参数与路径责任链

## 生态位

`internal/startup` 是所有入口共用的启动参数、模式解析、配置路径与 Plugin 根解析
模块。它服务 `main.go` 和 `boot`，保证第二个入口不会重新定义 flag、默认值或
文件查找顺序。

## 职责与非职责

职责：

- 定义 `Flags` 与 `BindFlags`，固化 CLI 名称、默认值和说明文案。
- 解析前端与权限档位。
- 解析数据根、配置文件、提示词、GUI 外观、工具目录和 Plugin 根责任链。
- 从账号档提取搜索可复用的 DeepSeek key。
- 生成 Plugin 根读数，并对零插件启动给出可行动错误。

非职责：

- 不构造 Runtime、Store、Application 或前端。
- 不实现会话、工具、权限、插件或存储业务规则。
- 不读取或输出真实账号内容；只按显式路径提取最小必要字段。

## 文件结构

| 文件 | 职责 |
|---|---|
| `flags.go` | `Flags` 与 `BindFlags`，启动参数对外契约。 |
| `modes.go` | 前端模式和权限档位归一化。 |
| `paths.go` | 配置、提示词、工具、数据根和运行参数路径责任链。 |
| `plugins.go` | Plugin 根链、去重、零插件闸门、根读数与精选目录判定。 |
| `deepseek_key.go` | 从账号档确定性地取 DeepSeek key。 |

## 核心实现

- 默认值即安全基线：权限默认 `manual`，前端默认来自构建期 `buildinfo`。
- 配置口径统一为“存在即读、缺失即初始化”：候选链第一份命中文件生效，落盘点
  固定在二进制旁边的应用目录，不往用户项目里凭空造配置。
- Plugin 根顺序为：显式 flag、环境变量、可执行文件旁、上一级、CWD；同名插件
  first-wins。
- `PluginRootReport` 是终端日志与 UI 通知共用的唯一读数。
- 路径变更必须保持现有用户数据兼容，不得隐式移动或清理 `.seelex`、`config`。

## 数据流与生命周期

```text
命令行
  -> BindFlags / flag.Parse
  -> ParseFrontendMode / ParsePermissionMode
  -> ResolveStorePath / config + prompt path chains
  -> boot.Run
```

路径解析在进程启动时完成；运行期不重新枚举配置后端，也不改变数据根。

## 依赖方向

- 允许依赖标准库、`internal/buildinfo`、`internal/bootseed`、
  `backend/engine/plugin`、`backend/limit/overlay`、`kernel/prompt/assets`、
  `store/root` 与零依赖 DTO。
- 不得依赖 `boot`、Application、GUI、TUI 或具体 Runtime 实现。

## 并发、存储与安全语义

- 本包无长期后台 goroutine。
- 配置初始化采用上层 `bootseed` 的原子写入语义。
- 不记录 API key、密码、DSN；账号档读取只返回调用方需要的最小字段。
- 路径拼接不得逃逸调用方指定的项目根；插件根和配置根必须去空、去重、保序。

## 扩展方式

新增 flag 只改 `Flags`、`BindFlags` 与对应契约测试；新增配置/提示词目录复用
`RuntimeConfigChain` 或 `bootseed`，不要另写文件名判定；新增 Plugin 查找位置只改
`PluginRootChain`，并同步 `boot`、README 与发行目录测试。

## Review 指南

- flag 默认值是否改变安全基线或发布契约。
- 配置读、写是否仍落在同一份命中路径。
- Windows 大小写路径是否去重。
- 是否泄露账号秘密或把搜索策略写回账号档。
- 多进程兼容逻辑是否与 `store/root` 唯一数据根锁重复或冲突。

## 测试与验证

```text
go test ./internal/startup -count=1
go test ./test/ -run 'SafeFlagDefaults|ParseFrontendMode|ParsePermissionMode|PluginRoot' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### deepseek_key.go

- `func DeepSeekSearchKey() string` — DeepSeekSearchKey 返回账号档里第一把 DeepSeek 密钥；取不到返回空串。
- `func deepSeekSearchKeyFrom(path string) string` — deepSeekSearchKeyFrom 从指定账号档取 DeepSeek 密钥（测试经此注入路径）。
- `func accountRoleOrder(roles map[string][]accountEntry) []string` — accountRoleOrder 返回角色名的确定顺序：偏好角色在前，其余按字典序。
- `func accountIsDeepSeek(account accountEntry, defaultsDeepSeek bool) bool` — accountIsDeepSeek 报告一条账号是不是 DeepSeek。

### flags.go

- `func BindFlags(fs *flag.FlagSet) *Flags` — BindFlags 把启动参数注册到 fs 并返回读写句柄；flag 名、默认值与说明文案是

### modes.go

- `func ParseFrontendMode(value string) (string, error)`
- `func ParsePermissionMode(value string) (string, error)` — ParsePermissionMode 解析 -permission 标志为**权限档位 id**（manual/edit/auto/

### paths.go

- `func RuntimeLimitsPath(storePath string) string` — RuntimeLimitsPath 把「运行时上限覆盖层」的落点绑到数据根：数据根来自 `-store`
- `func AccountsPath() string` — AccountsPath 返回 accounts.yaml 的路径。
- `func RuntimeConfigChain(name string) (candidates []string, seedRoot string)` — RuntimeConfigChain 返回配置文件在责任链上的候选（按优先级）与允许初始化的落盘根：
- `func GUIAppearancePath() string` — GUIAppearancePath 用同一套配置责任链给出 **GUI 外观设置**（当前皮肤 + 窗口材质）的
- `func EnsureConfigFile(name string, pack bootseed.Pack) string` — EnsureConfigFile 按责任链给出配置文件路径（口径见 internal/bootseed）：
- `func PromptDirChain() (candidates []string, seedRoot string)` — PromptDirChain 返回提示词资产目录在责任链上的候选与落盘根（口径同
- `func PromptDirChainAt(exePath string) (candidates []string, seedRoot string)` — PromptDirChainAt 是 PromptDirChain 的可测内核（可执行文件路径由调用方给，
- `func ResolvePromptDir() string` — ResolvePromptDir 按责任链给出**提示词资产目录**并返回它（调用方拿它设
- `func ResolvePromptDirAt(candidates []string, seedRoot string) string` — ResolvePromptDirAt 是 ResolvePromptDir 的可测内核（候选链与落盘根由调用方给）。
- `func ResolveAutoGetJobsDir() (string, bool)` — ResolveAutoGetJobsDir 定位 auto_get_jobs 脚本目录（责任链 + 缺失即初始化，
- `func ResolveToolDir(relative, exeDir string, pack bootseed.Pack) (string, bool)` — ResolveToolDir 是 ResolveAutoGetJobsDir 的可测内核（exeDir 由调用方给，测试才好
- `func ResolvePythonCommand() string` — ResolvePythonCommand 探测可用的 python 解释器（python → py；找不到时返回空）。
- `func ResolveStorePath(basePath string) string` — ResolveStorePath 确保多实例不冲突：检测 .lock 文件中的 PID，
- `func tryAcquireLock(lockFile string) bool` — tryAcquireLock 尝试创建锁文件并写入当前 PID。返回 true 表示获取成功。

### plugins.go

- `func PluginRootChain(flagValue, envValue, exePath string) []string` — PluginRootChain 返回 Plugin 根在责任链上的候选（按优先级、去重保序）：
- `func PluginRoots(flagValue string) []string` — PluginRoots 用真实环境（旗标 / SEELEX_PLUGINS / 可执行文件位置）解析责任链。
- `func DedupRoots(roots []string) []string` — DedupRoots 去空、去重（保序）。Windows 上路径大小写不敏感，同一份目录可能以两种
- `func RequirePlugins(roots []string, loaded []plugin.Plugin) error` — RequirePlugins 把"零插件"从静默降级变成一次显式失败。
- `func PluginRootReport(roots []string, loaded []plugin.Plugin) string` — PluginRootReport 生成"插件到底从哪来"的**一条读数**：责任链上每个根各供上了几个
- `func LogPluginRoots(roots []string, loaded []plugin.Plugin) string` — LogPluginRoots 把根读数写成启动期一行终端日志。**它不再是唯一出口**：同一条读数由
- `func ResolveCuratedRead(roots []string, loaded []plugin.Plugin) plugin.CuratedRead` — ResolveCuratedRead 从**已解析的插件根**（责任链，多根 first-wins）读一次精选目录。
- `func CuratedAssemblyJudge(read plugin.CuratedRead) func(name string, installed []string) error` — CuratedAssemblyJudge 把精选目录读数包成装配面的判决函数（"未定义的名字怎么判/怎么说"）。
- `func SplitPaths(value string) []string`
