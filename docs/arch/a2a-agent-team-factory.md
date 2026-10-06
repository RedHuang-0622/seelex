# A2A AgentTeam 与角色工厂（设计基线）

> 状态：目标设计，本文定义长期边界。当前实现已具备角色会话存储、role draft、
> floor、compact_ref/join_seq、headless `role.*` 与 goal TL 的 DS-A2A 原型；
> 通用工厂、角色注册表和前端装配面尚未完成。
>
> **2026-10-01 变更**：本文提到的"内置形态 / preset"（`agentteam/presets.go` 的
> `goal-a2a` / `review-team` / `research-team`）**已删除**——团队只来自数据（团队库
> 条目或调用方给的一份 `TeamSpec`），不再有代码里的模板目录，也没有"goal 上线自动
> 装配团队"这条隐式路径（它会整份替换掉会话已有的团队）。见
> [`../devlog/2026-10-01-no-builtin-team-shapes.md`](../devlog/2026-10-01-no-builtin-team-shapes.md)。

## 1. 一句话边界

- `subagent` 是外包执行资源，不是 AgentTeam 成员，不参与群聊排序、floor、role draft
  或 sequencer。
- `AgentTeam` 是 A2A 角色团队的运行时实例；每个成员是一个真正的角色会话。
- `goal` 的 TL 只是第一个 `AgentTeam` 实例。TL 的“独立会话 + 事件镜像 + 结构化
  指令回注 + 缺席降级”必须被抽象成通用 A2A 工厂能力，而不是继续堆 TL 专属分支。

## 2. 角色模型

### 2.1 RoleSpec

角色注册与前端角色管理的最小单位：

```text
RoleSpec
  role_name              # main/tl/reviewer/planner/domain-expert...
  role_kind              # exec | peer | member | scheduled
  system_prompt          # 角色固定前缀 P_x，恒定，不内联可变内容
  model/account_policy   # 模型或账号路由（后续）
  mirror_policy          # 哪些事件进镜像、评估窗口、触发信号
  directive_schema       # 允许产出的结构化载荷
  order_priority         # 团队顺序中的默认位置
  join_policy            # 内置 / 按 goal 创建 / 按需创建
  presence_policy        # online/offline 的可见语义
  tools_policy           # 只读、无工具或受限工具面
```

`role_name` 是 Seelex 逻辑身份，不是 provider role。provider 请求只允许标准 role：
`system/user/assistant/tool/latest_reminder`（真实端点实验已确认自定义 `tl` 会被拒绝）。

provider role 的分配口径（唯一判据是"这条内容对人类是不是用户输入"）：

| 内容 | provider role | `role_name`（metadata） |
|---|---|---|
| 用户真实输入 | `user` | `user` |
| task/goal/plan/subagent 的 active/状态材料 | `system` | 产出该材料的角色（如 `system`/`main`/`tl`） |
| 中断恢复说明、恢复注入的上下文 | `system` | `system` |
| 角色会话的真实模型发言 | `assistant` | 该角色 `role_name` |
| 工具调用结果（含中断占位） | `tool` | 调起方 `role_name` |

即：**只有用户输入是 `user`**；task/goal/plan/subagent 这类编排态材料一律 `system`，
靠 `role_name`/`role_session_id` 元数据区分归属，不靠 provider role 编码身份。

### 2.2 TeamSpec

```text
TeamSpec
  team_id
  team_kind              # goal-a2a | review-team | research-team | custom
  order_policy           # goal_loop | user_main_decided | scheduled_only
  order_roles            # 有序逻辑角色名
  roles[]RoleSpec
  gate_policy            # 终态 gate / 缺席矩阵 / 人工升级
  compact_policy         # main 为 compact 唯一权威
```

当前 `goal_loop` 是第一个固定实现：

```text
user -> main -> tl -> main -> tl -> ... 直到 TL verdict_done/escalate_human/终态 gate
```

后续 `user_main_decided` 只替换 sequencer 的“role 顺序函数”，不改变 message、
draft、compact 或恢复形态。

## 3. 运行时组件

通用组件放在 `application/core` 的 A2A/peersession 能力面，goal 域只提供 goal
适配器。

| 组件 | 职责 | goal 适配 |
|---|---|---|
| `AgentTeamFactory` | 由 `TeamSpec`/`RoleSpec` 创建角色会话、actor、draft 锁、presence | 构造 goal TL team |
| `Mirror` | 从 main 事件账本按策略抽帧，只在 evaluate 前事务式同步 | goal.start/goal.update/tool.checkpoint/context.compacted/terminal.proposed |
| `RoleDraft` | 每角色独立 append-only 未同步 WAL；sync 后删除 | TL 正文/裁决草稿 |
| `Sequencer` | 唯一 message 写者；round → role 顺序 → unit_seq；分配全局 seq、发布 floor | goal_loop 顺序函数 |
| `DirectiveBus` | 角色→main 的结构化建议信封；corr 幂等、受控注入 | TLDirective |
| `GateBridge` | 终态/审批类裁决；缺席矩阵；a 永不等待 b | goal finish gate / prescreen |
| `CacheReporter` | 记录角色回合的 input/cached tokens、前缀命中 | TL round cache stats |
| `PeerEvents` | role online/offline、floor、round、verdict、absence 等事件 | ADVISOR/TL 面板 |

## 4. 生命周期

```text
TeamSpec
  -> AgentTeamFactory.Create
  -> RoleSession spawn（role_session_id + system prompt P_x）
  -> bind main：写入 join_seq_id、order_roles、compact_ref 初始位点
  -> Mirror.BeforeEval：按触发信号同步增量帧
  -> role draft -> sequencer sync -> main message + floor
  -> DirectiveBus：结构化建议/裁决回注 main 受信区
  -> terminal gate：done/abort -> archive/reap（TL 备份保留）
```

恢复语义：

1. 角色会话自身备份（message/event） + 未同步 draft WAL；
2. `join_seq_id` + `compact_ref` 决定可见起点；
3. main compact 是唯一帧权威，角色只引用，不生成第二份帧；
4. 恢复失败必须显式报错，不伪造帧、不回退全局 RawHistory。

## 5. Subagent = tool calling 能力，不属于 AgentTeam

subagent 是劳务派遣式的能力调用，**不是 AgentTeam 成员**（不参与群聊排序、floor、
role draft、sequencer、成员表），生命周期语义等同于一个 tool call：

- 派发 = assistant 发出 tool_call（`fork_subagents`/节点工具等）；
- 运行中 = 工具正在执行，**只活跃在 task/worktable/子代理表格**；运行中的
  active 状态不写进 main 的 provider 历史；
- 完成 = 结果以 tool result 回流 main；
- 具备管理权的是调起方（main），subagent 不持有 AgentTeam 身份。

冷恢复严格复用 interrupted tool-chain 语义：

1. 保留原 assistant tool_call 行，不改写、不删除；
2. main 侧缺失的 subagent 结果按 tool result 占位：
   `[Seelex recovery note: interrupted tool call "..."]`，明确“可能未执行、
   结果已丢失，需 verify 或重新发起”——provider-only，UI 不渲染为真实工具输出；
3. 若表格里仍存在未完成的 subagent（`active` 且无结论事件）：重建该 subagent
   的执行现场与输入上下文，用 `system` 注入恢复说明（"这是恢复，之前做到哪、
   已完成什么、接下来继续什么"），**再重新派发继续跑**，而不是只留占位符；
4. main 侧不把 subagent 的 active/恢复说明写成 `assistant` 发言；恢复说明按
   §2.1 的表走 `system`。

这条路径与现有 `application/core/context_runtime/history.go` 的
`RepairInterruptedToolChains` 一致：tool_call 与 tool result 必须成对，
缺失结果用 provider-only tool 占位补齐，UI 不把它渲染成真实工具输出。

### 5.1 通用恢复模板：终止前未完成的工作

subagent 恢复不是特例，而是一份可复用模板；只要满足"派发已发生、结果未记录、
恢复后要接着做"，一律套用同一条流水线：

```text
1 定位       未完成单元 = 已发出的派发记录 + 缺失的结果记录（幂等键 = 派发 ID）
2 判定       active/未终结 → 需要重启；已终结（done/failed）→ 只补历史，不重启
3 补历史     main/父侧：缺失结果 → provider-only tool 占位（role=tool），成对合法
4 重建现场   恢复被执行单元自己的输入上下文（它自己的会话/证据/输入参数）
5 注入说明   system：说明这是中断后的恢复、已完成到哪、接下来继续什么
6 重新执行   同一幂等键重启执行；结果回流按正常路径写回
7 收敛       完成后删除临时现场，结论跟随被恢复单元的父级持久化
```

不变量：重启必须幂等（同一派发 ID 不得产生两条结果）；恢复说明走 `system` 且
只进被执行单元自己的上下文；父级历史里只出现占位 result 与最终结果。

## 6. 与酒馆（SillyTavern）对齐的角色管理

SillyTavern 的群聊实现给出的可复用结论：

- 角色身份在消息 metadata（`name`/`original_avatar`），provider role 仍为标准集；
- 群成员发言是 `assistant`，用户输入是 `user`，`is_system` 多用于 UI/旁白且默认
  不进入模型上下文；
- 状态/角色说明走 depth prompt，默认 `role=system`、`depth=4`；
- 发言顺序每次由持久化聊天记录 + 群配置重算，队列/active 仅是 UI 运行态；
- 没有持久化的“当前 active 角色”；恢复后从聊天记录和群配置重建。

Seelex 采用同样边界：AgentTeam 的 active/floor/presence 是运行态或 head 水位，
不是往 main 里塞系统说明；需要恢复时再从持久化事实重建。

## 7. 前端与后端装配面（后续）

后端角色管理：

- `RoleRegistry`：RoleSpec CRUD、模型/账号策略、mirror/directive schema；
- `TeamRegistry`：TeamSpec CRUD、order_policy/order_roles、gate/compact policy；
- `AgentTeamFactory`：按 TeamSpec 创建/归档实际角色会话；
- 角色配置只写注册表/lifecycle 字段，不直接改 message。

前端右侧栏：

- 成员名单：role_name、role_session_id、online/offline、floor 高亮；
- 工作顺序：`order_roles`（阶段二 user+main 可编排）；
- 定时 agent：单独分区，不入 `order_roles`；
- 角色配置：system prompt、模型/账号、工具面、mirror 策略、directive schema；
- subagent：只出现在执行/worktable 分区，不出现在 AgentTeam 成员表。

### 7.1 已落地的管理面（2026-09 实现复核）

上文是目标口径，这一节写**当前代码事实**（别把"登记了"读成"生效了"）：

- **存储分成三份事实，各有唯一写者**：

  | 事实 | 落点 | 作用域 |
  |---|---|---|
  | 会话在编员工（含提示词/权限） | `session/team/roles.json`（整份原子替换） | 会话（母本副本） |
  | 发言顺序 | `metadata/lifecycle.json` 的 `order_policy`/`order_roles` | 会话（母本副本） |
  | 团队模板库 | `<root>/team/library.json`（整份原子替换） | **全局** |
  | 员工库（可复用员工名册） | `<root>/team/employees.json`（整份原子替换） | **全局** |
  | 默认顺序 | `<root>/team/order.json`（整份原子替换） | **全局** |

  全局母本（团队库 + 员工库 + 默认顺序）落在数据根 `<root>/team/`，不随项目/会话
  分目录。**并发口径**：母本的写只有两条路径——库管理动作（直接写全局）与
  「确认·普及搭配到全局」（把会话副本的 {员工, 顺序} 回写母本）；会话内的入职/改序
  只写会话副本（registry + lifecycle head），读也在会话侧按深拷贝副本进行，因此
  并发会话不争全局写锁。旧的项目级团队库（`project-<hash>/teams/library.json`）
  只做只读回退：不搬数据、不删旧文件，回退内容在下一次整份写入里自然并入全局。

  团队库条目的装配 = `entry → TeamSpec → Factory.Materialize`（建角色会话 + 写
  `roles.json` + 写 `lifecycle`），因此"新建团队"与"员工入职"复用同一套落盘通道，
  不产生第二份顺序或角色事实。

- **提示词装配已生效（ADVISOR/TL 路径）**：`RoleSpec.SystemPrompt` 经装配根注入的
  读面进入 ADVISOR 回合；登记了就用登记的，未登记用内置角色设定，**输出契约永远
  追加**（goal 域要解析 `TLDirective`）。其余角色的提示词目前只是登记事实
  （没有自动回合就不会自己发言；真回合要 leader 派活）。

- **权限是登记 + 展示**：`tools_policy` 随注册表落盘并在管理面回读，运行时按角色
  的工具拦截**尚未接线**（真正的拦截在 seelebridge `PermissionGate`，按会话/全局）。
  这条边界必须保持在 UI 与文档里，否则用户会以为改完就生效。

- **提示词优化是一次有界 LLM 回合**：只产出候选文本 + 改动理由，不写会话消息、
  不落盘；落盘仍由用户在"入职/保存"里确认。

- **前端交互口径**：员工栏只写"谁在编"（`✕` 摘除 / 删除 / `+ 入职`）——**次序不由人
  编排**（2026-10-01：行首 `≡` 拖拽调序、位置列与 `↑/↓` 已随"撤掉全部人工编排"删除；
  次序 = 登记先后，目标态归 leader 的 team plan 的 `stages[].depends_on`）；
  入职/修改员工、新建/编辑团队都是冷加载面板（点 + 或编辑才渲染，`✕`/Esc 关闭）；
  团队用一张小表管理（**没有内置形态**：形态目录已于 2026-10-01 删除，团队只来自
  团队库条目）；「全局母本」块列出员工库 + 默认顺序 +
  会话副本差异与「确认·普及搭配到全局」；对话区按 `role_name` 给 EXEC / ADVISOR
  不同的背景分区（AT11 的可视化落点）。


## 8. Goal TL 的泛化迁移

当前 `application/core/goal` 的 `Supervisor`/`AdvisorSession` 是可运行的第一实例；
迁移顺序：

1. 把 `PeerState`、`Frame`、`Round`、`Mailbox`、状态机与缓存观测提取为通用
   peer-session 类型；
2. goal 只保留 `GoalFrame`、`TLSessionEmbed`、`TLDirective`、goal gate 的适配；
3. `AgentTeamFactory` 用 `TeamSpec` 实例化 TL，行为与当前 DS-A2A 测试保持一致；
4. 新增第二个团队实例（如 reviewer 或 research-team）验证同一工厂；
5. 最后开放 RoleRegistry/TeamRegistry 到前端与设置面。

## 9. 不变量

```text
AT1  subagent 永不进入 AgentTeam 成员表/order_roles/floor/draft/sequencer
AT2  逻辑 role_name 只做 metadata；provider role 只能是标准集
AT3  AgentTeam 每个成员 = 真正角色会话，有 role_session_id/join_seq_id/compact_ref
AT4  只有 sequencer 写 main message；角色只写自己的 draft
AT5  sync 成功即删 draft；半同步按 commit_id 幂等
AT6  main compact 是唯一帧权威；角色只引用
AT7  运行期 active/queue/presence 不注入 main 历史；subagent 冷恢复按 interrupted
     tool-chain 语义补 provider-only tool result 占位（role=tool），不写成
     assistant 发言
AT8  同一 AgentTeamFactory 必须能实例化 goal TL 之外的第二个团队
AT9  只有用户输入是 user role；task/goal/plan/subagent 状态材料与恢复说明一律
     system，身份靠 role_name/role_session_id 元数据表达
AT10 中断未完成的工作统一走 §5.1 恢复模板：幂等键定位 → 补占位 → 重建现场 →
     system 注入恢复说明 → 同键重跑 → 收敛
AT11 可见会话消息（前端 snapshot.conversation）必须携带 role_name/role_session_id/
     round_id：聊天区据 role_name 渲染 EXEC（main）/ADVISOR（tl）、据 round_id
     渲染轮次徽标；缺这段归属时前端只能回退 AGENT，两个 teammate 不可辨。
     ADVISOR 的回合原文按 assistant 行 + role_name=tl 发布（不是 system 行），
     实时流式、工具轮续写正文、冷恢复重建三条路径同口径
AT12 团队库（`<root>/team/library.json`）、员工库（`<root>/team/employees.json`）与
     默认顺序（`<root>/team/order.json`）是**全局**母本；会话在编员工表
     （`session/team/roles.json`）+ lifecycle 顺序是它的**深拷贝副本**。装配只能从
     库条目派生 TeamSpec 再走 Factory，不允许把库条目直接当成员表读，也不允许在库
     读路径上隐式落盘
AT13 角色提示词（`RoleSpec.SystemPrompt`）属用户登记事实，只能经装配注入的读面
     进入角色回合；回合的输出契约由宿主固定追加，员工提示词改不掉它；权限字段
     （`tools_policy`）在接线运行时拦截之前只能作登记与展示，UI/文档必须如实标注
AT14 母本的写只有两条路径：库管理动作（直接写全局）与「确认·普及搭配到全局」
     （会话副本 → 母本）；会话内的入职/改序只改会话副本，绝不动母本。旧的项目级
     团队库只做**只读回退**：不搬数据、不删旧文件，回退内容在下一次整份写入里自然
     并入全局
```

## 10. 参考

- [`ds-a2a-protocol.md`](../2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md)
- [`ds-a2a-detailed-design.md`](../2026-09-07-seele-a2a-framework-req/ds-a2a-detailed-design.md)
- [`docs/2026-09-10-backend-cache-goal-session/app-layer-wiring-prompt.md`](../2026-09-10-backend-cache-goal-session/app-layer-wiring-prompt.md)
- SillyTavern group chat: `public/scripts/group-chats.js`、`public/script.js`、`src/endpoints/groups.js`
