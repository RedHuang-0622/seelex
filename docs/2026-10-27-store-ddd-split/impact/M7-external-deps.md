# M7 外部依赖批次

状态：两个批次均已通过门禁并由独立 commit 固化；manifest、可重放 patch 与
`scripts/store-ddd/M7-external-migrate.py` 已补齐，用于 checkpoint 重放与审计。

Replay 证据：在独立 branch `codex/store-ddd-M7-replay`、checkpoint
`8fbae4e8` 上完成 dry-run、apply、领域测试、tree parity 与 rollback 演练。
Apply 后 tracked 代码与 `b50612ea` 完全一致，新建 fork 文件 SHA-256 一致；
rollback 后代码 diff 为 0，随后重新 apply 成功。journal 仅保存在系统临时目录，
不进入仓库。

## 批次

| 批次 | checkpoint | 结果 commit | 内容 |
|---|---|---|---|
| Prompt consumer | `8fbae4e8` | `9e8b4bb7` | `SessionContextRecord` → `PromptStackSnapshot` |
| Context fork | `9e8b4bb7` | `b50612ea` | session fork JSON 重写 → `storecontext.ForkState` |

## 依赖方向

- `store/context` 是 `PromptStackSnapshot`、compact 帧裁剪与 fork payload 重写的唯一 owner。
- `backend/engine/context/seelectx` 只消费 prompt/compact 窄投影，不反向拥有存储字段。
- `kernel/context` 只装配 prompt/compact 读面。
- `kernel/session` 只负责读取父 payload、调用领域函数、写回子 payload 和包装错误。
- `application/assembly` 测试替身改用同一投影。

## 验证

```text
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
go test ./e2e -run 'ContextOwnership|CompactStackReadFace|PromptStackReadFace|ContextForkOwnership|FacadeDependencyClosureRatchet'
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
```

## 回滚

1. 在 checkpoint `8fbae4e8` 的隔离 worktree 执行脚本 `--dry-run`，确认两个 patch 可顺序应用。
2. 使用 `--apply` 重放；失败时用脚本输出的 journal 执行 `--rollback`。
3. 已提交分支可用 `git revert b50612ea 9e8b4bb7` 回退，不使用 reset/clean。
4. 回滚不触碰 `.seelex`、配置、媒体或其它用户数据。
