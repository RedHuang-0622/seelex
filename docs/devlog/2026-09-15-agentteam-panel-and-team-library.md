# 2026-09-15 Agent Team 面板交互改版 + 员工提示词/权限装配 + 项目团队库

## 一、问题

1. **提示词"存了但没用"**：`RoleSpec.SystemPrompt` 能落盘、能回传，但入职表单里
   没有这个字段，运行时也没有任何消费者（ADVISOR 的 system prompt 硬编码在
   `seelebridge/runtime_goal_tl.go`）。
2. **权限只存不显**：`tools_policy` 同样只落盘，UI 上看不见、运行时也不拦截。
3. **团队是散装的**：只有代码里的三个 preset，没有"用户自己的团队"这回事，
   也没有能跨会话复用的团队模板库；面板里为一堆"装配 X"按钮。
4. **交互问题**：发言顺序要跑到另一张表里用 ↑/↓ 调；入职表单常驻占满窄右栏；
   右栏一进来「状态 / 账户 / Agent Team」三块全展开；选中页签只是一条下划线；
   对话里 EXEC 与 ADVISOR 的正文视觉上完全一样（provider role 都是 assistant）。

## 二、改动

### 1. 后端：项目级团队库（新持久事实）

- `sessionstore/team_library.go`：`project-<hash>/teams/library.json`，与
  `session/team/roles.json` 同形态（整份原子替换、按路径写锁、schema 版本、
  重复 team_id / 重复角色显式报错、顺序表**保序**并强制含 user/main）。
- `application/core/agentteam/library.go`：`Library` 读写面（`View`/`Entry`/
  `SaveTeam`/`DeleteTeam`）与投影（`SpecOfEntry`/`EntryFromRegistry`/
  `EntryFromSpec`）。
- `application/core/agentteam_service.go`：`AgentTeamLibrary` /
  `AgentTeamSaveTeam` / `AgentTeamSaveCurrentTeam` / `AgentTeamDeleteTeam` /
  `AgentTeamMaterializeTeam`（库条目 → `TeamSpec` → 既有工厂，不新增装配路径）。
- `internal/adapters/agentteam_ports.go`：团队库 DTO ↔ 存储映射（项目作用域由
  锚定会话解析）。
- `gui/bridge.go`：新增 5 个团队库 Bridge 方法 + 2 个提示词方法，并把
  `agentTeamApplication` 接口扩到新面（编译期用真实 `*application.Service` 钉死）。

### 2. 后端：员工提示词真正生效 + 一次有界优化

- `seelebridge/runtime_role_prompt.go`（新）：
  - `SetRolePromptProvider` / `rolePromptFor`：装配根注入"按角色名读已登记提示词"
    的读面（`main.go` 用 `app.AgentTeamRolePrompt(app.Snapshot().Session.ID, …)`）。
  - `OptimizeRolePrompt`：实现 `contract.RolePromptPort`，一次 LLM 回合产出
    `{optimized, notes}` 候选；**不落盘、不写会话消息**。
- `seelebridge/runtime_goal_tl.go`：ADVISOR 的 system prompt 拆成"角色设定"与
  "输出契约"；角色设定优先取登记提示词，**输出契约永远追加**（goal 域要解析
  `TLDirective`，不能被员工提示词改掉）。
- `dto`：`TeamMember` 增加 `system_prompt`/`model_policy`/`presence_policy`
  回读字段（编辑面板必须回填，否则一次编辑会清空登记值）；新增 `TeamLibrary`/
  `TeamLibraryEntry`/`RolePromptOptimizeRequest|Result` 与 `ToolPolicy*` 口径常量。

### 3. 前端：面板与交互

- `agent-team-view.js`：面板改为「团队库小表（含内置模板行 / + 新建团队）」
  +「员工栏（员工 + 发言顺序 + 权限/提示词 chip + 拖拽条）」+「Team 栏」；
  新增 `hirePanel`/`teamEditorPanel`（冷加载）与 `agentTeamOrderForDrag`
  （拖拽排序纯函数）、`toolsPolicyLabel`、`normalizeTeamLibrary`。
- `app.js`：团队库状态与拉取、冷加载面板开关（`✕`/取消/Esc）、拖拽（HTML5 DnD，
  落点确定即提交整表）、提示词优化与「应用到提示词」、团队库 CRUD 接线。
- `index.html`：`#status-panel` / `#accounts-section` 去掉 `open`（与
  `#team-section` 一致，默认收起）。
- `styles.css`：右栏选中页签改纸质书签拟物（纸色票签 + 底部尖角 + 纸面高光）；
  EXEC/ADVISOR 对话背景带（冷钢蓝 / 暖黄 + 左侧色条）；团队库表、冷加载面板、
  拖拽落点与权限 chip 样式。
- `components.js`：`messageRoleClass` 给消息加 `is-exec`/`is-advisor`/`is-role`。

## 三、验证证据

```text
gofmt -l .                                     → 无输出
go build ./...                                 → ok
go vet ./gui/... ./application/... ./sessionstore/ ./seelebridge/ ./internal/... → ok
go test ./gui/... ./application/core/... ./sessionstore/ ./seelebridge/ ./internal/adapters/ -count=1 → 全 ok
go test ./e2e/ -run 'README|DocumentationRules' → ok
node --test gui/frontend/dist/*.test.mjs       → 296 pass / 0 fail
```

新增/更新的守卫用例：

- `sessionstore/team_library_test.go`：路径布局、整份往返（顺序表保序）、重复
  team_id/角色报错、未知 schema 拒绝、项目隔离。
- `application/core/agentteam/library_test.go`：按 team_id 幂等覆盖、删除幂等、
  未知团队返回 `ErrUnknownTeam`、`SpecOfEntry` 保提示词/权限、
  `EntryFromRegistry` 丢弃 user/main、preset 可复制成库条目。
- `application/core/agentteam_library_service_test.go`：当前会话 → 库条目 → 装配
  回会话的端到端（含提示词/权限保留）；提示词读面与优化端口（未装配时报错）。
- `seelebridge/runtime_role_prompt_test.go`：登记提示词替换内置角色设定但契约
  永远追加、空白登记回退、优化输出 JSON 解析（围栏/噪声/缺字段/notes 截断）。
- `gui/bridge_team_test.go`：新 Bridge 方法的参数归一与窄转发、未装配时报错。
- `gui/frontend/dist/agent-team-view.test.mjs`：团队库表、员工栏（拖拽条/权限
  chip/冷加载槽位默认空）、两个冷加载面板、拖拽排序纯函数、转义。
- `gui/frontend/dist/components.test.mjs`：EXEC/ADVISOR 背景 class 分区。

## 四、边界与未做

- **权限只登记 + 展示**：`tools_policy` 落盘并在面板可见，但运行时按角色的工具
  拦截尚未接线（真正拦截在 seelebridge `PermissionGate`，按会话/全局）。UI 与
  文档如实标注，不假装生效。
- **装配不清空既有员工**：把团队 B 装配到已装团队 A 的会话，是"写 B 的员工 +
  把顺序换成 B"，A 的专属员工会留在注册表里、显示为"未排入顺序"（与 preset
  装配同口径，`DesignNotice` 会报出来）。是否要"装配即替换"需要产品口径。
- **只有 ADVISOR 有回合**：其余注册角色（reviewer/researcher/自定义 agent）没有
  运行时执行者，它们的提示词只是登记事实。
- **提示词优化依赖 LLM**：未配置可用 completer 时应用层返回可展示错误，不静默
  返回原文。
