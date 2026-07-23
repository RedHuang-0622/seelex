# Seelex 预发行 P0 前置审查报告

## 需求摘要

修复阻碍 Seelex 发布开发者 Alpha 的 P0 问题，并新增与 `tui/` 同级的 `gui/`：避免账户密钥进入发行包、收紧默认权限、统一版本来源、补齐可公开分发的基础元数据与 CI 门禁，让干净仓库具备可理解的首次配置入口，同时提供复用同一 `application.Service` 的桌面 GUI。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `config/accounts.example.yaml` | 新增 | 全文件 | 提供不含密钥、可跟踪的最小配置模板 |
| `.gitignore` | 修改 | `config/` 规则 | 保留真实账户配置忽略规则，同时明确允许示例配置 |
| `scripts/build.ps1` | 修改 | 运行时文件复制 | 只复制示例配置，禁止复制本地密钥文件 |
| `scripts/build.sh` | 修改 | 运行时文件复制 | 与 PowerShell 发布行为保持一致 |
| `Makefile` | 修改 | `package` 目标 | 与两个构建脚本保持一致，避免整目录复制 |
| `main.go` | 修改 | CLI flags、权限装配 | 默认使用 `manual`；非法权限模式启动失败；增加版本输出入口 |
| `version.go` | 修改 | 版本变量 | 建立可由 `-ldflags -X` 注入的单一版本来源 |
| `main_test.go` 或现有主包测试 | 修改/新增 | 版本与权限解析测试 | 覆盖默认值和非法输入等发布关键行为 |
| `smoke_test.go` | 修改 | 外部 LLM Smoke Test | 显式要求 opt-in，避免“缺配置即静默跳过”被误认为发布验证通过 |
| `.github/workflows/ci.yml` | 修改 | CI steps | 增加 gofmt、示例配置校验、打包安全检查和覆盖率摘要；保留 Linux race |
| `.github/workflows/release.yml` | 新增 | 全文件 | tag 触发跨平台构建、校验和与 GitHub Release |
| `LICENSE` | 新增 | 全文件 | 明确公开分发授权；建议 MIT，需用户确认 |
| `CHANGELOG.md` | 新增 | 全文件 | 记录 Alpha 范围、已知限制和安全默认值 |
| `README.md` | 修改 | 安装、版本、已知问题、发布说明 | 与代码和真实能力同步，移除失实的“已修复/已验证”描述 |
| `docs/feature-instrumentation.md` | 修改 | 版本与质量状态 | 同步协议、权限、发布门禁的真实状态 |
| `gui/` | 新增 | Go bridge、嵌入资源、前端源码 | 新增与 TUI 同级的桌面 GUI 适配层，不复制业务状态机 |
| `main.go` | 修改 | 前端选择与生命周期 | 支持在完成同一套依赖装配后选择 TUI 或 GUI |
| `go.mod` / `go.sum` | 修改 | GUI 依赖 | 引入桌面 WebView 容器所需依赖 |

## 依赖分析

- 上游依赖：Seele `permission` middleware、Go linker `-X`、GitHub Actions。
- 下游影响：CLI 首次启动、所有发布压缩包、README 安装流程、TUI 中的工具审批行为。
- 兼容性：默认权限从 `full_access` 改为 `manual` 是有意的安全行为变化；仍可通过 `-permission full_access` 显式恢复旧行为。
- 发布包配置：不再携带开发机账户配置。用户首次运行前必须从示例复制并填写 `config/accounts.yaml`。
- GUI 依赖方向：`gui → application`，与现有 `tui → application` 平行；`application` 不反向依赖任何界面包。

## 循环依赖检查

- [x] 变更集中在入口、脚本、配置与 CI，不新增 Go package 依赖。
- [x] 版本信息保留在 `main` 包，不引入反向依赖。
- [x] `tui` 与 `gui` 互不依赖，共同只消费 `application` API。

## 风险预估

- 默认权限变更：中概率、中影响；旧用户会多看到审批，README 和 CHANGELOG 必须明确。
- 发布脚本差异：低概率、高影响；需要在临时干净目录检查压缩包文件清单和密钥排除。
- GitHub Release workflow：中概率、中影响；本地无法完全模拟 GitHub token 权限，需要确保 workflow 使用标准 action 和最小权限。
- LICENSE 选择：低概率、高影响；属于法律授权决定，不能在未确认时擅自确定。
- 版本号：中概率、中影响；建议从当前文档自称的 `v0.0.4` 迁移为明确的 `v0.1.0-alpha.1`。
- GUI 容器：中概率、中影响；系统 WebView 在不同平台存在差异，需要保留 TUI 作为可靠降级入口。

## 建议方案

1. 建立 `accounts.example.yaml`，发布脚本只复制该模板，不生成可能被误用的真实配置文件，同时加入压缩包文件白名单检查。
2. 将权限默认值改为 `manual`，把模式解析提取成可测试函数，未知值拒绝启动。
3. 使用 `var Version = "dev"` 作为唯一版本源，支持 `-version`，发布 workflow 通过 tag 注入。
4. CI 增加 `gofmt` 门禁；先统一现有 Go 源码格式，避免门禁一上线即失败。
5. 添加 tag 驱动的跨平台 Release、SHA256 校验和、CHANGELOG 和许可证。
6. 将真实 LLM Smoke Test 改为显式环境变量 opt-in；常规 CI 依赖 mock/单元测试，不依赖外部账户。
7. 发布版本标记为 Alpha，并在已知限制中明确会话恢复、专业 Plugin E2E 和 GUI 尚未完成。

## 待确认

- 许可证：建议 MIT。
- 首个公开预发行版本：建议 `v0.1.0-alpha.1`。
