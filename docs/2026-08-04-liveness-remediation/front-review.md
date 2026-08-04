# 变更范围审查

## 目标

消除 Application、Runtime 与主会话之间的反向等待，并使关闭路径不会因可取消的外部 I/O 或 catalog 刷新长期失去响应。

## 修改边界

| 模块 | 位置 | 处理方式 |
|---|---|---|
| Application | `application/core` | Runtime 值投影、锁外收集/锁内应用、异步 catalog 缓存 |
| Runtime | `seelebridge` | 本地父证据缓存、子代理有界 merge-back mailbox |
| 生命周期 | `seelexctx/lifecycle` | 有时限 I/O、cancel 后 drain、context-aware snapshot/flush |

## 不变项

- 前端仍只消费 Application Snapshot/Event。
- Session/Workspace 的既有持久化格式不变。
- mailbox 满时不扩容；以显式丢弃计数保持子代理活性。

## 验收

1. Runtime 不再持有 Application callback。
2. `service.mu` 临界区没有 Engine/Runtime/Workspace/Session/Skill/Plugin Port 调用。
3. `Snapshot()` 不触发 catalog 存储访问。
4. 阻塞 Storage 在关闭时被取消，且 Actor/Pipeline 结束等待受 context 限制。
