# 前置审查报告

## 需求摘要

保留工具调用轮实际生成的 assistant 输出，使下一请求完整复现 DeepSeek 的
model-output cache prefix unit。

## 影响文件清单

| 文件 | 修改类型 | 位置 | 原因 |
|---|---|---|---|
| Seele `session/loop.go` | 修改 | `callLLM` tool_calls 分支 | 保留非空 `content` |
| Seele `session/chat_test.go` | 修改 | 流式 mock + 新回归 | 同一响应生成 content/reasoning/tool_calls |
| `backend/engine/context/history.go` | 修改 | `RepairEmptyHistoryContent` | 停止归零工具轮正文 |
| `store/durable_history.go` | 修改 | `Load` 三个出口 | 删除二次归零 |
| `store/provider_wire.go` | 删除 | 全文件 | 唯一职责已失效 |
| `application/core/context_*` | 修改 | 探针与守卫 | 以真实模型输出形状驱动测试 |
| `test/prefix_invariant_fullchain_test.go` | 修改 | 全链路断言 | 断言第二请求保留工具轮 content |

## 依赖分析

- 上游：Seele ReActLoop 生成 assistant 消息。
- 下游：Seelex engine history、durable history、wire assembler、visible view。
- Seelex 通过临时 `replace G:/Program/go/Seele` 做本地联调。

## 循环依赖检查

- [x] 无新增循环依赖
- [x] 未新增接口或跨层反向依赖

## 风险预估

- provider 对工具轮同时含 content/tool_calls 的兼容性：低；DeepSeek 官方示例支持。
- 旧会话已有空正文：不影响，保持空。
- 新旧 Seele 混用：高；Seelex 必须在 Seele v0.3.6 发布后移除 replace 并抬 require。
