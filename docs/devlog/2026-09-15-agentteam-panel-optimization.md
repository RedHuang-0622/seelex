# Agent Team 面板与相邻面板：可优化之处（审查 + 调研）

> 范围：GUI 右栏「Agent Team」四块（员工库 / 团队库 / 员工栏 / 发言调度）、员工与团队
> 的冷加载面板、相邻的会话条目与右栏子页；以及支撑它们的 Bridge / Application /
> sessionstore 能力面。
> 方法：本仓代码只读审计（带 `file:line` 证据）+ 多 agent 编排 UI 的行业对照（含出处
> 链接）。**本文只给结论与建议，不改代码**；已落地的改动见 §0。
> 基线：`1104ea0` + 本轮未提交改动（员工面板去小字备注）。

---

## 0. 本轮已落地（用户口径）

- 「修改员工 · ADVISOR」「入职员工」「新建员工 · 员工库」三处面板**不再有小字备注**：
  7 条字段说明与底部作用域说明全部撤掉，说明改挂控件/提交键的 `title`（hover 才出现）；
  「优化提示词」的运行态回执改用独立类 `team-prompt-state`，不再借备注样式
  （`gui/frontend/dist/agent-team-view.js` 的 `hirePanel` / `fieldItem`、
  `gui/frontend/dist/styles.css`）。
- 回归证据：`gui/frontend/dist/agent-team-view.test.mjs` 新增用例断言两种作用域、
  四个面板实例里都没有 `team-field-hint` / `team-editor-hint`，且可见正文里不再出现
  那几句原备注；`gui/bridge_test.go` 增补嵌入产物断言（说明必须走 `title`）。

---

## 1. 优先建议（按 影响 × 成本）

| # | 问题 | 影响 | 成本 | 归口 |
|---|---|---|---|---|
| A1 | **编辑员工会静默丢字段**：`RoleSpec` 有 10 个字段，成员表只回填 7 个，后端又是"整条覆盖" | 高（真数据丢失） | 低-中 | 前后端 |
| A2 | **Esc / 任意重绘丢弃未保存的入职表单** | 高（真数据丢失） | 低 | 前端 |
| A3 | **团队面板草稿行的拖拽会直接写会话**（拖拽来源记录了却从不读） | 中高（误写会话） | 低 | 前端 |
| A4 | **禁用按钮的理由挂在 `data-tip` 上，永远弹不出来** | 中（说不清） | 低 | 前端 |
| A5 | **非法拖拽落点静默失败**（无 toast、无落点语义） | 中 | 低 | 前端 |
| A6 | **发言顺序只能拖拽，键盘不可用**（拖拽条有 `role=button tabindex=0`，却没有 keydown） | 中（可达性） | 低-中 | 前端 |
| B1 | **整表级三动作无入口 + `order.json` 没有运行时读取点** | 中高（第三份事实空转） | 低 | 后端 |
| B2 | 全局母本"读改写"非原子、普及写三文件非事务 | 高（并发丢数据） | 中高 | 后端 |
| B3 | 会话存/普及到全局时 `team_id` 回落 `team_kind` → 跨会话互相覆盖 | 高 | 低-中 | 后端 |
| B4 | 装配 = 整份覆盖（丢掉会话已有员工）；registry/order 两步非原子 | 高（不可自救） | 中 | 后端 |
| C1 | `Runtime.Next` / `TurnScheduler` 未在生产装配；user 席位与逃生上限没有写通道 | 中高 | 高 | 后端 |

---

## 2. 前端：真问题与最小修法

### A1 编辑员工会静默丢字段（最高优先级）

- 证据：`application/contract/dto/agentteam.go:54-66`（`RoleSpec` 含
  `mirror_policy` / `directive_schema` / `order_priority`），但 `TeamMember`
  （同文件 `:92-105`）只带 7 个可回填字段；前端 `normalizeRoleSpecs`
  （`gui/frontend/dist/agent-team-view.js`）与入库/入职载荷
  （`gui/frontend/dist/app.js:1893-1905`）同样只映射 7 个键；后端
  `application/core/agentteam/registry.go:102-144` 按 `role_name` **整条替换**。
- 复现路径（不需要真跑）：装配 `goal-a2a` → 点 `tl`「修改员工」→ 原样保存 →
  `session/<id>/team/roles.json` 里 `tl` 的 `directive_schema`/`order_priority`
  消失（`presence_policy` 在 hire 表单里能回填，属于"侥幸没丢"的那一类）。
- 最小修法：前端三处改成"带全字段读写"（读到的对象原样带回，只覆盖用户改的键），
  `TeamMember` 补 `directive_schema`/`mirror_policy`（或让编辑面板另走一次
  `TeamComposition` 全量读）。风险：改动跨前后端，需同时更新
  `bridge_team_test.go` 的载荷断言。

### A2 Esc / 重绘丢草稿

- 证据：`app.js:1706-1711` 的 document 级 Esc 无条件 `closeAgentTeamEditors()`；
  `app.js:1851-1858` 直接 `slot.innerHTML=""` 关面板，没有 dirty 判断。
- 最小修法：关面板前比对表单当前值与打开时的快照，脏则 `confirm`；表单脏时事件
  重绘先搬草稿再重绘。风险：只动 `app.js` 事件路径，`bridge_test.go` 对该文件的断言
  是"嵌入完整性"，不锁这段逻辑。

### A3 团队草稿不跨界

- 证据：`app.js:2155` 写入 `agentTeamDragSource`（`library|member|staff`），
  `:2192/:2248` 只重置、**从不读取**；drop 处理只按落点分派（`app.js:2195-2199`）。
- 最小修法：drop 时读来源——来源是团队面板成员行时只允许落到
  `[data-team-member-list]`，落别处忽略（可配一句 toast）。

### A4 禁用理由弹不出来

- 证据：未排入员工的「摘除 ✕」是 `disabled` + `data-tip`（`agent-team-view.js` 的
  `staffRow`），而提示气泡的触发是 `pointerover`/`focusin` 委托到 `[data-tip]`——
  `disabled` 元素拿不到这两个事件。同格里另外两条禁用按钮用的是 `title`，口径不一致。
- 最小修法：这一条也换成 `title`，或在行上包一层带 `data-tip` 的 `span`。

### A5 非法落点静默失败

- 证据：落点语义（"插到某位成员之前"）只写在团队面板行的 `title`
  （`renderTeamMemberList`）里；员工栏拖拽条只写"拖拽调整发言顺序"；CSS 里准备了
  `content: attr(data-…)` 的落点文案钩子却没人填。`agentTeamOrderForDrag` 对不在顺序
  里的目标返回 `null`（单测锁了这条语义），`app.js` 的 drop 处理拿到 `null` 就 return。
- 最小修法：在 `app.js` 的 drop 里对 `null` 兜底 toast；给落点态补
  `data-team-drop-label`（或删掉这段死 CSS）。**不要**改
  `agentTeamOrderForDrag` 的返回语义（`agent-team-view.test.mjs` 有断言）。

### A6 键盘调序

- 证据：拖拽条是 `role="button" tabindex="0"`（员工库 `agent-team-view.js:224`、
  员工栏 `:444`），但 `app.js:2146-2251` 只注册了 drag 系列事件，没有 keydown。
- 最小修法：↑/↓ 换成 `agentTeamOrderForDrag(view, role, 前一位/后一位)`，复用同一条
  `AgentTeamSetOrder` 提交路径（注意：`nextAgentTeamOrder` 的 `up/down` 语义已被单测
  否决，不要复活）。

### A7 一屏三次表达顺序 / 口径打架

- 证据：员工栏的位置来自 `order_roles`（`#1/#2/#3`），发言调度串珠来自运行环
  `schedule.order`（"第 n 位"），`metaRow` 的 floor 直接显示逻辑名 `tl` 而不是
  `ADVISOR`；同一会话里 `main` 在员工栏是 `#2`、在串珠里是"第 1 位"。
- 最小修法：floor 文案改成"显示名（逻辑名）"，两处序号加前缀区分（"顺序 #n" vs
  "本轮第 n 位"）。风险：`agent-team-view.test.mjs` 有 `/发言中 · tl/` 断言，需
  用"追加"而不是"替换"的方式改文案。

---

## 3. 后端：能力缺口与一致性风险

### B1 整表级三动作与"没人读的默认顺序"

- `AgentTeamSaveCurrentTeam`（`gui/bridge.go:1080`）、`AgentTeamPublishToGlobal`
  （`:1178`）、`AgentTeamSetDefaultOrder` 在 GUI 已无入口；
  `team/order.json` 的唯一读点是 `AgentTeamGlobalConfig` 的**展示**字段
  （`application/core/agentteam_service.go:390-415`），运行时没有任何兜底读取点——
  也就是说"默认顺序"目前是第三份空转事实。
- 另外，`gui/README.md` 声明这三条"在 Bridge 面上保留（headless…）"，但
  `gui/headless_team.go:63-105` 的 `team.*` 只有 8 个方法，并不包含它们。
- 建议：要么给「默认顺序」接一个真实消费点（会话/团队都没有顺序时兜底），要么把
  这三条与 `order.json` 一起降级标注为"预留"，并同步 README 与 DTO。

### B2 母本写路径不原子

- 全局母本读改写是"读 → 改 → 整文件写"（`sessionstore/team_global.go` 一族，文件锁
  `teamConfigLocks` 只保进程内），普及到全局要写 `employees.json` + `order.json` +
  `library.json` 三个文件且没有事务；`SetOrder` 对顺序里的非法角色是**静默剔除**。
- 风险：并发/崩溃窗口里会丢条目；静默剔除会让"顺序改不动"看起来像 UI 故障。

### B3 `team_id` 回落与跨会话覆盖

- 会话存到全局/普及时的 `team_id` 回落到 `team_kind`，于是两支同名 kind 的会话会互相
  覆盖库条目；库条目与会话在编员工的口径也不一致。

### B4 装配语义

- 装配 = 把库条目**整份覆盖**写进会话（丢掉会话里已有的员工），且"写注册表 + 写
  lifecycle 顺序"两步非原子；顺序里出现非法项时，面板的顺序编辑会陷入不可自救状态。

### C1 运行态与写通道

- 真正推进环的 `Runtime.Next` / `TurnScheduler` 不在生产装配里；user 席位口径与
  逃生上限（轮次上限、连续无进展）目前**只有读面、没有写通道**，面板上只能看不能调。

---

## 4. 行业对照：什么值得抄、什么在窄栏里不可行

> 出处均为本次实际抓取到的官方文档/仓库原文；标 **【事实】** 者有链接，标 **【推断】**
> 者是我的判断。

1. **行序即发言次序 + 静音/强制发言**（SillyTavern Group Chat 的 Manual / Natural
   Order / List Order 与 Mute / Force Talk）**【事实】**
   → **高**：这正是 Seelex「员工栏」的范式。可低成本补两件：每行一个 muted 开关
   （静音=本轮不参与、强制=绕过顺序点名），以及"顺序被谁改了"的一句说明。
2. **拖拽之外的键盘通道**（Claude Code 的 agent panel 用 ↑↓ 选队友、Enter 打开
   transcript、Esc 打断）**【事实】**
   → **高**：对上 A6；键盘调序比拖拽在窄栏里更稳。
3. **空闲即收起**（Claude Code 的队友行在全部空闲 30 秒后自动隐藏，下次发言再出现）
   **【事实】** → **中高**：窄栏里"没有运行态的成员"可以折成一行，把纵向空间让给
   正在发生的事。
4. **画布/节点图**（AutoGen Studio Team Builder、Dify Workflow）**【事实】**
   → **低**：220–480px 里要放节点 + drop zone，命中区会小于 40px。要"图"的认知，
   建议只在团队库里放一张**只读缩略图**，点击进全屏抽屉。
5. **声明式逃生口**（AutoGen Studio 一键切 JSON、Claude Code 的 markdown+YAML
   subagent、n8n 的 draft/publish 两态）**【事实】**
   → **中高**：给"提示词 + 权限 + 类型"一份只读 JSON 视图（复制/粘贴即导入），比在
   窄栏里再塞第三次表单省地方；`draft/publish` 两态也正好对应 Seelex 已有的
   "会话副本 vs 全局母本"。
6. **能力只能在配置面改**（Dify 明确 agent 的能力只能在 Configure 里改，终端用户
   无法让它改自己的 prompt/skills）**【事实】** → **中高**：与 A1 同源，能力字段的
   写入面应当收窄而不是铺开。
7. **运行态多视图**（Temporal Web UI 的 Timeline / Compact / JSON，顶部固定
   Start/Close/Duration）**【事实】** → **中**：可借"折叠成一行"与"顶部固定关键
   指标"，横向时间线本身不适合窄栏。
8. **提示词体量预算**（Claude Code 对 subagent 定义有 token 预算告警）**【事实】**
   → **中**：员工多起来以后提示词会集体膨胀，面板里给一个体量提示比给一段说明有用。
9. **没有产品用"轮次环"表达发言顺序**【推断，基于本次全部调研对象】→ Seelex 的
   串珠条方向正确，只需要"当前这一段展开、其余折成一行"的折叠。
10. **窄栏里不要做的事**【推断】：节点图、多列能力表、横向时间线、轮次环——四者在
    220–480px 里都会退化成"挤字"，与 Seelex 前几轮已经清掉的噪声同类。

---

## 5. 建议的下一轮切分

- **第一批（纯前端、低风险、先修数据丢失与说不清）**：A2 / A3 / A4 / A5，外加
  A1 的**前端半边**（读写带全字段，后端补 `TeamMember` 两个字段）。
- **第二批（体验收口）**：A6 键盘调序、A7 口径统一、发言调度折叠 + 空闲收起。
- **第三批（需要拍板）**：B1 三动作与 `order.json` 的去留、B2 原子性、B3 `team_id`
  口径、C1 user 席位与逃生上限的写通道。
