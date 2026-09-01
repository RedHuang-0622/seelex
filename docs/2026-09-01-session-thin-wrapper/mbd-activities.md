# 会话薄封装活动图（Activity Diagrams）

> 配套 [mbd-overview.md](./mbd-overview.md)（阶段/门禁）、[mbd-us.md](./mbd-us.md)（用例）、
>       [test-cases.md](./test-cases.md)（四维度测试）

## 图例（UML 活动记号）

- 圆角矩形：动作；菱形：判定；实心条：fork/join；横向泳道：参与者；
- 每个图下方标注「对应测试」：以 F/B/P/R 四维度命名（见 test-cases.md）。

## AD-0 MBD 开发流程（阶段 + 门禁）

```mermaid
flowchart TB
    M[模型定稿<br/>M1-M5 + US] --> R1[写红测试<br/>B1/B2/R2…]
    R1 --> I1[9.1 契约骨架]
    I1 --> G1{门禁绿?<br/>全量+vet}
    G1 -- 否 --> R1
    G1 -- 是 --> R2[9.2 红测试<br/>T1.*/T3.7/P3]
    R2 --> I2[9.2 runChat 委托]
    I2 --> G2{门禁绿?<br/>事件指纹+node}
    G2 -- 否 --> R2
    G2 -- 是 --> I3[9.3 存储会话粒度]
    I3 --> I4[9.4 subagent 同构 + fork]
    I4 --> I5[9.5 清理]
    I5 --> G3{全量 -race<br/>+ 冒烟}
```

对应测试：R1/R2 全套、阶段门禁（§10）。

## AD-1 提交与执行（submit → 排队 → loop → 结果 → 持久化）

```mermaid
flowchart TB
    subgraph GUI["GUI"]
        G1[提交文本]
        G2[渲染 ΔV]
    end
    subgraph APP["application facade"]
        A1[路由到会话 i（显式 sid）]
        A2{会话运行中?}
    end
    subgraph SES["session/ 薄封装"]
        S1[入队 Q_i]
        S2[ChatStreamFor]
    end
    subgraph SEELE["Seele loop"]
        L1[执行 ReAct 循环]
        L2[llm/tool 边界]
    end
    subgraph HOOK["hooks / telemetry"]
        H1[投影 ΔV_i]
        H2[写 trace T_i]
    end
    subgraph ST["sessionstore"]
        P1[会话粒度 persist]
    end
    G1 --> A1 --> A2
    A2 -- 是 --> S1 --> G2
    A2 -- 否 --> S2 --> L1 --> L2
    L2 --> H1 --> G2
    L2 --> H2
    L1 --> P1
```

对应测试：UC1（F）、T1.3（F）、T2.8 队列上限（B）、P5 事件指纹（P）、R4 后台不取全局锁（R）。

## AD-2 会话生命周期（冷加载 / 热挂载 / 卸载）

```mermaid
flowchart TB
    E[恢复/创建会话 i] --> D{HasSession i?}
    D -- 假 --> C1[NewMainSessionWithID]
    C1 --> C2[seed 存储记录<br/>history/transcript/context]
    C2 --> C3[V := i]
    D -- 真 --> H1[attach 视图指针 V := i]
    H1 --> H2[订阅 i 事件流]
    C3 --> H2
    H2 --> R[前端基线 resync + 增量]
    R --> U{卸载?}
    U -- 是 --> U1[persist 会话粒度]
    U1 --> U2[UnloadSession 释放 bundle]
```

对应测试：T2.3 状态机（B）、T4.4 冷加载（F/B）、T4.1/T4.2 热挂载零写入（B）。

## AD-3 视图切换（仅移 V）

```mermaid
flowchart TB
    U[用户切到会话 j] --> D{HasSession j?}
    D -- 真 --> A[hot attach: V := j]
    D -- 假 --> L[cold load 重建 bundle]
    L --> A
    A --> SUB[订阅 j 事件]
    SUB --> BASE[前端基线 resync]
    BASE --> INC[跟随 j 增量]
    INC --> DONE[结束]
```

对应测试：T4.1–T4.3（B/F）、T4.5 切换竞态（B）、T4.6 死锁（R）。

## AD-4 subagent 会话并行执行与 merge-back

```mermaid
flowchart TB
    P[plan_run] --> FR[fork 并行]
    FR --> N1[节点 A<br/>SubagentSession]
    FR --> N2[节点 B<br/>SubagentSession]
    FR --> N3[节点 C<br/>SubagentSession]
    N1 --> L1[独立 Seele loop]
    N2 --> L2[独立 Seele loop]
    N3 --> L3[独立 Seele loop]
    L1 --> M[merge-back 主会话]
    L2 --> M
    L3 --> M
    M --> T[节点 trace/结果 → 轨迹视图]
    M --> S[task 同步到主会话 scope]
```

对应测试：UC7（F，子代理独立会话）、UC8 merge-back（F）、P1 并行规模（P）、R1 race（R）。

## AD-5 工具调用管线（FC 许可 + 沙箱中间件）

```mermaid
flowchart TB
    M[模型 tool call] --> FC{FC 许可<br/>manual/full_access}
    FC -- ask --> AP[审批交互]
    AP --> FC
    FC -- deny --> ERR[拒绝并回写错误]
    FC -- allow --> SB{沙箱中间件<br/>PathGate/worktree/限额}
    SB -- 通过 --> EX[执行]
    SB -- 拒绝 --> ERR
    EX --> R[结果回写 + trace]
    ERR --> R
```

对应测试：UC9 顺序断言（F）、既有 permission/pathgate（F）、B3 限额（B）。

## AD-6 会话粒度持久化

```mermaid
flowchart TB
    T[turn 完成/卸载] --> W[写 session:&lt;id&gt; 各分片]
    W --> W1[SessionRecord]
    W --> W2[history]
    W --> W3[transcript]
    W --> W4[toolresults]
    W --> W5[context]
    W1 --> IDX[更新 project 索引]
    W2 --> IDX
    W3 --> IDX
```

对应测试：T2.6（F/B）、B6 幂等（B）。

## AD-7 trace 注入与结果返回

```mermaid
flowchart TB
    L[Seele loop llm/tool 动作] --> B[Hook.Before<br/>h_n∘…∘h_1]
    B --> W[写 span → T_i]
    W --> A[Hook.After 透传]
    A --> Q[会话过滤查询]
    Q --> V[视图渲染 trace]
    L --> RET[返回 reply/reasoning/toolCalls]
    RET --> P[投影 ΔV_i]
```

对应测试：T1.1（F/R）、T1.2 链复合（F/B）、T1.3 结果返回（F）、T1.4 过滤（F/B）。

## AD-8 并发关闭（Shutdown）

```mermaid
flowchart TB
    S[Shutdown] --> C1[取消所有运行中会话]
    C1 --> W[等待执行收敛]
    W --> P[persist 各会话]
    P --> CL[关闭 registry/bundle/订阅]
    CL --> D[完成]
```

对应测试：R5 并发 Shutdown（R）、P4 资源有界（P）。

## AD-9 fork 会话

```mermaid
flowchart TB
    F[fork 父会话 i] --> D{父运行中?}
    D -- 是 --> ERR[拒绝]
    D -- 否 --> CP[深拷贝 record 前缀 + context]
    CP --> N[NewSessionUnit j<br/>parent=i, K=main]
    N --> SEED[seed 存储]
    SEED --> SW[V := j]
```

对应测试：T2.7 深拷贝隔离（B）、UC5（F）。

## 索引

| 图 | 主题 | 主要测试 |
|---|---|---|
| AD-0 | MBD 阶段流程 | 阶段门禁 |
| AD-1 | 提交/执行/持久化 | UC1, T1.3, T2.8, P5, R4 |
| AD-2 | 会话生命周期 | T2.3, T4.4, T4.1/4.2 |
| AD-3 | 视图切换 | T4.1–4.6 |
| AD-4 | subagent 并行 + merge-back | UC7/8, P1, R1 |
| AD-5 | FC + 沙箱管线 | UC9, B3 |
| AD-6 | 会话粒度持久化 | T2.6, B6 |
| AD-7 | trace 注入/返回 | T1.1–1.4 |
| AD-8 | 并发关闭 | R5, P4 |
| AD-9 | fork | T2.7, UC5 |
