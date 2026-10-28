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

| 文件 | 主要函数 |
|---|---|
| `flags.go` | `BindFlags`：注册并返回启动参数。 |
| `modes.go` | `ParseFrontendMode`、`ParsePermissionMode`：归一化并校验模式。 |
| `paths.go` | `RuntimeLimitsPath`、`AccountsPath`、`RuntimeConfigChain`、`GUIAppearancePath`、`EnsureConfigFile`、`PromptDirChain`、`PromptDirChainAt`、`ResolvePromptDir`、`ResolvePromptDirAt`、`ResolveAutoGetJobsDir`、`ResolveToolDir`、`ResolvePythonCommand`、`ResolveStorePath`。 |
| `plugins.go` | `PluginRootChain`、`PluginRoots`、`DedupRoots`、`RequirePlugins`、`PluginRootReport`、`LogPluginRoots`、`ResolveCuratedRead`、`CuratedAssemblyJudge`、`SplitPaths`。 |
| `deepseek_key.go` | `DeepSeekSearchKey`、`deepSeekSearchKeyFrom`、`accountRoleOrder`、`accountIsDeepSeek`。 |
