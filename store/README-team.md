# store/team（根包分卷）

## 生态位

角色会话与持久记录作用域适配

覆盖：`role_session*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### role_session.go

- `func roleSessionEngine(store *storeEngine, mainKey Key, roleName, roleSessionID string) (*storeEngine, Key)` — roleSessionEngine 返回角色会话的引擎与 key：main 复用当前引擎；非 main 以
- `func roleSessionRoot(store *storeEngine, mainKey Key, roleName, roleSessionID string) string` — roleSessionRoot 返回角色会话子树的物理根（路径规则唯一实现在 store/role）。
- `func (port mainSessionPort) MessageHead() (storesession.MessageHead, error)`
- `func (port mainSessionPort) CommitSync(commitID string, rows []Event, floor *storesession.Floor) (storesession.MessageHead, error)`
- `func syncRoleDraft(store *storeEngine, mainKey Key, roleName, roleSessionID string, order []string) (storerole.SyncResult, error)` — syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
- `func createRoleSession(store *storeEngine, mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error)` — createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
- `func readRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string) ([]Event, error)` — readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
- `func readRoleSnapshot(store *storeEngine, mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error)` — readRoleSnapshot 汇总角色会话观察面（只读）。
- `func assembleRoleWire(store *storeEngine, mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error)` — assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
- `func cloneCompactRef(ref *CompactRef) *CompactRef`
- `func cloneFloor(floor *Floor) *Floor`
- `func appendRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string, rows []Event) error` — appendRoleSessionRows 向角色会话写备份行。main 角色写主会话 message；非 main

### role_session_adapter.go

- `func NewRoleSessionAdapter(router *Router) *RoleSessionAdapter` — NewRoleSessionAdapter constructs the Role session storage adapter.
- `func (adapter *RoleSessionAdapter) CreateRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)` — CreateRoleSessionWorkspace 创建非 main 角色会话（TL/agent-team）。main 角色
- `func (adapter *RoleSessionAdapter) AppendRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []storerole.DraftRow) error` — AppendRoleDraftWorkspace 向角色自己的 draft 追加未同步行。
- `func (adapter *RoleSessionAdapter) ReadRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]storerole.DraftRow, error)` — ReadRoleDraftWorkspace 读取角色未同步 draft。
- `func (adapter *RoleSessionAdapter) SyncRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, order []string) (storerole.SyncResult, error)` — SyncRoleDraftWorkspace 由 sequencer 同步角色 draft 到主会话 message，成功后
- `func (adapter *RoleSessionAdapter) AppendRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []Event) error` — AppendRoleSessionRowsWorkspace 向角色会话写备份行（非 main 角色备份；main 会
- `func (adapter *RoleSessionAdapter) ReadRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]Event, error)` — ReadRoleSessionRowsWorkspace 读取角色会话备份行。
- `func (adapter *RoleSessionAdapter) RoleSnapshotWorkspace(projectID, mainSessionID, roleName, roleSessionID string) (RoleSnapshot, error)` — RoleSnapshotWorkspace 返回角色会话的只读观察面（headless 冒烟/巡检用）。
- `func (adapter *RoleSessionAdapter) AssembleRoleWireWorkspace(projectID, mainSessionID, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error)` — AssembleRoleWireWorkspace 构造角色可见物化 wire（main compact_ref + 已发布
- `func (adapter *RoleSessionAdapter) SetLifecycleOrderWorkspace(projectID, sessionID, policy string, roles []string) error` — SetLifecycleOrderWorkspace 写主会话（或角色会话）的群聊顺序策略。
- `func (adapter *RoleSessionAdapter) SetRoleLifecycleWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *CompactRef) error` — SetRoleLifecycleWorkspace 写角色会话的 join_seq_id 与 compact_ref。
- `func (adapter *RoleSessionAdapter) withRoleStore(projectID, mainSessionID, roleName, roleSessionID string, fn func(*storeEngine, Key) error) error`
- `func (adapter *RoleSessionAdapter) ListRoleSessionsWorkspace(projectID, mainSessionID string) ([]string, error)` — ListRoleSessionsWorkspace 枚举主会话下的角色会话目录名（goal_<hash>/ 与
- `func (adapter *RoleSessionAdapter) ListRoleSessions(mainSessionID string) ([]string, error)` — ListRoleSessions is the non-workspace variant using the active project.
- `func readDirNames(path string) ([]string, error)`
- `func (adapter *RoleSessionAdapter) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)` — EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false。
- `func (adapter *RoleSessionAdapter) ReadMessageFloorWorkspace(projectID, sessionID string) (*Floor, error)` — ReadMessageFloorWorkspace 读 message head 的 floor；空会话返回 nil。
- `func (adapter *RoleSessionAdapter) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error)` — ReadLifecycleOrderWorkspace 读 legacy lifecycle 顺序；缺失 head = 空顺序。
- `func registryScope(projectID, sessionID string) storelayout.Scope` — registryScope 保留给旧 roles.json fixture 冷读测试与兼容诊断。

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
