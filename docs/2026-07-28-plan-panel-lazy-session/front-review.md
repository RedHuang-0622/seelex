# Plan 常驻面板与会话懒创建前置审查

## 需求摘要

修正 GUI 的两个产品语义：Plan 必须在右侧常驻面板实时展示；点击“新建会话”只进入未持久化草稿态，直到第一次提交真实对话请求时才创建 Engine Session、绑定项目并进入会话列表。

## 当前事实

1. `#plan-view` 当前位于默认隐藏的 `runtime-modal`，右侧常驻面板没有 Plan 容器；`plan_load` 成功不会自动打开 Runtime 弹窗。
2. GUI 的“新建会话”按钮直接执行 `Submit("/new")`。
3. `/new` 会立即保存当前 Session、调用 `Engine.StartSession()`、绑定新 Session 与当前项目，并更新 Snapshot，因此每点击一次都会生成一个新 ID。
4. `renderSessions` 会把 Snapshot 中尚未持久化的当前 Session 插入列表，进一步造成“点击即创建”的可见效果。
5. Plan 后端状态没有被 `refreshRuntimeLocked` 覆盖；当前主要问题是挂载位置和可见性，不是 `plan_load` 解析失败。

## 目标语义

```text
点击“新建会话”
  -> 保存有实际历史的旧会话
  -> Application 进入 draft session
  -> 清空当前对话展示和旧 Plan
  -> 不调用 Engine.StartSession
  -> 不创建项目/session binding
  -> 不向 SessionStore 写入空会话

重复点击
  -> 幂等，仍是同一个 draft

第一次提交真实对话
  -> Engine.StartSession
  -> 继承当前项目读写范围
  -> 建立 session/project binding
  -> 使用首个问题作为展示名称
  -> 启动 ChatStream
  -> 成功执行后按现有原子存储接口保存
```

普通命令、Plugin 切换以及不带执行参数的 Skill 选择不应物化 draft；真正进入 `submitConversation` 的输入才物化会话。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `application/model/state.go` | 修改 | `SessionState` | 增加 additive `draft` 展示状态，区分未物化会话与真实 Session ID。 |
| `application/core/service.go` | 修改 | `Service`、Session/Workspace 用例 | 增加串行化的 draft transition、`BeginNewSession`、首次请求物化逻辑；恢复会话时退出 draft；项目切换时不得绑定空 Session ID。 |
| `application/core/chat.go` | 修改 | `submitConversation`/`startChat` 前置路径 | 第一次真实请求开始前物化 draft，保证 Session ID、项目 binding 和 Chat request 同步建立。 |
| `application/core/service_test.go` | 修改 | Session/Workspace 测试 | 覆盖点击不创建、重复点击幂等、首次发送创建、项目继承、恢复历史退出 draft。 |
| `application/core/command_test.go` | 检查/修改 | `/new` 测试 | 保留 CLI `/new` 兼容时明确它与 GUI draft API 的差异，避免旧测试误约束 GUI。 |
| `application/model/README.md` | 修改 | Session DTO | 记录 `draft` 是展示/生命周期状态，ID 仍是唯一持久化键。 |
| `application/core/README.md` | 修改 | Session 生命周期 | 记录 draft 到 materialized session 的状态转换和项目绑定时机。 |
| `gui/bridge.go` | 修改 | `Application` 与 `Bridge` | 暴露窄接口 `BeginNewSession()`，GUI 不再通过字符串命令模拟业务用例。 |
| `gui/bridge_test.go` | 修改 | fake application、转发和嵌入布局测试 | 验证新用例转发，并约束 `plan-view` 位于右侧面板而非 Runtime modal。 |
| `gui/frontend/dist/index.html` | 修改 | `right-panel`、`runtime-modal` | 将 Plan section 移到右侧常驻面板，移除隐藏弹窗内的主展示入口。 |
| `gui/frontend/dist/app.js` | 修改 | elements、`renderSessions`、`renderPlan`、new-session handler | 显示 draft 占位会话；按钮调用 `BeginNewSession`；无 Plan 时隐藏 section，有 Plan 时常驻更新。 |
| `gui/frontend/dist/styles.css` | 修改 | Plan/right-panel 样式 | 适配 300px 右栏和 draft session 状态，保持面板可滚动。 |
| `gui/frontend/README.md` | 修改 | Plan 与 Session 说明 | 同步实际挂载位置和懒创建行为。 |

## 依赖分析

- 上游输入：GUI 新建按钮、普通文本提交、Skill 带参数提交、历史会话恢复、项目切换。
- Application 是 draft 状态的唯一事实源；GUI 不维护独立的 `pendingNewSession` 布尔值，避免 refresh/event 后丢失。
- `ChatEngine.StartSession` 仍是 Session ID 的生成入口，只是调用时机从点击按钮移动到第一次 `submitConversation`。
- `SessionPort` 和 `sessionstore.Repository` 接口无需修改；draft 不进入持久化层。
- `WorkspacePort.BindSession` 只接收已经物化的 Session ID，不允许使用空 ID 或临时名称。
- Plan 数据协议 `runtime.plan` 和 `seelex.plan/v1` 无需改变，只迁移 DOM 挂载点。

## 循环依赖检查

- [x] Application 不依赖 GUI；GUI 继续只消费 Application API 和 DTO。
- [x] SessionStore 不感知 draft，不新增反向依赖。
- [x] Plan renderer 继续是纯 `runtime.plan -> DSL -> DOM` 转换。

## 风险预估

- 并发点击新建与发送：中概率、高影响。需要独立 session transition mutex，保证最多调用一次 `StartSession`。
- draft 期间切换项目：中概率、高影响。只能更新待继承的项目范围，不能绑定空 Session ID。
- draft 期间恢复历史：中概率、中影响。恢复成功后必须清除 draft 状态。
- 保存空会话：高概率、中影响。进入 draft 前仅在 Engine 有实际历史时保存当前会话。
- 首次请求失败：中概率、中影响。请求已经发送就视为 Session 已创建，即使模型调用失败也保留真实 Session ID。
- GUI 状态漂移：中概率、中影响。禁止仅在浏览器内维护 draft，必须由 Snapshot/Event 同步。

## 建议方案

1. 在 Application 增加显式 `BeginNewSession()` 用例和 `SessionState.Draft`，不复用 `/new` 字符串命令作为 GUI API。
2. `BeginNewSession` 幂等地保存旧的非空历史、清空展示状态并进入 draft，但不触碰 Engine Session ID 和存储层。
3. `submitConversation` 在创建 chat request 之前调用 `materializeDraftSession`；该函数串行执行 `StartSession`、项目 binding 和 Snapshot Session 更新。
4. `resumeSession`、项目绑定/解绑和删除逻辑显式处理 draft，禁止空 Session ID 进入 Workspace binding。
5. GUI 把 Plan section 放到 `right-panel` 的状态区之后；`renderPlan` 控制整个 section 的显示，而 `reconcilePlanDSL` 继续负责节点增量更新。
6. 更新 Go 契约测试和嵌入前端布局测试，再运行 Node Plan DSL 测试、Application/GUI 测试及全量 Go 测试。

## 验收标准

- 连续点击新建按钮任意次数，Engine `StartSession` 调用次数保持为 0，SessionStore 不增加空记录。
- 点击后界面显示一个选中的“新会话（发送后创建）”草稿项，旧对话和旧 Plan 不再展示。
- 第一次发送普通问题时 `StartSession` 恰好调用一次，Session 获得真实 ID，并继承当前项目。
- draft 列表标题固定显示“新会话（发送后创建）”；物化后立即使用首个问题作为 `name`，列表操作仍只使用 ID。
- 第二次及后续发送不再创建新 Session。
- 恢复已有会话会退出 draft，并恢复该会话自己的项目和历史。
- `plan_load` 后无需打开 Runtime 弹窗，右侧 Plan section 即可显示；节点事件继续增量更新。
- 无 Plan 时右侧不保留空白 Plan 卡片，完成后保留最终结果直到 clear/new/resume。
