# 会话薄封装测试用例（Test Cases）

> 依据: [mbd-models.md](./mbd-models.md)（M1–M5）、[mbd-us.md](./mbd-us.md)（UC1–UC11）、
>       [mbd-overview.md](./mbd-overview.md)（阶段门禁）
> 标记: ✅已有 / 🆕新增（新增项先行写红，转绿即验收）

## 0. 测试四维度

| 维度 | 符号 | 目标 | 手段 |
|---|---|---|---|
| 功能 | F | 功能是否正确 | 用例验收、断言输出/状态 |
| 边界 | B | 容易出 bug 的地方（非法迁移、空态、上限、竞态时序） | 状态机非法输入、nil/空、窗口/队列/体量上限、切换时序 |
| 压测 | P | 性能与规模（多会话、高频切换、大体量、内存有界） | 数量/频率/体量压测、goroutine 有界、耗时阈值 |
| 竞争/死锁 | R | 日常使用是否出现数据竞争/死锁 | `-race` 全套、锁序断言、死锁超时、后台不阻塞全局锁 |

## 1. M1 trace 注入与结果返回

| 编号 | 测试 | 类型 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|---|
| T1.1 | TestTracePerSessionIsolation | F/R | 两会话并行后各自 trace 只含自身 span（INV-T1） | 9.2 | 🟩 |
| T1.2 | TestTraceHookChainComposition | F/B | `H=h_n∘…∘h_1` 依序透传；空链/单环/满表不破坏控制流 | 9.2 | 🟩 |
| T1.3 | TestStreamResultReturnModel | F | `(output,ΔT,ΔV)`：reply/reasoning/toolCalls 返回且 View+trace 增量可观察 | 9.2 | 🟩 |
| T1.4 | TestTraceQuerySessionFiltered | F/B | 会话过滤查询；空/其它会话不命中；trace 满限不溢出 | 9.2 | 🟩 |

## 2. M2 session 组成部分

| 编号 | 测试 | 类型 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|---|
| T2.1 | TestSessionUnitComponents | F | `S_i=(id,K,parent,E,V,Q,C,B,status)` 元组契约 | 9.1 | 🟩 |
| T2.2 | TestKindParentInvariant | B | K=subagent⇒parent≠⊥；K=main⇒parent=⊥ | 9.4 | 🆕 |
| T2.3 | TestLifecycleThinStateMachine | B | 非法迁移拒绝（空 id、运行中 unload、未加载提交）；status 由 HasSession 驱动 | 9.1 | 🟩 |
| T2.4 | TestSessionWriteGoesThroughPort | B | 编译期断言：core 无会话容器字段，写必经端口 | 9.1 | 🟩 |
| T2.5 | TestDomainDisjoint | F/R | 两会话队列/视图/上下文互不污染 | 已有 | ✅ |
| T2.6 | TestSessionGranularityPersistence | F/B | `session:<id>` 原子读写；项目索引；subagent 同构落盘；重复写幂等 | 9.3 | 🟩 |
| T2.7 | TestForkSessionDeepCopy | B | fork 后父子引用不相交，改子不影响父 | 9.4 | 🟩 |
| T2.8 | TestQueueWindowLimits | B | 队列/历史窗口/上下文栈超上限：有界截断、不溢出、不丢事件 | 9.1 | 🆕 |

## 3. M3 前端投影（对话 Π_conv + 轨迹 Π_traj）

| 编号 | 测试 | 类型 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|---|
| T3.1 | TestConversationProjectionDeterministic | F | Π_conv 同输入同输出 | 已有 | ✅ |
| T3.2 | TestConversationChips | F | 思考/工具一行 chip；回复保留；chip 带轨迹 key | 已有 | ✅ |
| T3.3 | TestTrajectoryProjectionDeterministic | F | Π_traj 同输入同输出 | 已有 | ✅ |
| T3.4 | TestTrajectoryToolPairingById | F/B | tool 按 id 配对；缺 id 回退 name+顺序；孤儿 tool_result 兜底 | 已有 | ✅ |
| T3.5 | TestProjectionSessionFiltered | B | 非当前会话负载事件忽略；全局放行 | 已有 | ✅ |
| T3.6 | TestMessageKeyGlobalUnique | B | 消息 ID 全局唯一（跨会话不撞键） | 已有 | ✅ |
| T3.7 | TestChatTrajectoryConsistency | B | 同一 tool 两视图 status/duration/size 一致；chip 跳转命中 | 9.2 | 🟩 |
| T3.8 | TestProjectionEmptyAndOversize | B | 空会话/超长消息/超多工具：空态文案、截断上限、无 DOM 溢出 | 9.2 | 🟩 |
| T3.9 | TestTraceVsTrajectoryDataSources | F | 对话/轨迹=Π(View.Conversation)，trace=T_i，分离且一致 | 9.2 | 🟩 |

## 4. M4 视图切换

| 编号 | 测试 | 类型 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|---|
| T4.1 | TestHotAttachZeroWriteFingerprint | B | 切换前后目标会话 E/V/Q/C/B 字节级不变 | 已有 | ✅ |
| T4.2 | TestSwitchOnlyMovesV | B | 只换 V+订阅；A 执行态/transcript 不变 | 已有 | ✅ |
| T4.3 | TestSwitchToRunningResyncsBaseline | F | 切运行中会话先基线后增量 | 已有 | ✅ |
| T4.4 | TestColdLoadRebuildsBundle | F/B | 冷加载重建框架 Session + seed；历史逐字节一致；重复冷加载拒绝 | 9.1 | 🆕 |
| T4.5 | TestSwitchDuringRunAndQueue | B | 切换与后台完成/排队交错：不串写、不丢队、不重复 | 9.2 | 🆕 |
| T4.6 | TestSwitchDeadlockFree | R | 高频切换与后台完成交错无死锁（超时门禁） | 已有 | ✅ |

## 5. M5 多会话并行

| 编号 | 测试 | 类型 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|---|
| T5.1 | TestStressConcurrentSessions | P/R | 6 会话并行+切换风暴+队列并发，-race 全绿 | 已有 | ✅ |
| T5.2 | TestBackgroundDeltaNotBlockGlobalLock | R | 后台增量在活跃持全局锁时不被阻塞 | 已有 | ✅ |
| T5.3 | TestSubmitRoutingExplicitSid | B/R | 输入只进显式 sid 队列（防 TOCTOU） | 已有 | ✅ |
| T5.4 | TestTraceParallelIsolation | R | 并行两会话 trace 不相交 | 9.2 | 🟩 |
| T5.5 | TestGlobalSharedFaceBounded | B | G=(V,registry,bus,services) 之外无共享可变状态 | 9.5 | 🟩 |

## 6. 边界测试专项（B）

| 编号 | 测试 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|
| B1 | TestEmptyAndNilHandles | nil bundle / 空会话 / 空队列 / 空 trace：不 panic、明确错误 | 9.1 | 🟩 |
| B2 | TestIllegalLifecycleTransitions | 空 id / 运行中 unload / 未加载提交 / 双 cold load 均拒绝 | 9.1 | 🟩 |
| B3 | TestBoundsAndTruncation | 队列上限、历史窗口、trace 上限、工具输出截断：有界 | 9.1/9.2 | 🆕 |
| B4 | TestDeepCopyBoundary | fork/冷加载引用不相交；共享面仅只读常量 | 9.4 | 🟩 |
| B5 | TestSwitchRaceTiming | 切换瞬间事件到达/提交到达：基线 resync 兜底，不串写 | 9.2 | 🆕 |
| B6 | TestPersistenceIdempotent | 会话粒度重复写/恢复幂等，键不漂移 | 9.3 | 🟩 |

## 7. 压测专项（P）

| 编号 | 测试 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|
| P1 | TestManySessionsScale | 100 个 LIVE 会话并行执行：无死锁、goroutine 有界、完成 | 9.2 | 🆕 |
| P2 | TestSwitchStorm | 高频切换风暴（千次级）：事件不串、无泄漏 | 已有（stress 含切换风暴） | ✅ |
| P3 | TestLargePayloadProjection | 大体量消息/工具输出：投影耗时与 DOM 有界（阈值断言） | 9.2 | 🆕 |
| P4 | TestMemoryBounded | 连续建/卸会话：`runtime.NumGoroutine`/堆有界 | 9.4 | 🆕 |
| P5 | TestEventFingerprintStable | 相同脚本重复 N 次，事件序列一致（无乱序污染） | 9.2 | 🟩 |

## 8. 数据竞争与死锁专项（R）

| 编号 | 测试 | 断言 | 阶段 | 状态 |
|---|---|---|---|---|
| R1 | TestRaceFullSuite | `-race ./application/core ./session ./seelebridge ./gui` 全绿 | 全程 | 已有 ✅ |
| R2 | TestLockOrdering | 锁序固定（Transition→Domain→Unit→View），无 ABBA（静态+压力） | 9.1 | 🟩 |
| R3 | TestDeadlockTimeouts | 切换/提交/落盘均带超时门禁，挂死即失败 | 已有（部分） | ✅ |
| R4 | TestBackgroundNoGlobalLock | 后台流式/增量不取全局写锁 | 已有 | ✅ |
| R5 | TestShutdownConcurrent | 运行中多会话 + 并发 Shutdown：可终止、无泄漏、无 panic | 9.5 | 🟩 |

## 9. 用例验收（F）

| UC | 验收测试 | 断言 | 状态 |
|---|---|---|---|
| UC1 提交/排队 | TestSubmitQueuesToOwningSession | 运行中提交入队，只进自身会话 | ✅ |
| UC2 切换视图 | T4.1–T4.3 | 切换只移 V + resync | ✅ |
| UC3 恢复历史 | TestResumeSessionRestoresHistory | 恢复后引擎历史/视图与存储一致 | ✅ |
| UC4 查看轨迹/对话 | T3.* | 双投影正确、一致性 | 部分 🆕 |
| UC5 fork | T2.7 | 会话粒度深拷贝隔离 | 🟩 |
| UC6 ReAct loop 委托 | TestLoopDelegatedToSeele | core 无自编循环/预算/transcript | 9.2 🟩 |
| UC7 子代理独立会话 | TestSubagentCreatesOwnSession | plan 节点 → 独立 SessionUnit | 9.4 🟩 |
| UC8 merge-back | TestPlanMergeBackConsistent | 子代理结果经会话端口写回 | ✅ |
| UC9 FC+沙箱 | TestToolPipelineFCThenSandbox | 先 FC 许可后沙箱；授权即 allowance | ✅+顺序断言 🆕 |
| UC10 持久化 | T2.6/B6 | 会话粒度原子写、幂等 | 9.3 🟩 |
| UC11 trace | T1.* | 注入/返回/会话过滤 | 9.2 🟩 |

## 10. 阶段门禁

| 阶段 | 门禁（四维度覆盖） | 命令 |
|---|---|---|
| 9.1 | T2.1/T2.3/T2.4 + B1/B2 + R2 | `go test ./... -p 1`、`go vet ./...` |
| 9.2 | T1.*/T3.7/T3.8 + P3/P5 + R4 + 事件指纹 | `go test ./application/core ./seelebridge`、`node --test` |
| 9.3 | T2.6/B6 | `go test ./sessionstore ./session` |
| 9.4 | T2.2/T2.7 + B4/P4 + UC7 | `go test ./application/core ./seelebridge -race` |
| 9.5 | T5.5 + R1/R5 + 清理断言 | `go test ./... -p 1`、`-race` 关键包、冒烟 |

## 11. 冒烟（F，跨阶段保持绿）

- 单会话全链路：submit → 流式 → 工具 → persist → resume。
- 多会话基本切换 + 运行中 hot_attach 回看。
- 前端 `node --test` 全量 + `go build -tags "gui,desktop,production" ./...`。
- 产物 `seelex-flow` 冒烟（stage → smoke → deploy → smoke）。
