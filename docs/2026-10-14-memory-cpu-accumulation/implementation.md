# 实现状态（P0 释放面 + 可调窗口）

日期：2026-10-14 · 对应 `optimization-plan.md` 的 P0 全部 + 配置面。
读法：先看本页"改了什么 / 口径怎么变"，需要理由再看 `release-surface.md`、`verdict.md`。

## 一句话

会话释放面从「只丢 application 侧的引擎端口引用」改成**完整释放**
（引擎实例 + 桥侧会话本体 + 可见正文 + 下游句柄），并且把"留几个"抽成配置项
`limits.session_release_window`（默认 **1**：受保护集之外只保留上一个）。

## 配置面（用户可直接调）

`config/seelex.yaml` 的 `limits:` 段：

```yaml
limits:
  session_release_window: 1   # 受保护集之外最多保留几个「已加载」的会话
  #resident_limit: 6          # 分层覆盖（可选，显式 >0 才生效）
  #loaded_content_limit: 12   # 分层覆盖（可选，显式 >0 才生效）
```

| 键 | 语义 | 默认 |
| --- | --- | --- |
| `session_release_window` | **受保护集之外**保留几个已加载会话；`1` = 只保留上一个（切走一个不释放、再切走一个才释放最旧的） | `1` |
| `resident_limit` | 分层覆盖（老口径）：引擎 bundle 层按「驻留会话总数上限（**含**受保护会话）」算 | 未配置（`0`） |
| `loaded_content_limit` | 分层覆盖（老口径）：可见正文层同上 | 未配置（`0`） |

- **受保护集**（永不释放）：当前视图会话 ∪ 待审批会话 ∪ 运行/排队/在飞会话 ∪
  正在恢复的会话。判据只有一处：`application/core/session_release.go` 的
  `protectedSessionLocked`（两层 LRU 都必须调它，不允许各自复制条件）。
- **释放动作**（`releaseSessionMemory`）：flush 落盘 → 引擎实例
  （`EnginePort.UnloadSession`）→ 桥侧会话本体
  （`seelebridge.Runtime.ReleaseSessionBundle`）→ 下游句柄
  （`goalCoordinator.ForgetSession`）→ 可见正文 → 使用序记账。
  **保留**：会话单元、目录行、标题、统计与磁盘事实；重开走冷加载。
- **口径变化**：`loaded_content_limit` 的出厂值 `12` 已从配置里移除——留着一个
  显式值就会把统一窗口挤掉（分层键优先）。老口径没丢：显式写上 `resident_limit` /
  `loaded_content_limit` 就是老语义（"该层总数上限，含受保护会话"），
  折算方式见 `seelexctx.Limits.ReleaseWindow` 与 `releaseBudget`。

## 界面可调（设置 → 会话驻留）

窗口不必改配置文件也能调：设置弹窗新增「会话驻留」一段（`1..16` 的下拉框）。

- **立刻生效**：窗口是**现读**的（释放面每次决策调 `Limits.ReleaseWindow()`），换掉
  进程级 limits 快照的那一格之后，下一次释放就用新值——不必重启，也不必碰任何 LRU
  调用点。
- **落盘与优先级**：界面上改的值写进**数据根**的 `runtime-limits.json`（与
  `session-storage.json` / `workspace_index.json` / `scheduled-tasks.jsonl` 同级）。
  覆盖链因此变成 **内置默认 ← `config/seelex.yaml` 的 limits 段 ← `runtime-limits.json`**，
  启动时由 `applyRuntimeLimitsOverlay` 叠加。不写回 `seelex.yaml`：那份文件是**用户的
  配置**（带注释、手改），程序往回写要重排注释、抹平格式差异；两份各自留在自己的地方，
  谁压谁一眼可解释。坏文件/缺文件按"没有覆盖"继续（界面上拧过的参数不该阻断启动）。
- **次序是刻意的**：校验 →（落盘）→ 改内存。落盘失败 ⇒ 整步不改（返回错误、内存不动），
  于是"重启后的值"与"界面上的值"不会分叉，前端把控件回滚到最后一份权威值是诚实的。
- **区间 `1..16`**：不留 `0`（"谁都不留" = 每次切会话都冷加载，且与配置层"`0` = 未配置
  → 默认 1"撞车），也不留"永不释放"格（那等于把内存治理整块关掉）。边界只有一处：
  `seelexctx.SessionReleaseWindowMin/Max`；前端 `release-window.js` 的同一组常量由
  `settings-release-window.test.mjs` 逐值对账（防两端口径漂移）。
- **不设运行守卫**（与 `SetLoopLimit` / `SetReasoningEffort` 不同）：窗口只约束"哪个
  **已静默**的会话可以被释放"（受保护集含运行/排队/在飞会话），所以会话在跑时改它既
  打断不了谁，也放不掉任何在飞的内存。
- **读法**：弹窗打开时 `invoke("SessionReleaseWindow")` 拉一次读数——窗口是**进程级**
  设置，而快照的 runtime 是**按会话**组装的投影，塞进去会让"切会话时它跟着动"变成一个
  假事实（与 `SessionStorageConfig` 同一姿势）。

## 改动清单

| 文件 | 做了什么 |
| --- | --- |
| `seelexctx/limits.go` | 新增 `SessionReleaseWindow`（`session_release_window`）+ `Limits.ReleaseWindow()`；两个分层键默认改为未配置（`0` = 交给统一窗口），`WithDefaults` 不再给它们补 6/12；负值校验 |
| `config/seelex.yaml`、`internal/bootseed/assets/config/seelex.yaml` | 新增窗口键与注释、移除出厂 `loaded_content_limit: 12`（两份逐字节同步，走 `scripts/sync-bootseed-defaults.ps1`） |
| `application/core/session_release.go`（新） | 释放面唯一定义处：`protectedSessionLocked` / `releaseBudget` / `releaseSessionMemory` / `releaseSessionContentMemory` / `forgetResidentLocked` / `reconcileSessionRelease` |
| `application/core/resident_lru.go` | 引擎层按"受保护集之外"计数 + 统一走 `releaseSessionMemory`（不再自己调引擎卸载） |
| `application/core/content_lru.go` | 内容层同上（共享谓词 + 窗口额度）；删掉自成一体的候选挑选 |
| `application/core/session_lifecycle.go` | `UnloadSession` 改用 `releaseSessionMemory(releaseFull)`（顺带补齐桥侧本体与残格）；抽出 `switchViewToFreshDraftLocked`（卸载/删除掉当前视图后视图单例的收口） |
| `application/core/workspace_usecase.go` | `DeleteSession` 走完整释放（不再只删记录，内存里不留格）；删的若是当前视图，就地切到新草稿（不再靠"保留视图空壳单元"兜底） |
| `application/core/chat.go` | 回合收尾新增 `reconcileSessionRelease()`——**忙→闲**的唯一收敛点（切走之后跑完的会话在这里变候选） |
| `application/core/goal_coordinator.go` | 新增 `ForgetSession`：让按会话缓存的上下文存储句柄随释放离场（否则重开变成两个 store 句柄写同一会话） |
| `application/core/service_state.go`、`service_snapshot.go`、`session_history.go` | **视图意图按会话记账**（`sessionViewIntent`，见下节"释放面暴露的切换路径缺陷"）：`ResumeSession` 命中"同目标装载在途"时重指空壳且不推进意图，在途装载完成时仍能发布 |
| `internal/adapters/engine_port.go` | `UnloadSession` 清 `handoffLatest`（端口对已卸载会话不留状态）；新增 `ReleaseSessionBundle` 转发 + `EnginePortDeps.ReleaseSessionBundle` |
| `seelebridge/runtime_session_release.go`（新） | `ReleaseSessionBundle`（放掉 `bundles[sid]`）+ `HasSessionBundle` + `dropSessionResidue`（servedRoutes / routingNoticeSeen / teamworkCoords / teamworkBoardCache）。守卫：`ForkInFlight`、`AsyncPendingFor > 0`、`activeSessionID` 命中即拒绝 |
| `main.go` | 装配 `ReleaseSessionBundle: runtime.ReleaseSessionBundle` |

### 界面可调窗口（本轮追加）

| 文件 | 做了什么 |
| --- | --- |
| `seelexctx/limits.go` | 新增窗口边界与唯一校验判据 `SessionReleaseWindowMin/Max` + `ValidateSessionReleaseWindow`（设置面与启动期覆盖层共用，杜绝"能存进去、读回来却不认"） |
| `application/core/internal/limits/limits.go` | `SetSessionReleaseWindow`：只改一格并原子发布（不用 `Apply`——那会重跑 `WithDefaults`） |
| `application/core/limits.go` | 门面 `ApplySessionReleaseWindow`（校验在最前，非法值一个字节都不改） |
| `application/core/service_release_window.go`（新） | `Service.SetSessionReleaseWindow`（校验 → 落盘 → 改内存，返回生效值）与 `Service.SessionReleaseWindow` 读数 |
| `application/contract/ports.go` | 新增窄可选端口 `ReleaseWindowStore` + `Dependencies.ReleaseWindowStore`（`nil` = 只本次运行有效） |
| `runtime_limits_overlay.go`（新，根包） | 覆盖层读写：`runtimeLimitsPath` / `readRuntimeLimitsOverlay` / `applyRuntimeLimitsOverlay` / `writeRuntimeLimitsOverlay`（读-改-写 + 临时文件改名）/ `runtimeLimitsStoreFile` |
| `main.go` | 启动时叠加覆盖层（在 `seelex.yaml` 的 limits 之后、`runtimeLimits` 赋值之前）；装配 `ReleaseWindowStore` |
| `gui/bridge.go` | `Application` 接口 + `Bridge.SetSessionReleaseWindow` / `Bridge.SessionReleaseWindow` 透传 |
| `gui/frontend/dist/index.html` | 设置弹窗新增「会话驻留」一段（`#release-window` 下拉框 + `#release-window-status` 读数行） |
| `gui/frontend/dist/release-window.js`（新） | 取值面纯模块：`normalizeReleaseWindow` / `releaseWindowLabel` / `releaseWindowOptions` |
| `gui/frontend/dist/app.js` | 接线：注册两个 id、`openSettings` 拉一次读数、`change` 提交并采纳生效值、失败回滚 + 弹错 |

## 测试与验证

新增/重写用例：

- `application/core/session_release_test.go`：窗口 1 只保留上一个（引擎 + 桥侧本体
  一起放掉）；运行中会话受保护（即使最旧也不释放，窗口按"受保护集之外"计数）；
  最旧会话的正文随释放卸载且窗口标志/磁盘消息不变；`DeleteSession` 释放内存；
  `releaseIdle` 不碰当前视图会话；`ForgetSession` 让 goal 句柄离场。
- `application/core/resident_lru_test.go`：`TestSessionReleaseDefaults`（窗口默认 1、
  分层键默认未配置、额度折算）；原有两条**老口径**用例（`resident_limit` 显式 >0）
  语义不变，仍绿。
- `application/core/content_lru_test.go`：`TestLoadedContentLayerLimitDefaults`；
  原有四条老口径内容用例语义不变，仍绿。
- `seelebridge/runtime_session_release_test.go`：本体移出注册表 + 残格清理 + 活跃
  别名拒绝 + 幂等。
- `internal/adapters/engine_port_release_test.go`：释放面转发（含"桥拒绝 ⇒ false"、
  未装配 ⇒ false）、`handoffLatest` 随卸载离场。
- 释放面暴露的切换/卸载路径（见下节）：`TestPointViewAtRestoringShellKeepsTargetUnitContent`、
  `TestPointViewAtRestoringShellIgnoresSettledTarget`、
  `TestDeleteViewSessionSwitchesViewToFreshDraft`；根包
  `TestTwoRunningViewThirdSwitchStress` / `...FinishesHot` / `...FinishesCold`。

更新既有用例（契约随口径变更，非放宽）：

- `TestDeleteViewSessionKeepsViewUnit` → `TestDeleteViewSessionSwitchesViewToFreshDraft`：
  删除当前视图会话后**不再保留其空壳单元**（卸载/删除的既有契约是单元随释放离场），
  改为断言视图切到新草稿且该草稿有对应单元。
- 根包 `...FinishesHot/Cold` 的"内容仍在"判据改为**带收敛等待**：切换仍须即时返回
  （3s 硬上限不变），但"切回已被释放的会话"是冷加载，内容随后到齐。

回归（第四轮全量 `go test ./... -count=1`）：**全绿**——根包 90.4s、`application/core`
65.5s、`e2e` 12.1s、`internal/adapters` 8.1s、`seelebridge` 96.2s、`sessionstore` 80.3s、
`workspace` 39.8s，其余包全绿，无 FAIL。

前三轮曾红的四例都在**未触碰的包**里、现在都已收口（三条确定性失败已修，一条加载抖动
不复现）：

- `e2e` `TestRoleSessionPortsDoNotLeakStorageTypes`：用例的表里留着两个**已不存在**的
  文件（`application/core/agentteam_service.go`、`gui/headless_team.go`——Agent Team 面
  已随 `bab08c2f` 整体退场），于是死在"parse 打不开文件"上，而不是死在它真正想钉的
  import 泄漏上。已从表里移除并注明由 `role_session.go` / `gui/headless.go` 承载该面。
- `e2e` `TestRepositorySkillAndPluginLayouts`：断言"default 插件必须有 plan skill"，
  而 `$plan` 已退场。改成**磁盘即事实**的判据：`plugins/default/*/SKILL.md` 与 loader
  读出的技能集合一一对应（不许静默跳过），且技能目录不许同时能当独立插件加载——
  不再硬编码技能名（硬编码正是本条过期红的原因）。
- `internal/adapters` `TestRuntimePortForwardsEveryNarrowPort`：**真缺陷**，不是测试账。
  `application/core/project_board.go` 用类型断言探测 `TeamworkPlanSource`，而
  `RuntimePort` 没转发 `TeamworkPlanFor` ⇒ 断言恒 false ⇒ `ProjectBoardTeamPlanFor`
  永远返回 nil，项目看板的"团队计划 / 运行态"两格整块为空（与本文件注释里记的
  2026-10-02 那次 `TeamworkBoardSnapshot` 漏接同一类）。已补转发 + `var _` 断言 +
  探测清单一行。
- `workspace` `TestRepoGitLogClampsLimit`：`git log 查询超时`——加载下的超时抖动
  （全量里 32.7s 红、单独跑 1.6s 绿），第四轮全量已绿。

另记一条时序抖动：根包 `TestChainSwitchABStable`（跨两次运行比对事件全序）在第一轮全量
（并发负载）下红过一次，两次差异是**同一事件集合的不同交错**（`message.delta|0|4|6` 与
`message.delta|7|8|6` 谁先谁后），单独跑 3 次 + 整包跑 2 次均绿 ⇒ 判为并发负载下的
顺序抖动（Hypothesis：与既有跨会话事件交错有关，本改动只增加异步活动的密度）。

## 释放面暴露的切换路径缺陷（本轮一并修）

窗口收到 1 之后，"切回一个已被释放的会话"成了常见路径：目标未驻留 ⇒ 走**冷加载**；
若有别的会话在跑，`resumeSession` 激活 restoring 空壳后立即返回、装载在后台完成。
这条路踩到两件事，都已修：

1. **发布闸门用全局序号 ⇒ 切回同一目标会永远拿不到内容**。
   装载完成时判"我还是最新视图意图"用的是**全局** `viewEpoch`：切走再切回同一个会话
   （或中途切过别的会话）就会把它推走，在途装载被判为"已被更新的切换取代"，不发布 ⇒
   视图停在空壳上（用户看到空会话）。
   修法：`sessionViewIntent[sessionID]` 按会话记账（值仍取自那支单调时钟），装载只校验
   **自己这个会话**的意图 + "视图仍指向自己"。
2. **"同目标装载在途"的早退分支不保证视图归位**。该分支原本假定"空壳已激活"，但视图
   可能已被后来的切换带走（先激活了别的目标的空壳），于是 `ResumeSession(A)` 返回时视图
   还停在别人身上——违反"返回即视图 = 目标"的契约。
   修法：新增 `pointViewAtRestoringShell`——重指空壳与快照投影，**不重复装载、不推进意图**
   （推进意图会让在途装载永不发布，见第 1 条），也**不清目标会话单元的可见区**（在途装载
   可能已经把回读内容写进去了）。

两者都由根包用例钉住：`TestTwoRunningViewThirdSwitchStress`（高频切换下视图必须逐次归位）、
`...FinishesHot/Cold`（切回已释放会话时内容必须落地）。后两者的内容判据改成**带收敛等待**
（等"内容落地"而非"返回瞬间就有内容"）：切换本身仍按 3s 硬上限钉住"即时返回"。

3. **完整释放面不得保留会话单元**。第一版为兜住"删掉正在看的那个会话"在 releaseFull 里
   留了个例外：当前视图会话的单元不删。这违反卸载/删除的既有契约（`UnloadSession` 后
   单元必须没了，重开走 cold_load），`TestUnloadReleasesScope` /
   `TestUnloadRejectsRunningSession` 当场变红。收口方式改为**移除单元 + 视图切新草稿**：
   `switchViewToFreshDraftLocked` 由 `UnloadSession`（原有分支）与 `DeleteSession`（新增）
   共用——视图单例始终有对应单元，且这个单元是真的新草稿，不是一个已被释放的空壳。

## 窗口的口径后果（选 `session_release_window` 时要权衡的）

窗口越小，内存越省，但"切回较旧会话"越常走冷加载：

| 窗口 | 常驻（受保护集之外） | 切回上一个 | 切回更早的会话 |
| --- | --- | --- | --- |
| `1`（出厂） | 上一个 | 热切换（瞬时） | **冷加载**：空闲时同步装载（短暂"恢复中"后内容到齐）；有别的会话在跑时先看见 restoring 空壳、内容随后到齐 |
| `2`–`3` | 上两个/三个 | 热切换 | 热切换（更省心，代价是常驻内存翻倍） |
| `resident_limit` / `loaded_content_limit` 显式 >0 | 按老口径（该层总上限，含受保护会话） | — | — |

高频来回切换的用户建议把窗口开到 `2`–`3`：冷加载是**磁盘读 + 上下文重装**，
窗口 1 下每次切回更旧的会话都要走一遍（不丢数据，但会看到"恢复中"）。

## 已知退化与残留（按优先级）

1. **桥侧本体释放后 `SessionContextStoreFor` 返回 nil**（既有契约："未实例化/未绑定
   ⇒ nil"）。唯一消费者是 goal 协调器，且它已随释放被 `ForgetSession` 清格；该会话
   下次被使用时按盘 Reload。若在"未重开该会话"的情况下发生 goal 写（只可能来自非
   会话入口），这一段落内存态——下一次重开按盘重建。
2. **telemetry 无上限**（P1-1/P1-2）：`MemoryTracer` 的 per-session 事件列表与
   `TokenCountFor` 的全量求和仍在。前者要上游 prune API，后者可改为增量计数——
   两项都留在这里没做。
3. **快照深拷贝 / 目录缓存 / 内容序 O(n)**（P1-3/4/5）：`optimization-plan.md` 原样保留。
4. **node 维度残格**（`liveHistory`/`liveSubs`/`roleTurns`/`images`）不随会话释放：
   映射键是 nodeID/角色会话 ID，误删会踩别的会话的回放窗口，留待 P2 与账本一起做。
5. **`dist/seelex-gui-dev/config/seelex.yaml` 未同步**（仍是 `loaded_content_limit: 12`
   = 老口径内容层 12 个窗口）。dist 内可能有用户数据，未在未确认前改动；同步方式：
   备份后把 `config/seelex.yaml` 的 limits 段对齐（或重建 dist）。
6. 回退：`config/seelex.yaml.bak-2026-10-14` 是改动前的配置副本；代码侧 `git revert`
   本组改动即可（新文件 `session_release.go`、`runtime_session_release.go` 一并撤）。
