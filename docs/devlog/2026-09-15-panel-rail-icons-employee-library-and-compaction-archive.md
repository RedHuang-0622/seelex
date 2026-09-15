# 2026-09-15 面板降噪与栏内收起、图标化、员工库解耦、账户级联、提示词分节、压缩归档写通道修复

## 一、口径（用户给定）

1. 背景色渲染取消。
2. 左侧栏和右侧栏的隐藏按钮布局在左侧栏和右侧栏即可（不再占顶栏）。
3. 图标代替符号。
4. Agent Team 的员工库和 team 的库管理**解耦**——没选 team 也得能看到离散的员工。
5. 顺序有了拖拽就不需要按钮调整。
6. 员工入职 / 修改面板要**条目化、条目序列化**，不要现在这样乱。
7. 模型选择应该是**供应商 → model 级联**，不该是一个平铺列表。
8. 提示词装配参考 Claude 官方文档重写（用 XML 标签分节）。
9. 之后追一条：**压缩归档的写通道没接上**（`read_compressed_turn: durable commit
   storage is unavailable`）要解决。

## 二、改动

### 1. GUI 降噪（背景色渲染取消）

- 对话区不再按说话人铺整块底色：`.message.is-exec / .is-advisor / .is-role` 只保留
  左侧状态条（EXEC 冷钢蓝、ADVISOR 暖黄）+ 发言人名，正文素底。类名与
  `messageRoleClass` 保持原样，`is-exec` / `is-advisor` 仍是"谁在说话"的机器可读
  归属（测试断言不变）。
- 外壳去掉径向辉光（`.app-shell` 用单一 `--bg`），层次交给各面板自己的 `surface`
  与 1px 分隔。
- 右栏页签条去掉装饰性渐变；选中页签的拟物书签（前一批用户要求）保留。

### 2. 栏内收起 + chevron 图标

- 顶栏上那两枚按钮删掉，改在每条栏自己的头一行（`.panel-rail-head`）里：左栏按钮
  靠内边缘右对齐、右栏靠内边缘左对齐。
- 图标是 `chevron-left` / `chevron-right`（展开态一枚、收起态一枚），显隐由
  `html[data-left-collapsed]` / `html[data-right-collapsed]` 的 CSS 决定。
- 收起后每条栏保留 **26px 窄脊**（不再是 `0px` 直接消失）：内容整体 `display:none`
  但脊和按钮还在——按钮可达、不占位；`Ctrl+B` / `Ctrl+J` 与
  `seelex.*.collapsed` 记忆行为不变。

### 3. 图标化

- `components.js` 的 `ICONS` 扩充：`chevron-left/right/up/down`、`more`、`star`、
  `star-outline`、`circle`、`dot`、`user`、`layer`。
- 动态渲染的 HTML 一律用 `icon(name, size)` 内联 svg（`hydrateIcons` 只覆盖启动期
  静态 DOM）。替换点：会话行 `⋯ ★ ☆ ⑂ ✕`、会话置顶标记 `★`、账户当前标记
  `● ○`、全权开关 `✓`、员工栏拖拽手柄 `≡` 与摘除 `✕`、两处面板关闭 `✕`。

### 4. Agent Team：员工库与团队库解耦

- `globalMasterBlock`（「全局母本」块：员工库 + 默认顺序 + 副本差异 + 普及按钮）重组
  为独立的 **`employeeLibraryBlock`（「员工库」块）**，排在面板最前：
  - 表：员工 / 类型 / 权限 / 操作（修改、删除，都是图标按钮）；
  - 头动作：「新建员工」`data-team-employee-new`、「入库当前会话」
    `data-team-publish-global`；
  - 底注：本会话在编 N 人 + 与员工库是否一致（原来那句"会话副本与母本一致"）。
- 员工库的建 / 改走**员工库作用域**的冷加载面板：`hirePanel(team, member, "library")`，
  回填自 `TeamGlobalConfig.employees`，提交走 `AgentTeamSaveEmployee`（只写全局事实，
  不装配、不建角色会话）；会话作用域仍走 `AgentTeamInstantiateRole`（表单上的
  `data-team-hire-scope` 决定路由）。
- 团队的职责收敛为三件事：装配谁（装配按钮）、按什么顺序回答（本表的"全局默认顺序
  vs 本会话顺序"两行 + 编辑团队里的顺序策略 + 「顺序设为默认」）、呼叫谁（装配后在
  「发言调度」里逐人呼叫 / 暂离）。团队库不再承载员工增删改。
- 旧宿主（`AgentTeamGlobalConfig` 不下发）时不渲染任何库写按钮，只做只读展示——
  不伪造能点的按钮。

### 5. 顺序：只剩拖拽

- 员工栏删掉 `↑` / `↓` 两枚按钮（用户口径：有拖拽就不需要按钮），行首手柄继续承担
  全部调序；`nextAgentTeamOrder` 收敛为"摘除 / 恢复"两个动作（位置类的 `up` / `down`
  一律返回 `null`，即拒绝提交），位置由纯函数 `agentTeamOrderForDrag` 负责。
- 测试同步：原有 `up` / `down` 用例改为断言"这些动作必须被拒绝"。

### 6. 面板条目化

- `hirePanel` / `teamEditorPanel` 重写为**序列化字段**：一条字段一行（序号徽标 +
  标签 + 控件 + 说明），按 `身份 / 编排 / 能力 / 提示词`（团队面板是
  `身份 / 形态与顺序 / 成员与呼叫顺序`）分节。
- 新增 `fieldItem(index, label, control, hint)` / `fieldGroup(title, items)` 两个纯渲染
  助手与 `.team-field` / `.team-field-group` 系列样式；`.team-field-row`（左右并排
  两列）删除——右栏只有 220~480px，并排会把控件挤到 80px 宽，这正是"乱"的来源。

### 7. 账户：供应商 → 模型级联

- `renderAccounts` 先按 provider 分组（分组来自数据本身，不写死），当前停留的供应商
  优先、否则跟当前账户、否则第一个；再列该 provider 下的模型，一行一个模型
  （主标签是模型，副标签是账户名）。
- 供应商切换是纯前端视图状态（`state.accountProvider` + 最近一次 runtime 缓存），
  不重拉快照；点模型行仍是 `SelectAccount(name)`，语义未变。

### 8. 提示词装配按 Claude 规范分节

- `application/prompt`：`PromptStack.Render` 把每层包成 `<identity>` / `<base
  name="plugin-…">` / `<effort name="high">` / `<instructions>` 段（标签名从 kind/name
  归一化，只留 `a-z0-9_-`），替代裸 `---` 分隔线——模型能准确引用"哪一段在讲什么"，
  宿主也能按标签做局部替换与审计。
- `seelebridge`：ADVISOR 角色设定与输出契约改成 `<role>` / `<task>` /
  `<constraints>` / `<output_contract>`；评审上下文改走 `<review_context>`；
  「优化提示词」元指令改成 `<role>` / `<task>` / `<rewrite_checklist>` / `<rules>` /
  `<output_format>`，用户消息改走 `<employee_context>` / `<draft_prompt>` / `<task>`。
  JSON 契约字段（`kind/content/refs/severity`、`optimized/notes`）不变——goal 域与
  优化结果的解析不受影响。

## 三、压缩归档写通道（根因与修法）

### 根因（Confirmed）

- 装配点（`main.go`）把 `*session.Manager` 注进 `CompressedTurnArchiver.Sessions`，
  而 `StoreTurn` 断言的端口是
  `sessionCommitPort{ SaveCommit(sessionID string, commit sessionstore.Commit) error }`。
- `*session.Manager` **没有**这个方法（`session/manager.go` 只有 `Router()` /
  `WithRouter` / `SaveCurrent` / `Resume` / workspace 相关），类型断言必然失败 →
  每次压缩都返回 `read_compressed_turn: durable commit storage is unavailable`，
  窗口外轮次的原文**从未落盘**。
- 后果：`read_compressed_turn` 只剩"摘要 + 不可逆"，与它的设计意图（压缩丢失可逆）
  相反；系统提示里"the original content is durably stored"这句在本会话不成立。

### 修法

- 端口改为**显式项目作用域**写面：
  `sessionCommitPort{ SaveCommitWorkspace(projectID, sessionID string, commit) error }`。
  理由：`SaveCommit(sessionID, commit)` 走 Router 的活跃写作用域，而活跃作用域是
  **视图**状态（切项目就变），后台会话的归档会被写进别的项目（R3 键漂移）；读面用的
  是显式键 `LoadToolResultWorkspace(workspaceID, sessionID, ref)`，两侧必须同键。
- `CompressedTurnArchiver` 新增两个 provider：
  - `ProjectIDProvider(sessionID)`：按**会话自己的绑定**解析项目（与 `eventStore` 的
    resolver 同一套 `wsRepo.SessionWorkspace`）；
  - `WorkspaceIDProvider()`：未绑定会话的兜底 = 视图当前工作区
    （`session_runtime.WorkspaceID(Snapshot.CurrentWorkspace)`，与读面同源）。
- `session.Manager` 补 `SaveCommitWorkspace`（委托 `Router.SaveCommitWorkspace`；
  未装 Router 时显式报错，不静默丢原文）；`main.go` 装配点补齐两个 provider。

### 证据

- `session`：`TestSaveCommitWorkspaceRoundTrip` 用真仓库（临时目录）端到端跑
  写 → 读——经 `Manager` 写入的 `compressed:seg-1` 能被读面同一调用
  `Router.LoadToolResultWorkspace("", "sess-1", "compressed:seg-1")` 原样读回；
  `TestSaveCommitWorkspaceWithoutRouterFailsLoud` 保证缺 Router 时报错而非静默。
- `application/core`：`fakeCommitSession` 改为实现 `SaveCommitWorkspace` 并记录项目
  作用域；新增 `TestCompressedTurnArchiverScopesCommitToSessionProject` 断言"会话绑定
  优先、视图工作区兜底、归属仍以 ctx/provider 的会话 ID 为准"。
- `go build ./...` 干净；`go test ./...` 全绿。

## 四、验证

- 前端：`node --check` 三个改动文件 + `node --test *.test.mjs` → 300 passed / 0 failed
  （agent-team-view 的员工库块、漂移提示、顺序纯函数断言均已同步）。
- 后端：`go build ./...` + `go test ./...` → 无 FAIL。
- 文档：`python scripts/gen_core_readme_index.py` 刷新 `application/core` 分卷索引
  （员工库 / 归档端口改名同步）。

## 五、备注（残余风险与口径）

- 归档作用域在"会话未绑定项目"时退回视图工作区，与读面同源；若后台会话既未绑定、
  又在视图切到别的项目时发生压缩，写入与读回仍可能不同键（这是读面既有的口径，
  本次不改读面）。真正根治要把读面也改成按会话绑定解析，属于另一批改动。
- 「确认·普及搭配到全局」按钮文案改为「入库当前会话」；后端 API
  （`AgentTeamPublishToGlobal`）不变，GUI / 文档已同步。
- 本轮只把**用户可见**的措辞 / 布局同步进 README 与 CHANGELOG；`docs/arch` 与各包
  README 里"全局母本"作为**后端概念名**仍然成立（后端数据布局没变），故未改。
