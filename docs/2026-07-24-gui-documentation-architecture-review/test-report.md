# 测试报告

## 概览

| 范围 | 通过 | 失败 | 跳过/受限 | 结果 |
|---|---:|---:|---:|:---:|
| Go package 全量测试 | 15 个 package | 0 | `tui/splash` 无测试 | ✅ |
| 文档契约测试 | 3 个测试 | 0 | 0 | ✅ |
| 并发相关 package 三轮重复 | 6 个 package × 3 | 0 | race detector 未执行 | ⚠️ |
| Build / Vet | 2 项 | 0 | 0 | ✅ |

本轮为文档、Schema、示例和契约测试变更，没有修改运行时代码。覆盖率未重新采集，不能声称达到新的覆盖率阈值。

## 执行命令

| 命令 | 结果 | 关键指标 |
|---|:---:|---|
| `go build ./...` | ✅ | 全部 package 构建成功 |
| `go vet ./...` | ✅ | 零诊断 |
| `go test ./... -count=1` | ✅ | 15 个有测试 package 通过 |
| `go test . -run '^TestGUI' -count=1 -v` | ✅ | Schema、示例、模块 DAG、文档链接通过 |
| `go test ./application ./plugin ./session ./gui ./mcpstack ./seelebridge -count=3` | ✅ | 三轮重复通过；`seelebridge` 总耗时约 165s |
| `go test -race ...` | ⚠️ | 本机缺少 `gcc`；CGO race runtime 无法构建 |
| `git diff --check` | ✅ | 无空白错误 |

## 各维度

| 维度 | 结果 | 关键证据 |
|---|:---:|---|
| 单元/集成 | ✅ | 全量 `go test ./...` 通过 |
| JSON Schema | ✅ | Draft 2020-12 Schema 全部可编译，`$ref` 可解析 |
| 示例契约 | ✅ | `examples/*.json` 均有对应 Schema 测试且验证通过 |
| 模块依赖 | ✅ | 无重复 ID、未知依赖或有向环；模块文档存在 |
| 文档链接 | ✅ | `docs/gui/**/*.md` 本地链接全部可解析 |
| 静态分析 | ✅ | build、vet、diff check 通过 |
| 并发稳定性 | ⚠️ | 普通并发测试三轮通过；未获得 race detector 证据 |
| 性能 | ➖ | 只有文档/测试变更，未运行 benchmark |
| 安全 | ✅ | 示例无真实密钥；路径、认证、ACL、脱敏和门禁规则已文档化 |

## 受限项

Race 首次尝试因 `CGO_ENABLED=0` 被拒绝；显式设置 `CGO_ENABLED=1` 后，Go 报告 `C compiler "gcc" not found`。因此不能把 race 标为通过。CI 需要在带 C 编译器的 runner 上执行：

```bash
go test -race ./... -count=1
```

本轮未改运行时代码，普通全量和三轮并发用例通过，允许提交文档契约；[`architecture-review.md`](../gui/architecture-review.md) 中的现有并发 P0 项仍是启用多会话前的阻断项。

## 综合判断

- [ ] 完全通过
- [x] 有条件通过——文档与契约变更可提交；race 必须由 CI 补跑，运行时 P0 问题不得在多会话实现时忽略。
- [ ] 不通过
