# store/team（根包分卷）

## 生态位

团队计划/工作项、角色会话与节点会话记账

覆盖：`teamwork*.go`、`team_registry*.go`、`role_session*.go`、`node_session_store*.go`、`unit_record_writer*.go`；未归属文件由覆盖自检拦下。

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
- `func (repository *jsonRepository) WriteNodeSession(_ context.Context, key Key, record NodeSessionRecord) error`
- `func (repository *jsonRepository) ReadNodeSession(_ context.Context, key Key, subSessionID string) (NodeSessionRecord, error)`
- `func (repository *jsonRepository) ListNodeSessions(_ context.Context, key Key) ([]NodeSessionRecord, error)`
- `func (repository *jsonRepository) DeleteNodeSession(_ context.Context, key Key, subSessionID string) error`
- `func nodeSessionFileName(mainSessionID, subSessionID string) string` — nodeSessionFileName 生成 `<mainSessionID>-<subSessionID>.json` 文件名

### node_session_store_test.go

- `func TestNodeSessionStoreRoundTrip(t *testing.T)` — TestNodeSessionStoreRoundTrip 验证子代理会话记录按
- `func TestNodeSessionStoreListFromMainIndex(t *testing.T)` — TestNodeSessionStoreListFromMainIndex 验证从主会话索引直接列举全部子会话。
- `func TestNodeSessionStoreMissingAndDelete(t *testing.T)` — TestNodeSessionStoreMissingAndDelete 验证缺失语义与删除。
- `func TestNodeSessionStoreProjectIsolation(t *testing.T)` — TestNodeSessionStoreProjectIsolation 验证跨项目隔离。
- `func routerConfigPath(t *testing.T, router *Router) string` — routerConfigPath 返回测试 Router 的 JSON 存储根目录（sessions-json）。

### role_session.go

- `func roleDraftLock(path string) *sync.Mutex`
- `func (store *storeEngine) roleSessionRoot(mainKey Key, roleName, roleSessionID string) string` — roleSessionRoot 返回角色会话子树的物理根。main 直接使用主会话根；tl 使用
- `func (store *storeEngine) roleStore(mainKey Key, roleName, roleSessionID string) (*storeEngine, Key)` — roleStore 返回指定角色会话的引擎与 key。main 复用当前引擎；非 main 以角色
- `func (store *storeEngine) createRoleSession(mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error)` — createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
- `func roleSessionCommitID(roleName, roleSessionID string) string`
- `func roleDraftPath(roleStore *storeEngine, key Key, roleName string) string`
- `func appendRoleDraft(roleStore *storeEngine, key Key, roleName string, rows []RoleDraftRow) error` — appendRoleDraft 追加未同步 role draft。每个角色独立锁，互不阻塞。
- `func readRoleDraft(roleStore *storeEngine, key Key, roleName string) ([]RoleDraftRow, error)`
- `func deleteRoleDraft(roleStore *storeEngine, key Key, roleName string) error`
- `func sortRoleDraftRows(rows []RoleDraftRow, order []string) []RoleDraftRow` — sortRoleDraftRows 按 round_id → role 顺序 → unit_seq 稳定排序。这是
- `func roleDraftCommitID(roleName, roleSessionID string, rows []RoleDraftRow) string`
- `func roleDraftRowsToEvents(rows []RoleDraftRow) []Event`
- `func (store *storeEngine) syncRoleDraft(mainKey Key, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error)` — syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
- `func (store *storeEngine) readRoleSessionRows(mainKey Key, roleName, roleSessionID string) ([]Event, error)` — readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
- `func (store *storeEngine) readRoleSnapshot(mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error)` — readRoleSnapshot 汇总角色会话观察面（只读）。
- `func (store *storeEngine) assembleRoleWire(mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error)` — assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
- `func roleWireMessages(messages []wireMessage) []RoleWireMessage`
- `func cloneCompactRef(ref *CompactRef) *CompactRef`
- `func cloneFloor(floor *Floor) *Floor`
- `func roleSnapshotWarnings(snapshot RoleSnapshot) []string`
- `func (store *storeEngine) appendRoleSessionRows(mainKey Key, roleName, roleSessionID string, rows []Event) error` — appendRoleSessionRows 向角色会话写备份行。main 角色写主会话 message；非 main
- `func roleBackupCommitID(roleName, roleSessionID string, rows []Event) string`

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

- `func teamRegistryLock(path string) *sync.Mutex`
- `func (store *storeEngine) teamRegistryPath(key Key) string`
- `func normalizeTeamRoleSpec(spec TeamRoleSpec) TeamRoleSpec` — normalizeTeamRoleSpec 把角色条目规整为可比较形态：role_name 去空白、kind 兜底
- `func normalizeStringSet(values []string) []string`
- `func normalizeTeamRegistry(registry TeamRegistry) (TeamRegistry, error)` — normalizeTeamRegistry 校验并规整注册表；空 RoleName 或缺 role_name 的重复条目
- `func (store *storeEngine) readTeamRegistry(key Key) (TeamRegistry, error)` — readTeamRegistry 读注册表。文件不存在返回 Configured=false 的空注册表（未配置
- `func (store *storeEngine) writeTeamRegistry(key Key, registry TeamRegistry) error` — writeTeamRegistry 整份原子替换注册表；替换成功即发布。
- `func (store *storeEngine) removeTeamRegistry(key Key) error` — removeTeamRegistry 删除角色注册表（团队离场）。文件不存在 = 已经离场（幂等，
- `func (router *Router) ReadTeamRegistryWorkspace(projectID, sessionID string) (TeamRegistry, error)` — ReadTeamRegistryWorkspace 读主会话的角色注册表（项目作用域显式传入，禁止回退
- `func (router *Router) WriteTeamRegistryWorkspace(projectID, sessionID string, registry TeamRegistry) error` — WriteTeamRegistryWorkspace 整份写入主会话的角色注册表。
- `func (router *Router) RemoveTeamRegistryWorkspace(projectID, sessionID string) error` — RemoveTeamRegistryWorkspace 删除主会话的角色注册表（团队离场；幂等）。
- `func (router *Router) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)` — EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false（不报错），
- `func (router *Router) ReadMessageFloorWorkspace(projectID, sessionID string) (*Floor, error)` — ReadMessageFloorWorkspace 读主会话 message head 的 floor：当前轮到这里发言的
- `func (router *Router) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error)` — ReadLifecycleOrderWorkspace 读群聊顺序策略（lifecycle head 是运行时权威）。

### team_registry_test.go

- `func TestTeamRegistryRoundTripAndIsolation(t *testing.T)` — TestTeamRegistryRoundTripAndIsolation：角色注册表整份替换落到
- `func TestTeamRegistryRejectsUnknownSchema(t *testing.T)` — TestTeamRegistryRejectsUnknownSchema：schema 版本不认识时显式报错，不静默降级。
- `func TestReadLifecycleOrderToleratesMissingHead(t *testing.T)` — TestReadLifecycleOrderToleratesMissingHead 是 Agent Team 面板报错的回归：
- `func TestReadMessageFloorWorkspaceToleratesUnstartedSession(t *testing.T)` — TestReadMessageFloorWorkspaceToleratesUnstartedSession：没有 message head 的会话
- `func TestReadMessageFloorWorkspaceFollowsSequencerSync(t *testing.T)` — TestReadMessageFloorWorkspaceFollowsSequencerSync：floor 唯一写者是 sequencer
- `func TestEnsureRoleSessionWorkspaceIsIdempotent(t *testing.T)` — TestEnsureRoleSessionWorkspaceIsIdempotent：重复装配同一条 TeamSpec 不产生第二个

### teamwork.go

- `func (plan TeamworkPlan) IsEmpty() bool` — IsEmpty 报告"这份计划还没声明过团队"（team_plan 一次都没写过）。
- `func Teamwork(repository Repository) (TeamworkRepository, bool)` — Teamwork 把 Repository 收窄到 teamwork 面。JSON 后端实现它；其他后端返回 false。
- `func (router *Router) TeamworkFor() (TeamworkRepository, bool)` — TeamworkFor 返回**当前活跃后端**的 teamwork 读/写面（JSON 后端实现它；其他后端
- `func (store *storeEngine) teamworkDir(key Key) string` — teamworkDir 返回 teamwork 模块的数据目录。
- `func (store *storeEngine) teamworkEventsPath(key Key) string`
- `func (store *storeEngine) teamworkJobDir(key Key) string` — teamworkJobDir 返回 teammate 作业输出文件所在目录（产品自有；见 teamworkJobSubdir）。
- `func ValidateTeamworkPlan(plan TeamworkPlan, maxTeammates int) error` — ValidateTeamworkPlan 校验一份计划（M2 的第二道关口）。
- `func (repository *jsonRepository) WriteTeamworkPlan(_ context.Context, key Key, plan TeamworkPlan, maxTeammates int) error` — WriteTeamworkPlan 写入（整份替换）一份计划。maxTeammates <= 0 = 只做结构性校验。
- `func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (TeamworkPlan, error)` — ReadTeamworkPlan 读回计划。**缺失 = 空计划（还没 team_plan 声明过团队），不是错误**。
- `func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event TeamworkEvent) error` — AppendTeamworkEvent 追加一条审计事件（只追加、不重写）。
- `func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]TeamworkEvent, error)` — ReadTeamworkEvents 读回全部审计事件（按文件顺序 = 发生顺序）。
- `func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error)` — TeamworkJobOutputDir 返回（并确保存在）teammate 作业输出文件所在的产品自有目录。

### teamwork_accessor_test.go

- `func TestTeamworkAccessorNarrowsRepository(t *testing.T)`

### teamwork_items.go

- `func (item TeamworkWorkItem) StatusOrPending() string` — StatusOrPending 把空状态读成 pending（"空 = pending"的唯一读法）。
- `func (item TeamworkWorkItem) Terminal() bool` — Terminal 报告这个工作项是否已经有结论（done / failed）。
- `func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding` — TeamworkBindings 把绑定流水折成"每个 Work Item 当前的绑定"。
- `func validateTeamworkWorkItems(plan TeamworkPlan, roles map[string]struct{}) error` — ValidateTeamworkPlan 之外的**第二道校验**：里程碑图与 Work Item。
- `func validateMilestoneGraphAcyclic(milestones []TeamworkMilestone) error` — validateMilestoneGraphAcyclic 用 Kahn 拓扑消元检出里程碑依赖环。
- `func validateItemGraphAcyclic(milestone TeamworkMilestone) error` — validateItemGraphAcyclic 用 Kahn 拓扑消元检出**同里程碑内**工作项依赖环。
- `func (store *storeEngine) teamworkBindingsPath(key Key) string` — teamworkBindingsPath 返回绑定账本路径。
- `func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error` — AppendTeamworkBinding 追加一行绑定事实（只追加、不重写；崩溃残尾由读侧跳过）。
- `func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error)` — ReadTeamworkBindings 读回全部绑定事实（按文件顺序 = 发生顺序）。

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

- `func NewNodeSessionRecordWriter(store *NodeSessionStore) *NodeSessionRecordWriter` — NewNodeSessionRecordWriter 以既有存储为末端起一条链。
- `func (w *NodeSessionRecordWriter) With(links ...NodeSessionRecordLink) *NodeSessionRecordWriter` — With 派生一条**在外面再包一环**的链（本链不动）：新的一环先跑，之后才走本链原有的环。
- `func (w *NodeSessionRecordWriter) Save(projectID, mainSessionID string, record NodeSessionRecord) error` — Save 走完整条链再落盘（链上每一环只看得到自己这一层的事实）。
- `func (w *NodeSessionRecordWriter) Store() *NodeSessionStore` — Store 返回链的末端存储：**读面**（列举/读取/删除/项目作用域）仍走它，本类型只管

### unit_record_writer_test.go

- `func newUnitRecordWriterFixture(t *testing.T) (*NodeSessionStore, *NodeSessionRecordWriter)`
- `func TestUnitRecordWriterChainFillsWithoutClobbering(t *testing.T)`
- `func TestUnitRecordWriterWithoutStoreFailsLoud(t *testing.T)` — TestUnitRecordWriterWithoutStoreFailsLoud —— 链没装末端（未装配存储）时**显式报错**：
