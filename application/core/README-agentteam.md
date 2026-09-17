# core/agentteam（根包分卷）

## 生态位

AgentTeam 装配适配与群聊角色会话透传（端口形状与 A2A 元数据口径）

覆盖：`agentteam*.go`、`role_session*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### agentteam_floor_wiring_test.go

- `func (s *floorRecordingSessions) ReadFloorRole(string) (string, error)`
- `func TestAgentTeamViewCarriesFloorRole(t *testing.T)`
- `func TestAgentTeamViewWithoutFloorPortStaysEmpty(t *testing.T)` — TestAgentTeamViewWithoutFloorPortStaysEmpty 钉住降级：宿主没有 floor 读面

### agentteam_library_service_test.go

- `func newLibrarySessions() *librarySessions`
- `func (s *librarySessions) ReadTeamLibrary(string) (dto.TeamLibrary, error)`
- `func (s *librarySessions) WriteTeamLibrary(_ string, library dto.TeamLibrary) error`
- `func (s *librarySessions) ReadEmployeeLibrary(string) (dto.EmployeeLibrary, error)`
- `func (s *librarySessions) WriteEmployeeLibrary(_ string, library dto.EmployeeLibrary) error`
- `func (s *librarySessions) ReadDefaultOrder(string) (dto.DefaultOrder, error)`
- `func (s *librarySessions) WriteDefaultOrder(_ string, order dto.DefaultOrder) error`
- `func (s *librarySessions) globalWriteCount() int` — globalWriteCount 读当前全局母本写次数（断言"读不写盘"用）。
- `func (s *librarySessions) writeCount() int` — writeCount 读团队库写次数（加锁：直接读 sessions.writes 与写侧没有 happens-before，
- `func (s *librarySessions) setLibrary(library dto.TeamLibrary)` — setLibrary 覆盖夹具的团队库（加锁）：用例要先造一份"已经存过团队"的库时走它，
- `func (p *fakeRolePrompt) OptimizeRolePrompt(_ context.Context, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error)`
- `func withRolePrompt(port RolePromptPort) testServiceOption`
- `func TestAgentTeamSaveCurrentTeamAndMaterialize(t *testing.T)` — TestAgentTeamSaveCurrentTeamAndMaterialize 是"新建团队要计入存储、并能装配回
- `func TestAgentTeamRolePromptAndOptimize(t *testing.T)` — TestAgentTeamRolePromptAndOptimize：ADVISOR 提示词读面走注册表只读路径；
- `func TestAgentTeamGlobalPublish(t *testing.T)` — TestAgentTeamGlobalPublish：全局母本的读是只读；「确认普及搭配到全局」才把会话
- `func TestAgentTeamGlobalEmployeeCRUD(t *testing.T)` — TestAgentTeamGlobalEmployeeCRUD：员工库 / 默认顺序是"库管理"动作，直接写全局
- `func TestAgentTeamGlobalRequiresPort(t *testing.T)` — TestAgentTeamGlobalRequiresPort：宿主没实现全局母本端口时显式报错（不静默返回

### agentteam_ring_revive_test.go

- `func TestNewGoalRevivesStoppedTeamRing(t *testing.T)` — TestNewGoalRevivesStoppedTeamRing：环逃生后，新 goal 上线必须让它复活，并且
- `func TestSameGoalBeginIsIdempotentAndKeepsRing(t *testing.T)` — TestSameGoalBeginIsIdempotentAndKeepsRing：幂等 begin（同名返回既有 active）

### agentteam_role_index.go

- `func (index *roleSessionIndex) remember(mainSessionID string, view dto.TeamView)` — remember 记下一次"注册表读到的事实"。同一 (主会话, 角色) 覆盖旧值（角色改名/
- `func (index *roleSessionIndex) owner(roleSessionID string) []roleSessionBinding` — owner 返回该角色会话的归属（多个候选 = 角色会话号跨会话重号，返回全部）。
- `func (index *roleSessionIndex) policy(roleSessionID string) (string, bool)` — policy 返回该角色会话的权责口径（歧义时取最严）。
- `func mostRestrictiveToolsPolicy(left, right string) string` — mostRestrictiveToolsPolicy 取两个口径里更严的一个：readonly < readwrite < 其他。
- `func effectiveToolsPolicy(member dto.TeamMember) string` — effectiveToolsPolicy 把成员表行折算成**权限面上等价的口径**：显式权限格子非空时
- `func toolsPolicyRank(policy string) int`
- `func (service *Service) RoleSessionToolsPolicy(roleSessionID string) (string, bool)` — RoleSessionToolsPolicy 回答"这个角色会话是谁的、权责是什么"（权限门读面）。
- `func (service *Service) RoleSessionOwner(roleSessionID string) (string, bool)` — RoleSessionOwner 返回角色会话的归属主会话（歧义时返回第一个读到的；仅供诊断/

### agentteam_role_index_test.go

- `func TestRoleSessionPolicyResolvesOwningSessionNotCurrentView(t *testing.T)` — TestRoleSessionPolicyResolvesOwningSessionNotCurrentView：角色会话被读取过归属注册表
- `func TestRoleSessionIndexAmbiguityTakesMostRestrictive(t *testing.T)` — TestRoleSessionIndexAmbiguityTakesMostRestrictive：角色会话号重号时必须取最严口径。

### agentteam_runtime.go

- `func (store *teamRuntimeStore) get(mainSessionID string) *agentteam.Runtime`
- `func (store *teamRuntimeStore) put(mainSessionID string, runtime *agentteam.Runtime)`
- `func (store *teamRuntimeStore) drop(mainSessionID string)` — drop 释放某个主会话的运行时槽（会话删除/归档时经 releaseTeamRuntime 调用；
- `func roleSessionsOf(view dto.TeamView) map[string]string` — roleSessionsOf 从成员表取 role_name → role_session_id（环里角色发言时的会话
- `func (service *Service) goalLoopRoundLimitForTeam() int` — goalLoopRoundLimitForTeam 取治理循环实际生效的轮次上限：环的逃生路径第一道
- `func (service *Service) teamRuntimeFor(mainSessionID string, view dto.TeamView) *agentteam.Runtime` — teamRuntimeFor 返回（需要时创建）指定主会话的发言调度运行态，并把注册表
- `func (service *Service) syncTeamRuntime(mainSessionID string)` — syncTeamRuntime 在角色注册表/顺序发生变更后同步环（增删改员工与顺序调整的
- `func (service *Service) releaseTeamRuntime(mainSessionID string)` — releaseTeamRuntime 释放某个主会话的发言调度运行态（会话删除/归档时调用）。
- `func (service *Service) NoteTeamUserQueued(mainSessionID string, pending bool)` — NoteTeamUserQueued 告诉该会话的环"消息队列里有没有未消费的 user 输入"。
- `func (service *Service) noteTeamWorkPrefix(mainSessionID string)` — noteTeamWorkPrefix 把「主会话上下文（含主会话 draft）」的只读装配喂给团队环当前缀。
- `func (service *Service) teamScheduleFor(mainSessionID string) *dto.TeamSchedule` — teamScheduleFor 投影指定会话的调度运行态（nil = 该会话没有环）。
- `func (service *Service) teamRuntimeBySession(sessionID string) *agentteam.Runtime` — teamRuntimeBySession 返回（需要时创建）指定会话的发言调度运行态，供治理循环
- `func (service *Service) teamRoleSeatsFor(sessionID string) []RoleSeat` — teamRoleSeatsFor 返回该会话团队的角色座位来源（按发言链顺序），供 goal 治理循环
- `func roleSeatOf(member dto.TeamMember) RoleSeat`
- `func (service *Service) noteTeamUserSeat(sessionID string)` — noteTeamUserSeat 把"该会话队列里有没有未消费的 user 输入"同步给团队环：

### agentteam_service.go

- `func (adapter agentTeamAdapter) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (adapter agentTeamAdapter) ReadLifecycleOrder(sessionID string) (string, []string, error)`
- `func (adapter agentTeamAdapter) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (adapter agentTeamAdapter) ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error)`
- `func (adapter agentTeamAdapter) WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error`
- `func (adapter agentTeamAdapter) ReadFloorRole(mainSessionID string) (string, error)` — ReadFloorRole 实现 agentteam.FloorPort：宿主端口实现了 floor 读面才转读，
- `func (service *Service) agentTeamPorts() (agentTeamPort, contract.RoleSessionPort, error)`
- `func (service *Service) agentTeamFactory() (*agentteam.Factory, error)`
- `func (service *Service) agentTeamRegistry() (*agentteam.Registry, error)`
- `func (service *Service) AgentTeamPresets() []dto.TeamSpec` — AgentTeamPresets 列出内置团队形态（前端角色管理页的可选模板）。
- `func (service *Service) MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error)` — MaterializeAgentTeam 按 preset/自定义 TeamSpec 装配一支 AgentTeam。
- `func (service *Service) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error)` — MaterializeAgentTeamPreset 按内置 preset 名装配（goal-a2a / review-team / research-team）。
- `func (service *Service) AgentTeamView(mainSessionID string) (dto.TeamView, error)` — AgentTeamView 返回成员表（身份/顺序/定时分区/配置状态/发言调度运行态）。
- `func (service *Service) agentTeamRawView(mainSessionID string) (dto.TeamView, error)` — agentTeamRawView 返回不带运行态的成员表（装配面内部用；避免
- `func (service *Service) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error)` — AgentTeamPutRole 新增/覆盖一个角色配置。
- `func (service *Service) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error)` — AgentTeamDeleteRole 删除一个角色配置（并把它从工作顺序里摘除）。
- `func (service *Service) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error)` — AgentTeamSetOrder 写工作顺序（前端拖拽/上下移只提交这个字段）。顺序是环的
- `func (service *Service) AgentTeamInstantiateRole(mainSessionID string, role dto.RoleSpec, joinSeq uint64) (dto.RoleInstantiation, error)` — AgentTeamInstantiateRole 一步实例化一个角色（"员工入职"）：规整配置 → 幂等建
- `func (adapter agentTeamLibraryAdapter) ReadTeamLibrary() (dto.TeamLibrary, error)`
- `func (adapter agentTeamLibraryAdapter) WriteTeamLibrary(library dto.TeamLibrary) error`
- `func (service *Service) agentTeamLibrary(mainSessionID string) (*agentteam.Library, error)` — agentTeamLibrary 构造团队库读写面（锚定会话 = 作用域解析入口）。
- `func (adapter agentTeamGlobalAdapter) ReadEmployeeLibrary() (dto.EmployeeLibrary, error)`
- `func (adapter agentTeamGlobalAdapter) WriteEmployeeLibrary(library dto.EmployeeLibrary) error`
- `func (adapter agentTeamGlobalAdapter) ReadDefaultOrder() (dto.DefaultOrder, error)`
- `func (adapter agentTeamGlobalAdapter) WriteDefaultOrder(order dto.DefaultOrder) error`
- `func (service *Service) agentTeamGlobal(mainSessionID string) (*agentteam.Global, error)` — agentTeamGlobal 构造全局母本（员工库 + 默认顺序）读写面。
- `func (service *Service) AgentTeamLibrary(mainSessionID string) (dto.TeamLibrary, error)` — AgentTeamLibrary 返回项目团队库（团队模板清单）。未建库返回空库（不是错误）。
- `func (service *Service) AgentTeamSaveTeam(mainSessionID string, entry dto.TeamLibraryEntry) (dto.TeamLibrary, error)` — AgentTeamSaveTeam 新增/覆盖一条团队库条目（按 team_id 幂等），返回整份库。
- `func (service *Service) AgentTeamSaveCurrentTeam(mainSessionID, name, teamID string) (dto.TeamLibrary, error)` — AgentTeamSaveCurrentTeam 把当前会话在编的员工表存成一条团队库条目
- `func (service *Service) AgentTeamDeleteTeam(mainSessionID, teamID string) (dto.TeamLibrary, error)` — AgentTeamDeleteTeam 删除一条团队库条目（幂等）。
- `func (service *Service) AgentTeamMaterializeTeam(mainSessionID, teamID string, joinSeq uint64) (dto.TeamMaterializeResult, error)` — AgentTeamMaterializeTeam 把团队库里的一支团队装配到会话：库条目 → TeamSpec →
- `func (service *Service) AgentTeamGlobalConfig(mainSessionID string) (dto.TeamGlobalConfig, error)` — AgentTeamGlobalConfig 读全局母本（团队库 / 员工库 / 默认顺序）与当前会话副本的
- `func (service *Service) agentTeamComposition(mainSessionID string) (dto.TeamComposition, error)` — agentTeamComposition 投影"当前会话副本"的搭配：在编员工（不含 user/main）+ 实际
- `func (service *Service) AgentTeamSaveEmployee(mainSessionID string, role dto.RoleSpec) (dto.EmployeeLibrary, error)` — AgentTeamSaveEmployee 新增/覆盖全局员工库里的一个员工（按 role_name 幂等）。
- `func (service *Service) AgentTeamDeleteEmployee(mainSessionID, roleName string) (dto.EmployeeLibrary, error)` — AgentTeamDeleteEmployee 删除全局员工库里的一个员工（幂等）。
- `func (service *Service) AgentTeamSetDefaultOrder(mainSessionID, policy string, orderRoles []string) (dto.DefaultOrder, error)` — AgentTeamSetDefaultOrder 写全局默认顺序（母本发言次序）。
- `func (service *Service) AgentTeamPublishToGlobal(mainSessionID, name, teamID string) (dto.TeamGlobalConfig, error)` — AgentTeamPublishToGlobal 是「确认·普及搭配到全局」：把当前会话副本的
- `func (service *Service) AgentTeamRolePrompt(mainSessionID, roleName string) (string, error)` — AgentTeamRolePrompt 读某个角色登记的提示词（空 = 未登记）。ADVISOR 回合用它
- `func (service *Service) AgentTeamOptimizeRolePrompt(ctx context.Context, mainSessionID string, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error)` — AgentTeamOptimizeRolePrompt 跑一次有界 LLM 回合优化员工提示词（不写会话消息、

### agentteam_session_first_test.go

- `func TestAgentTeamSessionEditsNeverTouchGlobal(t *testing.T)`

### role_session.go

- `func (service *Service) rolePorts() (rolePorts, error)`
- `func (service *Service) roleSessionPort() (contract.RoleSessionPort, error)`
- `func (service *Service) schedulePort() (contract.SchedulePort, error)`
- `func (service *Service) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)`
- `func (service *Service) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error`
- `func (service *Service) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)`
- `func (service *Service) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)`
- `func (service *Service) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error`
- `func (service *Service) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)`
- `func (service *Service) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)`
- `func (service *Service) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)`
- `func (service *Service) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (service *Service) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error`
- `func (service *Service) ListRoleSessions(mainSessionID string) ([]string, error)`
- `func (service *Service) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (service *Service) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (service *Service) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error`

### role_turn.go

- `func (runner contractRoleTurnRunner) RunRoleTurn(ctx context.Context, request RoleTurnRequest) (RoleTurnOutcome, error)`
- `func (service *Service) roleTurnRunnerFor(string) RoleTurnRunner` — roleTurnRunnerFor 返回该会话的员工执行面（未装配端口 → nil）。

### role_turn_test.go

- `func (port *recordingRoleTurnPort) RunRoleTurn(_ context.Context, request dto.RoleTurnRequest) (dto.RoleTurnOutcome, error)`
- `func TestContractRoleTurnRunnerMapsEveryField(t *testing.T)` — TestContractRoleTurnRunnerMapsEveryField：座位请求 ↔ 跨层 DTO 必须逐字段对应。
- `func TestRoleTurnSeatTakesRoundInputFromContext(t *testing.T)` — TestRoleTurnSeatTakesRoundInputFromContext：座位在装配期构造、不持有"这一轮发生
- `func TestRoleTurnSeatKeepsExplicitInput(t *testing.T)` — TestRoleTurnSeatKeepsExplicitInput：请求里已显式给的输入优先于 ctx（显式选择
- `func TestRoleTurnRunnerRequiresPort(t *testing.T)` — TestRoleTurnRunnerRequiresPort：未装配端口 = 试水形态（agent 角色只占发言位、
