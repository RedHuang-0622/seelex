# obs（观测：请求画像 + 会话遥测）

## 生态位

`backend/engine/obs` 是**引擎侧的观测面**：把"这一轮发出去的请求长什么样"与"这个会话用了多少
token"变成可查询的读数。主要调用方是装配器（`backend/engine/assemble` 的 Runtime 在
`agent.NewWithComponents` 之前包一层）与 `internal/adapters`（会话入口注入 telemetry 归属键）。

物理位置：2026-10-16 按
[plan.md](../../../docs/2026-10-15-architecture-reshape/plan.md) §2.5 从
`seelebridge/{request_log.go,session_telemetry.go}`（该包已整体归位 `backend/engine/assemble`）
再下沉到这里；`events_unified.go` 等仍挂在 `Runtime` 上的文件留在 assemble，随 J2 的域拆分一起过来。

## 职责与非职责

- **做**：① 请求画像记录——`SEELEX_REQUEST_LOG=<jsonl>` 时，把每轮请求的消息摘要（角色、长度、
  内容指纹）与工具名清单 append 到 JSONL，供事后核对"发出去的是不是我以为的那份"；
  ② 会话遥测——把会话号放进 ctx（`WithTelemetrySessionID`）并按会话读出 token 计数
  （`SessionTokenCount`）。
- **不做**：不做任何业务判断、不改变请求内容（记录点只读传入的 messages/tools）、不落盘会话正文；
  画像里只留摘要与指纹，不留原文。

## 数据流

```text
Runtime 装配（assemble）
  └─ 若 SEELEX_REQUEST_LOG 非空：OpenRequestLogger(path)
       ├─ NewRequestLogCompleter(inner)       ← 同步调用：先 record 再转调原 Completer
       └─ NewRequestLogStreamCompleter(inner)  ← 流式调用：同上
会话入口（internal/adapters）
  └─ WithTelemetrySessionID(ctx, sessionID) → 引擎侧按会话记账
  └─ SessionTokenCount(tracer, sessionID)   → 读该会话的 token 合计
```

## 文件结构

| 文件 | 职责 |
|---|---|
| `request_log.go` | `RequestLogger` / `OpenRequestLogger` / `RequestLogEnv` / 两个包装器（同步与流式）+ 消息摘要与 wire 文本归一 |
| `session_telemetry.go` | `WithTelemetrySessionID` / `SessionTokenCount` |
| `request_log_test.go` | 画像落盘、开关语义、包装器透传 |

## 并发、安全与错误语义

`RequestLogger` 自持锁，`record` 串行追加；`OpenRequestLogger("")` 与未设环境变量都是空操作。
打开文件失败要如实返回（调用方据此决定"要不要带着诊断继续起"）。画像内容做了**摘要化**：
`digestRequestMessage` 只留角色、长度与内容指纹，避免把提示词原文写进日志。

## 依赖方向

允许：标准库、Seele（`agent`/`types`/`telemetry`）、`internal/telemetry`。
禁止：`application/*`（上层）、`backend/engine/assemble`（装配器反过来 import 本包）、
`store`（本包不落业务数据）。

## 扩展方式

新增观测点＝在本包加一个"只读传入值"的记录器，并在 `assemble` 的装配处包装；
不要在记录器里做判断或改写入参。开关一律走环境变量/构造参数，不读全局状态。

## Review 指南

- 记录器是否只读入参、绝不改 messages/tools？
- 画像是否仍然只留摘要与指纹（不落原文）？
- 包装顺序是否仍在最靠近 provider 的一层（早于它的包装会被漏记）？

## 测试与验证

```text
go test ./backend/engine/obs -count=1
```
