# 工作流 B：AgentTeam 管理（A2A 抽象工厂 + 角色管理设置面）

## 0. 边界

- `AgentTeam` 是 A2A 角色团队的运行时实例；**每个成员都是真正的角色会话**
  （`role_session_id`/`join_seq_id`/`compact_ref`）。
- `goal` 的 TL 只是**第一个实例**，不是特例；TL 的模式必须被抽成通用能力，
  之后新增 A2A 团队只做装配，不复制 TL 分支。
- subagent 归工作流 A，本流不碰 subagent 的续跑/恢复逻辑。
- 权威设计：[`docs/arch/a2a-agent-team-factory.md`](../../arch/a2a-agent-team-factory.md)
  §2–§4、§6–§9。
- 开工前必读：根 `MEMORY.md`（危险操作铁律 + 新功能归属决策）、`AGENTS.md`、
  `application/core/goal/README.md`、`sessionstore/README.md`、`gui/README.md`。

## 1. 现状（已实现，作为起点）

| 事实 | 位置 |
|---|---|
| 角色会话存储、role draft、sync 即删、floor、compact_ref/join_seq、角色 wire | `sessionstore/role_session.go`、`sessionstore/role_session_router.go` |
| 角色消息归属字段（`RoleName`/`RoleSessionID`/`RoundID`/`UnitSeq`） | `application/model/context.go`、`sessionstore/sessionstore.go` |
| 应用层窄转发 | `application/core/role_session.go`、`internal/adapters/session_workspace_ports.go` |
| headless `role.*`/`schedule.*` RPC | `gui/headless.go`、`gui/role_live_probe_test.go` |
| goal TL 原型（PeerState/Frame/Round/Mailbox/状态机/缓存观测） | `application/core/goal/{advisor.go,techleader.go,a2a.go,gate.go,directive.go}` |

**缺口**：TL 的 peer-session 机制仍长在 `goal` 包里；没有通用 `AgentTeamFactory`、
`TeamSpec`/`RoleSpec` 注册表，没有前端/后端角色管理装配面，也没有第二个团队实例
证明可泛化（AT8 未满足）。

## 2. 目标（按优先级，允许分批交付）

### P0 抽象与泛化（必须完成）

1. 把 `goal` 里的通用 peer-session 原语（`PeerState`、`Frame`、`Round`、mailbox、
   sequencer 顺序函数、presence、缓存观测）提取到通用位置（例：
   `application/core/agentteam/`），**goal 只保留适配**（`GoalFrame`、TL directive、
   goal gate）。
2. 提供 `TeamSpec`/`RoleSpec` + `AgentTeamFactory`：
   - `TeamSpec`：`team_id`、`team_kind`、`order_policy`、`order_roles`、`roles[]`、
     `gate_policy`、`compact_policy`；
   - `RoleSpec`：`role_name`、`role_kind`、`system_prompt`、`model/account_policy`、
     `mirror_policy`、`directive_schema`、`order_priority`、`join_policy`、
     `presence_policy`、`tools_policy`；
   - 工厂负责创建角色会话、draft 锁、presence、bind `join_seq_id`/`compact_ref`。
3. **行为等价**：goal TL 改用工厂实例化后，现有 goal/DS-A2A 测试全绿；TL 对外行为
   不变（顺序、指令、gate、缓存观测、冷恢复）。
4. **AT8 证明**：新增第二个团队实例（测试用即可，如 review-team / research-team），
   复用同一工厂、同一 sequencer、同一恢复形态；换的是 `order_policy`/角色集，
   不是复制代码。

### P1 角色管理装配面（后端 + headless）

5. `RoleRegistry`/`TeamRegistry`：RoleSpec/TeamSpec 的 CRUD，端口落在
   `application/contract/ports.go`，DTO 落在 `application/contract/dto`；
   持久化经 `sessionstore` 的 lifecycle head/等价通道，**不直接改 message**。
6. headless 暴露（B 拥有 `role.*`/`team.*` 段）：
   - 成员表：`role_name`/`role_session_id`/online-offline/floor 高亮所依据的字段；
   - 工作顺序：`order_roles` 读写；
   - 定时 agent：单独分区，不入 `order_roles`（复用既有 `schedule.*`）；
   - 角色配置：system prompt、模型/账号策略、工具面、mirror 策略、directive schema。
7. `gui/README.md` 与 `application/core/.../README.md` 同步记录 RPC/端口/DTO。

### P2 provider role 口径审计（与设计 §2.1 表对齐）

8. task/goal/plan 这类**编排态材料一律 `system`**，只有用户输入是 `user`；
   被发现以 `assistant`/`user` 伪装的状态材料，按表收口并补回归测试。
9. 自定义角色名（`tl` 等）只做 metadata；真实端点已实验确认自定义 role 会被拒绝
   （`seelebridge/custom_role_live_probe_test.go`），不得再往 provider role 里塞。

## 3. 实施要求

- 提取/泛化按小步走：每一步先让现有测试全绿，再继续下一处；禁止一次性大搬迁后
  再修测试。
- 禁止新建上帝包/上帝类型（`MEMORY.md` 判据）：`agentteam` 包只放团队编排原语，
  goal 语义、存储实现、GUI 交互各归其位；新包必须配 `README.md`（按
  `docs/arch/readme-spec.md` 十项 + 文件函数索引）。
- 名称只用于展示；`role_session_id`/`session_id` 始终是唯一键。
- 不 commit、不 push；不改 `config/accounts.yaml` 与 `*.local.yaml`。

## 4. 验收

- T-B-01：`AgentTeamFactory` 能按 `TeamSpec` 实例化 goal TL，行为与改造前等价
  （现有 goal / DS-A2A / TL headless 测试全绿）。
- T-B-02：同一工厂实例化第二个非 goal 团队并通过其顺序/恢复测试（AT8）。
- T-B-03：角色注册表 CRUD 经端口往返，配置只改注册表/lifecycle，不动 message。
- T-B-04：headless `role.*`/`team.*` 能读写成员表、`order_roles`、角色配置；
  契约测试覆盖。
- T-B-05：编排态注入的 provider role 审计通过（只有用户输入是 `user`）。
- T-B-06：并发/幂等：同一 `role_session_id` 不产生两个会话；工厂重复装配幂等。

验证命令（最低要求）：

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./application/... ./sessionstore ./internal/adapters ./gui
go test ./application/... ./sessionstore ./internal/adapters ./gui -count=1
go test -race ./application/core/... ./sessionstore -count=1
```

真实 API / headless 冒烟（沿用既有 `SEELEX_LIVE_SMOKE=1` 约定与
`gui/role_live_probe_test.go` 模式）：

- 用真实 API 跑一条 goal TL + 第二个团队的最小链路，抓 pprof（mutex/block）与
  `-race`，观察死锁/数据竞争与链路偏差；报告落 `tmp/` 或 `docs/test/`。

## 5. 交付物

- 通用 `agentteam` 原语 + `AgentTeamFactory`/`TeamSpec`/`RoleSpec`（代码 + README + 测试）。
- goal 侧改为适配器（代码 + `application/core/goal/README.md` 同步）。
- 第二个团队实例（测试或真实适配器）+ AT8 证据。
- `RoleRegistry`/`TeamRegistry` 端口/DTO + headless `team.*` RPC + README。
- provider role 口径审计记录。
- 验证记录追加到
  `docs/2026-09-08-session-storage-architecture/conformance-checklist.md`（新编号），
  并回填本目录 `README.md` 的"实现状态"。

## 6. 与其他流的接口

- subagent 的运行态/恢复归工作流 A；成员表里**不得**出现 subagent（AT1）。
- `gui/headless.go` 只改 `role.*`/`team.*` 段；需要动 `subagent.*` 段先发消息给 A。
