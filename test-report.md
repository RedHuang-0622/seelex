# Seelex 测试报告

> 日期：2026-07-17
> 提交：`54e3e0d`
> 工具链：官方 Go 1.25.8 windows/amd64（SHA-256 已校验）

## 概览

| 项目 | 结果 |
|---|---|
| Go 文件 | 36（生产 29、测试 7） |
| 发现的 Test/Benchmark/Fuzz 函数 | 53 |
| 可执行非根包测试 | 44 通过、0 失败、8 跳过 |
| 根包 smoke test | 未执行，package loading 被依赖错配阻断 |
| 全量 build/vet/test | 失败 |
| 非根包 statement coverage | 13.5% |
| race | 未完成：环境无 C 编译器；且高风险包本身无测试 |
| gofmt | 失败：36/36 文件需要格式化 |

## 执行结果

### 全量构建、vet、测试

执行：

```text
go build ./...
go vet ./...
go test ./... -count=1 -timeout=180s -coverprofile=coverage
```

三者均在 package loading 阶段失败：

```text
main.go:19:2: no required module provides package
github.com/RedHuang-0622/Seele/agent/core/tool/permission
```

仓库固定 `Seele v0.0.1`，该版本的模块缓存中只有 `builtin/holder/hub/interfaces/mcp` 等目录，没有 `permission`。

### 可独立编译的非根包

执行：

```text
go test ./compactor ./merger ./provider ./seelexctx ./session ./skill ./snapshot ./tui/... \
  -count=1 -timeout=180s -coverprofile=coverage-nonroot
```

结果：命令退出 0；44 个测试通过，8 个测试跳过。

跳过的 8 个用例全部位于 `seelexctx/integration_test.go`。它们依赖 `../config/account-openai.yaml` 和真实 LLM/Agent 配置，当前仓库没有该文件，因此 CI 也很可能长期跳过这些测试。

## 覆盖率

| 包 | Statements | 说明 |
|---|---:|---|
| `merger` | 100.0% | 小型纯逻辑包 |
| `compactor` | 98.1% | 小型纯逻辑包 |
| `snapshot` | 86.6% | 基础类型与格式化 |
| `provider` | 53.3% | 部分 helper 有测试，Engine/Trace 路径不完整 |
| `seelexctx` | 0.0% | 所有集成测试被跳过 |
| `session` | 0.0% | 无测试 |
| `skill` | 0.0% | 无测试；死锁与路径边界未覆盖 |
| `tui` | 0.0% | 无测试 |
| `tui/approve` | 0.0% | 无测试；并发审批未覆盖 |
| `tui/commands` | 0.0% | 无测试 |
| `tui/splash` | 0.0% | 无测试 |
| `tui/sugg` | 0.0% | 无测试 |
| `tui/stream` | 无测试文件 | 仅类型包 |
| 根包 `main` | 不可测 | 依赖错配导致 setup failed |
| `commandstack/mcp/freecad` | 不存在 | CAD 覆盖率为 0% |

非根包总体覆盖率：**13.5%**。高覆盖率集中在代码量较小的纯函数包，不能代表应用主路径质量。

## 静态与并发检查

| 维度 | 结果 | 备注 |
|---|:---:|---|
| `go vet ./...` | 失败 | 依赖错配，未进入完整分析 |
| `go build ./...` | 失败 | 同上 |
| `gofmt -l` | 失败 | 36/36 Go 文件被列出 |
| `go test -race` | 未完成 | 默认 CGO 关闭；启用后环境找不到 `gcc` |
| 安全扫描 | 未执行 | 仓库 CI 也没有 govulncheck/gosec 等步骤 |
| Python/FreeCAD 测试 | 不适用 | 尚无 Python Server 实现 |

即使安装 C 编译器，现有 race 用例也不会触达 `tui/approve`、`tui/stream`、`skill`，因为这些包没有测试。因此不能把未来的 race 通过等同于并发安全。

## CI 审查

`.github/workflows/ci.yml` 已覆盖 Ubuntu/Windows/macOS，并执行 download/build/vet/test，这是优点。但当前缺少：

- `gofmt`/格式检查；
- coverage profile 和最低阈值；
- race（至少 Linux）；
- 依赖漏洞扫描；
- hermetic integration tests；
- Python lint/test 与 FreeCAD headless E2E；
- 检查 `go.mod/go.sum` 在构建后是否保持干净。

当前 CI 按仓库内容应被 `permission` 包缺失阻断。

## 建议测试金字塔

### P0 平台

- `skill`: Create/Delete 超时测试、路径穿越、重复名、权限错误、并发 Load/Reload。
- `tui/approve`: 多请求 FIFO、取消、超时、关闭时唤醒所有等待者、race。
- `tui/stream`: 会话隔离、取消、通道背压、工具事件不丢失。
- `session`: save/resume/delete、失败注入、并发与路径边界。
- `main/config`: 空账号、无效 YAML、权限配置 fail-closed。

### CAD runtime

- 命令 schema 兼容、迁移、序列化 round-trip。
- undo/redo/branch/tag 的 property-based 测试。
- transaction 原子性、幂等重试、崩溃恢复、checkpoint 对账。
- 随机命令序列 fuzz，确保不会破坏历史不变量。

### MCP

- 模拟 stdio server：握手、并发请求、通知、取消、超时、进程崩溃、重连、大消息。
- 确保所有等待请求在连接断开时收到明确 error，而不是 nil response。
- 协议版本协商和兼容性矩阵。

### FreeCAD

- Python 单元测试使用 adapter/fake 隔离协议逻辑。
- FreeCADCmd 集成测试验证事务、recompute、约束和几何有效性。
- 黄金模型 E2E：JSON -> FCStd/STEP -> 重放 -> 关键几何属性一致。
- TechDraw 输出进行页面、视图、尺寸和导出文件断言。

## 综合判断

- [ ] 通过
- [ ] 有条件通过
- [x] 不通过：全量构建失败，应用主路径覆盖率不足，CAD 尚无实现与测试
