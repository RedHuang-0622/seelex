# 子代理详情查看：会话记录详设

> 状态：设计
> 日期：2026-08-03
> 目标：点击子代理节点即可查看该节点子代理**当前的具体工作情况**——
> 表现为子窗的会话记录（子代理的 user/assistant/工具调用/工具结果流）
> 以及能体现会话记录的活动轨迹（遥测 llm/tool 事件）。

## 1. 现状与缺口

已实现（alpha.3）：节点详情弹窗 = 事件时间线（queued/running/completed +
心跳）+ 状态/耗时/输出——**只有 lifecycle 状态，没有子代理内部对话**。

缺口：
- 子代理的会话记录（它看了什么、想了什么、调了哪些工具、结果如何）
  不可见——用户无法判断"子代理到底在干什么"。
- 子代理会话不落盘（`nodeSessionComponents` 无 History owner，工作历史
  默认隔离）——运行时才有完整对话。

## 2. 数据源（Actor 语义下全部安全）

| 数据 | 来源 | 读取安全性 |
|---|---|---|
| **会话记录**（user/assistant/tool 消息流） | 子代理 `*session.Session.History()` | ✅ **子代理 actor 自己的锁**——与主会话锁无关，plan_run 期间读取安全（死锁教训只针对主会话） |
| **活动轨迹**（llm 调用/tool 调用事件） | 共享遥测 `MemoryTracer.Query`（子代理与主代理共用 r.hook） | ✅ 独立锁 |
| **事件时间线**（节点 lifecycle） | 已有 `PlanNode.Events` | ✅ |
| 状态/耗时/输出 | 已有 PlanNode | ✅ |

关键洞察：**子代理会话是独立 actor**——运行中读它的 History() 不触碰主
会话锁，这是详情查看可行的根本（主会话详情则永远不可行，ChatStream 持锁）。

## 3. 数据面设计

### 3.1 节点会话注册表（运行时）

```
Runtime 内部：nodeSessions map[string]*session.Session（nodeID → 子代理会话）
  - SeelexAgentNode.Run 开始时注册（n.ID() → 子会话）
  - Run 结束（defer）注销 + 保留最后一份快照（lastNodeConversations[nodeID]）
  - 受 runtime 自身锁保护（与 exchanger 同构：短暂持有）

读取接口（只读，跨 actor 安全）：
  func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool)
    ├─ 运行中：从注册表读子会话 History()（子代理锁，安全）
    └─ 已结束：从 lastNodeConversations 快照读（plan_run 期间的最后状态）
```

### 3.2 会话记录适配（对话流）

```
子代理 History() → 详情对话流：
  - system/节点目标  → 折叠为 "目标" 行
  - user             → 用户输入（节点请求/后续轮次）
  - assistant(含工具调用) → 回复 + 工具调用声明
  - tool             → 工具结果（截断 ≤ evidence_chars）
  - 流式：运行中读取即最新（History 是已完成的轮次；流式中间态由
    SubagentActivity 事件补充）
```

### 3.3 活动轨迹（实时补强）

- 遥测事件投影：llm 调用（model/tokens）、工具调用（name/args/result）
  按子代理 sessionID 过滤（nodeSessionID 稳定 hash 可关联）→
  `SubagentActivity` 列表，随详情返回。
- 运行中的实时增量：详情弹窗打开期间轮询（前端每 2s 拉取详情接口），
  展示"正在工作"的动态（当前工具/最近输出）。

### 3.4 Bridge/应用接口

```
application.Service:
  SubagentSessionDetail(nodeID) (SubagentDetail, error)
    ├─ Conversation []Message        // 会话记录（子代理 history 适配）
    ├─ Activity    []SubagentActivity // 遥测轨迹（llm/tool）
    ├─ Status / Elapsed / Output     // 复用已有 PlanNode
    └─ Running     bool              // 是否执行中（实时 vs 快照）

GUI Bridge: invoke("SubagentSessionDetail", nodeID)
```

## 4. 前端交互

### 4.1 入口与呈现

- 点击节点卡片（整卡可点，不只是 `…` 按钮）→ 详情弹窗
- 弹窗**默认显示"会话记录"标签**（子代理对话流），其他标签：事件时间线 /
  输出 / 元信息
- 会话记录呈现（子窗风格，类聊天记录）：
  ```
  [目标]  audit the module and return findings
  [user]  执行节点 inspect
  [assistant] 我先读取 controller.go …   （流式时逐行出现）
  [tool]  read_file controller.go:365 …（截断）
  [assistant] 结论：FOUND
  ```
- 运行中：顶部状态条（RUNNING + 最近活动）；每 2s 轮询刷新

### 4.2 状态语义

| 节点状态 | 会话记录来源 | 提示 |
|---|---|---|
| running | 注册表实时 History + Activity | "执行中，2s 刷新" |
| completed/failed | lastNodeConversations 快照 | "执行结束，最后快照" |
| pending（未执行） | 无 | "节点未执行" |

## 5. 持久化（第二阶段，可选）

- `nodeSessionComponents` 加 History owner（sessionstore，key 按
  nodeSessionID）→ 子代理对话落盘
- 重进会话后节点详情可看历史子代理对话（跨会话）
- 第一阶段不做（保持工作历史隔离语义不变）

## 6. 安全与边界（Actor 语义）

- **只读子代理**：`NodeSessionConversation` 只读子代理会话（自己的锁）；
  **绝不**读取主会话（ChatStream 持锁，死锁教训）。
- 快照有界：lastNodeConversations 只保留最近一次 plan_run 的节点
  （nodeID 覆盖写），内存有界。
- 详情返回经 application 适配（截断：单条工具结果 ≤ evidence_chars、
  总对话 ≤ 50 条/节点——limits 配置）。

## 7. 子代理树（fork 内存态可视化，2026-08-07）

在会话注册表之上新增**子代理树**数据面（seelebridge/subagent_tree.go）：

```
fork_subagents 调用 → registerFork(parentID, specs)
  ├─ parentID = NodeScope.NodeID（子代理嵌套 fork）或 "main"（主代理合成根）
  └─ 每个子代理一条记录：{id, parentID, goal, status, sessionID, summary, …}

状态写入：
  registerNodeSession → noteSession（挂载运行中子会话，status=running）
  SeelexAgentNode.Run 结束 → completeSubagentNode(summary, err)
    ├─ err == nil → done + 会话摘要（最终输出）
    └─ err != nil → failed + 错误
  unregisterNodeSession → noteSnapshot（结束快照挂到树节点）

读取：Runtime.SubAgentTree() []SubAgentTreeNode
  - 每次调用重建只读投影（深拷贝语义），根 = "main" 合成节点
  - 节点携带紧凑 ContextSnapshot（结束节点复用导出快照零额外成本；
    运行中节点实时导出，单条截断 120 字符、findings ≤ 3）
  - 孤儿节点（父已不在注册表）归到主代理下，树保持完整
  - 纯内存态，明确不落盘（与 nodeSessions 同构）

事件路径（权威 Snapshot 增量）：
  plan 节点事件（HandlePlanNodeComplete / HandlePlanBranchEvent）在锁内
  重新投影 service.snapshot.Runtime.SubAgentTree = Engine.SubAgentTree()；
  collectRuntimeProjection 同样携带——GUI 经 subagent.changed / runtime.changed
  增量收到整棵树，无需独立轮询。
```

## 8. 实施步骤

## 8. 一句话

**点击子代理节点 → 子窗显示该节点的会话记录**（子代理自己的对话流 +
遥测活动轨迹）：数据全部来自子代理 actor（独立锁，安全），运行中实时、
结束后快照；对话不落盘（第二阶段再持久化）。
