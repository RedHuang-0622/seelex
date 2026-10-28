# kernel/limit/tier

## 生态位

`kernel/limit/tier` 是「主会话权限档位」的落点：**会话级持久化**、**生效档位解析**，以及
**执行门 / 审批面 / 视图快照**三处的同步口径。

主要调用方：

- `application/assembly`（门面：`SetPermissionTier` / `SetFullAccess` 用例，以及切换路径）——
  经 `application/policy.TierDeps/TierGate` identity alias 装配宿主面后调用本包；
  门面侧只留一行转发与宿主适配；
- 迁移前这些判据散在 `application/assembly/{session_permission_tier.go,session_scope.go,service_interaction.go}`，
  2026-10-16 随 M5-R-limitc **接收者转移**到此处（plan §2.8）。

## 职责与非职责

**做**：

1. 解析某会话的**生效档位**（会话槽优先，未选择回退进程默认；脏值不静默降级/放行）；
2. 会话级设置的**读写**（`Persist` / `Read`，含"未装配端口 = 内存态降级"）；
3. 把档位**落地**到会话槽 + 执行门 + 审批自动放行（`ApplyTier`），并在会话切换恢复时把
   视图快照重算成该会话的值（`SyncViewLocked`）。

**不做**：不持有状态、不加锁；不定义档位词表（在 `contract/dto`：
`NormalizePermissionTier` / `PermissionTierIsFullAccess` / `PermissionTierManual`）；不实现执行门
（`backend/tool/registry` 的 `PermissionGate`）与审批 broker（`backend/limit/approval`）——它只调用它们。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `tier.go` | `Deps`（宿主面）+ `SettingStore` / `Units` / `View` / `Gate` 端口 + 全部判据与落地动作 |
| `tier_test.go` | 端口契约用例（有效档位解析与回退、脏值不落槽但快照仍重算、未装配端口的降级、非视图会话不写快照） |

## 核心实现

- `Deps` 是**值语义**的宿主面集合：`Settings`（会话级设置，nil = 内存态降级）、`Units`（会话槽，
  Unit 自带锁）、`Approval`（`contract.ApprovalBroker`，nil = 无审批面）、`View`（视图会话 ID 与
  锁内写快照）、`Gate`（写执行门）、`DefaultTier`（进程默认档位）。
- 加锁纪律：`Deps` 自己不加锁。`SnapshotSessionID` / `WriteTier` 由**已持宿主视图锁**的调用方使用；
  `ViewSessionID` 由实现方自行持锁、只该在锁外调用。
- 一次切换要保的不变量：**快照里的档位 == 当前视图会话真正生效的档位**。漏掉一次，用户看到的就是
  上一个会话的档位——"以为免审其实要问人"只是别扭，"以为要问人其实免审"是安全问题。

## 数据流

```text
用户切档（门面 SetPermissionTier）
  词表归一（contract/dto）→ Persist（先落盘）→ ApplyTier（槽 + 门 + 审批面）
  → 门面写快照并发布 runtime.changed

会话切换 / 冷加载恢复
  Read（读不到按"未选择"）→ ApplyStored（脏值不落槽，但快照仍重算）→ SyncViewLocked
```

## 依赖方向

允许：标准库、`contract`（含 `contract/dto` 词表）、`contract.ApprovalBroker`。
禁止：`application/*`、`session`（会话域由宿主经 `Units` / `Settings` 端口代劳）、`store`。

## 并发、存储与错误语义

- 本包**无状态**：`Deps` 每次调用由宿主装配一次，方法本身不持锁、可并发调用（宿主端口各自持锁）。
- `Read` **含磁盘读**：调用方不得持有宿主视图锁（否则与切换路径互锁）。
- 存储语义：档位落"会话级用户设置"（项目级会话元数据 blob），不是 `SessionRecord`
  ——后者在当前存储布局下写不进去也读不回来（理由逐字记在门面文件头）。
- 错误语义：写盘失败**显式报错并保持原档位**；读失败按"未选择"处理（档位是增强项，不该阻断切换）。

## 扩展方式

- 新增一种"档位来源"：只改 `EffectiveTier` 的优先级链（一处判据），不要在调用方再写一份解析。
- 新增一个落地面（比如另一种门）：在 `ApplyTier` 里加一格、宿主面加一个端口；不要把落地顺序复制到调用方。

## Review 指南

- `EffectiveTier` 的优先级是否被改动？脏值是否仍**不落会话槽**（避免静默降级/静默放行）？
- 快照重算是否仍在"同一次临界区"里发生（会话切换路径）？非视图会话是否仍不写视图快照？
- 有没有第二份"生效档位"解析或第二份"落地动作"出现在调用方？
- `Deps` 是否被塞进状态/锁（破坏"无状态、宿主持锁"的口径）？

## 测试与验证

```text
go test ./kernel/limit/tier/ -count=1
go test ./application/assembly/ -run 'PermissionTier|Permission' -count=1   # 门面级集成（切档、切会话镜像、脏值）
go test ./backend/limit/approval/ -count=1                              # 审批面按会话归属
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### tier.go

- `func (d Deps) EffectiveTier(sessionID string) string` — EffectiveTier 返回指定会话生效的权限档位（G4：会话槽优先；未选择回退进程默认）。
- `func (d Deps) FullAccessFor(sessionID string) bool` — FullAccessFor 是档位的**兼容派生读面**：full 档 ⇔ 旧的全权开启。
- `func (d Deps) Persist(sessionID, tier string) error` — Persist 把档位写进会话级设置（"" = 清除该会话的选择）。
- `func (d Deps) Read(sessionID string) string` — Read 读取该会话持久化的权限档位（"" = 从未选择 / 未装配设置端口 / 读失败）。
- `func (d Deps) ApplyStored(sessionID, tier string)` — ApplyStored 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动放行，并把**视图会话**的
- `func (d Deps) ApplyTier(sessionID, normalized string)` — ApplyTier 把**已校验**的档位落到会话槽 + 执行门 + 审批面（不改视图快照；快照由调用方按
- `func (d Deps) RestoreFor(sessionID string)` — RestoreFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。
- `func (d Deps) SyncViewLocked(sessionID string)` — SyncViewLocked 把 sessionID 的**生效档位**重算进进程快照，保证「快照里的档位 == 该会话
- `func (d Deps) SyncGateFor(sessionID string)` — SyncGateFor 按会话档位同步执行门（G4：chat 起点调用，保证每个会话都按自己的选择运行——

### tier_test.go

- `func (units *fakeUnits) UnitPermissionTier(sessionID string) (string, bool)`
- `func (units *fakeUnits) SetUnitPermissionTier(sessionID, tier string)`
- `func (view *fakeView) ViewSessionID() string`
- `func (view *fakeView) SnapshotSessionID() string`
- `func (view *fakeView) WriteTier(sessionID, tier string)`
- `func (settings *fakeSettings) SessionPermissionTier(sessionID string) (string, error)`
- `func (settings *fakeSettings) SetSessionPermissionTier(sessionID, tier string) error`
- `func (approval *fakeApproval) SetPermissionAutoApprovalFor(sessionID string, on bool)`
- `func TestEffectiveTierPrefersSlotThenDefault(t *testing.T)` — TestEffectiveTierPrefersSlotThenDefault：会话槽优先；未选择回退进程默认；都没有 = manual。
- `func TestApplyTierTouchesSlotGateAndApproval(t *testing.T)` — TestApplyTierTouchesSlotGateAndApproval：一次落地写三处（槽 / 门 / 审批面）。
- `func TestApplyStoredKeepsSlotCleanButMirrorsView(t *testing.T)` — TestApplyStoredKeepsSlotCleanButMirrorsView：脏值不落槽，但视图快照仍要重算；
- `func TestSyncViewLockedOnlyForViewSession(t *testing.T)` — TestSyncViewLockedOnlyForViewSession：只有当前视图会话会被写；空 ID 不写。
- `func TestPersistAndReadDegradeWithoutSettings(t *testing.T)` — TestPersistAndReadDegradeWithoutSettings：未装配端口 = 内存态降级（不报错、读回空）。
