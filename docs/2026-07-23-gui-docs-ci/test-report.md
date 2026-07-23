# GUI 文档、Effort 与 CI 测试报告

## 1. 概览

测试日期：2026-07-23；环境：Windows amd64、Go 1.25、Node 22、Edge headless。

| 执行单元 | 通过 | 失败 | 跳过 | 关键覆盖率 |
|----------|-----:|-----:|-----:|------------|
| JavaScript syntax modules | 8 | 0 | 0 | 全部 `dist/*.js` |
| Node test cases | 26 | 0 | 0 | Effort 新增 4/4 |
| Go packages | 16 | 0 | 0 | Application 75.2%；GUI 88.7% |
| Build/static/visual gates | 6 | 6 | 0 | 默认 build、production build、vet、format、secret/nil、Edge |

## 2. 各维度结果

| 维度 | 结果 | 命令/证据 |
|------|:---:|-----------|
| JS 语法 | 通过 | 8 个 `*.js` 逐个 `node --check` |
| 前端单元/契约 | 通过 | `node --test gui/frontend/dist/*.test.mjs`，26/26 |
| Effort 边界 | 通过 | 四档映射、Max selector、拖动预览、单次提交、失败回滚 |
| Application/Bridge | 通过 | `go test ./application ./gui -count=1` |
| 全仓集成 | 通过 | `go test ./... -count=1 -timeout=180s -covermode=atomic -coverpkg=./...`，16 包 |
| 静态分析 | 通过 | `gofmt -l .` 无输出；`go vet ./...` |
| 默认构建 | 通过 | `go build ./...` |
| Windows GUI production | 通过 | `go build -tags "gui,desktop,production" ./...` |
| 密钥/空返回 | 通过 | 非测试 Go 文件无硬编码密钥；无 `return nil, nil` |
| 静态视觉 | 通过 | Edge 1440×900 分别检查 Lite 与 Max，控件不遮挡 topbar；Max 紫色状态清晰 |
| Race | CI 门禁 | 本机 `CGO_ENABLED=0`，由 Ubuntu `race-and-coverage` 执行，不伪报本地通过 |
| 漏洞扫描 | CI 门禁 | 本机无 `govulncheck`，由 `release-safety` job 执行 |

## 3. 受影响包覆盖率

| 包 | 覆盖率 | 目标判断 |
|----|-------:|----------|
| `application` | 75.2% | 达到项目当前 ≥75% 目标 |
| `gui` | 88.7% | Bridge/Core 契约覆盖充分 |

前端当前使用 Node 原生 test runner，未接入行覆盖率工具；本次以四个 Effort 行为用例和嵌入资源契约证明新增逻辑。

## 4. CI 等价性

本地已经执行 `GUI tests` job 的全部逻辑命令，以及 build matrix 中与 Windows 相关的默认/production build、format、vet、test。以下只能在推送后由 GitHub runner 给出最终结论：

- Ubuntu race + atomic coverage artifact；
- macOS/Linux clean build；
- GitHub Action `govulncheck`；
- workflow trigger 和受保护分支检查名的远端呈现。

## 5. 已知限制

1. 静态截图能确认 Lite/Max 布局和光效状态，不能证明不同系统 WebView 的动画帧时序。
2. 键盘、滚动锚点、modal 和真实 Wails event binding 尚无 Playwright/WebView E2E。
3. 本机无法运行 race，远端 `race-and-coverage` 是合并门禁。

## 6. 综合判断

本地门禁通过，可以提交并推送 `gui`。发布级判断仍以推送后 `GUI tests`、build matrix、race-and-coverage 和 release-safety 全绿为准。
