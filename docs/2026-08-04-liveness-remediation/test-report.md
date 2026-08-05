# 测试报告

## 覆盖重点

| 维度 | 用例 | 结果 |
|---|---|---|
| 单元 | Runtime 可见性投影、mailbox 容量与 drain | 通过 |
| 集成 | Runtime mailbox 注入 Engine 历史并进入 frontend Snapshot | 通过 |
| 边界 | Snapshot 在 catalog Storage 阻塞时保持纯内存读取 | 通过 |
| 关闭 | Actor/Pipeline 阻塞 Storage 被取消后结束 | 通过 |
| 并发 | `-race` 尝试 | 本机受 `CGO_ENABLED=0` 且无 C 编译器限制，未执行 |

## 执行命令

已通过：

```text
go vet -p 1 ./...
go test -p 1 ./... -count=1 -timeout=300s
go build -p 1 ./...
go build -p 1 -tags "gui,desktop,production" ./...
```

Windows 本机 `go env CGO_ENABLED` 为 `0`，且 PATH 中没有 `gcc`、`clang` 或
`cl`，因此 `go test -race` 无法执行，需由 Linux CI 补齐。真实付费 API 冒烟
测试保持 opt-in，未读取账号配置。
