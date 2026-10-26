# Session / Residency 端口化方案

## 方案对比

| 维度 | A：ResidencyPort 先完成 | B：SessionUseCase + Residency 同批全拆 |
|---|---|---|
| 状态风险 | 低：residency 是纯判据与值 | 高：需要镜像完整 SessionUnit/View 状态 |
| 端口宽度 | 窄：9 个真实调用能力 | 很宽：Chat/Queue/View/Lock/Domain 全暴露 |
| 可测试性 | 高：adapter parity 明确 | 中：需重演大量锁与单元状态 |
| 回退性 | 高 | 低 |
| 是否符合停止条件 | 是 | 触发“完整 session state”条件 |

## 推荐：先 A

`application/session.ResidencyPort` 使用 Application-owned 纯值：

- `EvictionSnapshot/Deps`；
- `ArchiveGate/Store/Engine/UseCaseDeps`；
- Order 使用 `[]string`；
- 输入索引只接收/返回 protocol 消息值；
- archive not-found 错误 identity 放 `contract`。

实现由 `wiring/adapters.NewResidencyPort` 提供，Middle 判据不复制。

## SessionUseCase 候选

推荐后续裁决方向：把会话聚合的真正所有者迁到 `application/session`，
或者按 ChatState / Queue / View / Approval / Residency / Lifecycle 拆成
多个消费方端口；禁止一个包含完整 SessionUnit 的万能接口。
