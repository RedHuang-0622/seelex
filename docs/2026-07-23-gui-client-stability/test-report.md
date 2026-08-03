# GUI 客户端稳定性测试报告

## 概览

| 范围 | 通过 | 失败 | 备注 |
|------|-----:|-----:|------|
| Node 前端测试 | 22 | 0 | 协议、客户端状态、组件、Markdown/think |
| Go 全仓包 | 15 | 0 | 含 application、gui、mcpstack、seelebridge、seelexctx、session、tui 等 |
| JavaScript 语法检查 | 全部 | 0 | `gui/frontend/dist/*.js` |

## 各维度

| 维度 | 结果 | 关键指标 |
|------|:---:|---------|
| 单元/边界 | 通过 | 22 个 Node 测试；新增旧事件重放、同 revision、多页历史稳定 ID 用例 |
| 集成 | 通过 | `go test ./... -count=1 -timeout=180s` |
| 静态分析 | 通过 | `go vet ./...`、所有 JS module `node --check` |
| 构建 | 通过 | `go build -tags "gui,desktop,production" ./...` |
| 安全 | 通过 | Markdown 危险链接和原始 HTML 用例通过；diff 未发现凭据；会话数据不进入构建 |
| 并发 | CI 门禁 | 本机 `-race` 因 Windows 环境 `CGO_ENABLED=0` 无法执行；`.github/workflows/ci.yml` 在 Ubuntu 执行全仓 race |
| 覆盖率 | 警告 | application 75.2%，gui 88.7%，二者合计 76.1%；低于 skill 的通用 80% 建议线，但 GUI 包已超过 |
| WebView E2E | 待手工验收 | 当前仓库没有真实 Wails WebView 自动化框架 |

## 执行命令

```text
node --test gui/frontend/dist/*.test.mjs
go test ./application ./gui -count=1
go test ./... -count=1 -timeout=180s
go vet ./...
go build -tags "gui,desktop,production" ./...
go test ./application ./gui -coverprofile=<TEMP>/seelex-gui-coverage.out
go test -race ./application ./gui -count=1
```

## 限制说明

本机 race 命令返回 `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`。这不是代码失败；race 仍由现有 Ubuntu CI 门禁执行。覆盖率警告主要来自 application 的既有整体覆盖率，新增协议和 GUI reducer 关键分支已有回归测试。

## 综合判断

- 有条件通过：可以进入本地 alpha 手工验收。
- 合并前条件：Ubuntu CI race 通过；完成一次真实 WebView 长输出、滚动、工具展开和队列验收。
