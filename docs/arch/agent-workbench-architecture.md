# Agent Workbench 总体架构

> 状态：拟议方案
> 日期：2026-07-24
> 产品输入：[`../product/agent-workbench/prd.json`](../product/agent-workbench/prd.json)
> 适用范围：JSON DSL 对话卡片、Agent E2E、右栏 Workspace 沙盒

## 1. 设计结论

未来工作台采用三条相互解耦、通过协议联动的主链：

1. **DSL 卡片属于 Conversation**：Agent 通过 `render_card` 工具提交 JSON，Core 校验后生成 `ConversationItem(kind=card)`，GUI 在对话区渲染。通用 `surface` 模型为未来保留，但 `seelex.card/v1` 只允许 `target.kind=conversation`。
2. **E2E 使用分层确定性方案**：Go headless scenario 验证真实 Application 状态机，Playwright + fake Wails Bridge 验证真实前端交互，Windows Wails smoke 验证桌面容器；真实模型只进入 opt-in nightly。
3. **Workspace 是专用右栏，不是 DSL 容器**：右栏固定提供 Overview、Files、Changes、Artifacts；文件系统边界由后端 `WorkspacePort + PathGuard` 强制执行。卡片只通过 `FileLink/ArtifactLink` action 请求右栏定位资源。

## 2. 当前基线与缺口

| 领域 | 当前基线 | 缺口 |
|------|----------|------|
| Conversation | `Message{content, tool}`、稳定 UI ID、keyed DOM | 没有结构化卡片 item、schema 或持久化 |
| Event | protocol v1、seq/revision、Snapshot fallback | 新 Card/Workspace event 会让旧 reducer 持续刷新 |
| GUI | Wails + 原生 HTML/CSS/ES Modules，无 npm runtime | 不能直接套用研究稿中的 React/A2UI runtime |
| E2E | Go/Node 单元与契约测试、真实 WebView 手工 | 没有完整用户旅程和浏览器 DOM 自动化 |
| 右栏 | 静态 Project、状态、资料来源 | 无目录、预览、diff、产物或资源定位 |
| 安全 | Permission Gate、ApprovalBroker | ProjectRoot 尚未成为所有文件查询的强制路径边界 |

## 3. 设计目标与非目标

### 目标

- 让结构化结果成为可恢复的 Conversation 数据，而不是前端猜测 Markdown。
- 保持 Application Core 为唯一业务事实源，前端只做受控 presentation 与交互态。
- 让右栏可处理大型 Workspace，但不把目录树和文件正文塞入 Snapshot。
- 主 CI 不依赖网络、真实模型或外部服务，失败可复现并有完整 trace。
- 建立清晰的安全边界：声明式卡片、相对资源 ID、后端路径守卫、现有审批链。

### 非目标

- 不在第一阶段更换 Wails 或迁移 React/Tauri。
- 不执行 Agent 生成的代码、HTML、CSS 或动态组件。
- 不把任意 DSL surface 放进 Workspace；Workspace 使用固定领域视图。
- 不把右栏扩展成完整 IDE、终端或多根远程文件系统。

## 4. 方案对比

### 4.1 DSL 卡片入口

| 维度 | A：前端解析 Markdown 中 JSON | B：Core `render_card` 工具 + typed protocol | C：引入 React/A2UI runtime |
|------|-----------------------------|--------------------------------------------|---------------------------|
| 角色边界 | 差，普通文本隐式变执行协议 | 好，结构化入口显式 | 好 |
| 安全性 | 低，易混入 HTML/action | 高，Core 与前端双校验 | 中高，仍需本项目权限适配 |
| 持久化 | 难，依赖文本重解析 | 清晰，Card mutation 独立存储 | 清晰但需整体迁移 |
| 当前栈改动 | 小 | 中 | 很大 |
| 流式更新 | 脆弱 | 原生 event/patch | 原生 |
| 测试 | 文本样例易漏 | schema fixture + Core + renderer | 需新工具链和组件测试 |
| 回滚 | 中 | 高，可回退 ErrorCard/Markdown | 低 |

推荐 **B**。最大风险是协议和 Session transcript 需要升级，但这是恢复、E2E 和未来 sidecar 都需要的基础投资。

### 4.2 Agent E2E

| 维度 | A：只跑真实模型 + Wails | B：Go scenario + Playwright fake Bridge + Wails smoke |
|------|--------------------------|-------------------------------------------------------|
| 确定性 | 低 | 高 |
| PR 速度 | 慢且受网络影响 | 快，主路径无网络 |
| Core 覆盖 | 间接、难定位 | 真实 Service/ports 状态机 |
| DOM 覆盖 | 有 | Playwright 有，Wails smoke 补容器差异 |
| 成本 | token 与外部服务成本高 | fixture 维护成本中等 |
| 真实度 | 单次高、重复性低 | 分层互补，可选 live nightly |

推荐 **B**。真实模型验证保留，但不作为每个 PR 的强制门禁。

### 4.3 Workspace 后端

| 维度 | A：Bridge 直接读文件 | B：Application `WorkspacePort` + guarded adapter |
|------|----------------------|---------------------------------------------------|
| 复用 | 仅 GUI | TUI、GUI、sidecar 可复用 |
| 安全边界 | 分散在 Bridge 方法 | 集中 PathGuard、Policy、审计 |
| 可测试性 | 依赖桌面层 | 临时目录即可完整测试 |
| DTO 一致性 | UI 私有 | Snapshot/Event/Query 统一 |
| 实现成本 | 低 | 中 |

推荐 **B**。Bridge 继续保持薄适配，Workspace 视图不是文件系统权限实现。

## 5. 总体上下文

```text
                    ┌──────────────── Seele Engine ────────────────┐
User prompt ───────►│ Chat / Tool loop / Permission middleware     │
                    └─────────────┬───────────────────┬─────────────┘
                                  │ render_card       │ file/tool actions
                                  ▼                   ▼
                    ┌──────────────── application.Service ────────────────┐
                    │ Turn + ConversationItem state                       │
                    │ Presentation coordinator                            │
                    │ Workspace summary + query orchestration             │
                    │ ApprovalBroker + EventHub + Session coordination     │
                    └─────────┬─────────────┬──────────────┬───────────────┘
                              │             │              │
                       PresentationPort  WorkspacePort  TranscriptPort
                              │             │              │
                              ▼             ▼              ▼
                      JSONL/card store  guarded FS/git  session sidecar
                              │             │
                              └──────┬──────┘
                                     ▼
                             Snapshot / Event v2
                                     │
                              gui.Bridge / Wails
                                     │
               ┌─────────────────────┴──────────────────────┐
               ▼                                            ▼
      Conversation renderer                         Workspace controller
  text/tool/DSL card in center                 Overview/Files/Changes/Artifacts
               │                                            ▲
               └── FileLink / ArtifactLink action ──────────┘
```

## 6. 模块边界

| 模块 | 单一职责 | 不能承担 |
|------|----------|----------|
| `application` | Turn、Conversation、Card/Workspace 业务编排和公开协议 | OS 路径细节、DOM、Playwright |
| `presentation`（计划新增） | Card schema、mutation、持久化、限额 | 执行 action、渲染 HTML |
| `workspace`（计划新增） | root、PathGuard、目录/预览/diff/artifact adapters | Chat、DSL 渲染、审批 UI |
| `session` | Engine history + transcript/presentation metadata 的原子会话 bundle | Workspace 文件内容 |
| `gui.Bridge` | Wails 方法/event 薄适配 | 路径授权、Card 业务校验 |
| `dsl-runtime.js` | 前端二次校验、registry、DOM presentation model | 修改 Core Snapshot、执行任意 action |
| `workspace-view.js` | 右栏查询与本地展开/选中状态 | 直接访问 filesystem |
| `e2e` | scenario、driver、fixture、trace | 生产业务实现 |

## 7. Protocol v2

DSL 与 Workspace 会新增 item/event/capability。当前 v1 客户端遇到未知 event 会 refresh，若 Snapshot 仍无法表达新能力会形成无意义循环，因此本方案显式升级为 v2。

### 7.1 Conversation item

```go
type ConversationItem struct {
    ID        string            `json:"id"`
    TurnID    string            `json:"turn_id"`
    Kind      string            `json:"kind"` // text | tool | tool_result | card | error
    Role      string            `json:"role"`
    Text      *TextContent      `json:"text,omitempty"`
    Tool      *ToolCall         `json:"tool,omitempty"`
    Card      *CardSurface      `json:"card,omitempty"`
    CreatedAt time.Time         `json:"created_at"`
}
```

`kind=card` 是 Conversation 的一等 item。它按 item sequence 出现在中间对话区，不进入 Workspace DOM。

### 7.2 Card surface

```go
type CardSurface struct {
    SchemaVersion string                   `json:"schema_version"` // seelex.card/v1
    ID            string                   `json:"id"`
    Revision      uint64                   `json:"revision"`
    Target        CardTarget               `json:"target"`
    Root          string                   `json:"root"`
    Components    map[string]CardComponent `json:"components"`
    Data          map[string]any           `json:"data,omitempty"`
    Status        string                   `json:"status"`
    Error         *CardError               `json:"error,omitempty"`
}

type CardTarget struct {
    Kind   string `json:"kind"` // v1: conversation only
    TurnID string `json:"turn_id"`
}
```

通用化体现在 `CardTarget` 和 `CardSurface` 不依赖 DOM；产品约束由 validator 强制 v1 只能选择 conversation。未来若增加 dashboard surface，必须新增 capability/schema 版本，不能偷偷把 v1 Card 挂到 Workspace。

### 7.3 Workspace summary 与查询 DTO

Snapshot 只包含：

```go
type WorkspaceState struct {
    ID             string `json:"id"`
    Name           string `json:"name"`
    Revision       uint64 `json:"revision"`
    Policy         string `json:"policy"`
    FileCount      int    `json:"file_count,omitempty"`
    ChangeCount    int    `json:"change_count,omitempty"`
    ArtifactCount  int    `json:"artifact_count,omitempty"`
    IndexStatus    string `json:"index_status"`
}
```

目录、预览、diff 与 artifacts 使用 Bridge query DTO，均带 `workspace_id + revision + cursor + truncated`，不参与普通 Snapshot 重同步 payload。

### 7.4 Event kinds

| Event | Payload | reducer 行为 |
|-------|---------|--------------|
| `conversation.item.added` | ConversationItem | 按 item ID upsert |
| `conversation.text.delta` | item ID + delta | 只更新 text item |
| `card.surface.updated` | item ID + CardSurface 或受限 patch | 更新对应 card item |
| `workspace.changed` | WorkspaceState | 更新摘要并失效相关 query cache |
| `artifact.registered` | ArtifactSummary | 更新计数，Artifacts tab 按需刷新 |

Snapshot 仍是权威恢复点；任何 patch 基线不匹配、revision 跳跃或目标不存在都触发 refresh。

## 8. 调用方接口

接口定义在使用方 `application`，实现通过 `Dependencies` 注入。

```go
type PresentationPort interface {
    Apply(context.Context, CardMutation) (CardSurface, error)
    LoadSession(context.Context, string) ([]CardRecord, error)
    DeleteSession(context.Context, string) error
}

type WorkspacePort interface {
    Summary(context.Context) (WorkspaceState, error)
    List(context.Context, WorkspaceListRequest) (WorkspacePage, error)
    Preview(context.Context, WorkspacePreviewRequest) (FilePreview, error)
    Diff(context.Context, WorkspaceDiffRequest) (DiffPreview, error)
    Artifacts(context.Context, ArtifactListRequest) (ArtifactPage, error)
    ResolveResource(context.Context, string) (WorkspaceResource, error)
}

type TranscriptPort interface {
    Append(context.Context, string, ConversationItem) error
    UpdateCard(context.Context, string, string, CardSurface) error
    Load(context.Context, string, TranscriptWindow) ([]ConversationItem, error)
}
```

只有只读查询进入 `WorkspacePort`。写入、删除和命令继续作为 Tool action 进入 Seele Permission Gate；不能通过 `WorkspacePort` 绕过审批。

## 9. 核心流程

### 9.1 JSON → 对话区 DSL 卡片

```text
Agent tool call render_card(args)
  → tool schema 基础校验
  → Application.RenderCard
  → presentation validator
       schema/version/size/depth/type/ref/action/URL/resource ID
  → PresentationPort.Apply + TranscriptPort.Append/Update
  → append/upsert ConversationItem(kind=card)
  → bump Snapshot revision
  → card.surface.updated Event
  → reducer 更新 conversation item
  → ConversationView 调用 CardRenderer
  → registry 白名单生成 DOM
```

校验失败返回结构化 tool error，让 Agent 可以修正；GUI 同时保留安全 ErrorCard，不渲染半有效树。

### 9.2 Card action → Workspace 定位

```text
User click FileLink
  → CardActionController 校验 action 名与 user gesture
  → Bridge.ResolveCardAction(cardID, actionID)
  → Application 检查 card/action/revision
  → WorkspacePort.ResolveResource(opaque ID)
  → 返回 WorkspaceNavigation{tab:"files", resource_id, range}
  → WorkspaceView 切 tab、加载父目录、定位预览
```

Card 不携带任意绝对路径。Workspace 也不接受前端直接拼接的系统路径。

### 9.3 Agent E2E

```text
scenario-v1.json
  ├─ Go ScenarioRunner → real application.Service + scripted ports
  │    └─ event.jsonl + final snapshot
  └─ Playwright Driver → real dist assets + fake Wails bridge
       └─ trace.zip + screenshots + console.json

nightly (optional)
  └─ real Wails smoke / live model scenario with budget + secret gate
```

## 10. 持久化与恢复

Engine History 是模型上下文，Transcript 是用户界面上下文，两者不能继续被视为同一种数据。

Session bundle 计划包含：

```text
session/<id>/
  engine-history.json      # Engine 上下文
  transcript.jsonl         # stable turn/item/card sequence
  presentations.jsonl      # card mutation log，可压缩
  artifacts.json           # 产物元数据，不含任意外部文件正文
  manifest.json            # schema versions、hash、提交状态
```

保存顺序使用临时文件 + manifest 最后原子替换。恢复时 manifest 不完整则回退上一个完整 generation；Card schema 未知则生成只读 ErrorCard，不阻塞文本历史。

## 11. Workspace 安全模型

信任边界：前端 path、Agent action、DSL resource ID、Workspace 文件内容全部不可信。

PathGuard 每次 IO 执行：

1. 只接受 UTF-8 slash 相对路径或后端签发的 opaque resource ID；
2. 拒绝 NUL、绝对路径、Windows drive/UNC、空 segment 和 `..`；
3. 以构造注入的 canonical root 解析；
4. 默认不跟随符号链接/reparse point，允许时必须确认最终目标仍在 root；
5. 用 `filepath.Rel` 与平台大小写规则再次检查；
6. 打开前重新校验，返回的 DTO 永不暴露未经授权的绝对路径；
7. 预览应用 byte/line/mime/binary 限制。

Workspace policy：

| Policy | 查询 | 写/删/命令 |
|--------|------|------------|
| `read_only` | 允许受限只读 | 拒绝 |
| `approval_required` | 允许受限只读 | Permission Gate + ApprovalBroker |
| `full_access` | 允许受限只读 | 仍走 Tool middleware，仅免人工确认 |

右栏不能切换 `full_access`；策略只由启动参数或受信配置决定。

## 12. 配置硬度

| 配置 | 等级 | 策略 |
|------|------|------|
| schema version、允许的 action 名 | 🔴 常量 | 代码与 fixture 固定 |
| Card 256 KiB、200 components、depth 12 | 🟠 默认+覆盖 | 管理员可降低，Agent/前端不可提高 |
| preview bytes/lines、directory page size | 🟠 默认+覆盖 | 构造 options |
| Workspace root、stores、clock、ID generator | 🔵 构造注入 | 测试用 fake/temp root |
| live model E2E provider/预算 | 🟡 环境变量 | 只在显式 workflow 使用 secret |
| DSL/Workspace rollout | ⚪ feature flag | `capabilities.dsl_cards/workspace` |

## 13. 循环依赖检查

```text
gui ───────────────► application contracts
presentation ──────► application contracts（实现 PresentationPort）
workspace ─────────► application contracts（实现 WorkspacePort）
session ───────────► application contracts（实现 TranscriptPort）
main ──────────────► application + presentation + workspace + session + gui
application ───────► 不 import presentation/workspace/gui
```

不存在 `application → workspace → application` 循环：Application 只知道接口；实现包可以 import 调用方契约，由 composition root 注入。

## 14. 实施步骤

| # | 步骤 | 计划文件 | 模式 |
|---|------|----------|------|
| 1 | 冻结 Card/Scenario/Workspace schema 与 limits | `schemas/*.json`、fixtures | Schema-first |
| 2 | 建立 protocol v2 ConversationItem/Capabilities | `application/state.go`、`event.go` | Versioned DTO |
| 3 | 实现 Card validator/store 与 `render_card` adapter | `presentation/`、`main.go` | Strategy + Adapter |
| 4 | 实现对话区 Card registry/renderer/action controller | `gui/frontend/dist/dsl-*.js` | Registry + Strategy |
| 5 | 建立 Go scenario runner 和 Playwright fake Bridge | `e2e/`、`gui/e2e/` | Driver + Adapter |
| 6 | 实现 Workspace PathGuard 和只读 adapters | `workspace/` | Facade + Policy |
| 7 | 扩展 Bridge queries 与右栏 Controller | `gui/bridge.go`、`workspace-view.js` | Caller-owned interface |
| 8 | 接通 FileLink/ArtifactLink 与右栏导航 | DSL action + Workspace resolve | Mediator |
| 9 | 增加 Session bundle、迁移与恢复 | `session/`、`application/app.go` | Repository |
| 10 | 加固 CI、Wails smoke、指标和文档 | workflow、tests、docs | Quality gate |

M1、M2 和 M3 在 M0 契约冻结后可并行；M4 必须等待三条主链的垂直切片完成。

## 15. 测试策略

| 层 | 核心用例 |
|----|----------|
| Schema fixture | valid/invalid JSON、unknown type、ref cycle、size/depth/action/URL |
| Go unit | Card validator、patch、PathGuard、resource ID、artifact hash |
| Application contract | Card item/event、Snapshot resync、Session restore、Workspace query failure isolation |
| Node unit | registry、safe renderer、keyed update、action allowlist、Workspace controller |
| Playwright | 键盘、焦点、scroll、approval、Card action、右栏定位、resume |
| Race | Card mutation + Snapshot、Workspace index invalidation、session save |
| Security | XSS、script URL、path traversal、symlink/reparse、binary/large file、secret redaction |
| Wails smoke | startup、ready、Submit、event relay、shutdown |

## 16. 回滚与降级

- Feature flag 关闭 DSL 时，`render_card` 工具不注册；历史 Card 显示只读 JSON 摘要或 ErrorCard。
- Workspace adapter 初始化失败时，Capabilities 标记 unavailable，右栏保留当前 Project Overview 与重试入口。
- protocol v2 客户端不连接 v1 Core，错误必须明确；同一发布包中的 Core/embedded GUI 保持原子版本。
- Playwright job 可暂时从 required 降为 advisory，但 Go scenario 与 schema/security tests 不可跳过。
- Session migration 失败时只读打开旧文本历史，不写回覆盖原数据。

## 17. 模块详设

- [JSON DSL 对话卡片](../gui/modules/dsl-card-runtime.md)
- [Agent E2E 交互](../gui/modules/agent-e2e-interaction.md)
- [右栏 Workspace 沙盒](../gui/modules/workspace-sandbox.md)
