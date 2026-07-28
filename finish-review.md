# Seelex 整体项目审查报告

审查日期：2026-07-28  
审查基线：`e75a7ca feat(app): add scoped sessions and graceful GUI`

## 一句话结论

Seelex 已经形成了一个结构完整、功能密度很高的 Developer Alpha：Application Core、双前端、
Plugin/Skill、Plan/Fork、项目会话范围和可插拔会话存储都已有真实代码，不是演示壳。

但它现在更像“Agent 控制平面”，还不是可安全交付的工程 Agent。最缺的不是更多按钮、Plugin
或模型能力，而是可信执行底座、状态事务边界、首次启动配置闭环和可验证的真实 E2E。

最终判断：**不通过生产发布；允许继续 Alpha 内测，前提是明确标注非沙箱、非生产和数据切换语义。**

## 变更与验证概览

| 项目 | 结果 |
|---|---|
| 当前提交 | `e75a7ca`，42 个文件，+3376/-136 |
| 全量 Go 测试 | 通过：`go test -p 1 ./... -count=1 -timeout 300s` |
| 全仓覆盖率 | 54.2% |
| Application | 73.0% |
| GUI Go 包 | 80.2% |
| Seele bridge | 50.7% |
| Session store | 57.0% |
| Workspace | 0.0% |
| TUI / splash | 6.5% / 0.0% |
| Go vet | 通过 |
| Windows GUI production build | 通过 |
| 前端 Node 测试 | 26/26 通过 |
| 本地 race | 未执行：`CGO_ENABLED=0` 且无 GCC；只能依赖 Linux CI |
| 跟踪文件密钥扫描 | 未发现已提交的真实 key |

测试绿灯证明当前已覆盖路径稳定；它不能证明权限、沙箱、真实数据库、真实 WebView、真实模型和
跨进程故障场景已经闭环。

## 五轴结论

| 维度 | 状态 | 评分 | 评价 |
|---|:---:|:---:|---|
| 正确性 | ⚠️ | B- | 主链路能跑，但账号回退、运行中状态变更和持久化错误处理会产生错误语义。 |
| 可读性 | ⚠️ | B | 接口边界总体清楚，但 `application/app.go`、`chat.go`、`sessionstore.go` 和 `main.go` 已明显过大。 |
| 架构 | ⚠️ | B | Application Core 与 Adapter 方向正确；执行安全、会话事务和配置生命周期仍是拼装状态。 |
| 安全性 | 🚨 | C | Full Access 不可降级、shell/MCP 非沙箱、grep symlink 越界、配置回退误导。 |
| 性能 | ⚠️ | B- | 当前规模可用；全树扫描、全量历史 JSON 和未清理 generation 会随项目增长恶化。 |
| Go 专项 | ⚠️ | B | build/vet/test 通过；race 本地未验证，持久化与并发状态切换仍缺测试。 |

## 严重问题

### 1. Full Access 开启后无法关闭

GUI 会调用 `SetFullAccess(true/false)`，但 `seelebridge.Runtime.SetFullAccess` 只处理 `true`；
传入 `false` 什么也不做。界面会显示 FA 已关闭，后端仍保持 Full Access。

- `gui/frontend/dist/app.js:569`
- `seelebridge/runtime.go:255`

这是直接的安全状态欺骗。修复必须保存并恢复完整的 manual permission config 与 approval handler，
并把后端真实权限模式放入 Snapshot，GUI 不能维护自己的布尔副本。

### 2. 缺少账号配置时伪装成 OpenAI 账号

`config/accounts.yaml` 不存在时，代码构造 `gpt-4o + api.openai.com + sk-fallback`。干净发行包
又故意不携带真实账号文件，因此首次启动可以进入 UI，却在发送消息时才错误访问 OpenAI。

- `seelebridge/config.go:41`
- `main.go:41`
- `scripts/build-gui.ps1:22`

这正是 MiniMax 被“丢失”的根因。正确行为应是显式的 `AccountNotConfigured` 状态和 GUI 配置向导，
而不是创建可发起外网请求的假账号。

### 3. 项目范围不是沙箱，而且存在实际越界路径

内置 `read_file`/`write_file` 的路径解析做得较认真，但整体仍不是安全边界：

- `bash` 只设置工作目录，命令可直接读取任意绝对路径或 `cd ..`；源码自己也承认不是 OS sandbox：
  `seelebridge/scoped_tools.go:20`、`:258`。
- MCP 和 Plugin 自定义工具不经过 `ProjectScope`，可以拥有自己的文件、网络和进程权限。
- `grep_search` 只校验遍历起点，随后直接 `os.ReadFile(path)`；项目内指向外部文件的 symlink 可被读取：
  `seelebridge/scoped_tools.go:69`、`:99`。
- 路径检查与实际写入之间仍有 TOCTOU 窗口。

因此当前“项目限制读写范围”的产品表述只能用于友好约束，不能用于对抗恶意 Prompt、Skill、
Plugin 或 MCP Server。

### 4. 本地模型密钥应立即轮换

真实 MiniMax key 虽然被 `.gitignore` 排除且未进入提交，但已出现在本次终端/工具输出历史中。
应把该 key 视为已经暴露，立即在服务端撤销并生成新 key。后续诊断脚本必须只打印
`configured`、哈希指纹或末四位，不能输出完整配置文件。

## 高优先级问题

### 5. 运行中可以切换会话关键依赖

`SelectAccount`、`SwitchEffort`、`SwitchPlugin`、`BindWorkspace`、`UnbindWorkspace`、存储切换和
会话删除没有统一的运行态守卫。`resumeSession` 有 `Chat.Running` 检查，但其他入口没有。

- `application/app.go:269`
- `application/app.go:283`
- `application/app.go:304`
- `application/app.go:344`
- `application/app.go:350`
- `application/session_storage.go:32`

最危险的是 Plugin 切换会清空 Engine history，而项目切换会修改全局 `ProjectScope`；如果正在执行
ReAct/Plan，后续工具调用可能在不同账号、Plugin 或项目根上继续。这违反“一个会话固定项目粒度和
读写范围”的核心承诺。

需要一个 application-owned `OperationGuard`：运行中冻结 account/plugin/effort/project/storage，
或者把它们全部快照进 request-scoped runtime，由当前请求只读取快照。

### 6. Workspace 持久化不是原子操作，而且错误被吞掉

Workspace 和 session binding 是项目会话机制的索引，但其变更使用普通 `os.WriteFile`，Create、
Delete、Bind、Unbind 都忽略持久化错误；`UpdateGitRemote` 甚至没有保存。

- `workspace/workspace.go:75`
- `workspace/workspace.go:139`
- `workspace/workspace.go:199`
- `workspace/workspace.go:216`
- `workspace/workspace.go:230`

崩溃、磁盘满或权限问题可能让内存状态成功、重启后状态丢失，甚至写出截断 JSON。`WorkspacePort`
的 Bind/Unbind 目前也不返回 error，导致上层无法建立事务。

### 7. 会话删除和项目绑定没有引用完整性

`DeleteSession` 只删会话存储，不清理 workspace binding；删除 workspace 会清 binding，但不会处理
该项目下的会话数据。结果是 dangling binding 或无法从 UI 到达的孤儿数据。

- `application/app.go:344`
- `session/manager.go:126`
- `workspace/workspace.go:199`

需要由 Application 聚合根执行事务式删除，并定义“删项目是否保留会话、迁移到未绑定区或级联删除”。

### 8. 存储后端切换没有迁移、版本和错误可见性

Router 能安全交换 repository，但只影响后续读写；旧后端数据不会迁移。切换后会话列表看起来像
全部消失。`Router.List` 还吞掉数据库错误并返回空列表，数据库故障和“真的没有会话”无法区分。

- `sessionstore/sessionstore.go:173`
- `sessionstore/sessionstore.go:220`

另外，SQL schema 没有版本表和迁移框架；JSON 每次写入产生新 generation，但没有垃圾回收；
SQLite/PostgreSQL 的 `ReadRange` 都先读取并反序列化完整历史，再在内存切片，不是真分页。

设置页必须区分：测试连接、切换空后端、复制/迁移、验证计数、提交切换、回滚。默认应该是迁移，
纯切换必须明确警告。

### 9. 账号配置契约与文档不一致

公开模板和 README 声称支持 `defaults.provider/max_tokens/timeout/temperature`，但解析结构只读取
`roles`；所有账号 Provider 被硬编码为 OpenAI，Runtime 又硬编码 `MaxTokens=200000`、
`Timeout=300`、`Temperature=0.7`。

- `config/accounts.example.yaml:4`
- `seelebridge/config.go:30`
- `seelebridge/config.go:84`
- `seelebridge/runtime.go:78`

所以当前真实能力是“OpenAI-compatible endpoint”，并不是文档描述的原生 OpenAI/Anthropic/
DeepSeek 多 Provider 配置。配置字段必须要么实现，要么删除，不能保持假契约。

### 10. Windows GUI 启动故障不可观测

GUI 以 `-H windowsgui` 构建，没有控制台；而启动失败只通过 `fatalf` 写 stderr 后 `os.Exit(1)`。
账号、Plugin、数据库或 WebView 初始化失败时，用户可能只看到“点了没反应”。

- `scripts/build-gui.ps1:23`
- `main.go:498`

需要启动错误对话框、滚动日志文件、诊断导出和 GUI 内的账号/网络状态页。

## 中优先级问题

### 11. 测试覆盖与风险分布相反

最关键的新模块恰好覆盖最低：Workspace 0%、Seele bridge 50.7%、Session store 57.0%；TUI 6.5%。
PostgreSQL 没有容器化集成测试，真实 WebView 没有自动化 E2E，真实 MiniMax/OpenAI 调用只靠人工，
Plan 并行 E2E 使用 fake AgentFactory，不能证明真实账号、网络、取消和限流组合。

CI 会生成覆盖率，但没有阈值；覆盖率下降不会阻止合并。race 在 CI 中存在，这是优点，但当前提交
是否已经通过远端 race 没有在本机证据中确认。

### 12. 扫描和历史路径不具备大项目规模能力

- `grep_search`/`glob` 使用 `filepath.Walk` 扫整棵树，没有统一 ignore、文件大小上限、二进制判断、
  glob 结果上限或索引。
- SQL history 以一大段 JSON 存储；窗口读取仍全量加载。
- JSON generation 不清理，长会话反复保存会持续占磁盘。
- 仓库没有 benchmark 和性能预算。

### 13. Application Core 正在变成 God Service

`application/app.go` 658 行、`chat.go` 759 行、`sessionstore.go` 641 行、`main.go` 485 行。
职责已经混合状态机、命令、项目、会话、Plan、生命周期和配置。继续加知识库、CodeGraph、远程协议
会快速放大锁顺序与事务问题。

建议拆成 `ConversationService`、`ProjectSessionService`、`RuntimeSettingsService`、
`PlanProjection` 和 `LifecycleCoordinator`，Application 只负责编排。

### 14. 优雅退出仍缺产品层反馈

当前关闭时会等待 Chat 完成，逻辑方向正确；但没有“正在等待会话结束”的 UI 状态、剩余任务说明、
超时或显式强制退出入口。若网络或审批长期阻塞，用户会认为窗口无法关闭。

### 15. 文档和版本已经落后于代码

README/CHANGELOG 仍以 `v0.1.0-alpha.1` 描述，未正式记录项目会话、三类存储、Fork M8 和优雅退出；
部分“权限尚未接入”的描述已经过时，部分“多 Provider/可配置默认参数”又超前于实现。

## 亮点

- Application Core 被 TUI/GUI 共用，Snapshot/Event/Interaction 的方向正确。
- 未绑定项目时，内置文件工具 fail closed，不再默认读取 Seelex 自身目录。
- `read_file` 和写入路径处理包含绝对路径、相对路径和 symlink root 校验。
- WorkPlan 两条 fork 路径已统一到 Seele ForkCoordinator，默认 fail-fast，分支账号 client 隔离清晰。
- GUI 的增量事件、Markdown 转义、危险链接限制和退出协调均有契约测试。
- Session storage 有统一 Repository 和后端 Router，JSON manifest 切换与 SQL transaction 是正确起点。
- 发布流程采用配置白名单，真实账号文件未进入 Git 和 ZIP。
- CI 已具备三平台 build、Linux race、GUI Node 测试、漏洞扫描和 release safety 的基本骨架。

## 目前真正缺少什么

### P0：可信执行与配置闭环

1. 修复 Full Access 可逆切换，以后端状态为准。
2. 删除假 OpenAI fallback，增加账号配置向导、连接测试、字段校验和安全存储。
3. 轮换当前本地 MiniMax key，并建立日志脱敏规则。
4. 引入跨平台 sandbox runner；至少把 shell、进程、网络和文件系统能力放入统一 capability broker。
5. MCP/Plugin 工具必须声明并受项目、网络、进程权限约束。
6. 修复 grep symlink 越界并增加安全回归测试。

### P1：项目会话事务闭环

1. 增加 request-scoped runtime snapshot 或统一 OperationGuard，运行中禁止切换关键依赖。
2. Workspace index 使用临时文件、fsync、原子替换，并让所有 mutation 返回持久化错误。
3. 建立 Session/Workspace 删除引用完整性与恢复策略。
4. 存储切换实现迁移、校验、回滚、schema version 和错误传播。
5. 将账号默认参数真正注入 ChatClient，并明确 Provider 适配层。

### P2：可运维性与发布门禁

1. Windows GUI 启动错误对话框、文件日志、诊断导出。
2. PostgreSQL 容器集成测试、真实 WebView E2E、真实 LLM opt-in golden test。
3. CI 增加覆盖率阈值，优先要求 Workspace、Session store、Seele bridge 达到 80%。
4. 增加长会话、十万文件项目、并发 Fork、取消、数据库掉线和磁盘满测试。
5. 为 JSON generation 做 GC，为 SQL history 做真正分页或消息表设计。

### P3：产品化能力

1. 项目首页、最近项目、账号/模型管理、首次启动向导。
2. 项目级共享知识库/CodeGraph，但必须建立在稳定的 project identity 与索引版本之上。
3. 远程/IDE 客户端协议、鉴权和兼容性策略。
4. 先完成一个 Dev Plugin 或 CAD Plugin 的真实纵向闭环，再继续扩展 Plugin 数量。

## 推荐路线

```text
M9  Security Foundation
    permission state + account onboarding + sandbox/capability broker

M10 Project/Session Transactions
    request snapshot + atomic workspace index + referential integrity

M11 Storage Reliability
    migration + rollback + schema version + real pagination

M12 Operability & Acceptance
    GUI diagnostics + real WebView/DB/LLM E2E + quality thresholds

M13 One Vertical Product Loop
    Dev 或 CAD 任选一个，做到真实项目输入 → Agent 执行 → 可验证产物 → 可恢复历史
```

## 最终判断

- [ ] 通过，可作为生产版本发布
- [x] 有条件通过，可继续 Developer Alpha 内测
- [x] 不通过生产发布

阻塞生产发布的条件：Full Access 可逆、账号配置闭环、真实 sandbox/capability 边界、运行时状态冻结、
Workspace 原子持久化、存储迁移与真实 E2E 门禁。

最锐利的评价是：**Seelex 已经证明“能把 Agent 功能装起来”，下一阶段必须证明“装起来之后不会越界、
不会丢状态、不会骗用户、出了问题能定位”。**
