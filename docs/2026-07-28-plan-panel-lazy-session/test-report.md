# 测试报告

## 概览

| 范围 | 结果 | 关键指标 |
|---|:---:|---|
| Go 全仓库 | ✅ | `go test ./... -count=1 -timeout=180s` 全部通过 |
| GUI 前端 | ✅ | Node 32/32 通过，全部 JavaScript 语法检查通过 |
| Application/Core 压力重复 | ✅ | 全包连续 10 轮通过 |
| Application/Core 覆盖率 | ⚠️ | 70.3%，低于 test-suite 标准建议值 85% |
| Race | ⚠️ | 本机 `CGO_ENABLED=0` 且无 C 编译器，无法执行；等待 Linux CI race job |
| 静态与安全 | ✅/⚠️ | gofmt、diff、go vet、build、安全 grep 通过；本机无 govulncheck |

## 各维度

| 维度 | 结果 | 关键指标 |
|---|:---:|---|
| 单元测试 | ✅ | Session draft、首问命名、项目继承、历史恢复、Bridge、Plan DSL 全部通过 |
| 集成测试 | ✅ | 全仓库 Go packages 通过；Windows scoped shell 项目根测试通过 |
| 边界测试 | ✅ | 重复点击幂等、空 ID 不 binding、draft 切项目、resume 退出 draft |
| 性能测试 | — | 本次没有修改计算热点，未运行 benchmark |
| 并发测试 | ⚠️ | `application/core` 连续 10 轮通过；本机不能启用 race detector |
| 模糊测试 | — | 本次未运行 fuzz |
| 内存测试 | — | 本次未运行专用内存分析 |
| 静态分析 | ✅ | `go vet ./...`、普通构建、GUI production tags 构建通过 |
| 安全测试 | ✅/⚠️ | hardcoded secret 与危险 nil-return grep 通过；govulncheck 本机不可用 |
| 泄漏检测 | ✅ | 全量测试退出正常；GUI graceful shutdown 既有测试继续通过 |

## 修复过的测试失败

| 用例 | 原错误 | 修复 |
|---|---|---|
| `TestRuntimeProjectScopedToolsUseBoundProject` | Windows `powershell -Command pwd` 在 Go 子进程中 10 秒超时 | 改用系统 PowerShell 绝对路径及 `-NoLogo -NoProfile -NonInteractive`；测试约 7 秒内完成，其中约 5 秒为 Runtime hub 固定启动等待 |

## 功能验收

- [x] 点击新建只进入 draft，不创建 Session ID。
- [x] 重复点击保持单一 draft，不保存空 Session。
- [x] 首次真实请求恰好调用一次 `StartSession`。
- [x] 首问立即成为 Session `name`，ID 只作为操作键。
- [x] draft 在项目切换后只继承 scope，不产生空 ID binding。
- [x] 恢复历史会话会退出 draft。
- [x] Plan 常驻右侧面板，无 Plan 时隐藏，更新继续使用 keyed reconcile。
- [x] Windows scoped shell 使用绑定项目作为工作目录。

## 综合判断

- [ ] ✅ 无条件通过
- [x] ⚠️ 有条件通过——本地功能、构建和全量测试均通过；合并前仍应等待 Linux CI 的 race 与 govulncheck 结果。
- [ ] 🚨 不通过
