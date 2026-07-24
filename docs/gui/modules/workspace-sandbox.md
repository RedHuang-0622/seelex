# 右栏 Workspace 沙盒模块详细设计

> 状态：拟议方案
> 产品需求：`CAP-WORKSPACE`
> 总体架构：[`../../arch/agent-workbench-architecture.md`](../../arch/agent-workbench-architecture.md)

## 1. 职责与产品边界

右侧栏从静态 Project 摘要升级为工程工作台，固定包含：

1. **Overview**：Workspace 状态、策略、索引、会话与任务摘要；
2. **Files**：按需目录树、文件预览、行定位；
3. **Changes**：git 变更或受限 fallback 变更、diff；
4. **Artifacts**：Agent 产物、hash、来源 turn/tool/approval、打开与复制定位。

Workspace 不承载任意 DSL 卡片。DSL 卡片始终在 Conversation；`FileLink/ArtifactLink` 只触发右栏专用视图定位。

安全边界在后端：右栏、Bridge、Agent 和 Card 都不能通过传入路径直接读取文件。

## 2. 当前迁移

当前 `gui.Bridge.Info()` 的 `ProjectInfo` 只发现固定资料源，`renderProject` 把 Project 状态和来源写入静态右栏。

迁移策略：

- 现有 Project name/status/sources 进入 Overview，保证首阶段向后兼容；
- `ProjectRoot` 从展示参数升级为构造 Workspace adapter 的 canonical root；
- protocol v2 的远程/通用 DTO 不暴露绝对 root；本地 GUI 如需展示路径，使用单独的 local-only capability，并禁止进入 Card、日志和 sidecar；
- Files/Changes/Artifacts 使用 Bridge query，不塞入 `AppInfo` 或完整 Snapshot。

## 3. 后端结构

```text
application.WorkspacePort (caller-owned)
             │
             ▼
workspace.Manager
  ├─ PathGuard
  ├─ ResourceRegistry
  ├─ DirectoryProvider
  ├─ PreviewProvider
  ├─ ChangeProvider strategy
  │    ├─ GitChangeProvider
  │    └─ AuditChangeProvider
  ├─ ArtifactRegistry
  ├─ Index / Cache
  └─ AuditSink
```

`workspace.Manager` 是 facade，不依赖 GUI。`gui.Bridge` 只把 Application query 绑定给 Wails。

## 4. Workspace identity 与 policy

```go
type WorkspaceConfig struct {
    Root              string
    Policy            WorkspacePolicy
    FollowSymlinks    bool
    MaxPreviewBytes   int64
    MaxPreviewLines   int
    DirectoryPageSize int
    Ignore            []string
}

type WorkspaceState struct {
    ID            string `json:"id"`
    Name          string `json:"name"`
    Revision      uint64 `json:"revision"`
    Policy        string `json:"policy"`
    IndexStatus   string `json:"index_status"`
    FileCount     int    `json:"file_count,omitempty"`
    ChangeCount   int    `json:"change_count,omitempty"`
    ArtifactCount int    `json:"artifact_count,omitempty"`
}
```

`Workspace.ID` 来自 canonical root identity，不把 root 本身编码进 ID。Policy 由启动参数或受信配置构造注入，右栏不能修改。

| policy | Workspace query | Tool 写/删/命令 |
|--------|-----------------|------------------|
| `read_only` | 允许受限读取 | 拒绝 |
| `approval_required` | 允许受限读取 | 进入 Permission Gate + ApprovalBroker |
| `full_access` | 允许受限读取 | 仍进入 Tool middleware，仅免人工确认 |

## 5. PathGuard

### 5.1 输入类型

公开查询只接受：

- slash 格式相对路径，主要用于用户直接展开目录；
- 后端签发的 opaque `resource_id`，用于 Card、Artifact、历史与导航；
- 绝不接受任意绝对路径作为 Bridge/Card 参数。

### 5.2 解析算法

```text
input
  → UTF-8/NUL/control check
  → slash normalization
  → reject absolute / drive / UNC / empty / dotdot
  → join canonical root
  → inspect every existing component for symlink/reparse
  → default: reject link traversal
  → optional follow: resolve and compare final target to root
  → filepath.Rel(root, target)
  → platform-aware inside-root check
  → operation-specific stat/open
  → return canonical relative path + metadata
```

Windows 必测：`C:`、`C:\`、`\\server\share`、extended-length prefix、mixed separators、alternate case、junction/reparse point、保留设备名。Unix 必测：absolute、`..`、nested symlink、broken symlink 和 rename race。

### 5.3 TOCTOU 策略

- 每次 IO 前重新 resolve，不缓存“已安全”的绝对 path；
- P0 默认不跟随任何 symlink/reparse point，显著缩小竞态面；
- destructive/write 操作不属于 Workspace query，继续由 Tool middleware 自己执行路径校验；
- 后续需要 follow 时，Unix 采用 `openat`/no-follow 语义，Windows 使用 handle final path/reparse 校验；不能只依赖一次 `EvalSymlinks`；
- Index 中的 entry 只是提示，实际 preview/diff 仍重新经过 PathGuard。

## 6. Resource ID

Card 不直接存路径，而存 `resource_id`：

```go
type WorkspaceResource struct {
    ID           string `json:"id"`
    WorkspaceID  string `json:"workspace_id"`
    Kind         string `json:"kind"` // file | directory | artifact
    RelativePath string `json:"relative_path"`
    Revision     uint64 `json:"revision"`
}
```

实现建议：用应用本地 secret 对 `workspace ID + kind + canonical relative path` 做 HMAC，并加类型前缀。opaque ID 不是授权替代，解析后仍必须 PathGuard；它提供防篡改、避免路径泄漏和跨 Card/Session 稳定引用。

Workspace root 变化、资源删除或策略改变时，Resolve 返回明确 stale/missing/denied 状态。

## 7. Query API

调用方接口位于 `application`：

```go
type WorkspacePort interface {
    Summary(context.Context) (WorkspaceState, error)
    List(context.Context, WorkspaceListRequest) (WorkspacePage, error)
    Preview(context.Context, WorkspacePreviewRequest) (FilePreview, error)
    Diff(context.Context, WorkspaceDiffRequest) (DiffPreview, error)
    Artifacts(context.Context, ArtifactListRequest) (ArtifactPage, error)
    ResolveResource(context.Context, string) (WorkspaceResource, error)
}
```

计划 Bridge 方法：

| 方法 | 用途 |
|------|------|
| `WorkspaceSummary()` | 手工刷新摘要 |
| `ListWorkspace(path, cursor, limit)` | 目录分页 |
| `PreviewWorkspace(resourceID, startLine, lineLimit)` | 文件窗口 |
| `WorkspaceDiff(resourceID, contextLines)` | 单文件安全 diff |
| `ListArtifacts(cursor, limit)` | 产物分页 |
| `ResolveWorkspaceResource(resourceID)` | Card/action 导航 |

Bridge 方法使用 Wails request context，返回 typed error；不持久化 UI 展开状态。

## 8. Directory DTO 与索引

```go
type WorkspaceEntry struct {
    ResourceID string    `json:"resource_id"`
    Name       string    `json:"name"`
    Path       string    `json:"path"`
    Kind       string    `json:"kind"`
    Size       int64     `json:"size,omitempty"`
    ModifiedAt time.Time `json:"modified_at,omitempty"`
    GitStatus  string    `json:"git_status,omitempty"`
    HasChildren bool     `json:"has_children,omitempty"`
}

type WorkspacePage struct {
    WorkspaceID string           `json:"workspace_id"`
    Revision    uint64           `json:"revision"`
    Entries     []WorkspaceEntry `json:"entries"`
    NextCursor  string           `json:"next_cursor,omitempty"`
    Truncated   bool             `json:"truncated"`
}
```

规则：

- 服务端排序：directories first，再按 Unicode case-fold name 稳定排序；
- cursor 包含目录 identity、revision 和最后 sort key，并做签名；
- 默认 page 200，最大 500；
- 不递归返回 children，UI 展开时再 query；
- 默认忽略 `.git` 内部对象、构建 cache、依赖大目录和受信 ignore 配置；
- `.gitignore` 只影响索引/展示优化，不能作为权限边界；
- 10,000+ 文件通过后台索引统计，首屏不等待全量完成。

## 9. File preview

```go
type FilePreview struct {
    ResourceID string `json:"resource_id"`
    Revision   uint64 `json:"revision"`
    Language   string `json:"language,omitempty"`
    Encoding   string `json:"encoding,omitempty"`
    MIME       string `json:"mime"`
    StartLine  int    `json:"start_line"`
    EndLine    int    `json:"end_line"`
    TotalLines int    `json:"total_lines,omitempty"`
    Content    string `json:"content,omitempty"`
    SHA256     string `json:"sha256,omitempty"`
    Binary     bool   `json:"binary"`
    Truncated  bool   `json:"truncated"`
}
```

默认 128 KiB/400 行，最大值由构造 options 限制。检测 NUL、高比例不可打印字节或已知 binary MIME 后不返回正文。未知编码不做有损转换；前端显示 metadata 与“二进制文件不预览”。

高亮使用已转义的本地轻量 renderer；不从文件内容加载语言插件或执行内容。

## 10. Changes

`ChangeProvider` 使用 Strategy：

```go
type ChangeProvider interface {
    Summary(context.Context) ([]ChangeEntry, error)
    Diff(context.Context, WorkspaceResource, DiffOptions) (DiffPreview, error)
}
```

### Git provider

- 调用 `git` 时使用参数数组，不通过 shell 拼接；
- 工作目录固定 canonical root；
- status 使用 porcelain v2 + NUL 格式；
- diff 固定 `--no-ext-diff --no-textconv`，resource path 置于 `--` 后；
- 禁止外部 diff、pager 和 hook；
- stdout/stderr/timeout 有硬限制；
- rename/copy 显式返回 old/new resource ID。

### Audit fallback

无 git 时只展示 Seelex Tool middleware 和 ArtifactRegistry 观察到的本次 Session 变更。它不声称发现所有外部修改，并在 UI 标记“Agent audit scope”。

Diff 默认 256 KiB/2,000 行，超限截断并给出统计；二进制只显示 changed metadata。

## 11. Artifact registry

```go
type ArtifactSummary struct {
    ID             string    `json:"id"`
    Name           string    `json:"name"`
    Type           string    `json:"type"`
    ResourceID     string    `json:"resource_id"`
    Size           int64     `json:"size"`
    SHA256         string    `json:"sha256"`
    SessionID      string    `json:"session_id"`
    TurnID         string    `json:"turn_id"`
    ProducerToolID string    `json:"producer_tool_id,omitempty"`
    ApprovalID     string    `json:"approval_id,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
    Status         string    `json:"status"`
}
```

元数据存 Session bundle；项目型产物默认建议位于 `.seele/artifacts/<session>/`，但注册前仍做 PathGuard、stat、大小和 hash。外部路径产物必须显式审批并使用单独 external artifact policy，不能伪装成 Workspace resource。

Artifact 生命周期：`declared → producing → ready | failed | missing | stale`。只有 `ready` 生成可用 ArtifactLink。

## 12. Workspace revision 与事件

触发 revision：

- index generation 完成或失效；
- Tool middleware 观察到文件变更；
- git status digest 变化；
- Artifact 注册/状态变化；
- Workspace policy 或 root generation 变化。

`workspace.changed` Event 只携带新的 `WorkspaceState` 摘要和 changed scopes：

```json
{
  "revision": 18,
  "changed": [
    "files",
    "changes",
    "artifacts"
  ]
}
```

前端按 scope 失效 cache，不清空未受影响的 tab。正在显示的 preview 若 revision 旧，显示 stale badge 并后台刷新；不能把用户滚动位置无条件重置。

## 13. 右栏 UI

计划结构：

```text
right-panel
  ├─ workspace-header (name, policy, index state, refresh)
  ├─ workspace-tabs
  │    ├─ Overview
  │    ├─ Files
  │    ├─ Changes
  │    └─ Artifacts
  └─ workspace-panel
       ├─ tree/list
       └─ preview/diff/detail
```

### 本地 UI 状态

- active tab；
- expanded resource IDs；
- selected resource/artifact；
- per-preview scroll/line anchor；
- query cursor/cache；
- panel width/collapsed state。

这些状态不进入 Application Snapshot。Workspace facts、policy、revision、resource 和 artifact 来自 Core。

### 响应式

| 宽度 | 行为 |
|------|------|
| ≥1200px | 三栏，右栏可调宽但有 min/max |
| 900—1199px | 右栏 overlay drawer，保持当前 tab |
| <900px | 单独 Workspace drawer，Conversation 不被压到不可用 |

### 可访问性

- tabs 使用 `role=tablist/tab/tabpanel`；
- tree 使用原生按钮/`role=treeitem`、Arrow Left/Right 展开；
- file/diff line 定位可键盘访问；
- loading/error/stale 使用 polite live region；
- focus 从 Card FileLink 导航到右栏目标标题，关闭 drawer 后返回原 Card action。

## 14. Card 联动

```text
Conversation Card FileLink click
  → ResolveCardAction(cardID, actionID, revision)
  → Core 从 Card store 读取 resource_id
  → WorkspacePort.ResolveResource
  → WorkspaceNavigation DTO
  → workspaceController.reveal
```

右栏不读取 Card props 里的 path，也不接受前端改写 action payload。导航失败时 Card 保留，action 标记 unavailable，右栏显示可重试错误。

## 15. 多会话绑定与并发

每个 open session 持有稳定 `workspace_id`，右栏跟随 active session 切换 binding；切页只恢复该页面的 tab/selected resource/scroll viewState，不改变后台 session 的 Workspace identity。

多个会话绑定同一 Workspace 时共享只读 index/cache，但 mutation 继续走 Tool middleware。审批 preview 记录 resource revision/hash，执行前重新 PathGuard 并比较 precondition；若另一会话已修改目标，返回 `WORKSPACE_REVISION_CONFLICT`，不得 last-write-wins。成功 mutation 发布 Workspace scope revision，使所有绑定会话只失效受影响查询。Artifact 额外保存 producer session ID，避免后台产物归属不明。

完整页面生命周期、scheduler 和审批路由见 [多会话页面并行详设](multi-session-pages.md)。

## 16. 缓存与性能

- 前端 query cache key：workspace ID + revision + method + resource/cursor；
- 同 key 并发请求合并，过期 response 按 revision 拒绝；
- directory cache 使用有限 LRU，切 Session/Workspace 时清空；
- preview/diff 只缓存最近 N 个，完整大文本不长期常驻 DOM；
- 后端 index 与 hash 使用有界 worker pool，Chat 主 goroutine 不等待全量索引；
- 文件系统 watcher 只做失效提示，事实读取仍由 query 完成；
- 目标：indexed query P95 <150ms，右栏交互反馈 <100ms，Snapshot Workspace <32KiB。

## 17. 错误模型

| code | 场景 | UI |
|------|------|----|
| `WORKSPACE_UNAVAILABLE` | adapter 初始化失败 | 保留 Overview，显示重试 |
| `WORKSPACE_PATH_INVALID` | path 格式非法 | 不回显危险原文，记录 audit |
| `WORKSPACE_PATH_DENIED` | root/symlink/policy 越界 | deny 状态，不自动审批 |
| `WORKSPACE_RESOURCE_STALE` | ID revision 过期 | 刷新并尝试重新定位 |
| `WORKSPACE_RESOURCE_MISSING` | 文件/产物删除 | missing detail |
| `WORKSPACE_BINARY` | 二进制预览 | metadata only |
| `WORKSPACE_LIMIT_EXCEEDED` | 目录/预览/diff 超限 | truncated + 分页/摘要 |
| `WORKSPACE_REVISION_CONFLICT` | 旧 query/action | 丢弃旧 response，刷新摘要 |
| `WORKSPACE_GIT_UNAVAILABLE` | 非 repo/git 不可用 | 切 audit fallback 或 unavailable |

## 18. 计划改动位置

| 层 | 文件/目录 | 变更 |
|----|-----------|------|
| Core contract | `application/state.go`、`ports.go`、`event.go` | Workspace summary/queries/event |
| Core service | `application/workspace.go` | query、navigation、revision 编排 |
| Domain | `workspace/` | Manager、PathGuard、providers、artifact registry |
| Composition | `main.go` | canonical root/policy/options 注入 |
| Bridge | `gui/bridge.go` | typed Workspace query bindings |
| Frontend | `workspace-view.js`、`workspace-controller.js` | tabs、cache、tree、preview |
| Shell | `index.html`、`app.js`、`styles.css` | 右栏替换、drawer、响应式 |
| DSL | `card-action.js` | resource action 中介 |
| Session | `session/` | artifact metadata、Workspace identity |

## 19. 测试矩阵

| 层 | 必测项 |
|----|--------|
| PathGuard unit | traversal、abs、drive、UNC、NUL、case、symlink、junction、race |
| Provider unit | pagination/cursor、binary、limits、git rename/diff、fallback audit |
| Artifact unit | lifecycle、hash、missing/stale、approval trace |
| Application | query error isolation、revision Event、resource resolve、Session switch |
| Bridge | 所有方法薄委托、typed error、context |
| Node | tabs、cache revision、tree keyboard、stale response、Card navigation |
| Playwright | Files/Changes/Artifacts journeys、drawer、focus return、large fixture |
| Security | path corpus、git option injection、secret redaction、oversized content |
| Race | index invalidation/query、artifact register/list、Snapshot update |

## 20. 验收追溯

| PRD | 设计落点 |
|-----|----------|
| WS-001 | 四 tab + responsive drawer |
| WS-002 | Summary in Snapshot，其他分页 query |
| WS-003 / WS-004 | relative/opaque input + PathGuard |
| WS-005 | preview/diff/directory limits |
| WS-006 | read-only query + Tool permission boundary |
| WS-007 | Change strategies + ArtifactRegistry |
| WS-008 | signed resource ID + Card action resolution |
| WS-009 | revision Event + scoped cache invalidation |
