# assembly/misc（根包分卷）

## 生态位

基础与杂项（vocabulary/assembly_helpers/limits/completion/compressed/diagnostics/fault-guard/perf/snapshot-budget/runtime/workspace/race）

覆盖：显式名单 31 个文件（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### limits.go

- `func workTableBounds() worktable.Limits` — workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的

### runtime_projection.go

- `func (port serviceRuntimeProjectionPort) CollectProjection(ctx context.Context) view_state.RuntimeStateProjection`
- `func (port serviceRuntimeProjectionPort) LockView()`
- `func (port serviceRuntimeProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (port serviceRuntimeProjectionPort) BumpLocked() uint64`
- `func (port serviceRuntimeProjectionPort) UnlockView()`
- `func (port serviceRuntimeProjectionPort) CurrentViewSessionID() string`
- `func (port serviceRuntimeProjectionPort) RuntimeSnapshot() model.RuntimeState`
- `func (port serviceRuntimeProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)`
- `func (port serviceRuntimeProjectionPort) PublishTaskDeltas()`
- `func (service *Service) publishRuntimeProjections()` — publishRuntimeProjections copies application-owned state under the view lock
- `func PublishInitialRuntimeProjections(service *Service)` — PublishInitialRuntimeProjections 是组合根触发**首次**运行时投影发布的唯一入口。

### snapshot_budget.go

- `func snapshotToolOutputLimit() int` — snapshotToolOutputLimit 返回可见会话快照单条工具输出的字符预算
- `func (service *Service) boundToolResultForSnapshot(name, content string) (visible string, ref string, truncated bool, totalChars int)` — boundToolResultForSnapshot 生成进入可见会话快照的工具输出（调用方持有

### workspace_host.go

- `func (host workspaceHost) LockTransition() func()`
- `func (host workspaceHost) State() switchworkspace.ViewState`
- `func (host workspaceHost) ApplyDraftBinding(info model.WorkspaceInfo, projection switchworkspace.Projection) uint64`
- `func (host workspaceHost) ApplyBinding(info model.WorkspaceInfo, projection switchworkspace.Projection, freshSessionID, notice string) uint64`
- `func (host workspaceHost) ApplyUnbind(sessionID string, projection switchworkspace.Projection) uint64`
- `func (host workspaceHost) PublishSnapshotChanged(revision uint64)`
- `func (host workspaceHost) RequestCatalogRefresh()`
- `func (host workspaceHost) ClearReviewRequest()`

### workspace_usecase.go

- `func (service *Service) releaseSessionAsync(sessionID string)` — releaseSessionAsync tells the execution domain that a session's background
- `func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection)` — applyWorkspaceProjectionLocked copies a middle-layer projection into the
- `func (port serviceWorkspaceProjectionPort) Projection() switchworkspace.Projection`
- `func (port serviceWorkspaceProjectionPort) LockView()`
- `func (port serviceWorkspaceProjectionPort) ApplyWorkspaceProjectionLocked(projection switchworkspace.Projection)`
- `func (port serviceWorkspaceProjectionPort) BumpLocked() uint64`
- `func (port serviceWorkspaceProjectionPort) UnlockView()`
- `func (port serviceWorkspaceProjectionPort) CurrentViewSessionID() string`
- `func (port serviceWorkspaceProjectionPort) PublishSnapshotChanged(revision uint64, sessionID string)`
