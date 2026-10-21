# 测试报告

## 红灯

- Seele `TestEngine_ChatStream_ToolCallReplaysModelOutputContent`：
  history content 为 nil。
- `TestRepairEmptyHistoryContentPreservesToolCallAssistantContent`：
  工具轮 content 被改写为空。
- `TestDurableHistoryPreservesToolCallAssistantOutputContent`：
  Durable 冷载 content 为 nil。

## 绿灯

```text
go test ./session -count=1                         # Seele
go test ./backend/engine/context ./store -count=1  # Seelex
go test ./application/core -count=1
go test ./test -run TestFullChainPrefixInvariantAcrossTurns -count=1
go test ./... -count=1 -timeout=300s               # Seelex
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
```

Seelex 全量测试仅 `internal/bootseed` 的既有配置漂移失败；其余包通过。

Seele 全量测试中 `cmd/smoke` 因真实 API key 无效失败，`jobs/builtin` 首轮偶发失败，
单测 `-count=3` 复跑通过；`session` 全包通过。
