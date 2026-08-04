# 四链路与关闭冒烟记录

日期：2026-08-04

| 验收链路 | 独立测试 | 本次结果 |
|---|---|---|
| 前端请求 → 后端 | `TestBridgeSubmitForwardsFrontendRequestToApplication` | 20 次通过 |
| 后端事件 → 前端 | `TestBridgeRelaysToolCompletedEventToFrontend` + `event-chain.test.mjs` | Bridge 20 次通过；Node 59/59 通过 |
| 后端数据流转 | `TestFullAccessBashToolCompletionReachesApplication` | Runtime → ToolHook → EventHub → Snapshot，20 次通过 |
| 真实 API 整体链路 | `TestManualSmokeRealAccountGUIBridgeBashFullChain` | `Bridge.Submit → bash(pwd && ls -la) → seelex:event → idle`，9.81 秒通过 |
| 关闭卡死兜底 | `TestCloseCoordinatorCancelsStalledChatAndQuits` | 20 次通过；最长 5 秒优雅等待后取消当前聊天并退出 |

## 执行命令

```text
go test ./gui -run 'TestBridgeSubmitForwardsFrontendRequestToApplication|TestBridgeRelaysToolCompletedEventToFrontend|TestCloseCoordinator' -count=20 -timeout=60s
go test . -run '^TestFullAccessBashToolCompletionReachesApplication$' -count=20 -timeout=90s -p 1
node --test gui/frontend/dist/*.test.mjs
$env:SEELEX_SMOKE_ACCOUNTS=(Resolve-Path config/accounts.yaml).Path
go test -tags manualsmoke . -run '^TestManualSmokeRealAccountGUIBridgeBashFullChain$' -count=1 -timeout=3m -v -p 1
go test ./... -p 1 -count=1 -timeout=120s
go vet -p 1 ./...
go build -p 1 ./...
go build -p 1 -tags "gui,desktop,production" ./...
```

Windows 本机为 `CGO_ENABLED=0` 且没有 GCC，无法在本机运行 Go `-race`。本次并发敏感路径以重复定向测试覆盖，未将其表述为 race 验证。

## 关闭问题范围

旧关闭路径对运行中聊天使用无期限的 `WaitForIdle(context.Background())`。工具、审批或 Provider 未把 `chat.running` 收敛为 false 时，Wails 会一直拒绝原生关闭请求。现在关闭路径最长等待 5 秒，之后调用 `CancelChat("")` 并进入原生退出；`run()` 延迟执行的 `app.Shutdown` 随后继续取消剩余运行时资源。
