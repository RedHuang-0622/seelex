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
