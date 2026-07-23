# 测试报告

## 概览

| 项目 | 结果 |
|------|------|
| 全量 Go 测试 | 通过 |
| GUI 重复测试 | 通过，`-count=3` |
| 默认构建 | 通过 |
| GUI 构建 | 通过，`-tags "gui,desktop,production"` |
| go vet | 通过 |
| gofmt | 通过，0 个未格式化文件 |
| 漏洞扫描 | 通过，0 个可达漏洞 |
| 跨平台 CLI 打包 | Windows amd64、Linux amd64、macOS amd64/arm64 通过 |
| Windows GUI 打包 | 通过，归档和 SHA-256 已生成 |
| 会话恢复 | 通过；覆盖历史替换、选中 ID 续写、reasoning/工具调用往返 |
| GUI 组件 | 通过；ES module 语法、Tool IN/OUT 配对、OUT 截断与完整 payload 保留 |
| GUI Markdown 与状态组件 | 通过；10 个 Node 测试覆盖常用语法、`<think>` 闭合/流式状态、代码块误判、加载状态、队列及 XSS |
| 本地 race | 未执行；Windows 环境 `CGO_ENABLED=0` 且无 GCC |

## 各维度

| 维度 | 结果 | 关键指标 |
|------|:---:|---------|
| 单元测试 | ✅ | 所有 package 通过 |
| 集成测试 | ✅ | `seelebridge`、`seelexctx` 集成用例通过 |
| 边界测试 | ✅ | 非法 frontend/permission、nil GUI app、事件关闭路径、未闭合思考块、代码标签误判及 Markdown XSS/危险 URL 已覆盖 |
| 性能测试 | ⚪ | 仓库当前无 Benchmark；未发现本次新增热路径阻塞 |
| 并发测试 | ⚠️ | GUI Bridge `-count=3` 通过；本地 race 受工具链限制，CI Linux race 为发布条件 |
| 模糊测试 | ⚪ | Markdown 解析器已做恶意 HTML/URL 定向边界测试；当前前端无 fuzz runner |
| 内存/泄漏 | ✅ | Bridge 使用 cancel + Subscription.Close + WaitGroup 收敛 goroutine |
| 静态分析 | ✅ | JS ES module check、`go vet`、默认/GUI build、gofmt、diff check 通过 |
| 安全测试 | ✅ | govulncheck 复扫 0 可达漏洞；Markdown/思考/队列内容统一转义并限制 URL 协议 |
| 发布泄漏 | ✅ | 四个平台包内 config 仅含 `accounts.example.yaml` |
| GUI 发布泄漏 | ✅ | Windows GUI 包仅含公开模板，包含 LICENSE/README/CHANGELOG |

## 覆盖率

| 包 | 覆盖率 |
|----|:---:|
| `gui` | 86.4% |
| `application` | 67.4% |
| `plugin` | 87.6% |
| `skill` | 82.6% |
| `session` | 100.0% |
| `tui` | 6.2% |

GUI 新包达到标准测试档的 85% 目标。全仓覆盖率仍受既有 TUI、入口和桥接包影响，未达到统一 85% 阈值。

## 关键验证命令

```text
go test ./... -count=1 -cover -timeout=180s
node --test gui/frontend/dist/markdown.test.mjs
go test -tags "gui,desktop,production" ./gui -count=3
go vet ./...
go build ./...
go build -tags "gui,desktop,production" ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
scripts/build.ps1 -Version v0.1.0-alpha.1
scripts/build-gui.ps1 -Version v0.1.0-alpha.1
```

## 综合判断

- [x] ⚠️ 有条件通过：代码与本地构建测试通过；公开 tag 前必须确认 GitHub Linux race、三平台 CI 和 Windows GUI Release job 通过。
