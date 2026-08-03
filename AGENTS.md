# Seelex repository instructions

本文件是仓库内 Agent、维护者和自动化工具的文档与代码协作规范。它作用于整个仓库；若子目录以后增加更具体的 `AGENTS.md`，子目录规则优先，但不得降低这里的安全、测试和文档要求。

## 1. 事实来源与依赖边界

- `application/` 是前端共享的应用层；`tui/` 和 `gui/` 只能通过 Application API 消费状态与提交操作，不复制业务状态机。
- `seelebridge/` 是 Seelex 到远程 Go module `github.com/RedHuang-0622/Seele v0.1.1` 的兼容与能力适配层；优先复用 Seele，不在 Seelex 重造引擎能力。
- `plugin/` 管理 Plugin 生命周期，`plugins/` 存放声明式能力包；`skill/` 负责 Skill 加载与可见性。
- `session/` 是会话用例适配器，`sessionstore/` 是原子、项目作用域的持久化实现，`workspace/` 管理项目及 session binding。
- `main.go` 和根目录适配器只负责装配。业务规则应进入拥有该规则的模块。
- 当前实现、规划设计和历史研发记录必须明确区分。代码与测试是“已实现”能力的最终事实来源。

## 2. 文档放置规范

| 文档类型 | 放置位置 | 规则 |
|---|---|---|
| 模块说明 | `<module>/README.md` | 与代码同目录，描述当前实现和生态位；代码行为变化时同步更新。 |
| 仓库入口 | `/README.md` | 产品定位、快速开始和模块导航，不堆叠模块内部实现。 |
| 稳定架构 | `docs/arch/` | 跨模块边界、长期设计原则、已接受架构决策。 |
| GUI 权威设计 | `docs/gui/` | GUI 协议、Schema、模块登记、API、recipes 和 ADR。 |
| 产品规格 | `docs/product/` | PRD、里程碑、验收指标；必须标识实现状态。 |
| 调研报告 | `docs/research/` | 外部方案比较、选型依据、来源与结论。 |
| 测试资料 | `docs/test/` | 测试策略、基线、手工验证记录。 |
| 一次性工作包 | `docs/YYYY-MM-DD-topic/` | front-review、plan、实现记录和阶段性验收；完成后不冒充长期事实来源。 |
| 研发日志 | `docs/devlog/` | 按时间沉淀的变更与 review 记录。 |
| Plugin 契约 | `plugins/<name>/plugin.md` | YAML front matter 是机器契约；同目录 README 解释生态位和维护方法。 |
| Skill 契约 | `plugins/<plugin>/<skill>/SKILL.md` | 指令正文和资源引用；资源放在同一 Skill 目录，不跨根引用。 |

禁止在仓库根目录新增临时方案、截图说明或散落的 review 文档。若文档不属于上表，应先判断它是模块事实、长期架构、产品规格、调研还是一次性工作包，再选择位置。

## 3. 模块 README 必备内容

每个 Go package 和可独立维护的运行模块都必须有 `README.md`，至少覆盖以下信息；简单声明模块可以合并章节，但不得省略定位、边界、Review 和验证：

1. 模块定位：它在 Seelex 生态中的位置，以及主要调用方。
2. 职责与非职责：明确做什么、刻意不做什么。
3. 目录或文件结构：关键文件及其职责。
4. 核心实现：主要类型、接口、状态和算法。
5. 数据流或生命周期：输入如何到达、状态如何变化、输出给谁。
6. 依赖方向：允许依赖和禁止反向依赖。
7. 并发、存储、安全或错误语义：只写与模块相关的约束。
8. 扩展方式：新增实现时应修改的位置和兼容要求。
9. Review 指南：最容易出错的边界与审查问题。
10. 测试与验证：最小测试命令及关键测试文件。

README 描述当前代码，不把规划写成既成事实。未来能力必须使用“规划”“尚未实现”或等价状态标识，并链接到对应设计文档。

## 4. 写作和链接规则

- 默认使用中文解释设计，代码标识、命令、协议字段和文件名保持原文。
- 先写结论和边界，再写实现细节；避免把源码逐行翻译成文档。
- 使用相对链接；链接目标必须存在。不要使用本机绝对路径。
- 示例必须可复制，但不得出现真实 API key、token、password、DSN 或个人目录。
- 配置示例只引用 `config/accounts.example.yaml`。不得读取、打印或提交 `config/accounts.yaml` 和 `*.local.yaml` 的内容；唯一允许的复制是本地 GUI 构建脚本按显式路径把配置作为不透明文件写入 ignored `dist/` 产物。公开 release、源码、日志和文档不得包含真实配置。
- 变更接口、JSON 字段、Schema、环境变量、CLI flag 或持久化格式时，必须同步更新对应 README、示例和测试。
- 删除或移动模块时，同时修正上级 README、文档索引和 contract tests。

## 5. 代码与文档变更流程

1. 阅读目标模块 README、相关测试和调用方。
2. 确认变更属于哪个模块，不把业务规则塞进 composition root 或 frontend。
3. 先更新契约和测试，再实现；纯文档变更也要检查链接与事实一致性。
4. 更新目标模块 README 的实现、数据流、review 风险或验证命令。
5. 运行与风险匹配的局部测试，再运行仓库 CI 对应检查。

常用验证：

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=120s
node --test gui/frontend/dist/*.test.mjs
```

Linux CI 另外运行 `-race -covermode=atomic -coverpkg=./...`。Windows 本地若 `CGO_ENABLED=0`，不得声称 race 已执行。

可分发构建必须遵循 clean → build 顺序：跨平台公开发布使用 `make release`；Windows Dev GUI 使用 `make rebuild-gui VERSION=<tag>`，并且必须通过 `LOCAL_CONFIG` 把真实账号配置不透明复制进本地产物；Windows Publish GUI 使用 `make publish-rebuild-gui VERSION=<tag>`，只允许包含 example。clean 只能作用于仓库内 `dist/`，不得把未校验变量或仓库根作为递归删除目标。

## 6. Review 清单

- 模块边界是否仍为单向依赖，frontend 是否只消费 Application DTO/Event。
- project/session 名称是否仅用于展示，ID 是否仍是 binding、存储和操作的唯一键。
- session 读写是否携带正确 project scope，切换项目是否造成历史串写。
- Router/Repository 的读写是否保持原子性，配置切换是否等待旧操作结束。
- Plugin 激活失败是否完整回滚 Tool、Skill 和 MCP 状态。
- 路径是否经过 `ProjectScope`/`PathGate`，是否可能逃逸项目根目录。
- Snapshot/Event 的 revision、seq、增量和 resync 语义是否同步更新前端 reducer。
- goroutine、订阅、数据库、MCP 和 Wails 关闭路径是否可终止且不会泄漏。
- 文档是否准确标注“已实现”和“规划”，是否暴露本机路径或秘密。

## 7. 提交约束

- 保留用户已有改动，不混入无关文件。
- 一个 commit 聚焦一个可解释的行为或文档主题。
- 提交前运行 `git diff --check`，确认模块 README 与实现同步。
- 除非用户明确要求，不自动 commit 或 push。
