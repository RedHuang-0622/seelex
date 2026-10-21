# DeepSeek 模型输出缓存边界修复

## 状态

已实现并发布 Seele `v0.3.6`；Seelex 已移除本地 replace 并切换到远程
`github.com/RedHuang-0622/Seele v0.3.6`。

## 根因

DeepSeek 在 user input 结束和 model output 结束分别建立完整 cache prefix unit。
离开 tool_calls 的 assistant `content` 后，下一请求只能命中 user-input 单元，
无法完整命中包含 `content + reasoning_content + tool_calls` 的 model-output 单元。

## 交付

- [front-review.md](front-review.md)
- [code-changes.md](code-changes.md)
- [test-report.md](test-report.md)
- [finish-review.md](finish-review.md)
