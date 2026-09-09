# v8 会话存储 M1–M4 实施记录（2026-09-09）

> 性质：一次性工作包实施记录。权威口径仍以 [my_design.md](./my_design.md)
> （v8.2）与代码/测试为准；本文件只记录本次交付范围、证据与遗留边界。

## 1. 交付结论

JSON 后端的新会话已默认切到 v8 布局：`metadata/guide.json` 路由 +
各模块独立 head（message/event/compact/stack/lifecycle/retention/subagent/
toolresult）、`message/message_{m}_{n}.jsonl` 事件行正文、`compact.jsonl`、
`event/event_{m}_{n}.jsonl`、`history.json` provider 缓存、`big_tool_result/`
与 `metadata-index/search.json`。旧会话（`manifest.json` 布局）只读兼容。

公开 `Repository`/`Router`/`SessionGranularStore` 方法签名不变；`Event`
新增 `commit_id`/`in_out_json`/`wire_material`（omitempty，向后兼容），公开
事件读路径返回时剥离扩展字段以保持旧契约。

## 2. M1–M4 状态

| 阶段 | 范围 | 状态 |
|---|---|---|
| M1 | message 事件行存储 + metadata 模块化（guide + module head） | 已实现 + T-M1-01..08 |
| M2 | R1/R2/R3 读取器（含尝试缓存） | 已实现 + T-R1-01..04 / T-R2-01..13 / T-R3-01..04 |
| M3 | lifecycle（draft/queue）+ fork session/subagent | 已实现 + T-LC-01..09 / T-FK-01..09 |
| M4 | LRU/retention、EVENT、单会话检索索引、blob GC | 已实现 + T-WM-01..05 / T-EV-01..04 / T-SR-01..03 / T-BL-01..03 / T-CFG-01 |

契约测试合计 65 条（`sessionstore/v8_m1_test.go` 至
`sessionstore/v8_m4_test.go`），门禁规则：先 T-* 全绿，再 headless 跨模块
装配冒烟。

## 3. 验证证据（本机 Windows，2026-09-09）

```text
go test ./sessionstore -count=1 -timeout=300s                      # 全绿（含 65 条 v8 契约）
go test -race ./sessionstore -run 'TestV8M1|TestV8LC|TestV8R2Stable|TestV8FK|TestV8WM' -count=1
go vet ./...
go build ./...
go build -tags "gui,desktop,production" ./...
go test . -run 'TestHeadlessRestorePrefixProbe' -count=1 -timeout=300s   # v8 新会话 12 轮写入 → 重启恢复 → 前缀逐条一致（9/9）
go test ./... -count=1 -skip 'TestBackgroundCompletionWhileSwitchingToC|TestGUIBuildKeepsLocalAndPublicConfigurationSeparate|TestTwoRunningViewThirdThenSwitchedFinishesHot|TestTwoRunningViewThirdThenSwitchedFinishesCold' -timeout=1800s
```

四个被排除用例经 detached HEAD（d29cfb8）基线对照均同样失败（环境性/
先前遗留，非本次改动引入）：`TestBackgroundCompletionWhileSwitchingToC`、
`TestTwoRunningViewThirdThenSwitchedFinishesHot/Cold`（会话视图污染断言，
本机 mock provider 时序下失败）、`TestGUIBuildKeepsLocalAndPublic
ConfigurationSeparate`（GUI 构建脚本契约，与本次存储改动无关）。

## 4. 关键接线与语义

- 新会话判定：目录无 `manifest.json` 即按 v8 创建；读路径按
  `metadata/guide.json` 存在与否分派（`sessionstore/v8_repository.go`）。
- `commit.Events` → message 事件行 append（Seq=0 续号、显式 Seq 严格递增、
  ≤ head 幂等跳过、崩溃残尾清理）；`ProviderHistory` → `history.json`
  可替换缓存；tool-result refs → `metadata/toolresult.json` 发布点。
- v8 会话 `ReadRollout` 返回 `ErrRolloutUnavailable`，恢复走
  record/事件尾回退（headless 探针已验证重启前缀一致）。
- headless 恢复探针断言已升级为「rollout 或 v8 布局证据 + 前缀一致」。

## 5. 遗留边界（未在本工作包内完成）

- R2 装配/attempt cache/LRU/lifecycle 的运行期接线（seelexctx 压缩装配、
  application 草稿/队列、用户确认的 LRU 触发）：v8 引擎与契约测试已就绪。
- SQLite/PostgreSQL/Redis 的 v8 化与 layout 迁移工具 / verify / reconcile
  （M5）。
- `session_storage.*` 配置键并入 seele.yaml limits（v8_config 默认值已就位，
  覆盖链路未接）。

## 6. 文件地图（新增）

- 引擎：`sessionstore/v8_metadata.go`、`v8_message.go`、`v8_event.go`、
  `v8_compact.go`、`v8_attempt_cache.go`、`v8_reader_r1_r3.go`、
  `v8_reader_r2.go`、`v8_lifecycle.go`、`v8_fork.go`、`v8_retention.go`、
  `v8_search.go`、`v8_blob.go`、`v8_config.go`
- 接线：`sessionstore/v8_repository.go`
- 契约测试：`v8_m1_test.go` / `v8_m2_test.go` / `v8_m3_test.go` /
  `v8_m4_test.go`（legacy 回归 fixture：`v8_legacy_helpers_test.go`）

## 7. 运行期接线与 AB（2026-09-09 第二轮增补）

- R2 运行期装配：`resumeSessionCold` 对 v8 会话经
  `session_runtime.SessionWireAssemblerPort` →
  `Router.AssembleWireWorkspace` 直接装配 engineHistory（compact 摘要 + 尾窗
  + 最近 K 条尝试），非 v8 回退旧链路。
- compact/lifecycle/LRU 接线：`SessionContextStore.PushCompact` 桥接 v8
  compact 通道并触发 retention advisory；`resumeSessionCold` 调用
  `LifecycleRecover`；`Router.LRUDeleteWorkspace` 提供用户确认删除入口。
- AB 冒烟：`go test . -run TestABSessionChainSmokeLegacyVsV8`，两条链路
  “重启后首请求 == 不重启继续”且跨链路逐条一致；指标与热点分析见
  [ab-session-chain-report.md](./ab-session-chain-report.md)。

## 8. 取代旧链路（2026-09-09 第三轮增补）

- R2 装配核心抽为纯函数 `v8AssembleWireRows`；legacy JSON 会话（旧布局存量
  数据）把 transcript 行映射为 message 行后走同一 R2 装配——
  `Router.AssembleWireWorkspace` 对 JSON 两种布局都返回 ok=true，恢复组装
  在 JSON 后台上完成统一。
- rollout 重放恢复退役：`resumeSessionCold` 不再调用
  `LoadSessionRolloutTranscriptWorkspace`；rollout 实现与读接口随后整体
  删除（见 §9），不再参与运行期恢复。

## 9. 命名与唯一链路收口（2026-09-09 第四轮增补）

- 文件命名去掉版本前缀与文档代号：`v8_*.go` → 语义化文件名（message_rows、
  module_heads、structural_events、compact_frames、wire_assembler、
  history_resume_readers、lifecycle、fork_store、retention、search_index、
  big_tool_result、attempt_cache、storage_settings、json_layout、
  runtime_api、jsonl_io），测试同理；代码标识符同步去掉 v8/R1/R2/R3
  前缀（如 v8AssembleWire → assembleWire、v8R1Page → pageHistoryRows、
  v8R2Result → wireResult），错误文本统一 `session storage:`。
- 旧链路删除：`rollout.go`/`rollout_test.go`/`transcript_log_test.go`/
  `legacy_range_test.go` 与 legacy fixture 已删除；rollout 读写接口与
  `ReadRollout*`、`SessionRolloutPort` 一并移除；`WriteCommit` 为 JSON 唯一
  写路径（事件行 + 模块 head），不再写 manifest/generation/transcript/
  rollout；AB 用的 legacy 环境开关已删除。
- 存量旧 manifest 会话保留只读回退（读路径按 `metadata/guide.json` 判定），
  仅用于打开历史数据，不参与新写入与新链路演进。
