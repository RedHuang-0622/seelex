# 热会话切换「时而灵时不灵」：链路复现与修复

日期：2026-09-06
性质：bugfix 工作记录（先复现 → 修复 → 重跑复现 → 沉淀回归）

## 1. 现象

GUI 侧边栏会话切换（热→热、热→冷）**时而灵时不灵**：点击目标会话后，后端
视图其实已切换，但对话正文偶尔停在旧内容/空壳基线，不再更新；直到下一次
用户交互触发的整份刷新，或再切走/切回一次，内容才恢复。与目标会话是热
（驻留）还是冷（未驻留、走 restoring 空壳 + 后台装载）无关。

## 2. 读取链路（切换的数据通路）

```
core resumeSession(sessionID)                     application/core/session_history.go
  ├─ hot  → bumpViewEpoch + hotAttachSession      只移视图指针 + 投影
  ├─ cold+空闲 → bumpViewEpoch + resumeSessionCold(同步装载)
  └─ cold+运行中 → beginAsyncRestore(restoring 空壳) → 后台 resumeSessionCold
  │     （装载完成后按 viewEpoch 判定是否发布基线）
  ▼
Bridge.ResumeSession(B)                            gui/bridge.go
  视图 ID 变化 → resubscribe()：关闭旧订阅 A、建新订阅 B
  → startRelay(B) 首发 seelex:ready（= 权威基线 Snapshot）
  新订阅 delivery_seq 从 1 重计（hub.go 每订阅独立编号）；ackedSeq 归 0
  ▼
前端 runtime-events.js / client-state.js / protocol.js
  seelex:ready → acceptSnapshot：会话变化 ⇒ lastEventSeq=0（复位已应用水位）
  seelex:event  → applyEvent：按 delivery_seq 判连续性 / 缺口补取 / 重复丢弃
```

内容增量只在本订阅的 `delivery_seq` 上判连续性：切换即重订阅，新订阅
`seq=1..N`；旧会话的高水位若不清零，会把新订阅前 N 段吞成“重复”。

## 3. 根因（已复现确认）

切换竞态中，**旧订阅 relay 的“在途事件”可能在新会话权威基线之后才到达渲染
层**，并且旧版前端把这类“不属于当前视图会话”的事件按 `dropped` 处理时
**推进了 applied 水位**（`protocol.js`：`lastSeq: seq`，`client-state.js` 随之
`reportApplied()` 回报宿主）：

- Bridge.resubscribe 与旧 relay goroutine 是并发关系：旧 relay 可能在
  resubscribe 关闭订阅前已取出 1 条 A 事件，其 `emit` 与新 relay 的
  `seelex:ready(B)` 投递交错（跨进程投递无顺序保证）；
- 前端接受 B 基线后水位已复位 0，此时迟到的 A 事件（delivery_seq 是**旧订阅
  编号**，通常远大于 B 新订阅 1..N）命中 `belongsToView` 不匹配 → 旧版
  `dropped` 分支把 `lastEventSeq` 抬到旧编号；
- 此后 B 的 `seq<=该编号` 的事件全部被判“重复”静默丢弃，B 正文冻结在基线；
- 由于还向宿主回报了污染的 ackedSeq，bridge 的重推/补取也不会介入。

为什么“时而”：必须同时满足 (a) 切换发生时源会话正在产生事件（旧 relay 手里
恰好有在途事件）、(b) 该在途事件的投递排到新基线之后。繁忙会话切换时概率
非零但不必然 → 时而灵时不灵；与目标是热还是冷无关（污染发生在**进入方订阅**
的水位上，与装载路径无关）。

## 4. 复现（RED）

确定性复现放在渲染层流水线（与 Wails 事件投递乱序/竞态等价的交错输入）：

- `gui/frontend/dist/session-switch-stale-event.test.mjs`（新增，node 测试）：
  - 场景 1 热→热：A 订阅事件推高水位 → 切 B（基线，水位复位）→ B seq1..2 落地
    → 注入旧订阅 A 迟到事件 seq=99 → B seq=3 增量。修复前 B 停在 `B0B1B2`，
    seq=3 被吞（正文冻结）；修复后 `B0B1B2B3`。
  - 场景 2 热→冷：A 运行中切冷 B（restoring 空壳基线）→ snapshot.changed
    触发整份 refresh（内容基线）→ B seq2..3 落地 → 注入 A 迟到事件 seq=77 →
    B seq=4。修复前停在 `B0B1B2`；修复后 `B0B1B2B3`。
- 修复前两个用例均红（fail 2）；修复后绿（pass 2）。

## 5. 修复

1. `gui/frontend/dist/protocol.js`：`belongsToView` 不匹配的事件丢弃但
   **不推进水位**（`lastSeq` 保持原值）。视图外事件对本订阅连续性无语义，
   推进只会吞掉新订阅从 1 重计的 delivery_seq。
2. `gui/frontend/dist/client-state.js`：`dropped` 事件既不推进水位也**不回报
   宿主已应用**（避免把宿主 ackedSeq 抬到新订阅不可能到达的旧编号）。
3. `gui/bridge.go`：relay **代际守卫**（`relayGen`）——每次（重）订阅换代；
   relay 投递每条事件前校验自己仍是当前代际，被 resubscribe 取代后立即退出，
   已取出的旧订阅余量事件不再外投（收窄污染源；渲染层幂等容忍为正确性兜底）。

## 6. 回归沉淀与验证

- 前端新增回归：`session-switch-stale-event.test.mjs`（上）；同步修正
  `protocol.test.mjs` 中“dropped 推进水位”的旧断言为“不推进、不占编号”。
- Go 侧新增：`gui/bridge_events_test.go` `TestBridgeRelayGenerationSupersedesStaleRelay`
  （换代后旧代际失效、新 relay 正常投递）。
- 验证：
  - `node --test gui/frontend/dist/*.test.mjs` → 197 pass / 0 fail；
  - `node --check` 改动 JS 全绿；
  - `go test -race ./gui/ -count=1` → ok；
  - 复现用例先红后绿（红：仅回退两处前端修改时 fail 2；绿：修复后 pass 2）。

## 7. 改动文件

- `gui/frontend/dist/protocol.js`（水位语义）
- `gui/frontend/dist/client-state.js`（dropped 不回报）
- `gui/bridge.go`（relay 代际守卫）
- `gui/frontend/dist/session-switch-stale-event.test.mjs`（新增回归）
- `gui/frontend/dist/protocol.test.mjs`（断言更新）
- `gui/bridge_events_test.go`（Go 回归）
- `gui/frontend/README.md`（测试清单说明）
