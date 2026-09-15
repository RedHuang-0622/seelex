# 2026-09-15 Agent Team 全局母本（团队库 / 员工库 / 默认顺序）+ 会话深拷贝副本

## 一、口径（用户给定）

- 团队库、员工库、默认顺序都是**全局粒度**，不再是项目粒度；
- 为了增加并发：**写落全局母本，读在会话侧深拷贝成私有副本**；会话内的顺序
  改动**不影响全局**，除非用户点「确认·普及搭配到全局」。

即：全局母本 = 唯一写目标；会话副本（`session/team/roles.json` + lifecycle
顺序）= 读/改的现场；普及 = 会话副本 → 母本的显式回写。

## 二、改动

### 1. 存储：全局母本 + 旧布局只读回退

- `sessionstore/team_global.go`（新）：数据根 `<root>/team/` 下三份整份替换型
  文件，各有按路径写锁、schema 版本、重复键显式报错：
  - `library.json` 团队模板库；
  - `employees.json` 员工库（可复用 `RoleSpec` 池）；
  - `order.json` 默认顺序（`order_policy` + `order_roles`）。
  Router 出口：`Read/WriteTeamLibraryGlobal`、`Read/WriteEmployeeLibraryGlobal`、
  `Read/WriteDefaultOrderGlobal`。
- `sessionstore/team_library.go`：团队库路径由 `project-<hash>/teams/library.json`
  改为全局；删掉项目级写路径，保留**旧布局只读回退**（全局库缺失时按锚定项目读
  一次；不搬数据、不删旧文件，回退内容在下一次整份写入里自然并入全局）。

### 2. 契约 + 适配 + 应用层

- `dto`：新增 `EmployeeLibrary`、`DefaultOrder`、`TeamComposition`、
  `TeamGlobalConfig`；`TeamLibrary` 注释改为全局。
- `internal/adapters/agentteam_ports.go`：团队库读写切全局；新增员工库/默认顺序
  四个读写方法与映射；顺手把散在三处的角色配置映射收敛成
  `teamRoleSpecToDTO/FromDTO`（提示词/权限只剩一个口径）。
- `application/core/agentteam/global.go`（新）：`Global` 读写面 +
  `NormalizeEmployeeLibrary` / `NormalizeDefaultOrder`（内置角色剔除、顺序保序、
  user/main 补齐）；`library.go` 导出 `IsBuiltinRole` / `OrderRolesOf` 供
  application 层复用同一口径。
- `application/core/agentteam_service.go`：`AgentTeamGlobalConfig`（母本 + 会话副本
  投影，只读）、`AgentTeamSaveEmployee`/`AgentTeamDeleteEmployee`/
  `AgentTeamSetDefaultOrder`（库管理，直写全局）、`AgentTeamPublishToGlobal`
  （普及：员工 → 员工库、顺序 → 默认顺序、搭配 → 一条团队库条目）。

### 3. Bridge + 前端

- `gui/bridge.go`：5 个新 Bridge 方法；`agentTeamApplication` 接口同步扩展（继续
  用真实 `*application.Service` 钉死，防方法名漂移）。
- `agent-team-view.js`：新增「全局母本」块（员工库表 + 默认顺序 + 会话副本差异 +
  「顺序设为默认」/「确认·普及搭配到全局」）、`normalizeTeamGlobal` /
  `teamGlobalDrift`；`renderAgentTeam` 增加第 4 个入参（旧调用不传即隐藏该块）。
- `app.js`：拉取 `AgentTeamGlobalConfig`（可选面，失败只隐藏该块）+ 三个动作接线。
- `styles.css`：`.team-drift` 差异提示。

## 三、验证证据

```text
gofmt -l .                        → 无输出
go build ./...                    → ok
go vet ./gui/... ./application/... ./sessionstore/ ./internal/adapters/ → ok
go test ./sessionstore/ ./application/core/... ./gui/... ./internal/adapters/ -count=1 → ok
node --test gui/frontend/dist/*.test.mjs → 300 pass / 0 fail
go test ./e2e/ -run 'README|DocumentationRules' → ok
```

新增/更新的守卫用例：

- `sessionstore/team_library_test.go`：全局路径布局、整份往返、重复键报错、全局
  可见性（A 项目写 B 项目读得到）、旧项目库只读回退 + 首次写入并入全局且旧文件
  保留。
- `sessionstore/team_global_test.go`：员工库/默认顺序的路径、往返、排序、重复键、
  未知 schema。
- `application/core/agentteam_library_service_test.go`：读母本零写盘、普及把副本写
  回母本（保提示词/权限）、员工库 CRUD（内置角色拒绝、未知角色剔除、删除幂等）、
  未装配端口显式报错。
- `gui/bridge_team_test.go`：新方法未装配报错 + 参数归一/窄转发。
- `gui/frontend/dist/agent-team-view.test.mjs`：母本块渲染与按钮、差异提示、
  畸形载荷、转义（19 pass）。

## 四、边界与未做

- **旧项目库只做只读回退，不主动迁移**：全局库缺失时按当前锚定项目读一次；第一次
  写全局库时把读到的条目一并写入。其它项目的历史项目库不会被枚举合并——不删、
  不动，需要时可再读回。
- **普及是"会话 → 母本"的单向回写**：员工按 `role_name` 幂等覆盖员工库，顺序整表
  覆盖默认顺序，并写一条团队库条目。没有"母本 → 会话"的强制同步：会话副本一旦
  建立就与母本解耦（这正是并发口径要的）。
- **员工库与团队库条目的关系**：团队库条目自带角色配置，员工库是"可复用员工池"
  这一份额外事实；两者由普及动作同时更新，但没有做"条目引用员工库 id"的规范化。
- **默认顺序目前只是登记 + 展示**：装配/新建会话尚未把它当缺省顺序消费（顺序的
  运行时权威仍是各会话 lifecycle head）。
