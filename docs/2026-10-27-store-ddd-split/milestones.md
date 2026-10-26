# Seelex `store` DDD 拆分开发里程碑

日期：2026-10-27
状态：**实施计划；尚未编码**
上位方案：[`plan.md`](plan.md)
外部依赖批次协议：[`external-change-protocol.md`](external-change-protocol.md)
开发会话入口：[`next-session-prompt.md`](next-session-prompt.md)

## 1. 里程碑原则

1. 一个里程碑只交付一个可解释的架构能力，禁止把 Board、Layout、Session 和 Facade 清理混在同一批。
2. 每个里程碑都必须可独立编译、测试和回滚。
3. 兼容期只允许“唯一业务实现 + 根包薄转发”，禁止保留两套落到同一份数据的业务实现。
4. 每个里程碑先写红测或结构门禁，再写生产代码；回滚不能靠放宽测试。
5. 不改变 JSON v8 路径、JSON tag、schema version、head 水位、锁粒度和崩溃语义。
6. 回滚使用反向补丁或对里程碑提交执行 `git revert`。
   禁止 `git reset --hard`、`git checkout --`、`git clean`、删除目录或清理用户数据。
7. 回滚代码前保存失败证据：失败测试、命令、日志摘要、受影响的旧面/新面。
8. 如果发现新代码已经写入了旧实现不能读取的数据，立即停止写入并升级处理；
   不得删数据、覆盖配置或把“回滚代码”伪装成“回滚数据”。
9. 任何会修改外部包或外部包所消费导出面的批次，必须先提交外部影响清单和 checkpoint，
   再在专用 branch/worktree 中运行一次性迁移脚本；脚本失败自动恢复，
   验证失败撤回批次提交。

## 2. 里程碑总览

| ID | 里程碑 | 主要交付 | 风险 | 依赖 | 独立回滚点 |
|---|---|---|---|---|---|
| M0 | 基线门禁 | 行为特征测试、方法面 ratchet、import 门禁 | 低 | 无 | 仅回滚测试/文档 |
| M1 | Board 试点 | `contract/dto` Board 形状、`store/board`、消费方窄端口 | 中 | M0 | 回滚 Board 新包与转发 |
| M2 | Layout 内核 | 路径、module head、锁分片、guide | 高 | M0、M1 模式验证 | 回滚 `internal/layout` 与委托 |
| M3 | Artifact | tool result、blob、media、search | 中 | M2 | 回滚 Artifact 包与端口 |
| M4 | Commit 拆分 | transcript/artifact/compaction 提交、Turn 编排 | 高 | M0、M3 | 回滚 Turn 编排与 `Commit` adapter |
| M5 | Session 核心 | message/event/head/draft tail | 高 | M2、M4 | 回滚 Session 核心包 |
| M6 | Session 状态机 | lifecycle/queue/retention/compact/fork | 高 | M5 | 回滚状态机迁移 |
| M7 | Context | prompt、栈、checkpoint、wire 归位 | 中 | M2、M5 | 回滚 Context 包 |
| M8 | Teamwork | plan/event/binding/team registry | 高 | 当前 teamwork 迁移落定、M2 | 回滚 Teamwork 包 |
| M9 | Role / WorkUnit | role session/draft/permission、node record | 中 | M8 | 回滚 Role/WorkUnit 包 |
| M10 | Facade 清零 | 删除 God 面、迁移剩余调用方 | 高 | M1–M9 | 恢复上一版 alias/转发 |
| M11 | 拆分验收 | 全仓门禁、文档、发布候选 | 高 | M0–M10 | 回滚发布候选，不动数据 |

### 2.1 Token、工时与成本预估

口径：

- Token 是跨会话累计消耗，包含源码/文档阅读、工具输出、实现、测试和 review，
  不是最终 context 驻留量。
- 工时是有效执行时间，不含等待用户、审批和后台任务。
- `application` 拆包约 1 天 1 夜、成本约 150 后**仍未完成**。因此 150 是
  “未收敛投入下限”，不是完整拆包的完成成本；原口径未注明币种，本文只使用同一成本单位。
- `store` 涉及持久化发布顺序、锁、崩溃恢复和 22 个直接生产 importer，
  在应用层未完成的基准上追加至少 50% 的风险余量。
- 预估区间约为 ±40%，模型价格、上下文压缩和测试重跑次数会显著影响成本。

| 里程碑 | Token 预估 | 有效工时 | 校准成本 | 主要波动因素 |
|---|---:|---:|---:|---|
| M0 基线门禁 | 100k–200k | 3–6h | 25–55 | Commit 失败注入、fixture 规模 |
| M1 Board 试点 | 120k–230k | 4–7h | 35–65 | contract 身份切换和测试替身 |
| M2 Layout 内核 | 180k–320k | 6–10h | 50–90 | 锁身份、Windows 发布测试 |
| M3 Artifact | 150k–280k | 5–9h | 40–75 | media/blob/GC 覆盖 |
| M4 Commit 拆分 | 220k–400k | 8–14h | 70–125 | 半失败矩阵和重试语义 |
| M5 Session 核心 | 240k–450k | 9–16h | 80–145 | 发布可见性和并发读 |
| M6 Session 状态机 | 300k–600k | 12–22h | 100–200 | queue/LRU/compact/fork 交叉 |
| M7 Context | 200k–380k | 8–14h | 70–125 | 栈恢复、wire 前缀稳定 |
| M8 Teamwork | 200k–400k | 8–14h | 70–125 | 当前契约迁移和绑定账本 |
| M9 Role / WorkUnit | 160k–320k | 6–11h | 50–95 | 子代理恢复、floor/message |
| M10 Facade 清零 | 280k–550k | 12–20h | 100–190 | 22 个外部包同时收敛 |
| M11 拆分验收 | 140k–280k | 5–9h | 45–80 | 全仓测试和跨平台门禁 |
| **总计** | **2.29M–4.41M** | **86–152h** | **600–1100** | 不含超过 40% 的未知返工 |

控制规则：

1. 开工前记录预估；结束时记录可观测实际用量。运行时没有 token 计量时必须写“不可观测”，
   不得编造数字。
2. 实际用量超过预估上限 1.5 倍时暂停并重新拆分；不得靠少跑测试、少做 review 压预算。
3. 工时或成本接近上限时，优先缩小当前里程碑范围，不跨里程碑夹带改动。
4. 不得把“应用层仍未拆完时投入的 150”当成 store 完整拆分的成本承诺。

## 3. 通用完成定义

每个里程碑的 PR/提交必须包含：

- 目标、非目标、领域归属和聚合不变式。
- 新增/修改的 DTO、端口、struct 及所有权。
- 红测首次失败的证据。
- “原位置 → 新唯一位置”的删除/合并清单。
- 新旧依赖边和 God 类型方法数变化。
- 目标模块 README 更新。
- 最小测试命令及结果。
- 明确的数据影响：默认必须是“零路径、零格式、零迁移”。
- 回滚演练结果：至少证明反向补丁可编译，旧实现可读取该里程碑产生的 fixture。
- 若触及外部依赖：影响清单、迁移脚本、checkpoint hash、dry-run 结果、失败恢复证据。
- 最终状态报告必须明确区分“已完成项、正在完成项、未完成项”，并强制给出
  “未决风险、下一步内容”；任何一项为空时也必须写“无”及判定依据。
- Token 预估、可观测实际值或“不可观测”说明，以及成本偏差原因。

### 3.1 外部依赖门禁

以下任一情况触发外部依赖批次协议：

- 修改当前里程碑领域目录之外的生产或测试文件；
- 修改 `contract` 中已被外部包消费的类型或字段；
- 修改根 `store` 的导出符号；
- 需要批量替换 import、类型名、方法签名或构造方式。

执行顺序固定为：

1. 创建专用 `codex/store-ddd-<milestone>` branch/worktree。
2. 在专用 worktree 中扫描并记录外部文件、直接依赖、反向依赖和使用符号。
3. 确保清单内文件没有任务外的未提交改动。
4. 提交 checkpoint commit，保存 hash，不 push。
5. 脚本 dry-run，确认工作区根、branch、目标集合和内容哈希。
6. 脚本一波流应用；失败时自动恢复。
7. 编译和局部测试；成功则提交批次。
8. 全量门禁失败则 `git revert` 批次提交，保留 checkpoint 与 worktree。

详细清单格式、脚本约束和恢复流程以
[`external-change-protocol.md`](external-change-protocol.md) 为唯一事实源。

## 4. M0：基线门禁

### 目标

在拆任何结构前，固定当前行为和结构边界，防止后续把“搬迁错误”误判为“合理重构”。

### 交付

1. message/event/compact/retention/lifecycle/tool-result/media 的物理布局 golden。
2. `Commit` 写序特征测试：artifact → message → compaction。
3. `Commit` 半失败特征测试：记录当前 message 已发布后 compaction 失败时的真实语义。
4. 锁身份测试：同模块同锁、不同模块不同锁、未知模块显式失败。
5. `Router`、`jsonRepository`、`storeEngine` 方法数 ratchet。
6. `./store/...` 全量 I6 import 门禁。
7. 代表性会话 fixture：能从当前版本冷读，供后续每批回归。

### 验收

```text
go test ./store/... ./e2e/... -count=1
go test ./test/ -count=1
git diff --check
```

同时产出当前方法数、直接 import 根 `store` 的生产文件和领域文件清单。

### 回滚

触发条件：

- 特征测试无法表达当前真实行为；
- 半失败语义与现有注释冲突且无法从代码和测试判定；
- 新 ratchet 会阻止合法现有改动。

回滚步骤：

1. 停止后续批次，不修改生产实现。
2. 反向删除错误测试或 ratchet，保留已确认有效的 fixture。
3. 把冲突写成待裁决项，不改产品语义迁就测试。

数据影响：无。M0 不运行写迁移，不清理 `.seelex`、配置或 `dist`。

## 5. M1：Board 垂直试点

### 目标

用最低风险领域验证“contract DTO → 消费方端口 → 新领域包 → 根包薄转发 → 删除旧实现”的完整路径。

### 范围

- `project_board.go`
- `project_board_markdown.go`
- 对应测试
- `middle/view/board` 的 Board 端口
- `contract/dto` 的最小 Board 形状

明确不搬：

- `board.go`：会话看板元数据，后续归 Session。
- `project_known.go`：等待 ProjectRecord 契约归属。

### 交付

1. `ProjectBoardRow`、`ProjectBoardScope`、读选项进 `contract/dto`，JSON tag 不变。
2. `store/board` 成为追加、折叠、压实和 markdown 渲染的唯一实现。
3. 根 `store` 的类型 alias 与 `Router` 方法只做兼容转发。
4. `middle/view/board` 不再以 store 类型定义自己的端口。
5. 删除根包重复实现，并登记删除清单。

### 验收

```text
go test ./store ./store/board ./middle/view/board ./e2e -count=1
go test ./test/ -run ProjectBoard -count=1
```

必须证明：

- 目录白名单、tombstone、Seq、压实和 BOARD.md byte 语义不变；
- 旧 fixture 冷读成功；
- `store/board` 不 import 根 `store`；
- 根包不存在第二份追加/折叠判据。

### 回滚

触发条件：

- Board fixture 字节或折叠结果不一致；
- 消费方仍被迫 import 根 `store` 才能使用 Board；
- 新包出现对 Session/Teamwork 的反向依赖；
- `board.go` 或 `project_known.go` 被误带入。

回滚步骤：

1. 让 Board 消费方重新指向根兼容转发。
2. 反向恢复根 Board 实现。
3. 删除 `store/board` 和新增 Board 契约类型；如已有外部调用，先保留 alias 直到调用点归零。
4. 保留磁盘上的 `worktable.jsonl`、`teamwork.jsonl`、`BOARD.md`；
   这些文件格式未变，禁止删除或迁移。
5. 跑 Board 单测和旧 fixture 冷读。

数据影响：零格式变更。回滚不删除用户项目中的 `.seelex/board`。

## 6. M2：Layout 内核

### 目标

把“会话布局、module head、原子发布和按模块锁”从各领域 receiver 上移到一个无业务判定的内核。

### 交付

1. 新建 `store/internal/layout`。
2. `ModuleID` 由领域拥有；新增模块不再修改中央锁字段集合。
3. guide、head 信封、checksum、共享读、rename 重试由 layout 唯一实现。
4. head 重建策略由领域提供，layout 不按 message/event/compact/stack 写 switch。
5. `storeEngine` 的路径/head/lock 方法改为委托。

### 验收

```text
go test ./store/... -run 'Head|Lock|Atomic|Pending|Guide' -count=1
go test ./store/... ./e2e/... -count=1
```

必须证明：

- 同模块锁身份不变；
- 不同模块仍不共享串行点；
- Windows rename/共享读/自愈重读不变；
- guide 与所有现有 head 路径不变；
- 新增一个测试模块不需要改中央字段表。

### 回滚

触发条件：

- 锁身份变化或出现静默别名；
- 任一 head 自愈路径失效；
- Windows rename/shared-read 回归；
- 模块 ID 进入落盘路径后改变文件位置。

回滚步骤：

1. 恢复 `module_heads.go` 的路径/head/lock 实现。
2. 移除 root 对 `internal/layout` 的委托。
3. 反向删除 `store/internal/layout`。
4. 保留已经写出的 metadata 文件；文件名和 payload 与旧实现相同，旧实现直接冷读验证。
5. 重跑锁身份和 head 自愈用例。

数据影响：零字段、零路径变化。若发现模块 ID 改变了物理路径，视为阻断缺陷，
立即停止写入，不得通过迁移目录修复。

## 7. M3：Artifact

### 目标

把不可变工具结果、blob、media 和检索索引从会话仓储中分离。

### 交付

1. `store/artifact`。
2. `ArtifactRef`/结果引用使用 contract DTO。
3. tool-result、media、blob、search 的 consumer ports 落到 application/backend 消费侧。
4. 根 `jsonRepository` 相关方法改为转调。
5. GC、配额和内容寻址判据只在 Artifact 保留一份。

### 验收

```text
go test ./store/... ./store/artifact/... ./backend/tool/... ./e2e/... -count=1
```

必须证明：

- message 可见前 artifact 已落盘；
- 同字节去重、媒体配额、超大结果回读不变；
- fork 后删除父会话仍能读子会话 artifact；
- Artifact 包不知道 board、teamwork、context 状态机。

### 回滚

触发条件：

- 引用消失、孤儿率异常或 GC 误删；
- media 配额/尺寸判定变化；
- 工具结果在 message 发布前不可读；
- 消费方被迫依赖整个 `jsonRepository`。

回滚步骤：

1. 重新装配旧 artifact 端口。
2. 恢复 `jsonRepository` 的 artifact 方法。
3. 移除新端口注入与 `store/artifact`。
4. **不删除** `meta/`、`big_tool_result/` 或检索索引；
   先用旧实现只读验证。
5. 跑工具结果、媒体、fork 回联用例。

数据影响：零格式变化。回滚不得触发 GC 清理；必要时暂时关闭自动回收，等待人工确认。

## 8. M4：Commit 与 Turn 编排

### 目标

把跨 transcript、Artifact、Compaction 的写序从 `store.Commit` 中提升为应用用例，
同时保留当前失败和幂等语义。

### 交付

1. `SessionCommit`、`ArtifactCommit`、`CompactionRecordCommit`。
2. application 层 `CommitTurn` 编排当前顺序。
3. `store.Commit` 降为 adapter DTO，字段兼容但不承载业务判定。
4. 半失败、重试和孤儿 artifact 行为由 M0 特征测试钉住。

### 验收

```text
go test ./application/core/... ./store/... ./wiring/... -count=1
go test ./test/ -count=1
```

必须证明：

- 相同输入产生与 M0 相同的可见性、错误和 reopen 结果；
- 同 commit 重试幂等语义不变；
- `ProviderHistory`/`State` 的退役兼容行为不变；
- 没有为了拆分而偷偷实现跨通道原子事务。

### 回滚

触发条件：

- 写序或可见性变化；
- compaction 失败后的状态与基线不同；
- 重试产生重复 message/record；
- 旧 `Commit` 调用点无法通过 adapter 保持兼容。

回滚步骤：

1. 恢复旧 `WriteCommit`/`writeCommitLayout` 作为唯一写实现。
2. 移除 `CommitTurn` 注入和新的聚合提交调用。
3. 保留旧 `Commit` adapter，保证现有应用调用点无需同时回滚。
4. 不删除已写入的 compaction/message/artifact 文件，先做冷读对照。
5. 重跑 M0 的半失败矩阵。

数据影响：零格式变化。若半失败矩阵不一致，视为阻断，不允许用数据清理掩盖。

## 9. M5：Session 核心

### 目标

迁出 message 事件行、structural event、message head 和 draft tail，形成 Session 聚合核心。

### 交付

1. `store/session` 的 transcript/event/head/draft-tail 类型和实现。
2. `Router`/`SessionGranularStore` 只转发。
3. `Repository` 中的历史读写改用消费方窄端口。
4. `Event` 继续使用 `contract/protocol.TranscriptEvent` 同身份 alias。

### 验收

```text
go test ./store/... ./backend/engine/session/... ./application/core/... ./test/ -count=1
```

必须证明：

- message head 是唯一发布点；
- 草稿行不可见，非零 publish/rollback 语义不变；
- event anchor、seq、commit_id 不变；
- Windows 并发读/清理行为不变；
- Session 包不依赖 lifecycle 上层或 UI。

### 回滚

触发条件：

- 任何 message 可见点变化；
- draft tail 被误提升；
- event 幂等窗口变化；
- 冷恢复丢行/重排行；
- 根转发出现第二份 message 判据。

回滚步骤：

1. 恢复根 `message_rows`、`structural_events`、`event_range` 的旧实现。
2. 将 Router/Repository 转回旧实现。
3. 删除 Session 核心包的装配。
4. 保留 message/event 分片和 head；旧代码读取同一物理布局。
5. 跑冷读、草稿、发布、并发读测试。

数据影响：零路径和格式变化。回滚前先确认没有未发布的 draft tail 正在运行；
若存在，按现有恢复协议处理，不手工改 head。

## 10. M6：Session 状态机

### 目标

迁移 lifecycle、queue、retention、compaction 和 fork 到 Session 聚合的明确子模块，
但不拆散依赖同一 message 水位的不变式。

### 交付

1. lifecycle/queue 的 draft↔queue 原子迁移归属 Session。
2. retention 只操作 watermark 前缀。
3. compact 帧和 compaction records 的边界校验归属 Session。
4. fork/subagent 副本由 Session 用例调用 Artifact 端口。
5. 各模块继续使用独立锁，不合并串行点。

### 验收

```text
go test ./store/... ./backend/engine/session/... ./backend/engine/assemble/... ./test/ -count=1
```

必须证明：

- 队列 FIFO、失败回草稿、确认出队不变；
- LRU 不越过 watermark；
- compact 边界与 fork 起点校验不变；
- 并发 fork 不共享父 generation；
- 锁热点和模块锁身份通过。

### 回滚

触发条件：

- 队列顺序或失败迁移不一致；
- LRU 误删非 watermark 前缀；
- compact 链断裂；
- fork 复制丢失 artifact/event；
- 需要合并两把领域锁才能通过测试。

回滚步骤：

1. 恢复旧 lifecycle/retention/compact/fork 方法。
2. 拆除 Session 子模块装配，恢复根转发。
3. 保留 draft/queue/retention/compact 文件与 metadata。
4. 使用旧代码冷恢复，不手工写 head 或 watermark。
5. 跑队列、LRU、compact、fork 测试。

数据影响：零格式变化。如果恢复失败，停止写入并保留现场；
禁止清空 `.seelex` 或重写用户会话目录。

## 11. M7：Context

### 目标

把 prompt、栈、checkpoint 和 wire 装配从 Session 事实源中拆开；
`SessionContextRecord` 不再作为跨域万能快照。

### 交付

1. `store/context`。
2. 各栈使用所需 contract DTO/投影，不共享一个万能 record。
3. `SessionContextStore` 拆成按栈消费的读面。
4. `wire_assembler` 迁往 engine context 域或消费方读服务。
5. 冷恢复和 provider 前缀稳定性测试保持。

### 验收

```text
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
```

必须证明：

- Plan/Task/Goal/Skill/Compact 栈操作不变；
- compact 桥接只发生一次；
- provider wire 前缀缓存稳定性不变；
- context 包不依赖 board/teamwork/media 状态。

### 回滚

触发条件：

- 栈恢复顺序或 LIFO 变化；
- compact 双写/漏写；
- provider wire 前缀变化；
- 消费方仍持有 `SessionContextRecord` 才能工作。

回滚步骤：

1. 恢复 `SessionContextStore`、`SessionContextRecord` 与 `wire_assembler` 旧实现。
2. 将 engine context 接线回旧端口。
3. 删除 `store/context` 装配。
4. 保留 metadata system/compact 和 context 相关文件，按旧 reader 冷读。
5. 跑栈、compact、prefix invariant 用例。

数据影响：零格式变化。Context 迁移不得写入第二份 prompt/stack 数据。

## 12. M8：Teamwork

### 前置

当前 teamwork 契约迁移必须已落定并通过测试；未满足时本里程碑不得开工。

### 目标

迁移 team plan、team event、WorkItem binding 和 team registry。

### 交付

1. `store/teamwork`。
2. 计划/审计/绑定账本各自明确的不变式。
3. 消费方使用现有 `contract/dto/teamwork_plan.go` 与窄端口。
4. 跨域整队收口留在 application/teamwork 用例，不进入 store 根包。

### 验收

```text
go test ./store/... ./store/teamwork/... ./backend/multiagent/teamwork/... ./test/ -count=1
```

必须证明：

- plan 整份替换、event 只追加、binding 最后一行折叠不变；
- WorkItem 校验和依赖环检测不变；
- 看板归档仍是派生投影；
- teamwork 包不 import board 的投影实现。

### 回滚

触发条件：

- plan/audit/binding 形状或折叠变化；
- 收口状态机出现第二判据；
- 看板投影反向成为事实源；
- 当前契约 alias 漂移。

回滚步骤：

1. 恢复旧 teamwork 写读方法。
2. 将消费方端口接回根兼容转发。
3. 删除 `store/teamwork` 装配。
4. 保留 plan/event/worktrees 文件，旧实现直接冷读。
5. 跑 teamwork、board archive、item gate 用例。

数据影响：零格式变化。禁止删除 `worktrees.jsonl` 或 team plan 文件。

## 13. M9：Role / WorkUnit

### 目标

迁移角色会话、draft、权限、node/subagent 记录，消除 `roleStore` 和 `newSubagent`
在根引擎上的递归式上帝方法。

### 交付

1. `store/role` 与 `store/workunit`。
2. Role sync 继续由 sequencer 唯一写 main message。
3. WorkUnit 记录继续按主会话索引。
4. 两个包只依赖 layout、contract 和各自需要的窄端口。

### 验收

```text
go test ./store/... ./store/role/... ./store/workunit/... ./backend/multiagent/... ./test/ -count=1
```

必须证明：

- role draft 成功同步后才删除；
- floor 与 message 同步发布；
- subagent 终态结论跟随主会话；
- 重启恢复和主会话索引不变；
- 不再通过根 `storeEngine` 创建同型子引擎。

### 回滚

触发条件：

- draft 提前删除或重复同步；
- floor/message 不一致；
- subagent 记录不可枚举；
- Role/WorkUnit 反向依赖 Teamwork 实现。

回滚步骤：

1. 恢复角色/子代理旧方法。
2. 重新使用根兼容转发和旧的会话子树创建方式。
3. 删除 Role/WorkUnit 新包装配。
4. 保留 draft、角色会话、subagent 文件与主会话事件；不执行清理。
5. 跑 role sync、subagent recovery、unit record 用例。

数据影响：零格式变化。回滚不得删除已同步的 role draft 或 subagent 历史。

## 14. M10：Facade 清零

### 目标

让根 `store` 不再承载业务实现，删除 `Router`、`Repository`、
`SessionGranularStore` 的泛化能力出口。

### 交付

1. 所有生产调用方只持消费方窄端口。
2. `BoardsFor`、`TeamworkFor`、`ToolRecordFor` 等能力发现出口删除。
3. 根包 God 类型方法只留下组合根 lifecycle 或彻底删除。
4. 上层直接 import 根 `store` 的生产文件归零，或只剩明确兼容面。
5. 更新架构图、模块 README、import ratchet。

### 验收

```text
go build ./...
go test ./application/... ./middle/... ./backend/... ./store/... ./wiring/... ./e2e/... -count=1
```

必须证明：

- 无循环依赖；
- 无第二份领域判定；
- 方法面 ratchet 达到目标；
- wiring 只负责构造，不承载业务顺序。

### 回滚

触发条件：

- 仍有无法用消费方端口表达的合法用例；
- facade 删除后 wiring 出现业务判定；
- 多处调用点被迫依赖具体实现；
- import 图中出现领域包反向依赖。

回滚步骤：

1. 恢复上一里程碑保留的 alias 与 Router 薄转发。
2. 不改领域包内部实现，只恢复装配出口。
3. 保留所有数据文件与格式。
4. 重新跑全仓构建和调用方契约测试。

数据影响：无。Facade 回滚只涉及代码和装配。

## 15. M11：拆分验收

### 目标

以发布候选标准验证拆分没有改变行为、数据兼容和关闭路径。

### 验收

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=1200s
go test ./test/ -count=1
node --test gui/frontend/dist/*.test.mjs
```

额外验证：

- 旧会话冷读、恢复、fork、压缩和媒体回读；
- 多进程数据根锁；
- 正常关闭时无 goroutine、数据库、MCP、Wails 或文件句柄泄漏；
- 结构指标：God 方法数、直接根 import 数、领域包依赖边。

### 回滚

触发条件：

- 任一发布门禁红；
- 数据根锁或关闭路径回归；
- 跨平台构建失败；
- 性能回归超过既定预算。

回滚步骤：

1. 撤回发布候选，不发布新二进制。
2. 按失败里程碑执行对应回滚，不跨里程碑“大回退”。
3. 保留用户数据与现场日志；不运行 `make clean/release` 或删除 `dist`。
4. 修复后从失败里程碑的门禁重新开始，不跳过前序验收。

## 16. 建议提交边界

| 提交 | 内容 |
|---|---|
| C0 | 基线测试、ratchet、import 门禁 |
| C1 | Board contract + `store/board` + consumer port |
| C2 | `internal/layout` + 根委托 |
| C3 | Artifact 包 + 端口 |
| C4 | Commit 拆分 + Turn 编排 |
| C5 | Session transcript/head/draft-tail |
| C6 | Session lifecycle/retention/compact/fork |
| C7 | Context + wire 归位 |
| C8 | Teamwork |
| C9 | Role / WorkUnit |
| C10 | Facade 删除 |
| C11 | 全仓验收与文档收口 |

每个提交只包含该里程碑的可解释变化。若一个提交同时修改 God 类型、数据格式和 UI，
则不满足本里程碑拆分要求。

## 17. 回滚演练要求

每个里程碑合并前至少完成一次回滚演练：

1. 在临时工作树或独立分支生成该里程碑提交。
2. 用旧实现冷读该里程碑生成的 fixture。
3. 反向应用该提交并重新编译。
4. 运行里程碑对应的最小测试集。
5. 记录“可回滚 / 不可回滚”及原因。

不可回滚的情况必须阻止合并，典型包括：

- 新实现改变了物理路径或 schema；
- 新数据无法由旧 reader 读取；
- 回滚必须删除 `.seelex`、配置或 `dist` 内容；
- 回滚必须保留第二套业务实现；
- 回滚需要手工修改 head/watermark。

## 18. 决策门

编码前需要确认：

1. 以 M0 → M11 作为固定顺序。
2. 每个里程碑独立验收和回滚，不合并成一次大提交。
3. 回滚默认是反向补丁或 `git revert`，不采用运行时双实现开关。
4. 出现格式兼容风险时停止并升级，不通过清理数据恢复。
