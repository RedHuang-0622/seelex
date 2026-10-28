# store/internal/layout

## 生态位

JSON v8 会话布局机制 owner：物理路径、guide、module head 信封/校验和，以及按会话
与模块分片的锁身份。唯一调用方是根 `store`；领域重建策略仍留在拥有数据的 store
领域代码。

## 职责与非职责

- 职责：稳定路径派生、guide 幂等创建、head 原子发布/严格读/宽容读、
  有界自愈重读顺序、锁一对一。
- 非职责：解释 message/event/compact payload、按数据文件重建领域 head、决定模块
  是否启用，或读写用户内容。

## 文件索引

- `layout.go`：`Scope`、`ModuleID`、`Guide`、`Head`、`Layout` 及路径/锁/head 原语。
- `layout_test.go`：路径、guide wire、head checksum/payload 与锁身份。

## 数据流

领域调用方持对应模块锁 → 先把不可变数据写到物理通道 → `Publish` 编码 head 信封、
计算 checksum 并原子替换 `metadata/<module>.json`。读者经 `ReadHead` 做共享读并
校验 schema/session/module/checksum；`ReadWithHeal` 拥有“重读一次 → 可选锁 →
领域 repair”的顺序，payload 如何重建仍由调用方回调提供。

## 依赖方向

只依赖标准库与 `store/root`。禁止 import 根 `store`、任何领域包、application、
middle、backend 或 wiring。

## 兼容约束

`project-<hash>/session-<hash>/metadata/<module>.json`、guide/head JSON 字段、
checksum 算法、锁粒度和原子发布方式必须逐字保持。模块 ID 只是元数据名字，不能
改变物理子目录。

## Review 指南

检查同一模块是否始终得到同一锁、不同模块是否绝不别名、未知根模块是否在根适配层
显式失败、head 是否只在数据完成写入后发布，以及布局包是否开始解释领域 payload。

## 验证

```text
go test ./store/internal/layout ./store/... -run 'Head|Lock|Atomic|Pending|Guide' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### layout.go

- `func New(root string) *Layout` — New constructs a layout rooted at the session JSON data root.
- `func (l *Layout) SessionRoot(scope Scope) string` — SessionRoot returns project-<hash>/session-<hash> beneath the data root.
- `func (l *Layout) MetadataDir(scope Scope) string` — MetadataDir returns the session metadata directory.
- `func (l *Layout) ModulePath(scope Scope, module ModuleID) string` — ModulePath returns metadata/<module>.json.
- `func (l *Layout) GuidePath(scope Scope) string` — GuidePath returns metadata/guide.json.
- `func IsSessionDir(sessionRoot string) bool` — IsSessionDir reports whether a directory contains the JSON v8 guide.
- `func (l *Layout) Lock(scope Scope, module ModuleID) *sync.Mutex` — Lock returns one stable mutex for a session/module pair. Distinct module IDs
- `func (l *Layout) guideLock(scope Scope) *sync.Mutex`
- `func (l *Layout) EnsureGuide(scope Scope) (Guide, error)` — EnsureGuide idempotently creates or reads the session guide.
- `func Checksum(head Head) (string, error)` — Checksum returns the checksum of the envelope excluding its checksum field.
- `func PayloadCommitID(module ModuleID, payload any) string` — PayloadCommitID derives a deterministic commit id from a module head payload.
- `func (l *Layout) Commit(scope Scope, module ModuleID, commitID string, payload any, updatedAt time.Time) error` — Commit is the whole-replacement write of one module head: ensure the guide,
- `func (l *Layout) Publish(scope Scope, module ModuleID, commitID string, payload any, updatedAt time.Time) (string, error)` — Publish atomically writes a module head.
- `func (l *Layout) ReadHead(scope Scope, module ModuleID) (Head, error)` — ReadHead reads and validates one module head.
- `func (l *Layout) ReadLenientHead(scope Scope, module ModuleID) (Head, bool)` — ReadLenientHead reads only enough of the envelope to recover repair metadata.
- `func (l *Layout) ReadWithHeal( scope Scope, module ModuleID, lockHeld bool, lock func() *sync.Mutex, beforeRetry func(), repair func(error) error, ) (Head, error)` — ReadWithHeal performs the bounded invalid-head recovery sequence:
- `func IsNotExist(err error) bool` — IsNotExist reports the shared missing-state classification.

### layout_test.go

- `func TestPathsGuideHeadAndLocks(t *testing.T)`
- `func TestReadWithHealRunsDomainRepairAfterBoundedRetry(t *testing.T)`
- `func hash16(value string) string`
