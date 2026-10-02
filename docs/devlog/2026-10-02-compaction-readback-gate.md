# 读过再折：失败的压缩不许覆盖 agent 的上下文（先红后绿）

- 日期：2026-10-02
- 范围：`application/core/context_runtime/{ports.go,compaction_index.go,coordinator.go}`、
  `seelebridge/runtime_compaction_index.go`、`seelexctx/dag.go`、
  `internal/adapters/{compaction_index_port.go,runtime_narrow_ports.go}`
- 现场：agent 的上下文在折叠之后看不见上文（用户报「你发来的正文只有 B」——上一轮
  列出 A/B/C 三选项的那条已经不在模型上下文里）
- 用户口径（原文）：「折叠只是压缩失败的一个错误记录……**agent 不需要失败的压缩来
  覆盖之前的上下文**，只有压缩成功（有 llm 读后感返回）才能让 agent 从新的 compact
  栈顶开始上下文。」

---

## 1. 现象：判据只看「摘要器装没装」，而重放是**运行时**才失败的

`prepareExecutionContextFor` 的折叠闸此前只有一条**结构**判据：

```go
noSummary := fold && !c.compactionSummaryAvailable()   // 摘要器装没装
```

真正的模型回读发生在推帧（B 段，锁外）：

```text
… → fitExecutionHistory → replaceFoldHistory(折叠窗口) → A 段(状态提交) → B 段(推帧=模型调用) → C 段(记录)
```

于是「这次到底有没有模型读后感」是在 `replaceFoldHistory` **之后**才知道的。前缀重放
两次调用都失败时（`replay-failed` / `chunk-replay-failed` / 素材不合法），chapter2
落回本地确定性折叠（`summary_source=local`）——上下文已经被换成折叠窗口，而那一帧只有
元数据、对检索毫无用处。这正是「折叠之后看不见上文」，也是 `2026-09-29-replay-fast-fail-local-fold.md`
§5 遗留的「拿到真实报错再定」那条尾巴。

## 2. 先红：一条能复现的用例

`application/core/context_compact_local_fallback_repro_test.go`（新）在索引面之上再实现
「折叠之前先试一次模型读后感」的探针，并让回执回答 `summary_source=local`：

```text
--- FAIL: TestFoldWithFailedModelReadbackLeavesContextUntouched (0.08s)
    context_compact_local_fallback_repro_test.go:66: 模型读后感没拿到（summary_source=local），
    上下文却被折了：最旧的轮次已不在 provider 历史里（agent 看不见上文）
```

判别力在三处：最旧轮次是否仍在引擎历史里、有没有落压缩记录、有没有推压缩栈顶。
对照组 `TestFoldWithModelReadbackStillFolds` 钉住另一侧——读数**拿到**模型读后感时折叠
必须照常（否则"永远不折"这种错误实现会蒙混过关）。

## 3. 改法：把读数提到折叠之前（窄可选探针，缺省不改既有行为）

| 位置 | 改动 |
|---|---|
| `context_runtime/compaction_index.go` | 新增窄可选探针 `compactionReadbackProbe`（`ReadbackCompactionSummary`，**不得有副作用**）+ `Coordinator.readbackCompactionSummary` + `CompactionIndexReceipt.hasModelSummary()` |
| `context_runtime/coordinator.go` | 折叠判据之后、装配之前实测一次读数：读不到 → `noSummary=true`（不折上下文、不推栈顶、不落记录、**不推进上下文版本**，终局 `skipped_no_summary reason=no_model_readback`）；读到了 → 摘要经 `PrecomputedSummary` 带给推帧。判据关（judge）提到读数之前收口 |
| `context_runtime/ports.go` | `CompactionIndexRequest.PrecomputedSummary`（同一次折叠只调用一次模型）+ `SummarySource` 协议字面量 |
| `seelebridge/runtime_compaction_index.go` | 实现探针（跑一次 DAG 拿 Chapter 2，不归档、不压栈）；`PushCompactionFrame` 把 `PrecomputedSummary` 透传进 `CompactionInput` |
| `seelexctx/dag.go` | `CompactionInput.PrecomputedSummary`：非空 → `chapter2Node` 直接用它（`summary_source=replay`），不再调用模型 |
| `internal/adapters/*` | 转发面补 `ReadbackCompactionSummary` + 一条 `var _` 断言（漏转发 = 读数闸静默缺省） |

三条边界：

- **探针缺省 = 行为逐位不变**：未实现 `compactionReadbackProbe` 的 fake/harness 与不接
  摘要的宿主走既有路径（判据照常折、推帧照常推），因此既有的三条索引面用例与
  `TestFoldWithoutModelSummaryLeavesContextAndStackUntouched` 一条都不需要改。
- **门禁顺序不变**：读数闸落在判据关与装配关之间，`CompactionGates`（judge→assemble→
  replace→index→frame→store→record）与前端 `compactionGateLabels` 不动。
- **一次折叠一次模型调用**：读数拿到的正文随推帧带下去（`PrecomputedSummary`），
  推帧的 DAG 不再打第二次模型——`index` 关的耗时口径因此不变。

版本推进同样推迟到读数之后（`candidateVersion` 在锁内算、在 A 段落定）：判据命中但读数
拿不到读后感时，这次折叠不该在上下文版本史上留痕。

## 4. 回归证据

```text
go build ./...                                                    → exit 0
go vet ./...                                                      → exit 0
go test ./application/core/... ./seelebridge/... ./seelexctx/... ./internal/... -count=1
  → all ok（application/core 17.9s、context_runtime 2.1s、seelebridge 19.8s、
     seelexctx 1.1s、internal/adapters 1.4s）
go test ./application/core/ -run 'TestFoldWithFailedModelReadback|TestFoldWithModelReadbackStillFolds' -v
  → PASS PASS
gofmt -l <改动文件>                                               → 干净
```

## 5. 未做 / 边界

1. **提交**：改动与本文档同一次提交（本轮用户明确要求提交；仓库默认不自动 commit）。
2. **真 API 冒烟未跑**：读数闸的判定面在离线夹具（读数回执由夹具给），真前缀重放失败
   （账号限额/网关 4xx）的形态仍需一次 live 复核——但那次复核现在有据可查：失败痕写
   `reason=no_model_readback source=... note=<真实报错>`，不再是"折了但没读后感"。
3. **`scripts/gen_core_readme_index.py` 根包分卷自检仍是红的**（历史遗留：
   `teamwork_service.go` / `teamwork_board_projection_test.go` 未归卷），因此根包分卷
   README 未由脚本刷新；本次新增用例的索引条目手工写进 `application/core/README-context.md`
   （格式与生成器一致）。这是**既有**问题，不在本次范围内。
4. **GUI 未点检**：运行中的 GUI 是旧构建；本次改动不涉及前端契约（门禁顺序不变），
   重开进程后按既有冒烟检查单点一遍即可。
