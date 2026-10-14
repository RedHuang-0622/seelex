# Seelex-Dot 的 A2A 星形拓扑（头脑风暴记录）

日期：2026-10-13 · 状态：**头脑风暴，未裁决，未改代码**
记录人：agent（按用户口述逐条留档）

> 本文只做两件事：**① 逐条保留用户原话口径（不改写）**；**② 标出它与仓内现状的咬合点、
> 我已看到的张力、以及后续谈话要拍的清单。**
> 本文不是设计，不是需求单，不是实施计划。任何一条在拍板前都不构成待办。

---

## 1. 原始想法（用户口述，保留原词）

用户原词：`seelexdot` / `seelexrole` / `seelexmcp` / `main seelex` / `高管员工` / `基层员工`。

1. **Seelex 的 dot 的 A2A，打算走 MCP 的路线**，跨进程乃至远程；
2. 拓扑做成**星形**；
3. 通过**每个员工分发一个邮箱 + 一个 GitHub 账号**来达到效果；
4. **SeelexRole 为高管员工时**，支持在**自己的 branch 里发布自己模块的内容**；
5. **SeelexDot 只能是高管员工**；
6. **真正的基层员工在 `$teamwork` 下面**；
7. **基层员工只能持有 worktree，不能持有远端 branch 发布能力**；
8. 权限管理要改：**从「分层的权限管理」改成「装配的权限管理」**——即
   **`rw` 与 computer use 能力平等，可以通过勾选获得你能使用的能力**。

后续待谈（用户点明的两个议题）：

- A. **实现 Seelex-MCP 时，哪些契约需要被管理和实现**；
- B. 哪些是 **Seelex 高管员工**与 **main Seelex** 需要各自注意的。

---

## 2. 我对这份想法的读法（我的表述，非用户原话）

```text
                     ┌──────────────────────────────┐
                     │   main Seelex（星心 / CONTROLLER）│
                     └───────────────┬──────────────┘
              MCP（跨进程/远程）      │
        ┌────────────────┬───────────┴───────┬────────────────┐
        ▼                ▼                   ▼                ▼
   SeelexDot A      SeelexDot B         SeelexDot C      SeelexDot …
   = 高管员工         = 高管员工           = 高管员工
   （可发自己的 branch）  （可发自己的 branch）
   └── teamwork（进程内）──┘
        ├─ 基层员工（teammate）: 只有 worktree
        └─ 基层员工（teammate）: 只有 worktree
```

- **两个层级、两种权限形态**：
  - **高层 = SeelexDot = 高管员工**：一个独立的 Seelex 实例（跨进程/远程），**能对外产生产物**
    （发布自己模块到自己的 branch）；它是星形拓扑的一个**辐射点**，main Seelex 是**星心**。
  - **基层 = teammate**：活在 teamwork 下面（今天的 `$teamwork` teammate），**只能持有 worktree**，
    **没有远端发布能力**。
- **两件"分发物"承担两个不同的角色**（我的读法，需用户确认哪种是原意，见 §4 Q2）：
  - **邮箱** → 地址 + 异步 inbox（离线不丢、天然审计、人也能读）；
  - **GitHub 账号** → 制品与代码的公共账本 + 权限面（谁能 push 到哪）。
- **写权不是"有没有"，而是"写到哪一层"**：
  `worktree（基层）` → `自己的 branch（高管）` → `main（main Seelex）`。
  这跟既有裁决 D9 同向：**版本/分支管理一律走 git，Seelex 不内置管理系统**
  （`docs/arch/teamwork-leader-worker-architecture.md` §0 D9）。
- **权限形态**：从「主体 × 路由组 × 位」的**分层表**（今天）改成**能力集的装配**
  （勾选即得），且 `rw` 与 `computer use` **平权**——不再有"共享外设位"这种特殊格子。

---

## 3. 与现状的咬合点（有锚点）

| # | 想法 | 现状锚点 | 关系 |
|---|---|---|---|
| 1 | A2A 走 MCP 跨进程/远程 | `docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md` §3 已声明：**"投递语义：进程内 = 队列/事件流投递；跨进程 = 同一 JSON over 任意 transport。不区分——协议相同，进程内是零拷贝特例。"** | **不是新发明，是把已声明的"transport 无关"落成一个具体 binding**。协议信封不用动，要动的是 binding 层 |
| 2 | MCP 作为那个 binding | MCP **客户端**已支持 SSE 远端：`vendor/github.com/RedHuang-0622/Seele/tools/mcp/provider.go:108-114`；配置面 `config/mcp.example.yaml` 的 `transport: stdio \| sse` + `url`；Seelex 自己的包**没有任何 server**（`net.Listen`/`ListenAndServe`/`NewMCPServer` 零命中） | 客户端侧现成，**服务端侧要从零写**（`docs/arch/remote-agent-node-feasibility.md` §4 #1） |
| 3 | 高管 = 角色会话（SeelexRole） | 角色（员工）回合执行体：`seelebridge/runtime_role_turn.go`；主体分配 `emp_<角色名>` 与用户权限**同一张表**（`seelebridge/runtime_tools.go:68`、`seelebridge/teamwork/teamwork.go:36`） | 高管 = 今天的"角色（员工）"再加**跨进程 + 发布权**两件事 |
| 4 | 基层在 teamwork 下面、只有 worktree | teammate = 角色会话 + **git worktree**（`seelebridge/worktree/**`）；作业作用域 `{Session: 主会话, Subject: emp_<role>}`（`seelebridge/teamwork/README.md:149`） | **已是现状**，这条是"保持不动" |
| 5 | 基层**不能**持有远端 branch 发布能力 | 今天的位模型：`emp_ro` / `emp_rw` **`GroupRWDesktop` 字段整块不存在**、`GroupCTL`/`GroupADM` 位为 0（`seelebridge/tools/permission_policy.go:194-232`）；口径是"**位缺 → 该工具对该主体不可路由（= 不在 PATH 上）**"（`docs/arch/agent-permission-subjects.md` §2.3） | **这里有真空**：见 §4 Q4——"不在 PATH 上"拦不住一个手里有可 push 凭据的 `bash` |
| 6 | 从分层权限改成装配权限 | 仓里已有**同一族 idiom 的先例**：「**窄可选能力面 + 类型断言：有就有、没有就是没装配**」——`TeamworkBoardProjection` / `TeamworkJobCompletion` / `ReasoningEffortPort` / `SessionLoopLimitPort` / `CompactionIndexPort`（`application/contract/ports.go`）；另有 `ToolFaceForSubject`（`seelebridge/tools/permission_policy.go:620`）作为"主体 → 工具面"的**唯一折算点** | 装配式权限 = 把这条既有 idiom 用到权限面上；`ToolFaceForSubject` 是天然插口 |
| 7 | 高管能"发布自己模块" | `docs/arch/teamwork-leader-worker-architecture.md` §0 **D9**：版本/分支管理一律走 git，Seelex 不内置管理系统；§12.4 已裁决**不碰**框架作业表合表 | 发布能力应落在 **git（branch/protected rules）+ 凭据**上，Seelex 只做编排 |
| 8 | SeelexDot = 高管员工的载体 | 员工执行面可替换：`roleEngine`（6 方法，无 channel）+ `Runtime.SetRoleEngineFactory`（`seelebridge/runtime_role_turn.go:52,216`），README 自陈用途之一是"**更严隔离面**" | **远端高管节点最干净的落点**：`remoteRoleEngine` 把 `ChatStream` 转发到远端 Seelex |

---

## 4. 我已看到的张力与要拍的清单

> 这些不是反对意见，是**后续谈话的输入**。每条都标了"为什么必须拍"。

**Q1 · 「Dot」与「Role」是两种东西，需要把关系钉死。**
今天的"员工" = **进程内的一个角色会话**；而 SeelexDot = **一个进程/一台机器上的 Seelex 实例**。
"SeelexDot 只能是高管员工"这句话把两者绑在一起，于是必须回答：
main Seelex 自己算不算一个 Dot？星心能不能同时也是高管？一个 Dot 上能不能承载多个高管角色？
——不钉死这条，"星形"的节点数、路由键、生命周期都没有定义。

**Q2 · 邮箱是 transport，还是只当身份？**（两种读法 → 两套契约）
- 读法甲（**邮箱 = A2A 的 store-and-forward transport**）：好处是离线不丢、天然审计、人也能读；
  代价是延迟、顺序与幂等要靠协议自己兜（**好消息：`ds-a2a-protocol.md` §3 的信封已带 `corr`
  （关联 id，用于幂等去重）与 `seq`/`ref_seq`（因果水位）**，缺的是 binding 的投递语义与重投窗口）。
- 读法乙（**邮箱 = 只作地址与身份，消息仍走 MCP**）：契约面小得多，但"离线不丢"要另找方案。
两种读法对"要管什么契约"的答案完全不同。

**Q3 · 「自己的模块」与「发布」的边界没定。**
"模块"是目录 / 包 / 一个 repo / 一个 workspace？（`WorkspacePort` 已有 `rootPath` + `gitRemote` 的概念）。
"发布"是 push branch / 开 PR / 打 tag / 发制品？——**这个边界直接决定 protected branch 规则怎么配**，
而 protected branch 是"高管能发自己的、动不了别人的"的**唯一可靠执法点**。

**Q4 · ⚠️ 最重要的一条：能力必须落在凭据层，不能只落在工具注册层。**
今天的口径是"位缺 → 工具不在 PATH 上"。但**基层员工是能跑 `bash`/`bash_bg` 的**
（`sub` 的 `rw(project)` 位、`emp_rw` 的 `rw(project)` 位都是齐的）。
**一个手里有可 push 凭据的基层员工，用 `bash` 一样能推分支**——"不注册工具"拦不住它。
所以"基层不得持有远端 branch 发布能力"必须落成：

- 基层拿到的 **git 远端凭据是只读的**（或干脆没有 remote 写权限），
- 高管的凭据**只能推自己的 branch**（由 GitHub 侧 protected branch / ruleset 兜底）。

> 一句话：**凭据是能力的物理载体，工具面只是能力的说明面。** 二者要一致，否则权限模型是装饰。

**Q5 · 改成"勾选式装配"之后，`missing_bit` 的既有语义怎么办？**
今天是产品口径：**位缺（违权）→ 走执行选择页面提权**（宿主的人类 = sudo 口令），
`sub` 则是"没有人类在环 → 缺位直接拒"（`docs/arch/agent-permission-subjects.md` §4）。
改成勾选后必须回答：**没勾的能力是"不存在"还是"可提权"？** 这是整个权限改造的根问题。
附带：`sub`（子代理）在平权模型下的定位——今天它是"缺位直接拒"，平权后它是"装配给它哪些能力"。

**Q6 · `rw` 与 `computer use` 平权，等于取消"共享外设"这条特例。**
今天 `rw_desktop` 之所以是独立一格、默认 `ask`，理由是它写的是**共享外设**
（"一块桌面，所有并行代理共用"，`agent-permission-subjects.md` §2.2）——**多代理抢同一块桌面是物理冲突**。
平权之后，这个冲突不会消失，只会从"位表里的一格"变成"一个被勾选的能力"。
→ 需要一条**运行时互斥**（谁在占这块桌面）来替代原来靠"默认 ask + 位缺"表达的稀缺性。

---

## 5. 非目标（本轮明确不做）

- 不改 `seelebridge/tools/permission_policy.go`、不改位表、不动 `missing_bit` 语义；
- 不写 Seelex-MCP 的服务端，不新增 binding，不改 `ds-a2a-protocol.md`；
- 不碰 teamwork / worktree / `emp_<role>` 现状；
- 本文不进 `docs/arch/`（那是"长期有效的架构事实"）；**在拍板前它只是头脑风暴**，
  落成架构事实时应另开 `docs/arch/seelex-dot-a2a-star-topology.md`（或改本目录并升级状态）。

---

## 6. 与既有文档的关系

| 文档 | 关系 |
|---|---|
| `docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md` | **协议基线（权威）**。本文的想法是给它的"任意 transport"选一个具体 binding，**协议本身不动** |
| `docs/arch/teamwork-leader-worker-architecture.md` | 高层/基层两个层级与 D9（git 管版本）、D10（作用域 `{Session, Subject}`）对齐 |
| `docs/arch/agent-permission-subjects.md` | 今天的分层权限（主体 × 路由组 × 位）；§4 的三条产品口径是 Q5 的直接上下文 |
| `docs/arch/remote-agent-node-feasibility.md` | 远端点可行性与暴露阶梯 L0–L3；本文的 SeelexDot 是 L1/L2 的用例 |
| `docs/arch/a2a-agent-team-factory.md` | **已退场的历史**（Agent Team 用户面 2026-10-12 删除）；`RoleSpec` 字段表仍是角色建模的参考，勿当现状读 |
