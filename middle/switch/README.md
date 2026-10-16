# middle/switch

> 包名 = `switching`（目录名不能当包名的情形：`switch` 是 Go 关键字，与 `middle/view/state` 落成
> `view_state` 是同类处理）。

## 生态位

`middle/switch` 是中间层"**会话切换与多会话运行**"的落点（plan §2.4：`middle/residency` 管 LRU 驻留、
`middle/switch` 管多会话运行切换）。当前已有：

- 本包（`switching`）：**会话展示元数据**的读写判据（`session_meta.go` 家族）；
- 子域 [`history/`](history/README.md)：历史分页的**窗口装配判据**。

生命周期 / 切换 / 队列 / 驻留调度器家族（`session_{lifecycle,release,draft,fork,ctx,pending_tail,queue_recover}`、
`service_{scheduler,queue,release_window}`、`async_completion`、`service_interaction`、`workspace_usecase`、
顶层 `session/`）随 R6 续做搬入。

## 职责与非职责

**做**：

1. `SetMeta`：写单个会话的展示元数据（置顶 / 别名 / 手动排序位），成功后唤醒目录刷新让所有客户端重拉；
2. `ReadMeta`：读单个会话的展示元数据；
3. 释放面判据（`release.go`，M5-R6-release-surface 接收者转移）：`Busy`（待审批 / 运行中 / 排队输入 /
   在飞请求都算忙）与 `Protected`（受保护集：当前视图 / 待审批 / 运行·排队·在飞 / 正在恢复）——
   引擎驻留层（`resident_lru`）与正文层（`content_lru`）的收敛循环都只调这一份；
4. 判据与文案只此一份：空会话 ID 拒绝（`session ID is required`）、未装配存储
   （`ErrMetaUnsupported` = "session meta storage is not assembled"）、写盘失败冒泡且**不**唤醒。

**不做**：不做会话事实（谁在跑、绑定哪个工作区）；不持久化（元数据落在存储层的项目级 meta blob，
由宿主端口 `MetaStore` 代劳）；不发布事件（目录刷新由宿主的 `Wake` 回调负责）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `meta.go` | `MetaDeps`（`MetaStore` + `Wake`）+ `SetMeta` / `ReadMeta` + `ErrMetaUnsupported` |
| `meta_test.go` | 空 ID / 未装配 / 写盘失败不唤醒 / 成功唤醒一次 / 读回转发 |
| `release.go` | `BusyState` + `Busy`、`ProtectedInput` + `Protected`：释放面的两条判据 |
| `release_test.go` | 忙态四轴、受保护集四条来源、空会话 ID 与空视图 ID 的边界 |
| `history/` | 历史分页的窗口装配判据（见其 README） |

## 核心实现

- `MetaDeps` 是**值语义**的宿主面：`Store`（窄接口，结构满足 `session.SessionMetaPort`，nil = 未装配）
  与 `Wake`（唤醒目录刷新；nil = 不唤醒）。
- **唤醒只在成功之后**：写盘失败就不推目录（否则所有客户端会被叫起来重拉一份没变化的数据）。
- 展示元数据的语义：全零值 = 清除该会话的条目（取消置顶 + 清空别名 + 复位排序）——由存储侧表达，
  本包只负责"校验 → 写 → 唤醒"这条次序。

## 数据流

```text
前端设元数据 → 门面 SetSessionMeta（断言 Deps.Sessions 上的可选元数据端口）
  → switching.SetMeta（校验会话 ID → Store.SetSessionMeta → 成功则 Wake）
  → 宿主 Wake = 请求会话目录 worker 重拉（发布全局 snapshot.changed）
```

## 依赖方向

允许：标准库、`middle/protocol/model`（`SessionMeta` 形状）。
禁止：`session`（端口由宿主断言后注入）、`store`、`application/*`、`backend/*`。

## 并发、存储与错误语义

- 本包**无状态、无锁**：宿主端口各自持锁；`Wake` 由宿主保证可并发调用。
- 不落盘：持久化在存储层；本包不读盘、不写盘。
- 错误语义：参数错误直接返回（文案固定）；未装配存储返回 `ErrMetaUnsupported`（调用方可用 `errors.Is`）；
  存储错误原样冒泡；三条路径都不唤醒目录。

## 扩展方式

- 新增会话级展示项：加进 `SessionMeta`（契约/协议层）并由存储侧落地，本包的"校验 → 写 → 唤醒"次序不变。
- 新增会话切换用例：在本包加纯判据或薄编排，端口仍由宿主注入；不要把 `session` 域类型带进来。

## Review 指南

- 失败路径是否仍**不唤醒**目录刷新？
- 空会话 ID 与未装配存储的两条文案是否仍唯一（`rg` 只能命中本包）？
- 是否出现第二份元数据校验/唤醒逻辑（宿主侧或前端侧）？

## 测试与验证

```text
go test ./middle/switch/... -count=1
go test ./application/core/ -run 'SessionMeta|Meta' -count=1   # 门面级：断言端口 + 转发
go test ./wiring/adapters/ -run SessionSetting -count=1        # 存储端口实现
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### meta.go

- `type MetaStore interface` — 会话展示元数据的读写口（结构满足 `session.SessionMetaPort`）。
- `type MetaDeps struct` — 宿主面：`Store`（nil = 未装配）+ `Wake`（nil = 不唤醒）。
- `var ErrMetaUnsupported` — 未装配元数据存储（目录枚举不受影响，元数据恒为零值）。
- `func SetMeta(deps MetaDeps, sessionID string, meta model.SessionMeta) error` — 写元数据并唤醒目录刷新（失败路径不唤醒）。
- `func ReadMeta(deps MetaDeps, sessionID string) (model.SessionMeta, error)` — 读元数据。

### release.go

- `type BusyState struct` — "会话是否正忙"的四个读数（待审批 / 运行中 / 排队输入 / 在飞请求）。
- `func Busy(state BusyState) bool` — 四个读数任一非零即忙（零值 = 不忙，同 `sessionBusy(nil)` 口径）。
- `type ProtectedInput struct` — 受保护集判据的输入（会话 ID + 视图会话 ID + 恢复态 + 忙态）。
- `func Protected(input ProtectedInput) bool` — 是否属于受保护集（绝不释放）；空会话 ID 亦判为受保护。
