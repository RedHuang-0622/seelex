# Store 旧数据读写操作覆盖清单

基线：`66f20702`。清单范围是 God 类型上的公开方法；内部锁内方法不在产品调用面内。

## 汇总

| Receiver | 基线公开操作 | 当前保留 | 已迁移/删除 |
|---|---:|---:|---:|
| `Router` | 113 | 71 | 42 |
| `jsonRepository` | 26 | 21 | 5 |
| `SessionGranularStore` | 40 | 38 | 2 |

## 判据

- 旧公开操作仍在当前 receiver 上：保留为 Store 持久化机制。
- 旧公开操作消失：必须在逐项表中映射到新 owner，否则本脚本失败。
- 调用方迁移按逐项表的目标 owner 对照，不以测试变绿替代结构覆盖证明。


## Router

| 旧操作 | 状态 | 新 owner / 唯一入口 |
|---|---|---|
| `AppendFrameworkEvent` | retained | root Store persistence API |
| `AppendFrameworkEventWorkspace` | retained | root Store persistence API |
| `AppendGoalAuditEvent` | retained | root Store persistence API |
| `AppendMessageDraftWorkspace` | retained | root Store persistence API |
| `AppendProjectBoardRows` | migrated | internal/host/boardrouter + store/board |
| `AppendRoleDraftWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `AppendRoleSessionRowsWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `AssembleRoleWireWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `AssembleWireWorkspace` | migrated | store.WireStore |
| `Close` | retained | root Store persistence API |
| `CommitCompactFrameWorkspace` | retained | root Store persistence API |
| `CompactFramesWorkspace` | retained | root Store persistence API |
| `CreateRoleSessionWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `CurrentGenerationWorkspace` | retained | root Store persistence API |
| `Delete` | retained | root Store persistence API |
| `DeleteWorkspace` | retained | root Store persistence API |
| `DerivedRecordWorkspace` | migrated | kernel/session projection + Host raw reads |
| `DerivedResumeRecordWorkspace` | migrated | kernel/session projection + Host raw reads |
| `DiscardPendingMessageTailWorkspace` | retained | root Store persistence API |
| `EnsureRoleSessionWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `FirstUserInputsWorkspace` | retained | root Store persistence API |
| `ForkStacks` | migrated | store.StackStore + runtime_api.go |
| `GoalAuditEvents` | retained | root Store persistence API |
| `LRUDeleteWorkspace` | retained | root Store persistence API |
| `LayoutV8` | retained | root Store persistence API |
| `LifecycleRecoverWorkspace` | retained | root Store persistence API |
| `List` | retained | root Store persistence API |
| `ListRoleSessions` | migrated | store/role + store.RoleSessionAdapter |
| `ListRoleSessionsWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ListToolResultsWorkspace` | retained | root Store persistence API |
| `ListWorkspace` | retained | root Store persistence API |
| `Load` | retained | root Store persistence API |
| `LoadCheckpointWorkspace` | retained | root Store persistence API |
| `LoadCompactionRecordsWorkspace` | retained | root Store persistence API |
| `LoadContextState` | retained | root Store persistence API |
| `LoadContextStateWorkspace` | retained | root Store persistence API |
| `LoadConversationRangeWorkspace` | migrated | kernel/session projection + Host raw reads |
| `LoadEventRangeWorkspace` | retained | root Store persistence API |
| `LoadEventTail` | retained | root Store persistence API |
| `LoadEventTailWorkspace` | retained | root Store persistence API |
| `LoadProjectRecord` | retained | root Store persistence API |
| `LoadRange` | retained | root Store persistence API |
| `LoadRangeWorkspace` | retained | root Store persistence API |
| `LoadSessionDisplayMetaWorkspace` | retained | root Store persistence API |
| `LoadState` | retained | root Store persistence API |
| `LoadStateWorkspace` | retained | root Store persistence API |
| `LoadSystemPromptWorkspace` | retained | root Store persistence API |
| `LoadToolResult` | retained | root Store persistence API |
| `LoadToolResultWorkspace` | retained | root Store persistence API |
| `LoadWorkspace` | retained | root Store persistence API |
| `MessageCount` | retained | root Store persistence API |
| `PendingMessageTailWorkspace` | retained | root Store persistence API |
| `ProjectBoardWatermark` | migrated | internal/host/boardrouter + store/board |
| `QueueConfirmConsumedWorkspace` | retained | root Store persistence API |
| `QueueEnqueueWorkspace` | retained | root Store persistence API |
| `QueueFailConsumedWorkspace` | retained | root Store persistence API |
| `QueueItemsWorkspace` | retained | root Store persistence API |
| `QueueMarkConsumedWorkspace` | retained | root Store persistence API |
| `QueueRecoverItemsWorkspace` | retained | root Store persistence API |
| `ReadFrameworkEvents` | retained | root Store persistence API |
| `ReadFrameworkEventsWorkspace` | retained | root Store persistence API |
| `ReadLifecycleOrderWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ReadMediaWorkspace` | migrated | store/artifact.SessionStore |
| `ReadMessageFloorWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ReadProjectBoard` | migrated | internal/host/boardrouter + store/board |
| `ReadProjectBoardMarkdown` | migrated | internal/host/boardrouter + store/board |
| `ReadProjectBoardRows` | migrated | internal/host/boardrouter + store/board |
| `ReadProjectKnown` | migrated | store/project |
| `ReadRoleDraftWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ReadRoleSessionRowsWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ReadTeamRegistryWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `ReclaimProjectBoard` | migrated | internal/host/boardrouter + store/board |
| `RecoverPendingMessageTailWorkspace` | retained | root Store persistence API |
| `RemoveTeamRegistryWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `RetentionAdvisoryWorkspace` | retained | root Store persistence API |
| `RoleSnapshotWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `Save` | retained | root Store persistence API |
| `SaveCheckpointWorkspace` | retained | root Store persistence API |
| `SaveCommit` | retained | root Store persistence API |
| `SaveCommitWorkspace` | retained | root Store persistence API |
| `SaveContextState` | retained | root Store persistence API |
| `SaveContextStateWorkspace` | retained | root Store persistence API |
| `SaveProjectRecord` | retained | root Store persistence API |
| `SaveSessionDisplayMetaWorkspace` | retained | root Store persistence API |
| `SaveState` | retained | root Store persistence API |
| `SaveStateWorkspace` | retained | root Store persistence API |
| `SaveSystemPromptWorkspace` | retained | root Store persistence API |
| `SaveWorkspace` | retained | root Store persistence API |
| `ScheduleCancelWorkspace` | retained | root Store persistence API |
| `ScheduleFireWorkspace` | retained | root Store persistence API |
| `ScheduleRegisterWorkspace` | retained | root Store persistence API |
| `SessionArchivedWorkspace` | retained | root Store persistence API |
| `SessionMetaWorkspace` | retained | root Store persistence API |
| `SessionTitleWorkspace` | retained | root Store persistence API |
| `SetLifecycleOrderWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `SetProjectBoardRootFor` | migrated | internal/host/boardrouter + store/board |
| `SetRoleLifecycleWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `SetSessionArchivedWorkspace` | retained | root Store persistence API |
| `SetSessionTitleWorkspace` | retained | root Store persistence API |
| `SetWorkspace` | retained | root Store persistence API |
| `StackActive` | migrated | store.StackStore + runtime_api.go |
| `StackHistory` | migrated | store.StackStore + runtime_api.go |
| `StackPopTop` | migrated | store.StackStore + runtime_api.go |
| `StackPush` | migrated | store.StackStore + runtime_api.go |
| `StackReplace` | migrated | store.StackStore + runtime_api.go |
| `StackSetStatus` | migrated | store.StackStore + runtime_api.go |
| `StackStorageStats` | migrated | store.StackStore + runtime_api.go |
| `StackVerify` | migrated | store.StackStore + runtime_api.go |
| `SyncRoleDraftWorkspace` | migrated | store/role + store.RoleSessionAdapter |
| `Workspace` | retained | root Store persistence API |
| `WriteMediaWorkspace` | migrated | store/artifact.SessionStore |
| `WriteProjectKnown` | migrated | store/project |
| `WriteTeamRegistryWorkspace` | migrated | store/role + store.RoleSessionAdapter |

## jsonRepository

| 旧操作 | 状态 | 新 owner / 唯一入口 |
|---|---|---|
| `AppendFrameworkEvent` | retained | root Store persistence API |
| `Close` | retained | root Store persistence API |
| `CollectMedia` | migrated | store/artifact |
| `CurrentGeneration` | retained | root Store persistence API |
| `Delete` | retained | root Store persistence API |
| `List` | retained | root Store persistence API |
| `ListMedia` | migrated | store/artifact |
| `ListToolResults` | retained | root Store persistence API |
| `Ping` | retained | root Store persistence API |
| `Read` | retained | root Store persistence API |
| `ReadContextState` | retained | root Store persistence API |
| `ReadConversationRange` | migrated | kernel/session projection + Host raw reads |
| `ReadEventRange` | retained | root Store persistence API |
| `ReadEventTail` | retained | root Store persistence API |
| `ReadFrameworkEvents` | retained | root Store persistence API |
| `ReadMedia` | migrated | store/artifact |
| `ReadProjectRecord` | retained | root Store persistence API |
| `ReadRange` | retained | root Store persistence API |
| `ReadState` | retained | root Store persistence API |
| `ReadToolResult` | retained | root Store persistence API |
| `WriteAtomic` | retained | root Store persistence API |
| `WriteCommit` | retained | root Store persistence API |
| `WriteContextState` | retained | root Store persistence API |
| `WriteMedia` | migrated | store/artifact |
| `WriteProjectRecord` | retained | root Store persistence API |
| `WriteState` | retained | root Store persistence API |

## SessionGranularStore

| 旧操作 | 状态 | 新 owner / 唯一入口 |
|---|---|---|
| `Bind` | retained | root Store persistence API |
| `Context` | retained | root Store persistence API |
| `ConversationRange` | migrated | Host raw event/range reads |
| `CurrentGeneration` | retained | root Store persistence API |
| `Delete` | retained | root Store persistence API |
| `DiscardPendingMessageTail` | retained | root Store persistence API |
| `EnsureIndexed` | retained | root Store persistence API |
| `EventRange` | retained | root Store persistence API |
| `FirstUserInputs` | retained | root Store persistence API |
| `History` | retained | root Store persistence API |
| `HistoryForProject` | retained | root Store persistence API |
| `HistoryRange` | retained | root Store persistence API |
| `ListToolResults` | retained | root Store persistence API |
| `LoadCompactionRecords` | retained | root Store persistence API |
| `LoadContextRaw` | retained | root Store persistence API |
| `LoadRecordRaw` | retained | root Store persistence API |
| `LoadSession` | retained | root Store persistence API |
| `PendingMessageTail` | retained | root Store persistence API |
| `QueueConfirmConsumed` | retained | root Store persistence API |
| `QueueEnqueue` | retained | root Store persistence API |
| `QueueFailConsumed` | retained | root Store persistence API |
| `QueueItems` | retained | root Store persistence API |
| `QueueMarkConsumed` | retained | root Store persistence API |
| `QueueRecoverItems` | retained | root Store persistence API |
| `RecoverPendingMessageTail` | retained | root Store persistence API |
| `ResolveProjectForSession` | retained | root Store persistence API |
| `SaveCommit` | retained | root Store persistence API |
| `SaveContext` | retained | root Store persistence API |
| `SaveHistory` | retained | root Store persistence API |
| `SaveRecordRaw` | retained | root Store persistence API |
| `SaveSession` | retained | root Store persistence API |
| `SaveSessionTitle` | retained | root Store persistence API |
| `SessionTitle` | retained | root Store persistence API |
| `SessionsOf` | retained | root Store persistence API |
| `SetProjectSource` | retained | root Store persistence API |
| `SetWorkspaceResolver` | retained | root Store persistence API |
| `ToolResult` | retained | root Store persistence API |
| `ToolResults` | retained | root Store persistence API |
| `Transcript` | migrated | Host raw event/range reads |
| `TranscriptTail` | retained | root Store persistence API |

## 根包公开函数

基线 43 个，当前保留 22 个。

| 旧函数 | 状态 | 新 owner / 唯一入口 |
|---|---|---|
| `ApplyLimits` | migrated | store/project.ApplyLimits |
| `CompleteEventUnits` | retained | root Store persistence API |
| `EventKindOf` | retained | root Store persistence API |
| `EventParagraphs` | retained | root Store persistence API |
| `FoldProjectBoardRows` | migrated | store/board.Fold |
| `IsParagraphBoundary` | retained | root Store persistence API |
| `IsSessionNotFound` | retained | root Store persistence API |
| `MediaRefHash` | migrated | store/artifact.MediaRefHash |
| `NewAttemptCache` | retained | root Store persistence API |
| `NewCheckpointStore` | retained | root Store persistence API |
| `NewCommitWriter` | retained | root Store persistence API |
| `NewDurableHistory` | retained | root Store persistence API |
| `NewEventStore` | retained | root Store persistence API |
| `NewMaterialCache` | retained | root Store persistence API |
| `NewNodeSessionRecordWriter` | migrated | store/workunit.NewWriter |
| `NewNodeSessionStore` | migrated | store/workunit.NewSessionStore |
| `NewProjectKnowledgeBuilder` | migrated | store/project.NewProjectKnowledgeBuilder |
| `NewRolePermissionStore` | migrated | store/role.NewPermissionStore |
| `NewRouter` | retained | root Store persistence API |
| `NewRouterStorage` | retained | root Store persistence API |
| `NewSessionContextStore` | retained | root Store persistence API |
| `NewSessionGranularStore` | retained | root Store persistence API |
| `NewSessionMetaStore` | retained | root Store persistence API |
| `NewSessionSSHStateStore` | retained | root Store persistence API |
| `Open` | retained | root Store persistence API |
| `OpenStores` | retained | root Store persistence API |
| `ParagraphEnd` | retained | root Store persistence API |
| `ProjectBoardDir` | migrated | store/board.ProjectBoardDir |
| `ProjectBoardKinds` | migrated | contract/dto project-board kinds |
| `ProjectBoardMarkdown` | migrated | store/board.Markdown |
| `ProjectBoardSessionDir` | migrated | store/board.SessionDir |
| `ProjectKnownMarkdown` | migrated | store/project.ProjectKnownMarkdown |
| `ReferencedMediaHashes` | migrated | artifact GC caller-owned reference set |
| `RefreshProjectKnowledge` | migrated | store/project.RefreshProjectKnowledge |
| `SessionIDFromEvent` | retained | root Store persistence API |
| `SessionIDFromLocations` | retained | root Store persistence API |
| `TeamworkBindings` | migrated | store/teamwork.Bindings |
| `TeamworkPlanIsEmpty` | migrated | contract/dto.TeamworkPlanIsEmpty |
| `TeamworkWorkItemStatusOrPending` | migrated | contract/dto.TeamworkWorkItemStatusOrPending |
| `TeamworkWorkItemTerminal` | migrated | contract/dto.TeamworkWorkItemTerminal |
| `ValidateGoalBoardMeta` | migrated | store/board.ValidateGoalBoardMeta |
| `ValidateTeamBoardMeta` | migrated | store/board.ValidateTeamBoardMeta |
| `ValidateTeamworkPlan` | migrated | store/teamwork.ValidatePlan |
