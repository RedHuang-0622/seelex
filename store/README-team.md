# store/team（根包分卷）

## 生态位

团队计划/工作项、角色会话与节点会话记账

覆盖：`teamwork*.go`、`team_registry*.go`、`role_session*.go`、`node_session_store*.go`、`unit_record_writer*.go` + 显式名单（见生成器 `STORE_VOLUMES`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### node_session_store.go

- `func NewNodeSessionStore(router *Router) *NodeSessionStore` — NewNodeSessionStore 构造子代理会话记录存储。
- `func (store *NodeSessionStore) ProjectID() string` — ProjectID 返回 Router 当前的 active workspace（空 = 未绑定项目作用域，
- `func (store *NodeSessionStore) Save(projectID, mainSessionID string, record NodeSessionRecord) error` — Save 写入（覆盖）一条子代理会话记录。
- `func (store *NodeSessionStore) Load(projectID, mainSessionID, subSessionID string) (NodeSessionRecord, error)` — Load 读取一条子代理会话记录；不存在返回 ErrNodeSessionNotFound。
- `func (store *NodeSessionStore) List(projectID, mainSessionID string) ([]NodeSessionRecord, error)` — List 从主会话索引列举全部子代理会话记录（按 NodeID 排序）。
- `func (store *NodeSessionStore) Delete(projectID, mainSessionID, subSessionID string) error` — Delete 删除一条子代理会话记录。

### node_session_store_test.go

- `func TestNodeSessionStoreRoundTrip(t *testing.T)` — TestNodeSessionStoreRoundTrip 验证子代理会话记录按
- `func TestNodeSessionStoreListFromMainIndex(t *testing.T)` — TestNodeSessionStoreListFromMainIndex 验证从主会话索引直接列举全部子会话。
- `func TestNodeSessionStoreMissingAndDelete(t *testing.T)` — TestNodeSessionStoreMissingAndDelete 验证缺失语义与删除。
- `func TestNodeSessionStoreProjectIsolation(t *testing.T)` — TestNodeSessionStoreProjectIsolation 验证跨项目隔离。
- `func routerConfigPath(t *testing.T, router *Router) string` — routerConfigPath 返回测试 Router 的 JSON 存储根目录（sessions-json）。

### role_permission.go

- `func NewRolePermissionStore(router *Router) *RolePermissionStore` — NewRolePermissionStore 构造会话级角色权限登记存储。
- `func (layout rolePermissionLayout) WithProjectDir(projectID string, fn func(dir string) error) error`

### role_permission_test.go

- `func openRolePermissionRouter(t *testing.T) *Router`
- `func rolePermissionFileOnDisk(t *testing.T, router *Router) string` — rolePermissionFileOnDisk 找项目级角色权限文件（项目目录名带 hash 前缀）。
- `func TestRolePermissionStoreRoundTrip(t *testing.T)`
- `func TestRolePermissionExplicitEmptyVectorIsNotMissing(t *testing.T)`
- `func TestRolePermissionDropScopes(t *testing.T)`
- `func TestRolePermissionConcurrentRoleWritesDoNotLoseUpdates(t *testing.T)` — TestRolePermissionConcurrentRoleWritesDoNotLoseUpdates 是本文件的核心并发用例：
- `func TestRolePermissionRejectsEmptyCoordinates(t *testing.T)`
- `func TestRolePermissionStoreWithoutRouter(t *testing.T)` — TestRolePermissionStoreWithoutRouter 钉住"没装配存储就别假装成功"。

### role_session.go

- `func roleSessionEngine(store *storeEngine, mainKey Key, roleName, roleSessionID string) (*storeEngine, Key)` — roleSessionEngine 返回角色会话的引擎与 key：main 复用当前引擎；非 main 以
- `func roleSessionRoot(store *storeEngine, mainKey Key, roleName, roleSessionID string) string` — roleSessionRoot 返回角色会话子树的物理根（路径规则唯一实现在 store/role）。
- `func (port mainSessionPort) MessageHead() (storesession.MessageHead, error)`
- `func (port mainSessionPort) CommitSync(commitID string, rows []Event, floor *storesession.Floor) (storesession.MessageHead, error)`
- `func syncRoleDraft(store *storeEngine, mainKey Key, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error)` — syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
- `func createRoleSession(store *storeEngine, mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error)` — createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
- `func readRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string) ([]Event, error)` — readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
- `func readRoleSnapshot(store *storeEngine, mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error)` — readRoleSnapshot 汇总角色会话观察面（只读）。
- `func assembleRoleWire(store *storeEngine, mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error)` — assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
- `func roleWireMessages(messages []wireMessage) []RoleWireMessage`
- `func cloneCompactRef(ref *CompactRef) *CompactRef`
- `func cloneFloor(floor *Floor) *Floor`
- `func roleSnapshotWarnings(snapshot RoleSnapshot) []string`
- `func appendRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string, rows []Event) error` — appendRoleSessionRows 向角色会话写备份行。main 角色写主会话 message；非 main

### role_session_router.go

- `func (router *Router) CreateRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (RoleSessionInfo, error)` — CreateRoleSessionWorkspace 创建非 main 角色会话（TL/agent-team）。main 角色
- `func (router *Router) AppendRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []RoleDraftRow) error` — AppendRoleDraftWorkspace 向角色自己的 draft 追加未同步行。
- `func (router *Router) ReadRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]RoleDraftRow, error)` — ReadRoleDraftWorkspace 读取角色未同步 draft。
- `func (router *Router) SyncRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error)` — SyncRoleDraftWorkspace 由 sequencer 同步角色 draft 到主会话 message，成功后
- `func (router *Router) AppendRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []Event) error` — AppendRoleSessionRowsWorkspace 向角色会话写备份行（非 main 角色备份；main 会
- `func (router *Router) ReadRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]Event, error)` — ReadRoleSessionRowsWorkspace 读取角色会话备份行。
- `func (router *Router) RoleSnapshotWorkspace(projectID, mainSessionID, roleName, roleSessionID string) (RoleSnapshot, error)` — RoleSnapshotWorkspace 返回角色会话的只读观察面（headless 冒烟/巡检用）。
- `func (router *Router) AssembleRoleWireWorkspace(projectID, mainSessionID, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error)` — AssembleRoleWireWorkspace 构造角色可见物化 wire（main compact_ref + 已发布
- `func (router *Router) SetLifecycleOrderWorkspace(projectID, sessionID, policy string, roles []string) error` — SetLifecycleOrderWorkspace 写主会话（或角色会话）的群聊顺序策略。
- `func (router *Router) SetRoleLifecycleWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *CompactRef) error` — SetRoleLifecycleWorkspace 写角色会话的 join_seq_id 与 compact_ref。
- `func (router *Router) withRoleStore(projectID, mainSessionID, roleName, roleSessionID string, fn func(*storeEngine, Key) error) error`
- `func (router *Router) ListRoleSessionsWorkspace(projectID, mainSessionID string) ([]string, error)` — ListRoleSessionsWorkspace 枚举主会话下的角色会话目录名（goal_<hash>/ 与
- `func (router *Router) ListRoleSessions(mainSessionID string) ([]string, error)` — ListRoleSessions is the non-workspace variant using the active project.
- `func readDirNames(path string) ([]string, error)`

### role_session_test.go

- `func roleSessionFixture(t *testing.T) (*storeEngine, Key)`
- `func TestRoleSessionCreateBackupAndLifecycle(t *testing.T)`
- `func TestGoalStackRecordsRoleSessionAnchor(t *testing.T)`
- `func TestRoleDraftSyncOrderFloorAndIdempotency(t *testing.T)`
- `func TestRoleSnapshotProjectsPendingDraftUntilSync(t *testing.T)` — TestRoleSnapshotProjectsPendingDraftUntilSync 钉住「未同步草稿」的整条生命周期：
- `func TestRoleSnapshotMarksRowsOutsidePrefixMatch(t *testing.T)` — TestRoleSnapshotMarksRowsOutsidePrefixMatch 钉住「teammate 自己那份 team work
- `func TestAssembleRoleWireForMainIsMainSessionContext(t *testing.T)` — TestAssembleRoleWireForMainIsMainSessionContext 钉住「team work 前缀的作者」：
- `func (store *storeEngine) syncRoleDraftAndEnsureNoRows(mainKey Key, roleName, roleSessionID string, order []string, rows []RoleDraftRow) error`
- `func (store *storeEngine) appendRoleDraftForTest(mainKey Key, roleName, roleSessionID string, rows []RoleDraftRow) error`
- `func TestLifecycleOrderAndRoleRefSurviveDraftCommit(t *testing.T)`
- `func TestMaterialCacheInvalidation(t *testing.T)`
- `func TestScheduleEventsAreRecorded(t *testing.T)`

### team_registry.go

- `func (router *Router) ReadTeamRegistryWorkspace(projectID, sessionID string) (TeamRegistry, error)` — ReadTeamRegistryWorkspace 读主会话的角色注册表（项目作用域显式传入，禁止回退
- `func (router *Router) WriteTeamRegistryWorkspace(projectID, sessionID string, registry TeamRegistry) error` — WriteTeamRegistryWorkspace 整份写入主会话的角色注册表。
- `func (router *Router) RemoveTeamRegistryWorkspace(projectID, sessionID string) error` — RemoveTeamRegistryWorkspace 删除主会话的角色注册表（团队离场；幂等）。
- `func registryScope(projectID, sessionID string) storelayout.Scope` — registryScope 构造注册表的会话作用域（与 `layoutScope` 同一口径：sessionID 去空白）。
- `func (router *Router) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)` — EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false（不报错），
- `func (router *Router) ReadMessageFloorWorkspace(projectID, sessionID string) (*Floor, error)` — ReadMessageFloorWorkspace 读主会话 message head 的 floor：当前轮到这里发言的
- `func (router *Router) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error)` — ReadLifecycleOrderWorkspace 读群聊顺序策略（lifecycle head 是运行时权威）。

### team_registry_test.go

- `func TestTeamRegistryOwnerRoundTripAndIsolation(t *testing.T)` — TestTeamRegistryOwnerRoundTripAndIsolation：角色注册表整份替换落到
- `func TestTeamRegistryRejectsUnknownSchema(t *testing.T)` — TestTeamRegistryRejectsUnknownSchema：schema 版本不认识时显式报错，不静默降级。
- `func TestReadLifecycleOrderToleratesMissingHead(t *testing.T)` — TestReadLifecycleOrderToleratesMissingHead 是 Agent Team 面板报错的回归：
- `func TestReadMessageFloorWorkspaceToleratesUnstartedSession(t *testing.T)` — TestReadMessageFloorWorkspaceToleratesUnstartedSession：没有 message head 的会话
- `func TestReadMessageFloorWorkspaceFollowsSequencerSync(t *testing.T)` — TestReadMessageFloorWorkspaceFollowsSequencerSync：floor 唯一写者是 sequencer
- `func TestEnsureRoleSessionWorkspaceIsIdempotent(t *testing.T)` — TestEnsureRoleSessionWorkspaceIsIdempotent：重复装配同一条 TeamSpec 不产生第二个

### teamwork.go

- `func TeamworkPlanIsEmpty(plan TeamworkPlan) bool`
- `func ValidateTeamworkPlan(plan TeamworkPlan, maxTeammates int) error`
- `func TeamworkWorkItemStatusOrPending(item TeamworkWorkItem) string`
- `func TeamworkWorkItemTerminal(item TeamworkWorkItem) bool`
- `func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding`

### teamwork_accessor_test.go

- `func TestTeamworkAccessorNarrowsRepository(t *testing.T)`

### teamwork_items_test.go

- `func itemPlan() TeamworkPlan` — itemPlan 是一份**里程碑 + Work Item** 口径的计划：里程碑之间靠屏障串行，
- `func TestValidateTeamworkPlanAcceptsMilestoneWorkItems(t *testing.T)`
- `func TestValidateTeamworkPlanRejectsCrossMilestoneItemDependency(t *testing.T)`
- `func TestValidateTeamworkPlanRejectsItemCycles(t *testing.T)`
- `func TestValidateTeamworkPlanRejectsMilestoneCycles(t *testing.T)`
- `func TestValidateTeamworkPlanRejectsItemWithUnenrolledRole(t *testing.T)`
- `func TestValidateTeamworkPlanRejectsDuplicateItemIDAcrossMilestones(t *testing.T)`
- `func TestTeamworkBindingLedgerIsAppendOnlyKV(t *testing.T)` — TestTeamworkBindingLedgerIsAppendOnlyKV 钉住绑定账本的 KV 语义：
- `func TestAppendTeamworkBindingRequiresWorkItem(t *testing.T)`

### teamwork_plugins_test.go

- `func pluginPlan(members ...TeamworkMember) TeamworkPlan` — pluginPlan 造一份最小合法计划（存储层的校验要求至少一份里程碑），成员带插件清单。
- `func TestValidateTeamworkPlanPluginStructure(t *testing.T)` — TestValidateTeamworkPlanPluginStructure 钉住存储层的**结构**关口：插件清单可被

### teamwork_test.go

- `func teamworkFixture(t *testing.T) (*jsonRepository, Key)`
- `func samplePlan() TeamworkPlan`
- `func TestTeamworkJobOutputDirIsSessionScopedAndIdempotent(t *testing.T)`
- `func TestTeamworkPlanRoundTrip(t *testing.T)`
- `func TestReadTeamworkPlanMissingIsEmptyPlan(t *testing.T)` — 计划**缺失** = "还没有 team_plan 过"，是一个状态，不是错误（2026-10-15 契约变更）。
- `func TestTeamworkPlanRejectsDynamicViolations(t *testing.T)`
- `func TestTeamworkMaxTeammatesDefaultsToSix(t *testing.T)`
- `func TestTeamworkAuditIsAppendOnly(t *testing.T)`
- `func TestTeamworkAuditSkipsCrashTail(t *testing.T)`
- `func TestTeamworkStorageKeepsPlanAndAuditSeparate(t *testing.T)`
- `func TestTeamworkPlanClosedRoundTrip(t *testing.T)` — TestTeamworkPlanClosedRoundTrip：闭板三字段（state / closed_at / closed_reason）
- `func TestTeamworkPlanColdReadsLegacyStagesPayload(t *testing.T)` — TestTeamworkPlanColdReadsLegacyStagesPayload：阶段制时代留在计划里的 `stages`
- `func TestTeamworkAuditColdReadsLegacyRetireRow(t *testing.T)` — TestTeamworkAuditColdReadsLegacyRetireRow：旧口径 `team_retire` 留在审计流里的

### unit_record_writer.go

- `func NewNodeSessionRecordWriter(recorder workunit.Recorder) *NodeSessionRecordWriter` — NewNodeSessionRecordWriter 以既有存储为末端起一条链。

### unit_record_writer_test.go

- `func newUnitRecordWriterFixture(t *testing.T) (*NodeSessionStore, *NodeSessionRecordWriter)`
- `func TestUnitRecordWriterChainFillsWithoutClobbering(t *testing.T)`
- `func TestUnitRecordWriterWithoutStoreFailsLoud(t *testing.T)` — TestUnitRecordWriterWithoutStoreFailsLoud —— 链没装末端（未装配存储）时**显式报错**：
