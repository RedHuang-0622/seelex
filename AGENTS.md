# Seelex repository instructions

本文件是仓库内 Agent、维护者和自动化工具的文档与代码协作规范。它作用于整个仓库；若子目录以后增加更具体的 `AGENTS.md`，子目录规则优先，但不得降低这里的安全、测试和文档要求。

## 0. 危险操作铁律（必读）

执行任何可能删除、覆盖或移动用户数据的操作前（`make clean/release`、`rebuild-gui`、`rm`、`Remove-Item`、`git clean/reset`、目录移动等），必须先阅读根目录 `MEMORY.md`，并用中文向用户预警：将删除什么、影响哪些用户数据（会话记录 `.seelex`、配置 `config/accounts.yaml`/`config/mcp.yaml`/`config/search_engine.yaml`/`*.local.yaml`/`seele.yaml`/`seelex.yaml`、dist 内副本）、是否可恢复；检查运行中的 seelex 进程并先备份，获得用户确认后再执行。新增代码前同样先读 `MEMORY.md` 的「新功能归属决策」，避免产生上帝类/上帝模块/上帝包。本规则优先于其它流程性说明。

## 1. 事实来源与依赖边界

- `application/` 是前端共享的应用层；`tui/` 和 `gui/` 只能通过 Application API 消费状态与提交操作，不复制业务状态机。
- `backend/engine/assemble` 是 Seelex 到远程 Go module `github.com/RedHuang-0622/Seele` 的兼容与能力适配层（引擎侧宿主面，仍在按域下沉）；`backend/{engine,tool,multiagent,limit}` 是 Seelex 自己的领域实现。优先复用 Seele，不在 Seelex 重造引擎能力。
- `backend/engine/plugin/` 管理 Plugin 生命周期，`plugins/` 存放声明式能力包；`backend/engine/skill/` 负责 Skill 加载与可见性。
- `session/` 是会话用例适配器，`store/` 是原子、项目作用域的持久化实现，`store/workspace/` 管理项目及 session binding。
- 入口只做五步：`wiring/config.BindFlags` → `flag.Parse` → pprof 构建钩子 → `-version` →
  `wiring/bootstrap.Run` → `Serve` → `Shutdown`。启动参数在 `wiring/config`，装配顺序在
  `wiring/bootstrap`，工具与能力注册在 `wiring/registry`；业务规则进入拥有该规则的模块，
  不许堆进入口。
- `contract/` 是零依赖契约层（DTO 与端口），`test/` 是仓库级集成用例包（模块内部用例仍随包走）。
- 当前实现、规划设计和历史研发记录必须明确区分。代码与测试是“已实现”能力的最终事实来源。

## 2. 文档放置规范

| 文档类型 | 放置位置 | 规则 |
|---|---|---|
| 模块说明 | `<module>/README.md` | 与代码同目录，描述当前实现和生态位；代码行为变化时同步更新。 |
| 仓库入口 | `/README.md` | 产品定位、快速开始和模块导航，不堆叠模块内部实现。 |
| GitHub 自动化 | `.github/AUTOMATION.md` | `.github` 的模块说明例外使用该文件名；禁止创建 `.github/README.md`，避免覆盖仓库首页的根 README。 |
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

`.github` 是唯一命名例外，其模块文档为 `AUTOMATION.md`，因为 GitHub 会优先把 `.github/README.md` 渲染为仓库首页。

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

除上述十项外，模块 README 统一遵循 [`docs/arch/readme-spec.md`](docs/arch/readme-spec.md)：

- 以 `## 生态位` 开头说明定位与主要调用方，同一语义不混用“模块定位/生态位”标题。
- 追加自动生成的「文件与函数索引」：逐文件列出每个函数的签名 + 首行摘要，
  摘要事实源为源码 doc 注释（`application/assembly` 由
  `scripts/gen_core_readme_index.py` 生成，其余包复用同款提取逻辑）。
- 根包/大包按文件前缀分卷（`README-<卷名>.md` + 主 README 导航）；叶子包
  维持单 README。
- 语言统一中文（代码标识符保留原文）；UTF-8 无 BOM；含中文的生成脚本必须
  落盘执行，禁止经 shell 管道内联中文。

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
go test ./test/ -count=1                     # 仓库级集成用例（真 API 族自查环境变量后 skip）
bash scripts/hooks-selftest.sh               # 改过 .githooks 才需要（hermetic，秒级）
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
- 路径是否经过 `ProjectScope`，是否可能逃逸项目根目录。
- Snapshot/Event 的 revision、seq、增量和 resync 语义是否同步更新前端 reducer。
- goroutine、订阅、数据库、MCP 和 Wails 关闭路径是否可终止且不会泄漏。
- 文档是否准确标注“已实现”和“规划”，是否暴露本机路径或秘密。

## 7. 提交约束

- 保留用户已有改动，不混入无关文件。
- 一个 commit 聚焦一个可解释的行为或文档主题。
- 提交前运行 `git diff --check`，确认模块 README 与实现同步。
- 除非用户明确要求，不自动 commit 或 push。

## 8. 复用与单一实现纪律

本节的判据是：**同一件事不许有第二份实现**。它约束一切新增代码，包括 AI 子代理的自主拆分/实现。
与 `MEMORY.md`「新功能归属决策」的分工：那一节回答"放哪里"，本节回答"要不要写第二份"。

1. **动手前先找既有实现**：先读目标模块 README 的「文件与函数索引」，再按概念（不是按名字）搜同义函数；
   已有就调用或扩展它，没有才新增。写适配器之前先问：能不能让既有实现**结构上满足**新接口
   （结构满足 + 编译期断言 > 适配层 > 复制一份）。
2. **只并"本质重复"**：判据相同 + 语义相同 + 会一起漂移并伤到人 → 必须并成一份。
   只是长得像的（例如两处都起进程树，但超时/取消语义不同）**不并**，但要在注释里写清"为什么像却不并"，
   防后人误并。偶然重复（同一写法落在两个不同概念上）不算缺陷，不为它动刀。
3. **一份判据 = 一处函数**：同一判据、同一语义在第二处出现，即视为缺陷（不是"更清楚"）。
   词表、分类、状态判定、恢复说明、回收动作都属于此列。
4. **接口先行 + 依赖倒置**：共享实现之前先定接口；接口放零依赖的契约层，实现放能依赖具体依赖的那一层，
   调用方只持接口，具体实现在装配处（composition root）注入。禁止契约层反向依赖实现层。
5. **装配优先于继承**：一层只装配它需要的端口，其它端口不传；实现里禁止出现按层/按主体判断的分支
   （`if kind == ...`、`if role == ...`）——层差异只允许出现在**装配表**与**策略的调用点**上。
6. **交付证据**：交付必须列出"删掉了哪些重复实现（文件:行 → 新唯一位置）"；
   证据是"只剩一份实现 / 编译器钉住（编译期断言、结构上满足）"，不是"用例全绿"；
   跨实现共用同一套语义时，还必须有**跨实现的一致性用例**（同一份输入跑两遍）。
7. **红灯先行**：修"漂移类"缺陷（同一件事两份实现互相不一致）前，先写会红的用例复现现象，
   再修、再绿；不许改测试去迁就实现。
8. **收工自检**：这件事在仓库里是否已有实现？我的判据是否只出现在一处？交付里有没有删除清单？
   证据是结构性的还是只是"测试绿了"？只加不改的交付一律退回重做。

## 9. 契约优先的拆分与迁移纪律

任何新增功能（`feat`）、行为变更、接口/JSON/持久化格式变更，或会迫使下游实现一起改变的契约调整，
在写生产代码前都必须先通过"设计门禁"。顺序固定为：

**领域归属 → 聚合与不变式 → 契约（DTO / port / interface）→ struct 与职责 → 下游影响 →
红测 → 迁移实现 → 删除重复路径。**

1. **先做领域归属**：说明这件事属于哪个领域、哪个聚合根或应用用例，聚合内必须维持哪些不变式；
   同时列出它刻意不属于哪些模块。无法说清归属时停止实现，不得以"先放 `application/assembly`、
   `backend/engine/assemble` 或顶层 `contract` 再说"作为过渡方案。
2. **先定契约与结构**：在实现前明确列出要新增/修改的 DTO、port、interface、关键 struct 及所有权。
   稳定跨层值放 `contract/dto` / `contract/protocol`；可替换边界由消费方定义端口；
   聚合内部状态与私有 struct 留在拥有该不变式的领域包。禁止把所有东西继续堆进单一契约包。
3. **契约驱动不是接口先行后随便落地**：接口必须由真实调用方和领域职责推导，实现只依赖它需要的端口；
   具体实现留给拥有具体依赖的模块，在 composition root 装配。禁止为了迁移方便制造双向端口或上帝接口。
4. **先写下游影响矩阵**：列明生产者、消费者、存储/wire、前端 reducer、测试与兼容 alias；
   说明哪些行为保持逐字兼容，哪些是显式语义变更。若契约改动会迫使以下任一者变化，必须先确认整体设计：
   - Application / Backend / Middle 的依赖方向；
   - 聚合边界、状态所有权或恢复状态机；
   - JSON、事件、持久化或前端 wire；
   - 多个高冲突文件，或两个以上领域包。
5. **先红后绿**：先写契约断言、结构门禁或行为用例，证明当前缺口；再实现。迁移类改动必须同时加入
   跨实现一致性用例或依赖预算，不能只凭"现有测试仍绿"证明完成。
6. **按结构迁移，不按文件堆搬**：迁移单位是职责与契约边界，不是整份文件。每批只移动一个可解释能力，
   保留一层转发/alias 时应登记旧面、新面、调用方、删除里程碑和归零验证；禁止把旧实现复制成第二份。
7. **设计门禁可停止实施**：若领域归属、聚合边界、契约身份或兼容语义存在未决冲突，先报告冲突与候选方案；
   不得擅自改变产品语义，也不得以抽象层掩盖尚未想清的设计。
8. **交付必须带结构证据**：列出新增/变更的 DTO、interface、struct，删除或合并的重复实现，
   依赖边和文件规模变化，以及哪些设计假设仍需用户裁决。

## 10. 防上帝模块与存储领域纪律

本节约束 `store/` 及一切大型领域包的后续演进。方案事实源见
[`docs/2026-10-27-store-ddd-split/plan.md`](docs/2026-10-27-store-ddd-split/plan.md)
与 [`milestones.md`](docs/2026-10-27-store-ddd-split/milestones.md)。

### 10.1 屎山是怎样长出来的

本仓库已经出现的反模式不是“文件太多”，而是下面几条叠加：

1. 新能力总是挂到既有 `Router` / `jsonRepository` / `storeEngine` receiver 上。
   增加一个方法比为能力找 owner 更省事，于是方法、文件、锁和隐式状态持续向单类型汇聚。
2. 领域文件在物理上分开了，但共享同一个上帝 receiver、中央模块枚举和锁表。
   这种“按文件拆”不会降低耦合，只会把上帝类摊到更多文件里。
3. `Repository` / `SessionGranularStore` / `BoardsFor` 一类万能接口或能力发现口，
   让所有消费者依赖全部存储能力；消费者没有自己的窄端口，边界就无法收缩。
4. 跨领域写序被塞进 Store 的一个方法，例如一次 commit 同时协调 artifact、
   transcript 与 compaction；原子性和失败语义因此绑定在存储实现上。
5. 兼容 alias / 一行转发没有 ratchet、没有删除 owner、没有归零测试，
   迁移完成后仍被保留，新一轮开发又在 facade 上继续长方法。
6. 测试继续围着旧门面写，而不是围着聚合与唯一实现写，导致旧门面永远“仍被使用”，
   兼容层无法退场。
7. 为了“先拆出去”新增的 `new*` / getter / facade 如果只做二次封装，
   等于把同一调用链拉长一层，文件数变化不等于职责拆分。

### 10.2 强制规则

1. **Store 根包只保留持久化机制**：数据根、原子写、head 信封、布局、锁分片、
   关闭与兼容转发；不得新增业务判据、业务状态机或跨领域用例。
2. **按限界上下文建包**：Session、Context、Artifact、Board、Teamwork、Role、
   WorkUnit 等各自拥有类型和实现；领域包禁止 import 根 `store`、Application、
   Middle 或 Backend，领域包之间禁止直接 import。
3. **端口由消费者定义**：Application/Middle/Backend 只声明自己实际使用的窄端口；
   具体 Store 实现通过结构满足端口。禁止把所有能力塞进一个新的万能接口。
4. **God 类型只许收缩**：`Router`、`jsonRepository`、`storeEngine`、
   `SessionGranularStore` 的公开方法数必须写入 ratchet；新增领域方法默认禁止。
   任何例外必须同时给出 owner、迁移去向、删除里程碑和归零测试。
5. **禁止中央锁表继续增长**：新模块使用领域命名的 `ModuleID` 和锁注册，
   不得为每个新域继续修改固定字段集合或 `mutexFor` switch。
6. **跨领域事务归应用层**：Store 只提供单一聚合的原子读写；
   transcript/artifact/compaction 等跨域写序由 Application Unit-of-Work 编排。
7. **兼容层必须有死亡条件**：根包 alias 只允许同身份 alias，转发只允许一层；
   必须在兼容台账登记旧面、新面、首调用方、删除里程碑、归零命令和回退方式。
8. **测试跟着 owner 走**：领域测试放领域包；根包只保留兼容 parity、
   跨聚合门禁和真实的 composition-root 测试。禁止用旧门面测试替代新 owner 测试。
9. **禁止“包一层算拆分”**：若新代码只是 `return lower.New(...)`、类型 alias、
   component getter 或同义转发，不计作完成拆分；应把调用方直接重定向到 owner，
   再删除中间层。
10. **先 M0 再迁生产实现**：Store 拆分必须先完成特征测试、失败语义基线、
    锁身份、方法 ratchet 和 `./store/...` import 门禁；未完成 M0 不得进入
    Session/Commit 等高风险迁移。
11. **外部依赖批次必须走协议**：修改当前领域目录之外的调用点、共享 DTO 或导出面，
    必须按 [`external-change-protocol.md`](docs/2026-10-27-store-ddd-split/external-change-protocol.md)
    建专用 worktree、提交 checkpoint、dry-run 外部影响脚本并保留回滚证据。

### 10.3 Review 红线

出现下列任一情况，直接退回，不以“测试全绿”放行：

- 在 Store 根上帝类型上新增领域方法或新的 `*For` 能力发现口；
- 领域文件继续使用共享 `storeEngine` receiver，或反向 import 根 `store`；
- 新增一个只转发到下层实现的 `new*` / getter / facade；
- 把跨领域提交、锁或失败恢复顺序写进 Store；
- 兼容转发没有删除里程碑，或调用方仍从旧门面测试获得“仿真实生产”假象；
- 只搬文件，不搬类型、方法、锁、测试与 import 所有权；
- 用清理数据、重写 head 或删除 `.seelex` 掩盖拆分回归。

## 11. 子代理编排纪律

Agent 可以按用户要求把工作交给子代理，但必须保持**扁平、最小上下文、文件互斥**。

1. **禁止嵌套派遣**：只有当前根 Agent 可以派遣子代理；子代理不得再派生子代理，
   不得通过新建 thread、worktree 或“临时 helper”绕过。需要更多并行度时，
   子代理向根 Agent 报告，由根 Agent 统一分批。
2. **最小上下文包**：默认使用 `fork_turns: none`。任务包只提供：
   目标、必须阅读的文件、领域不变式、独占文件、禁止触碰文件、验证命令和交付格式。
   不要继承完整聊天历史；只有任务无法用最小包表达时才允许继承，并在任务中说明原因。
3. **文件单车道**：同一文件同一时刻只能有一个子代理修改。
   `application/assembly` 的 `service*.go`、`faces.go`、`service_components.go`、
   Store 的 `Router`/`SessionGranularStore` 等 God 文件默认单车道。
4. **每批可解释**：一个子代理只做一个能力面或一个里程碑，生产改动建议 <600 行、
   公开接口建议 <=20 个；超出时先在批次报告里拆分，不继续扩张。
5. **先红后绿**：子代理必须提交首次失败证据、修改后测试结果、删除/合并清单、
   指标变化、阻塞和未触碰文件；不得只回复“已完成”。
6. **权限收紧**：子代理不得 commit/push、clean/reset、rebuild/release、删除或移动
   用户数据，除非用户在当前任务中明确授权。编辑必须用补丁或受限机械脚本，
   不得用临时写入覆盖无关文件。
7. **冻结再验收**：子代理停止后，根 Agent 先核对 `git status` 和文件面，
   再运行受影响测试与结构门禁；一个批次未验收前不启动依赖它的下一批。
8. **失败即上报**：遇到未决语义、文件冲突、跨包契约变化或冲突的未提交改动，
   子代理立即停止并报告，不通过新增适配层、复制实现或扩大范围绕过。
