# store/role

## 生态位

角色身份、策略、draft、权限与角色会话路径的限界上下文实现。M8 把 team registry
（`session/team/roles.json`）迁入本包；M9 完成 role draft、角色会话子树、
permission 与 floor/message sync 端口。主要调用方是根 `store.Router` 的迁移期
单层转发及 Middle/Backend 角色用例。

## 职责与非职责

- 职责：team registry 整份原子替换；role draft WAL 与残尾修复；permission
  文件/缓存/锁序；角色会话路径与提交身份；sync 的“先发布后删 draft”判据。
- 非职责：团队计划编排（归 `store/teamwork`）、主 message head/floor 的底层
  提交机制、worker 执行、job 调度或跨域事务。

### 为什么是 `store/role` 而不是 `store/teamwork`

1. **内容是角色身份与策略**：`RoleSpec` 描述"这个会话里有哪些角色、各自用哪个 system
   prompt / 模型策略 / 工具范围 / 顺序优先级"。它不是 teamwork 计划（计划是"谁、什么
   顺序"的硬编排），也不属于会话消息或生命周期。
2. **物理位置就是本领域子树**：`<sessionRoot>/team/roles.json` 与 M9 的角色会话子树、
   role draft、permission、floor/message lifecycle 同处 `<sessionRoot>/team`。让 teamwork
   包持有它，会把角色身份拆到两个限界上下文。
3. **读写语义独立**：整份原子替换、无 head 水位、不写 message、不碰 sequencer——与
   teamwork 的"head + 两条追加流水"是三套机制，不能共用一个 owner。

## 目录结构

- `registry.go`：常量、`RoleSpec` / `Registry` 形状、`Normalize` 判据与 `Store` 读写删除。
- `draft.go`：append-only draft WAL、残尾修复、排序、commit id、`SyncDraft`。
- `permission.go`：会话/角色默认权限、账号/继承/SSH 白名单的持久化与缓存。
- `session_path.go`：`goal_*` / `role_*` 子树路径、session/backup commit identity。
- 对应 `_test.go`：registry、draft 半失败、permission 三态/并发、路径与身份稳定性。

## 核心实现

`Registry` 是 `roles.json` 的完整内容（整份替换型）；`Configured` 由读侧按文件是否存在
推导，不落盘（`json:"-"`）。`SchemaVersion` 固定为 1，未知版本写入显式报错、读侧照旧。
`Normalize` 去掉 `team_id` / `team_kind` / `order_policy` 两端空白，把角色条目规整为
可比较形态（`role_kind` 兜底 `agent`，字符串集合去空去重排序），按
`OrderPriority` → `RoleName` 稳定排序；空 `role_name` 与重复 `role_name` 显式报错。
`Store.Path` 是 `<sessionRoot>/team/roles.json` 的唯一定义。

`SyncDraft` 先读 draft、稳定排序并计算 commit id；main message 端口成功发布
head/floor 后才删除 draft。端口失败或 head 读取失败时 draft 必须保留，重跑通过
`AlreadySynced` 分支只清理半同步窗口。

## 数据流与生命周期

写入：调用方给出完整 `Registry` → `Normalize` 校验并规整 → 打 `updated_at` →
路径锁内 `MkdirAll(<sessionRoot>/team, 0700)` → `storeroot.WriteAtomic(path, data, 0600)`
整份替换。读取：文件不存在返回 `Configured=false` 的空注册表（未配置不是错误）；文件
损坏显式报错，不返回半份配置；`schema_version` 为 0 的旧文件按当前版本读入。删除：
文件不存在即视为已经离场（幂等），不写空注册表。

## 依赖方向

允许依赖 `store/internal/layout`、`store/root` 与标准库。禁止 import 根 `store`、
application、middle、backend、wiring，也禁止 import 其他 `store/<domain>` 实现。

## 并发、存储与错误语义

同一物理注册表路径映射到同一把 `sync.Mutex`（按路径装载），只串行化同路径的整份替换；
不同会话互不阻塞。写入先规范化再碰磁盘：校验失败时磁盘一个字节都不动。原子替换成功后
才发布，读侧永远看不到半份注册表。删除与读取对"文件不存在"的语义必须一致：读写删除
都把缺失当成正常状态。

## 扩展方式

M9 的角色会话子树、draft、permission 与 floor/message lifecycle 应新增文件在本包内实现，
复用 `Layout` 提供的会话根与锁身份，不再往根 `storeEngine` 挂方法。新增持久化形状时
先定 DTO/形状判据，再写落盘；旧 `roles.json` 必须保持可读。

## Review 指南

重点检查：路径是否仍为 `<sessionRoot>/team/roles.json`；`Configured` 是否仍由文件存在性
推导而没有混进 JSON；规范化是否幂等且写出字节稳定；校验失败是否零落盘；删除是否仍
幂等；锁是否按物理路径独立；是否有人把角色状态写进了根 `storeEngine` 或 teamwork 包。

## 测试与验证

```text
go test ./store/role -count=1
go test ./store -run 'Role|Draft|Permission|Subagent' -count=1
go test ./e2e -run 'TestStoreDDDM9RoleWorkUnitOwnershipGate' -count=1
```

`e2e/store_ddd_teamwork_io_ownership_gate_test.go` 验证本包是 registry 的 owner，根包
只剩单层转发。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### draft.go

- `func draftLock(path string) *sync.Mutex`
- `func DraftPath(roleRoot, roleName string) string` — DraftPath 返回 `<roleRoot>/draft/<roleName>.jsonl`。
- `func AppendDraft(roleRoot, roleName, roleSessionID string, rows []DraftRow) error` — AppendDraft 追加未同步 role draft。每个角色独立锁，互不阻塞。
- `func ReadDraft(roleRoot, roleName string) ([]DraftRow, error)` — ReadDraft 读该角色的未同步 draft。文件不存在 = 没有待同步行（不是错误）；
- `func DeleteDraft(roleRoot, roleName string) error` — DeleteDraft 删除该角色的 draft 文件（= 已发布，没有待同步行）。
- `func SortDraftRows(rows []DraftRow, order []string) []DraftRow` — SortDraftRows 按 round_id → role 顺序 → unit_seq 稳定排序。这是 sequencer 的
- `func DraftCommitID(roleName, roleSessionID string, rows []DraftRow) string` — DraftCommitID 推导这批 draft 的确定性提交身份。
- `func RowsToEvents(rows []DraftRow) []protocol.TranscriptEvent` — RowsToEvents 把 draft 行转成待发布的事件行：补齐角色归属、round/unit 与
- `func SyncDraft(roleRoot, roleName, roleSessionID string, order []string, port SessionPort) (SyncResult, error)` — SyncDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等判定 →

### draft_test.go

- `func draftFixture(t *testing.T) (string, []DraftRow)` — draftFixture 提供一个独立角色根目录与一批可复用的草稿行。
- `func TestDraftPathIsUnchanged(t *testing.T)` — TestDraftPathIsUnchanged 钉住零数据迁移：草稿仍在 <roleRoot>/draft/<roleName>.jsonl。
- `func TestAppendReadRoundTripKeepsFields(t *testing.T)` — TestAppendReadRoundTripKeepsFields：写入再读回必须逐字段相等，且磁盘行的 JSON
- `func TestAppendFillsMissingOwnership(t *testing.T)` — TestAppendFillsMissingOwnership：空归属必须由参数补齐，已有归属绝不覆盖。
- `func TestAppendRequiresRoleNameAndToleratesEmpty(t *testing.T)` — TestAppendRequiresRoleNameAndToleratesEmpty：空 role 名是调用错误；空行集是空操作。
- `func TestReadDraftMissingFileIsEmpty(t *testing.T)` — TestReadDraftMissingFileIsEmpty：文件不存在 = 没有待同步行，不是错误。
- `func TestReadDraftSkipsTruncatedTailAndBadLines(t *testing.T)` — TestReadDraftSkipsTruncatedTailAndBadLines：末尾无换行的半行是崩溃残尾，只读完整行；
- `func TestAppendTruncatesCrashTailThenAppends(t *testing.T)` — TestAppendTruncatesCrashTailThenAppends：残尾先被截断，再追加新行——既不丢已完整
- `func TestConcurrentAppendKeepsEveryRow(t *testing.T)` — TestConcurrentAppendKeepsEveryRow：同角色并发追加必须一行不丢、一行不重。
- `func TestSortDraftRowsOrder(t *testing.T)` — TestSortDraftRowsOrder：round → 顺序表内排名 → 未上表角色名 → unit_seq，且入参不被改。
- `func TestDraftCommitIDIgnoresOrderAndFollowsContent(t *testing.T)` — TestDraftCommitIDIgnoresOrderAndFollowsContent：身份对行序不敏感，对内容敏感。
- `func TestRowsToEventsFillsOwnershipAndKind(t *testing.T)` — TestRowsToEventsFillsOwnershipAndKind：draft 行转事件时必须补齐归属/round/unit 与
- `func (port *fakeSessionPort) MessageHead() (storesession.MessageHead, error)`
- `func (port *fakeSessionPort) CommitSync( commitID string, rows []protocol.TranscriptEvent, floor *storesession.Floor, ) (storesession.MessageHead, error)`
- `func TestSyncDraftEmptyDraftIsNoop(t *testing.T)` — TestSyncDraftEmptyDraftIsNoop：没有待同步行时零结果、零端口调用。
- `func TestSyncDraftPublishesAndDeletesDraft(t *testing.T)` — TestSyncDraftPublishesAndDeletesDraft：端口成功后草稿删除，计数与 head 读数透传。
- `func TestSyncDraftKeepsDraftWhenCommitFails(t *testing.T)` — TestSyncDraftKeepsDraftWhenCommitFails：端口失败 → 返回错误且草稿**仍在**
- `func TestSyncDraftHeadErrorKeepsDraft(t *testing.T)` — TestSyncDraftHeadErrorKeepsDraft：幂等判定读 head 失败也不得动草稿。
- `func TestSyncDraftAlreadySyncedOnlyCleansDraft(t *testing.T)` — TestSyncDraftAlreadySyncedOnlyCleansDraft：head 已带同一 commit_id（半同步窗口）→
- `func TestDeleteDraftRemovesFile(t *testing.T)` — TestDeleteDraftRemovesFile：删除后读面回到空集。

### permission.go

- `func NewPermissionStore(layout ProjectLayoutPort) *PermissionStore` — NewPermissionStore 构造会话级角色权限登记存储。
- `func (store *PermissionStore) Vectors(projectID, sessionID string) (map[string][]string, error)` — Vectors 返回该会话下**所有**已登记角色的权限向量（一次读盘，会话加载点用）。
- `func (store *PermissionStore) Vector(projectID, sessionID, role string) ([]string, bool, error)` — Vector 读单个角色的权限向量；第二个返回值报告"是否显式登记过"（false 即从未
- `func (store *PermissionStore) Put(projectID, sessionID, role string, vector []string) (RolePermissionRecord, error)` — Put 写入（或覆盖）一个角色的权限向量，返回落盘后的记录。
- `func (store *PermissionStore) Drop(projectID, sessionID, role string) error` — Drop 删除单个角色的登记（= 下一次加载重新按链路解析）。
- `func (store *PermissionStore) DropSession(projectID, sessionID string) error` — DropSession 清掉一个会话的全部角色登记（会话删除时用；不做任何"保留最近一条"
- `func (store *PermissionStore) Defaults(projectID string) (map[string][]string, error)` — Defaults 返回项目下**所有**角色级默认向量（键 = 角色名）。
- `func (store *PermissionStore) Default(projectID, role string) ([]string, bool, error)` — Default 读单个角色的默认向量；第二个返回值报告"是否显式定过默认"。
- `func (store *PermissionStore) RoleAccount(projectID, role string) (string, bool, error)` — RoleAccount 读取角色默认使用的账号名；第二个返回值报告是否显式设置过。
- `func (store *PermissionStore) SetRoleAccount(projectID, role, account string) error` — SetRoleAccount 设置角色默认账号。账号与权限默认共用同一条记录，
- `func (store *PermissionStore) RoleInheritance(projectID, role string) (bool, bool, error)` — RoleInheritance 读角色是否继承 mainagent 的默认权限。第二个返回值报告是否显式
- `func (store *PermissionStore) SetRoleInheritance(projectID, role string, inherit bool) error` — SetRoleInheritance 设置角色默认权限是否继承 mainagent；不影响会话显式登记。
- `func (store *PermissionStore) RoleSSHUserSpaces(projectID, role string) ([]string, error)` — RoleSSHUserSpaces 读取 SSH 能力可访问的远端路径前缀。
- `func (store *PermissionStore) SetRoleSSHUserSpaces(projectID, role string, paths []string) error` — SetRoleSSHUserSpaces 写 SSH 路径前缀。空切片表示显式不放行任何路径；
- `func (store *PermissionStore) RoleEffectiveSSHUserSpaces(projectID, sessionID, role string) ([]string, error)` — RoleEffectiveSSHUserSpaces 返回某会话中角色生效的 SSH 路径白名单：
- `func (store *PermissionStore) SetRoleSessionSSHUserSpaces(projectID, sessionID, role string, paths []string) error` — SetRoleSessionSSHUserSpaces 写入某会话中单个角色的 SSH 路径覆盖。
- `func (store *PermissionStore) PutDefault(projectID, role string, vector []string) (RolePermissionRecord, error)` — PutDefault 写入一个角色的默认向量（空集 = 该角色默认什么都不给，不删记录）。
- `func (store *PermissionStore) DropDefault(projectID, role string) error` — DropDefault 删除一个角色的默认向量（= 该角色回到链路解析）。
- `func (store *PermissionStore) sessionKeys(projectID, sessionID string) []permissionKey` — sessionKeys 枚举该会话当前已建格的坐标（索引读锁内复制）。
- `func (store *PermissionStore) entry(key permissionKey) *permissionEntry` — entry 取（必要时建）一个登记格；索引只在 indexMu 下改。
- `func (store *PermissionStore) loadEntry(entry *permissionEntry, projectID, sessionID, role string) error` — loadEntry 把该角色从盘上装进登记格（已装过则直接返回）。
- `func (store *PermissionStore) snapshot(projectID string) (permissionFile, error)` — snapshot 读一份项目文件的副本（文件锁内读，读完即放锁）。
- `func (store *PermissionStore) mutate(projectID string, apply func(*permissionFile)) error` — mutate 在项目文件锁内做一次"读-改-写"（一个项目一份文件，跨角色共用一把文件锁
- `func (store *PermissionStore) fileLock(projectID string) *sync.Mutex` — fileLock 按项目取落盘锁（不同项目可并发）。
- `func readPermissionFile(directory string) (permissionFile, error)` — readPermissionFile 读项目级角色权限文件；缺文件 = 空登记（不是错误）。
- `func writePermissionFile(directory string, file permissionFile) error` — writePermissionFile 原子写项目级角色权限文件（文件 0600，目录 0700）。
- `func normalizedRoleVector(vector []string) []string` — normalizedRoleVector 归一一份向量：去空白、丢空项、保序去重，且**永不返回 nil**

### permission_test.go

- `func newFakeLayout(t *testing.T) *fakeLayout`
- `func (layout *fakeLayout) dir(projectID string) string` — dir 复刻实现里的项目目录解析（projectID → 目录），供断言直接读盘用。
- `func (layout *fakeLayout) WithProjectDir(projectID string, fn func(dir string) error) error`
- `func (layout *fakeLayout) permissionPath(projectID string) string` — permissionPath 返回该项目的权限文件物理路径。
- `func TestPermissionPutVectorRoundTrip(t *testing.T)` — TestPermissionPutVectorRoundTrip：Put 后 Vector 命中，Drop 后回到"从未定过"。
- `func TestExplicitEmptyVectorDiffersFromUnset(t *testing.T)` — TestExplicitEmptyVectorDiffersFromUnset：显式空集是"什么都不给"，与"没定过"必须分得开，
- `func TestPermissionFileShapeAndMode(t *testing.T)` — TestPermissionFileShapeAndMode：文件名、版本、目录/文件权限与原子写后的 JSON 形状。
- `func TestCorruptPermissionFileReadsEmptyThenRewrites(t *testing.T)` — TestCorruptPermissionFileReadsEmptyThenRewrites：坏文件按空登记读（收紧方向），
- `func TestRoleDefaultsRoundTrip(t *testing.T)` — TestRoleDefaultsRoundTrip：角色级默认向量的写入/读取/删除与三态纪律。
- `func TestRoleAccountInheritanceAndUserSpaces(t *testing.T)` — TestRoleAccountInheritanceAndUserSpaces：账号 / 继承 / SSH 路径三类默认共用一条记录，
- `func TestRoleEffectiveSSHUserSpacesPrefersSession(t *testing.T)` — TestRoleEffectiveSSHUserSpacesPrefersSession：会话级覆盖优先于项目级角色默认。
- `func TestDropSessionClearsOnlyThatSession(t *testing.T)` — TestDropSessionClearsOnlyThatSession：会话删除只清自己的登记。
- `func TestConcurrentPutDifferentRolesKeepsAll(t *testing.T)` — TestConcurrentPutDifferentRolesKeepsAll：同一个项目文件被多个角色并发写，
- `func TestValidationErrorsAndUnassembledStore(t *testing.T)` — TestValidationErrorsAndUnassembledStore：参数校验与未装配端口的显式失败。

### registry.go

- `func New(layout *storelayout.Layout) *Store` — New 构造角色注册表持久化面。
- `func (s *Store) Path(scope storelayout.Scope) string` — Path 返回注册表物理路径。
- `func (s *Store) lock(path string) *sync.Mutex`
- `func Normalize(registry Registry) (Registry, error)` — Normalize 校验并规整注册表：空 RoleName 或重复 role_name 显式报错（不静默丢弃，
- `func normalizeRoleSpec(spec RoleSpec) RoleSpec` — normalizeRoleSpec 把角色条目规整为可比较形态：role_name 去空白、kind 兜底为
- `func normalizeStringSet(values []string) []string`
- `func (s *Store) Read(scope storelayout.Scope) (Registry, error)` — Read 读注册表。文件不存在返回 Configured=false 的空注册表（未配置不是错误）；
- `func (s *Store) Write(scope storelayout.Scope, registry Registry) error` — Write 整份原子替换注册表；替换成功即发布。
- `func (s *Store) Remove(scope storelayout.Scope) error` — Remove 删除角色注册表（团队离场）。文件不存在 = 已经离场（幂等，不是错误）：

### registry_test.go

- `func registryFixture(t *testing.T) (*Store, *storelayout.Layout, storelayout.Scope)`
- `func TestRegistryPathIsUnchanged(t *testing.T)` — TestRegistryPathIsUnchanged 钉住"零数据迁移"：注册表仍在 <sessionRoot>/team/roles.json。
- `func TestReadMissingMeansUnconfigured(t *testing.T)` — TestReadMissingMeansUnconfigured：文件不存在 = 没配置过团队，不是错误。
- `func TestWriteReadRoundTripAndNormalizes(t *testing.T)` — TestWriteReadRoundTripAndNormalizes：写面按规范化落盘，读面原样返回。
- `func TestWriteReplacesWholeRegistry(t *testing.T)` — TestWriteReplacesWholeRegistry：整份替换——第二份写入后第一份的角色不能残留。
- `func TestNormalizeIsIdempotent(t *testing.T)` — TestNormalizeIsIdempotent：规范化不能反复漂移（两次规整结果必须逐字相同）。
- `func TestNormalizeRejectsInvalidInput(t *testing.T)` — TestNormalizeRejectsInvalidInput：空名、重名、未知 schema 显式报错，不静默丢弃。
- `func TestWriteRejectsInvalidRegistryWithoutTouchingDisk(t *testing.T)` — TestWriteRejectsInvalidRegistryWithoutTouchingDisk：校验失败不得落盘。
- `func TestRemoveIsIdempotent(t *testing.T)` — TestRemoveIsIdempotent：删除是"团队离场"，重复删除不是错误；删除后读面回到未配置。
- `func TestReadCorruptRegistryFailsLoudly(t *testing.T)` — TestReadCorruptRegistryFailsLoudly：文件损坏必须报错，不返回半份配置。
- `func TestReadColdStartAndLegacySchemaZero(t *testing.T)` — TestReadColdStartAndLegacySchemaZero：换实例仍可读，schema 缺省按当前版本理解。
- `func TestConcurrentWritesStayWhole(t *testing.T)` — TestConcurrentWritesStayWhole：同一路径的整份替换互相串行，最终文件必须是一份完整配置。

### session_path.go

- `func SessionRoot(mainRoot, roleName, roleSessionID string) string` — SessionRoot 返回角色会话子树的物理根。main 直接使用主会话根；tl 使用
- `func SessionCommitID(roleName, roleSessionID string) string` — SessionCommitID 是角色会话创建时的提交身份。
- `func BackupCommitID(roleName, roleSessionID string, rows []protocol.TranscriptEvent) string` — BackupCommitID 是角色备份 append 的提交身份。
- `func JoinPayload(mainSessionID, roleName, roleSessionID string, joinSeq uint64) []byte` — JoinPayload 是 role_session 结构化事件的负载（字段名逐字保持）。

### session_path_test.go

- `func TestSessionRootMainReusesMainRoot(t *testing.T)` — TestSessionRootMainReusesMainRoot 钉住"main 角色就是主会话本身"：不建第二份子树。
- `func TestSessionRootPrefixByRole(t *testing.T)` — TestSessionRootPrefixByRole 钉住目录名前缀：tl → goal_<hash>，其它 → role_<hash>。
- `func TestSessionRootHashSuffixMatchesRootHash(t *testing.T)` — TestSessionRootHashSuffixMatchesRootHash 钉住 hash 判据同源：不得在本包复制一份哈希。
- `func TestSessionCommitIDIsStable(t *testing.T)` — TestSessionCommitIDIsStable 钉住角色会话创建身份的字符串形态（旧盘上已存在）。
- `func TestBackupCommitIDFollowsRows(t *testing.T)` — TestBackupCommitIDFollowsRows 钉住备份身份：同一批行稳定、内容变了就变。
- `func TestJoinPayloadKeepsFieldNames(t *testing.T)` — TestJoinPayloadKeepsFieldNames 钉住 role_session 结构化事件的负载 wire（四个键逐字保持）。
