# Session / Residency 外部端口前置审查

日期：2026-10-26

## 真实调用方

### Residency

`application/core` 有 9 个生产文件直接 import `middle/residency`：

- `snapshot_budget.go`：预览截断；
- `session_query.go`：目录刷新等待；
- `session_input_index.go`：输入索引；
- `content_lru.go` / `resident_lru.go`：Order、Prune、Budget、Eviction；
- `session_release.go`：Budget、Order Forget；
- `session_archive_use_case.go`：Archive 门控与步骤；
- `archive_session.go`：归档 not-found 错误别名；
- `service_state.go`：resident/content 两层 LRU 序字段。

这些调用都不需要完整 Service；输入是值、函数回调与其他域自己的 DTO，
因此可以用消费方 `ResidencyPort` 完全承接。

### SessionUseCase

`application/core` 有多个生产文件直接使用 `Domain`、`SessionUnit`、`View`
的字段与方法，涉及：

- ChatState、Queue、Cancel/Stream/Batcher；
- View 锁内读写、消息窗口、ContentUnloaded；
- resident/approval/runtime slot；
- Domain actor 注册、活动视图、单元枚举、Close；
- 冷恢复和 draft shell 的单元构造。

如果端口直接替换这些结构，会把完整 session state 和视图可变字段镜像进
Application；如果复制这些字段，又会产生第二份状态机。该半批触发停止条件。

## 影响范围

| 区域 | Residency | SessionUseCase |
|---|---|---|
| Application/core | 可删除 import | 暂停 |
| Application/session | 新增端口和纯值 deps | 待裁决 |
| Middle | 保持判据唯一，可选 alias error identity | 保持状态 owner |
| Wiring | 唯一 adapter | 尚不创建 |
| Wire/持久化 | 不变 | 不变 |

## 风险

- 归档错误 identity：必须继续让宿主旧名与 Middle 错误 `errors.Is` 相等；
- LRU 序裸切片必须继续在锁外复制，不能因去掉 `Order` 类型而破坏快照；
- SessionUnit 若硬抽接口，容易变成完整状态镜像和万能 adapter。

## 建议

先完成 ResidencyPort；SessionUseCase 停在设计门禁，等待选择：

1. 将会话聚合同步迁入 `application/session`，Middle 只保留策略；
2. 明确定义多个窄状态端口（Chat/Queue/View/Residency/Approval），接受较宽的
   会话聚合 API，但禁止万能 Service；
3. 保留 sessionusecase 为 Application 内部领域包，重画依赖层。

本轮不采用未经裁决的方案 2/3。
