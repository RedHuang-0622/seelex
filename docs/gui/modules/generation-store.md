# Generation Repository 模块详细设计

> 状态：拟议方案
> 总体架构：[`../architecture.md`](../architecture.md)
> 契约：[`../schemas/generation-manifest.schema.json`](../schemas/generation-manifest.schema.json)

## 1. 职责与边界

Generation Repository 为每个 session 保存不可变 checkpoint，负责 staging、内容 hash、manifest 校验、原子发布、current 指针、枚举、回退和回收。它不解释 Conversation/Card 业务，不创建 Engine，也不把存储路径暴露给 GUI/HTTP。

## 2. 调用方接口

接口计划定义在 `application`：

```go
type GenerationRepository interface {
    Begin(context.Context, SessionID, ParentGenerationID) (GenerationWriter, error)
    Current(context.Context, SessionID) (Generation, error)
    Open(context.Context, SessionID, GenerationID) (Generation, error)
    List(context.Context, SessionID, PageQuery) (GenerationPage, error)
    Rollback(context.Context, SessionID, GenerationID, ExpectedGenerationID) error
}

type GenerationWriter interface {
    Put(context.Context, ResourceDescriptor, io.Reader) error
    Commit(context.Context, CommitMetadata) (Generation, error)
    Abort(context.Context) error
}
```

Writer 是一次性状态机：`open → committed | aborted`。`Commit`/`Abort` 重复调用返回 typed error；析构或 context cancel 不能把 staging 发布为 committed。

## 3. 发布状态机

```text
Begin → Staging → ResourcesWritten → ManifestVerified
                                      ├─ Commit → Published → CurrentUpdated
                                      └─ Abort  → Garbage
```

- 每 session 同时最多一个 Commit 临界区；资源生成可在锁外完成。
- 最终目录与 staging 必须在同一文件系统，以保证 rename 原子性。
- `current` 通过临时文件、flush 和 atomic replace 更新。
- 进程在任一点崩溃时，恢复结果只能是旧 current 或完整新 current，不能是半 generation。

## 4. 完整性与兼容性

读取顺序为 manifest schema → protocol compatibility → resource path/size count → resource hash → content hash → parent metadata。任何失败都返回 diagnosis，不自动覆盖损坏目录。

默认限制通过构造配置注入：单资源大小、generation 总大小、资源数量、父链深度、保留数量和 staging TTL。资源路径必须通过独立 PathPolicy；Schema 校验不是文件系统安全边界。

## 5. 并发与锁

- repository registry 锁只保护 writer/current 元数据，不覆盖文件复制、hash 或 flush。
- 同 session 的两个 commit 通过 expected parent/current 做 compare-and-swap，失败方返回 `GENERATION_CONFLICT`。
- 不同 session 可并行提交；全局回收器不能删除 active writer、current 或 pinned generation。
- rollback 只切换 current 指针，不修改目标 generation；与 commit 使用同一 session CAS。

## 6. 恢复与回收

启动优先读取 current；无效时按已验证的提交时间和 parent 链寻找最近完整 generation。自动回退必须记录 audit/diagnosis，并保留坏 generation 供调查。

回收只删除：未被 current/parent protection/pin 引用、超过保留窗口且无 reader lease 的 generation。staging 使用独立 TTL；清理前验证绝对路径仍位于 session store 根目录。

## 7. 错误

| code | 含义 |
|---|---|
| `GENERATION_CONFLICT` | current 已变化 |
| `GENERATION_INCOMPLETE` | 缺 manifest 或资源 |
| `GENERATION_HASH_MISMATCH` | 内容完整性失败 |
| `GENERATION_INCOMPATIBLE` | schema/protocol 不支持 |
| `GENERATION_LIMIT_EXCEEDED` | 数量或大小超限 |
| `GENERATION_POINTER_FAILED` | generation 完整但 current 更新失败 |

## 8. 测试与验收

必须覆盖每个发布步骤后的崩溃、重复 commit、CAS 冲突、坏 hash、路径逃逸、跨设备 rename、current 半写、父链断裂、reader/GC 并发和 Windows rename 语义。验收时运行 [`../recipes/commit-generation.md`](../recipes/commit-generation.md)、回滚、重建与故障恢复演练。
