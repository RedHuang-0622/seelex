# wiring/config

## 生态位

`wiring/config` 是**启动参数与配置/路径责任链**的唯一实现处。它回答四个问题：

- 这份配置/目录**该读哪一份**（责任链：CWD 的 `config/` → CWD 根目录 → `<exe>/config/`）；
- 找不到时**该把内嵌默认档初始化到哪里**（落盘根固定为 `<exe>/config`，绝不在用户项目目录里
  凭空造目录）；
- 命令行参数**叫什么、默认值是什么**（`Flags` + `BindFlags`）；
- 数据根**落在哪个路径**（多实例避让后缀）与插件根**从哪些根加载**（多根 first-wins）。

主要调用方：根组合根（`main.go`）与它后面的 `wiring/bootstrap`、`wiring/registry`；测试基座
（`test/`）与 `e2e/` 也直接消费这里的判据。它同时是"第二个入口"（`cmd/<变体>`）能复用同一套
启动参数与配置口径的前提——新入口 `BindFlags` 一次即可。

## 职责与非职责

**做**：启动参数绑定；配置文件/提示词目录/工具目录/插件根/数据根的责任链解析；缺失即初始化；
前端模式与权限档位的**词法**解析；插件根读数与精选目录读数的生成（`PluginRootReport` /
`ResolveCuratedRead`）。

**不做**：装配 Runtime/Application（属于 `wiring/bootstrap`）；注册工具与能力（属于
`wiring/registry`）；启动任何前端；解释配置的**业务语义**——`limits` 的默认值与校验属于
`kernel/limit`，MCP/搜索/权限段各自的解析属于 `mcpstack/config`、`backend/tool/websearch`、
`backend/limit/gate`；也不持有任何进程级状态。

## 文件结构

| 文件 | 职责 |
|---|---|
| `flags.go` | `Flags` + `BindFlags`：启动参数的唯一登记处（名字、默认值、说明文案即对外契约）。 |
| `paths.go` | 配置/目录责任链、缺失即初始化、数据根锁路径避让。 |
| `plugins.go` | 插件根责任链、去重、零插件显式报错、根读数、精选目录读数。 |
| `modes.go` | `-frontend` / `-permission` 的词法解析（档位 id 的权威词表在 `contract/dto`）。 |
| `config_unit_test.go` | 本包自己的判据（数据根避让 + "进程存活判据只有一份实现"的门禁）。 |

## 核心实现

- **责任链三条口径**：① 存在即读（链上第一份存在的文件/目录就是答案，一个字节都不写）；
  ② 缺失即初始化（全缺 → 用内嵌默认档在 `<exe>/config` 落盘，再读它）；③ 落盘根固定
  `<exe>/config`（CWD 可能是别人的项目目录）。三条由 `RuntimeConfigChain` / `EnsureConfigFile`
  与目录版 `PromptDirChain` / `ResolvePromptDir` 成对实现，语义一致。
- **插件根链**：`-plugins` → `$SEELEX_PLUGINS` → `<exe>/plugins` → `<exe>/../plugins` →
  `plugins`(CWD)，去重保序（Windows 大小写不敏感）。**语义是顺序覆盖而非"择一"**：加载器本身
  多根 first-wins，显式旗标因此能覆盖发行包里的同名插件。
- **零插件是显式失败**：`RequirePlugins` 在链上一个都没加载到时报错并列出试过的根——静默降级
  会让"模型拿不到领域纪律"看起来像模型不听话。
- **数据根避让**：`ResolveStorePath` 用 `.lock` 文件里的 PID 判断占用；进程存活判据**复用**
  `store/root.ProcessAlive`，本包不实现第二份（Windows 上"进程已终结但句柄未释放"必须判死，
  否则数据根锁永远判不陈旧）。
- **进程存活判据只有一份**：Windows 侧要问内核（`WaitForSingleObject`），unix 侧是
  `signal 0` + `EPERM` 保守判活；两处都在 `store/root`。

## 数据流

启动参数（`flag`）与环境变量进入本包 → 责任链解析出**路径/读数** → 交给 `wiring/bootstrap`
（装配）、`wiring/registry`（工具注册）与 `plugin`（加载器根）。本包不回调上游：它只把
"该读哪一份"算出来，读与写由调用方决定。

## 依赖方向

允许：标准库、`backend/limit/overlay`（覆盖层路径拼接口径）、`internal/bootseed`（缺失即初始化）、
`kernel/prompt/assets`（提示词资产清单）、`internal/buildinfo`（默认前端）、`plugin`、
`contract/dto`（档位词表）、`store/root`（进程存活判据）、`Seele/tools/permission`（档位别名）。

禁止：`application/*`、`backend/engine/*`、`gui`、`tui`、`wiring/bootstrap`、`wiring/registry`
（都是反向依赖）。新增 import 超出上面这张表即越界。

## 错误语义

- 责任链全缺且无处落盘：返回链上第一候选 / 空串，**让调用方回退代码默认值**，不阻断启动；
  只有"读到的文件解析失败"才是错误（由拥有该段的包报出）。
- 零插件：`RequirePlugins` 返回错误，文案点名试过的根与两个显式指定方式。
- 精选目录缺失不算配置缺陷（`CuratedRead.Absent`），但错误非 nil，装配面据此显式拒绝。
- 数据根锁被活进程持有：不报错，改为避让后缀（`sessions` → `sessions_1` → …）。

## 扩展方式

新增启动参数：加字段 + `BindFlags` 注册 + 说明文案 + 契约用例（默认值即安全基线）+ 本 README。
新增一种"该读哪一份"的配置：复用 `RuntimeConfigChain`/`PromptDirChain` 与
`internal/bootseed.Resolve`，**不要**在调用方再拼一次 `<exe>/config`——那正是本包存在的理由。
`BindFlags` 的 `FlagSet` 参数让第二个入口（如 `-frontend backend` 的专用二进制）只绑它需要的
那几格。

## Review 指南

- 是否出现**第二份责任链**：任何非本包代码拼接 `<exe>/config` 或直接 `os.Executable()` 决定
  路径，都是漂移。
- 是否出现**第二份进程存活判据**：本包必须只用 `store/root.ProcessAlive`。
- flag 名字/默认值/文案改动是否同步了 README、`release_test.go` 的安全基线契约与 shell 脚本。
- 落盘根是否可能落到用户项目目录（必须 `<exe>/config`）。
- 精选目录读数的"差异不是错误"口径是否被改回去（使用者自装插件不该被自己的 app 警告）。

## 测试与验证

```text
go test ./wiring/config/ -count=1
gofmt -l wiring/config
go list -deps ./wiring/config
```

关键用例：本包 `config_unit_test.go`（数据根避让/复用陈旧锁 + 进程存活判据只有一份实现）；
仓库级用例（M5-S4 已归位 `test/`）：`boot_seed_test.go`（缺失即初始化与
"没有 main.py 就不登记"）、`boot_prompt_test.go`（提示词目录链三代口径）、
`plugin_roots_test.go`（根链顺序/去重/零插件报错/调用点唯一）、
`plugin_curated_runtime_test.go`（精选目录读数与 first-wins）、`release_test.go`
（`-permission` 默认 manual、`-frontend` 默认 tui、版本号必须是 dev）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### config_unit_test.go

- `func TestResolveStorePathReusesStaleLock(t *testing.T)` — TestResolveStorePathReusesStaleLock 钉住"陈旧锁不该继续避让"：`.lock` 里的 PID 已经
- `func TestResolveStorePathBumpsWhileLockIsHeld(t *testing.T)` — TestResolveStorePathBumpsWhileLockIsHeld 钉住避让的**有界**形态：同一个基路径连续认领
- `func TestProcessLivenessHasOneImplementation(t *testing.T)` — TestProcessLivenessHasOneImplementation 是"一份判据 = 一处函数"的机械门禁：
- `func codeOnly(text string) string` — codeOnly 去掉行注释与块注释，只留可编译的那部分源码。
- `func repoRoot(t *testing.T) string` — repoRoot 向上找到 go.mod（与 e2e 的 testutil.RepoRoot 同一判据，这里只本包自用）。

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

