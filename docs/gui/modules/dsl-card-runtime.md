# JSON DSL 对话卡片模块详细设计

> 状态：拟议方案
> 产品需求：`CAP-DSL`
> 总体架构：[`../../arch/agent-workbench-architecture.md`](../../arch/agent-workbench-architecture.md)

## 1. 职责与位置

DSL 卡片是 **Conversation 内容项**。它与文本消息、Tool Card、审批提示按时间顺序出现在中间对话区，不是右侧 Workspace 的内容。

模块负责：

- 接收 Agent `render_card` 工具的 JSON mutation；
- 在 Core 中完成 schema、限额、引用、action 与资源校验；
- 把合法 CardSurface 绑定到 session/turn，持久化并发布事件；
- 在 ConversationView 中用白名单 registry 渲染原生卡片；
- 处理增量 patch、错误降级、恢复和用户 action；
- 把 FileLink/ArtifactLink action 中介到右栏 Workspace 定位。

模块不负责：

- 在 Workspace 中渲染任意 DSL；
- 执行 HTML、JavaScript、CSS、表达式或远程组件；
- 绕过 Core 直接提交工具、文件写入或审批决定；
- 从普通 Markdown 自动发现并执行 JSON。

## 2. 通用 surface 与产品约束

底层数据结构使用 `CardSurface`，使 validator、store 和 renderer 不依赖具体 DOM 容器。通用化只用于复用协议与实现，不扩大 v1 产品范围。

```text
CardSurface
  ├─ target.kind = conversation  ← seelex.card/v1 唯一允许值
  ├─ target.turn_id
  ├─ root component ID
  ├─ components adjacency map
  └─ data model
```

如果未来需要 dashboard、report 或 artifact preview，应发布新 capability/schema 版本。Workspace 仍优先使用固定 Files/Changes/Artifacts 领域视图，不因为 surface 通用就自动接收 DSL。

## 3. Agent 工具契约

计划注册专用工具：

```text
render_card(operation, surface_id, expected_revision, target, root, components, data, patch)
```

### Operation

| operation | 必需字段 | 语义 |
|-----------|----------|------|
| `upsert` | target/root/components | 创建或完整替换结构；已有 surface 需 revision precondition |
| `patch` | surface_id/expected_revision/patch | 只更新允许的数据或状态路径 |
| `delete` | surface_id/expected_revision | 删除卡片 item；保留审计 tombstone |

工具 schema 是第一层校验，Application validator 是不可绕过的第二层校验。无效请求返回稳定错误码，允许模型修正，但不自动放宽限制。

### Upsert 示例

```json
{
  "operation": "upsert",
  "surface_id": "card_perf_review",
  "target": {
    "kind": "conversation",
    "turn_id": "turn_42"
  },
  "schema_version": "seelex.card/v1",
  "root": "root",
  "components": {
    "root": {
      "type": "Card",
      "props": {
        "title": "性能审查"
      },
      "children": [
        "metrics",
        "diff",
        "file"
      ]
    },
    "metrics": {
      "type": "Grid",
      "props": {
        "columns": 2
      },
      "children": [
        "before",
        "after"
      ]
    },
    "before": {
      "type": "Metric",
      "props": {
        "label": "修改前",
        "value": {
          "$bind": "/metrics/before"
        },
        "unit": "ms"
      }
    },
    "after": {
      "type": "Metric",
      "props": {
        "label": "修改后",
        "value": {
          "$bind": "/metrics/after"
        },
        "unit": "ms",
        "tone": "positive"
      }
    },
    "diff": {
      "type": "Diff",
      "props": {
        "language": "go",
        "content": {
          "$bind": "/diff"
        },
        "collapsed": true
      }
    },
    "file": {
      "type": "FileLink",
      "props": {
        "label": "定位到实现",
        "resource_id": "wsres_7c51",
        "line": 89
      }
    }
  },
  "data": {
    "metrics": {
      "before": 184,
      "after": 73
    },
    "diff": "@@ -89,3 +89,5 @@"
  }
}
```

## 4. Core 数据模型

```go
type CardMutation struct {
    Operation        string                   `json:"operation"`
    SchemaVersion    string                   `json:"schema_version"`
    SurfaceID        string                   `json:"surface_id"`
    ExpectedRevision uint64                   `json:"expected_revision,omitempty"`
    Target           CardTarget               `json:"target,omitempty"`
    Root             string                   `json:"root,omitempty"`
    Components       map[string]CardComponent `json:"components,omitempty"`
    Data             map[string]any           `json:"data,omitempty"`
    Patch            []CardPatchOperation     `json:"patch,omitempty"`
}

type CardComponent struct {
    Type     string         `json:"type"`
    Props    map[string]any `json:"props,omitempty"`
    Children []string       `json:"children,omitempty"`
}
```

Core 输出 `CardSurface`，其中补充 revision、status、created/updated time、validation digest 和 target。客户端不使用工具原始 arguments 作为权威卡片。

`ConversationItem(kind=card)` 持有完整 CardSurface 或不可变引用。Conversation 顺序由 Application 分配的 item sequence 决定，不能由 surface ID 排序。

## 5. Component registry

### P0 白名单

| 类别 | Type | 允许能力 |
|------|------|----------|
| 布局 | `Card` | 标题、说明、tone、children |
| 布局 | `Stack` | vertical/horizontal、gap、children |
| 布局 | `Grid` | 1—4 columns、children |
| 内容 | `Text` | plain text、variant |
| 内容 | `Markdown` | 现有安全 Markdown 子集 |
| 内容 | `Code` | language、只读 content、copy |
| 内容 | `Diff` | unified diff、折叠、文件 label |
| 数据 | `Table` | 列定义、行数据、对齐、最大行数 |
| 数据 | `Metric` | label/value/unit/tone |
| 状态 | `Badge` | label、有限 tone |
| 状态 | `Progress` | 0—1 value、label |
| 交互 | `Actions` | Button children |
| 交互 | `Button` | label、action ID、有限 style |
| 导航 | `FileLink` | opaque resource ID、可选行列 |
| 导航 | `ArtifactLink` | opaque artifact ID |
| 降级 | `Error` | 安全错误摘要、copy diagnostics |

### P1 候选

- `Chart`：bar/line/pie，先验证数据规模再懒加载 Canvas renderer；
- `Explain`：分步讲解并高亮同一卡片内的目标组件；
- `SearchResults`：Workspace resource ID 列表；
- `Image`：只允许 Artifact/Workspace resource，不允许 data URL 和任意本地路径。

新增 Type 必须同时提供：Core prop validator、前端 renderer、size budget、a11y contract、正反 fixture 和 E2E 样例。

## 6. 校验管道

```text
JSON decode
  → tool-level required fields
  → schema version
  → operation + revision precondition
  → payload byte limit
  → component count / child count / depth
  → ID format + root/ref existence
  → cycle detection
  → registry type/prop validation
  → binding path validation
  → action allowlist
  → URL/resource resolution policy
  → canonical digest
```

默认硬限制：

| 限制 | 默认 |
|------|-----:|
| JSON payload | 256 KiB |
| components | 200 |
| tree depth | 12 |
| children/component | 50 |
| Table rows | 500，默认只渲染前 100 |
| text/code/diff field | 64 KiB |
| patch operations | 100 |

管理员可通过构造配置降低限制；Agent、Skill、Plugin 和前端不能提高。

## 7. Binding 与 patch

v1 binding 只支持只读 JSON Pointer：

```json
{
  "value": {
    "$bind": "/metrics/after"
  }
}
```

禁止表达式、函数、模板求值、对象原型访问和动态属性名。绑定失败显示字段级 fallback，不执行字符串。

Patch 使用受限 RFC 6902 子集：

- 只允许 `add`、`replace`、`remove`；
- path 必须位于 `/data` 或 registry 声明的 mutable state；
- 禁止修改 schema version、target、surface ID、root、component type 和 action 定义；
- 必须携带 `expected_revision`；冲突返回 `CARD_REVISION_CONFLICT`，不做 last-write-wins；
- 全部 patch 先作用于副本，完整校验成功后一次提交。

## 8. 对话区渲染集成

计划新增：

```text
gui/frontend/dist/
  dsl-schema.js          # 前端防御性结构检查
  dsl-registry.js        # type → renderer/validator/defaults
  dsl-renderer.js        # CardSurface → presentation model
  card-action.js         # user gesture → Bridge action
  card-view.test.mjs
```

`components.js` 的 Conversation presentation model 增加：

| item kind | DOM key | renderer |
|-----------|---------|----------|
| text | `message:<id>` | Markdown |
| tool | `tool:<tool.id>` | Tool Card |
| card | `card:<surface.id>` | DSL renderer |

卡片节点位于 `#conversation`，按 ConversationItem 顺序插入。`conversation-view.js` 继续负责 keyed reconcile、滚动与局部状态保存；Workspace controller 不参与 Card DOM。

前端二次校验不是 Core 校验替代，而是防止缓存损坏、版本错配或调试注入破坏 DOM。

## 9. 安全渲染规则

- 所有 Text/label/value 默认使用 `textContent` 或 `escapeHtml`；
- Markdown 复用现有安全子集和 URL allowlist；
- Code/Diff 永远作为文本，不解释 HTML；
- registry renderer 固定在本地 bundle，不接受 module URL；
- prop 不允许 `style`、`className`、`innerHTML`、`on*` 或任意 DOM attribute map；
- tone/layout 使用枚举映射到预定义 CSS class；
- 外链只允许 http/https，必须用户点击并显示目标 host；
- FileLink/ArtifactLink 只携带 opaque ID，由 Core 解析；
- ErrorCard diagnostics 默认截断，不显示秘密字段和完整本地绝对路径。

## 10. Action 模型

Action 定义在 CardSurface 的独立表中，Button 只引用 action ID：

| action | 行为 | 安全条件 |
|--------|------|----------|
| `copy` | 写 clipboard | 必须用户点击；内容受长度限制 |
| `open_url` | 打开外链 | allowlist scheme + 用户确认/手势 |
| `workspace_reveal` | 右栏定位资源 | Core 解析 opaque resource ID |
| `submit_prompt` | 把固定文本作为新用户输入 | 点击前显示文本；继续走 Submit/queue |
| `resolve_interaction` | 回答当前 Interaction | ID 必须匹配仍打开的 Core Interaction |

前端调用 `Bridge.ResolveCardAction(cardID, actionID, expectedRevision)`。Core 从持久化 Card 读取 action，不能相信前端回传完整 action payload。

## 11. 持久化与恢复

Card mutation 追加到 Presentation log，Conversation transcript 保存 card item 的稳定 ID、turn、sequence 和当前 revision。周期性 compact 生成最新 surface snapshot。

恢复流程：

1. 加载 Session manifest 与 Engine History；
2. 加载 transcript window；
3. 加载该 window 引用的最新 CardSurface；
4. schema 已知则校验并恢复；
5. schema 未知/损坏则构造 ErrorCard，保留原 record hash；
6. Snapshot 返回可见 window，分页时按相同规则加载更早 cards。

卡片恢复失败不阻塞文本、Tool 和会话继续运行。

## 12. 错误模型

| code | 场景 | 行为 |
|------|------|------|
| `CARD_INVALID_JSON` | decode 失败 | tool error，不创建 surface |
| `CARD_UNSUPPORTED_SCHEMA` | 未知版本 | ErrorCard/只读诊断 |
| `CARD_LIMIT_EXCEEDED` | 大小/数量/深度超限 | 拒绝，返回命中的 limit |
| `CARD_INVALID_GRAPH` | cycle/悬空引用 | 拒绝完整 mutation |
| `CARD_UNSUPPORTED_COMPONENT` | Type 未注册 | 拒绝或 ErrorCard，不忽略节点 |
| `CARD_REVISION_CONFLICT` | patch 基线旧 | 返回当前 revision，允许模型重读后重试 |
| `CARD_ACTION_DENIED` | action 不允许/过期 | toast + 审计，不执行 |
| `CARD_RESOURCE_MISSING` | opaque ID 失效 | Card 保留，链接显示 unavailable |

## 13. 性能策略

- surface 校验与渲染 O(components + edges + payload bytes)；
- patch 只替换受影响 component 的 presentation node；
- 大 Table 默认分页或窗口化，不把 500 行一次写入 DOM；
- Code/Diff 延续 preview 上限与按需展开；
- Card 不可见时保留轻量 placeholder，进入视口再填充重内容；
- Snapshot 只返回当前 Conversation window 内的 card item；
- 统计 validation duration、render duration、fallback count 和 payload size histogram。

## 14. 计划改动位置

| 层 | 文件/目录 | 变更 |
|----|-----------|------|
| Schema | `schemas/card-v1.schema.json` | 工具和 fixture 共享 schema |
| Core contract | `application/state.go`、`event.go`、`ports.go` | ConversationItem、Card DTO/Event/Port |
| Core service | `application/presentation.go` | mutation、item、revision、action 编排 |
| Domain | `presentation/` | validator、registry metadata、store |
| Composition | `main.go` | 注入 stores，注册 render_card |
| Bridge | `gui/bridge.go` | ResolveCardAction |
| Frontend | `dsl-*.js`、`components.js`、`conversation-view.js` | 对话区 Card 渲染与动作 |
| Session | `session/` | transcript/presentation bundle |

## 15. 测试矩阵

| 层 | 必测项 |
|----|--------|
| JSON fixture | 每种组件 valid；unknown/cycle/dangling/limits/actions invalid |
| Validator unit | graph、binding、patch atomicity、revision、digest |
| Application | tool→card item→event→Snapshot、错误隔离、resume/page |
| Node | registry、escape、key、patch、局部状态、action allowlist |
| Playwright | 卡片位于对话区、键盘操作、FileLink 打开右栏、scroll anchor |
| Security | XSS corpus、URL scheme、prototype keys、oversized/deep payload |
| Race | 同 surface patch、Snapshot、session compact 并发 |

## 16. 验收追溯

| PRD | 设计落点 |
|-----|----------|
| DSL-001 | 专用 render_card 工具，不解析 Markdown |
| DSL-002 / DSL-003 | CardSurface + 校验管道 |
| DSL-004 | P0 registry |
| DSL-005 | upsert + 受限 patch |
| DSL-006 | ErrorCard 与错误模型 |
| DSL-007 | action table + Core resolve |
| DSL-008 | transcript/presentation store |
| DSL-009 | ConversationItem(kind=card)，Workspace 不渲染 DSL |
