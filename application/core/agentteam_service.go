// agentteam_service.go 把 AgentTeam 工厂/角色注册表接到 Application 能力面。
//
// 边界（AGENTS.md §1、arch 稿 §7）：application 只做**窄转发 + DTO 适配**，
// 不解释顺序/幂等语义；角色会话、注册表与 lifecycle 顺序分别由 sessionstore
// 的对应入口原子发布。headless/前端只消费 dto.TeamView / dto.TeamRegistry。
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// agentTeamPort 是会话端口可选实现的 A2A 角色管理面。生产实现为
// internal/adapters.SessionPort（DTO 形态，不把 sessionstore 类型漏给 application）。
type agentTeamPort interface {
	EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)
	ReadLifecycleOrder(sessionID string) (string, []string, error)
	ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error)
	WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error
}

// agentTeamFloorPort 是会话端口**可选**实现的 floor 读面（message head 当前发言
// 角色）。宿主没实现时成员表不填 floor_role——旧宿主能照常装配，不因缺一个读面
// 而整个 Agent Team 面板报错。
type agentTeamFloorPort interface {
	ReadFloorRole(mainSessionID string) (string, error)
}

// agentTeamDismissPort 是会话端口**可选**实现的离场面（删除角色注册表）。
// 与 floor 读面同一取舍：未实现的宿主照常装配/读取，只是不支持"团队离场"。
type agentTeamDismissPort interface {
	RemoveTeamRegistry(mainSessionID string) error
}

// agentTeamAdapter 把会话端口（DTO 形态）适配为 agentteam.Port。
// 顺序读写复用既有 contract.RoleSessionPort.SetLifecycleOrder，避免第二套写入口。
type agentTeamAdapter struct {
	port agentTeamPort
	role contract.RoleSessionPort
	// employees 是装配期分配员工权限的写面（未装配 → 只装配不分配，判定按档位默认）。
	employees contract.EmployeePermissionPort
}

func (adapter agentTeamAdapter) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	return adapter.port.EnsureRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (adapter agentTeamAdapter) ReadLifecycleOrder(sessionID string) (string, []string, error) {
	return adapter.port.ReadLifecycleOrder(sessionID)
}

func (adapter agentTeamAdapter) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return adapter.role.SetLifecycleOrder(sessionID, policy, roles)
}

func (adapter agentTeamAdapter) ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error) {
	return adapter.port.ReadTeamRegistry(mainSessionID)
}

func (adapter agentTeamAdapter) WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error {
	if err := adapter.port.WriteTeamRegistry(mainSessionID, registry); err != nil {
		return err
	}
	// 装配期**同时**分配员工权限：员工是在这一刻进团的（谁在编、什么权责都在
	// registry 里），权限分配必须与它同一个动作——否则会出现"已经在编、权限还没
	// 分配"的窗口，而员工回合在该窗口里按什么判都没有依据。
	//
	// 分配失败显式上抛（不静默继续）：注册表已经写下去了，但"员工权限没落上"
	// 必须让装配方看见——静默继续等于让员工按默认口径跑，而调用方以为已经分配。
	if adapter.employees != nil {
		if err := adapter.employees.AssignEmployeePermissions(registry.Roles); err != nil {
			return fmt.Errorf("分配员工权限: %w", err)
		}
	}
	return nil
}

// RemoveTeamRegistry 实现 agentteam.DismissPort：宿主端口暴露离场面时才转调，
// 否则显式报错——"未实现"不能被当成"已经走人"（否则调用方会以为团队已离场，
// 而面板里它还挂着）。
func (adapter agentTeamAdapter) RemoveTeamRegistry(mainSessionID string) error {
	port, ok := adapter.port.(agentTeamDismissPort)
	if !ok {
		return errors.New("session port does not expose agent team removal")
	}
	return port.RemoveTeamRegistry(mainSessionID)
}

// ReadFloorRole 实现 agentteam.FloorPort：宿主端口实现了 floor 读面才转读，
// 否则返回空串（成员表 floor 留空，不报错）。
func (adapter agentTeamAdapter) ReadFloorRole(mainSessionID string) (string, error) {
	floor, ok := adapter.port.(agentTeamFloorPort)
	if !ok {
		return "", nil
	}
	return floor.ReadFloorRole(mainSessionID)
}

func (service *Service) agentTeamPorts() (agentTeamPort, contract.RoleSessionPort, error) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, nil, errors.New("agent team storage is not assembled")
	}
	role, ok := service.Deps.Sessions.(contract.RoleSessionPort)
	if !ok {
		return nil, nil, errors.New("session port does not expose role session storage")
	}
	port, ok := service.Deps.Sessions.(agentTeamPort)
	if !ok {
		return nil, nil, errors.New("session port does not expose agent team role registry")
	}
	return port, role, nil
}

func (service *Service) agentTeamFactory() (*agentteam.Factory, error) {
	port, role, err := service.agentTeamPorts()
	if err != nil {
		return nil, err
	}
	return agentteam.NewFactory(agentTeamAdapter{port: port, role: role, employees: service.Deps.EmployeePermissions})
}

func (service *Service) agentTeamRegistry() (*agentteam.Registry, error) {
	port, role, err := service.agentTeamPorts()
	if err != nil {
		return nil, err
	}
	return agentteam.NewRegistry(agentTeamAdapter{port: port, role: role, employees: service.Deps.EmployeePermissions})
}

// publishTeamChanged 通告"会话团队事实变了"（装配/工作顺序/入职/编辑成员都要发）。
//
// 为什么必须发：Agent Team 面板的数据（成员表/顺序/调度）**不在**会话快照里，
// 两个前端都按需 RPC 拉取（GUI 的 status 子页 Agent Team 区块、TUI 的 Alt+T），
// 并且各自把上一次读回的结果按会话键缓存。没有这条事件时，只有"发起这次装配的
// 那个前端动作"会主动重取（GUI 曾靠 composer 文本以 `@` 开头来猜），因此
// goal 自动装配、面板收起后再展开、另一个前端触发的装配都会让面板停在旧成员表。
//
// revision 记 0（同 chat.changed 口径）：载荷不在快照里，带上 revision 会被
// 协议层的"快照已表示"陈旧判据丢掉，而面板缓存并不随快照翻转。
func (service *Service) publishTeamChanged(mainSessionID string) {
	if service == nil || strings.TrimSpace(mainSessionID) == "" {
		return
	}
	service.publishSessionEvent(EventTeamChanged, 0, "", mainSessionID, nil)
}

// MaterializeAgentTeam 按一份 TeamSpec 装配一支 AgentTeam。
// joinSeq 是本次装配把角色挂到主会话的可见起点。
func (service *Service) MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	factory, err := service.agentTeamFactory()
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	result, err := factory.Materialize(mainSessionID, spec, joinSeq)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	// 装配即建环：新团队的"下一个谁发言"立即可观测（并带上逃生上限）。
	service.teamRuntimeFor(mainSessionID, result.View)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		result.View.Schedule = schedule
	}
	// 装配是所有来路（`@` 召唤 / 团队库条目 / RPC 一键装配）的共同收口：面板事实
	// 只在这里之后成立，因此通告也钉在这里，而不是散在各个入口。
	service.publishTeamChanged(mainSessionID)
	return result, nil
}

// DismissAgentTeam 让本会话的在编团队离场（"干完就走人"）：删注册表 + 复位顺序，
// 随后同步发言环并通告面板。
//
// 与 MaterializeAgentTeam 对称：装配是所有来路的共同收口（并在这里发 team.changed），
// 离场同样只有这一个收口——`@` 召唤的团队、前端一键装配的团队都按同一条口径离场，
// 不新增第二份"谁还算在编"的事实。
func (service *Service) DismissAgentTeam(mainSessionID string) error {
	if service == nil || strings.TrimSpace(mainSessionID) == "" {
		return errors.New("agent team: main session ID is required")
	}
	factory, err := service.agentTeamFactory()
	if err != nil {
		return err
	}
	if err := factory.Dismiss(mainSessionID); err != nil {
		return err
	}
	// 环是派生状态：注册表没了，环也要按新事实重建（空顺序 = 没有成员可发言）。
	service.syncTeamRuntime(mainSessionID)
	service.publishTeamChanged(mainSessionID)
	return nil
}

// AgentTeamView 返回成员表（身份/顺序/定时分区/配置状态/发言调度运行态）。
func (service *Service) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
	view, err := service.agentTeamRawView(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	// 读路径也建环：前端「下一个谁发言」需要运行态；建环只读事实（链表顺序来自
	// lifecycle），不写盘、不新增第二份顺序。读路径**不**发 team.changed：
	// 那是"事实变了"的通告，在读到事实时发会退化成"面板重取→再通告"的自激循环。
	service.teamRuntimeFor(mainSessionID, view)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		view.Schedule = schedule
	}
	return view, nil
}

// agentTeamRawView 返回不带运行态的成员表（装配面内部用；避免
// teamRuntimeFor ← AgentTeamView 的互相递归）。
func (service *Service) agentTeamRawView(mainSessionID string) (dto.TeamView, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamView{}, err
	}
	view, err := registry.View(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	// 每读一次注册表，就把"角色会话 → 归属主会话 + 权责"记进反向索引：权限门、审计、
	// 角色回合执行体都只拿得到角色会话号，它们要问出归属只能靠这份索引。
	//
	// 缺这一步的后果是**权限面 fail-open**：索引恒空 → 按角色会话号解析恒 false →
	// 员工的角色会话一律按 root 判（不拦），而"谁是员工"这件事只有在 ctx 里显式
	// 带着主体时才成立。验收见 agentteam_role_index_test.go。
	// 写入点在**读面**（而不是写面）是刻意的：顺序/成员/权责的唯一事实是注册表，
	// 读面知道的就是最新事实；写面反而可能读到未落盘的中间态。
	service.roleSessions.remember(mainSessionID, view)
	return view, nil
}

// AgentTeamPutRole 新增/覆盖一个角色配置。
func (service *Service) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	stored, err := registry.PutRole(mainSessionID, role)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	service.publishTeamChanged(mainSessionID)
	return stored, nil
}

// AgentTeamDeleteRole 删除一个角色配置（并把它从工作顺序里摘除）。
func (service *Service) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	stored, err := registry.DeleteRole(mainSessionID, roleName)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	service.publishTeamChanged(mainSessionID)
	return stored, nil
}

// AgentTeamSetOrder 写工作顺序（前端拖拽/上下移只提交这个字段）。顺序是环的
// 唯一事实来源，写完必须同步环——否则"下一个谁发言"会停在旧次序上。
func (service *Service) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamView{}, err
	}
	view, err := registry.SetOrder(mainSessionID, policy, orderRoles)
	if err != nil {
		return dto.TeamView{}, err
	}
	service.teamRuntimeFor(mainSessionID, view)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		view.Schedule = schedule
	}
	// 顺序是环的唯一事实来源，也是面板"发言顺序"列的内容：写完就通告。
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamInstantiateRole 一步实例化一个角色（"员工入职"）：规整配置 → 幂等建
// 角色会话 → 落注册表 → 按 join_policy 决定是否进工作顺序 → 报执行者绑定。
// 这是 Agent Team 管理面的"增加员工"入口；修改员工走同一条路径（按 role_name
// 覆盖），因此前端不需要"新增/编辑"两套调用。
func (service *Service) AgentTeamInstantiateRole(mainSessionID string, role dto.RoleSpec, joinSeq uint64) (dto.RoleInstantiation, error) {
	factory, err := service.agentTeamFactory()
	if err != nil {
		return dto.RoleInstantiation{}, err
	}
	result, err := factory.InstantiateRole(mainSessionID, role, joinSeq)
	if err != nil {
		return dto.RoleInstantiation{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	// 入职/改员工都会改成员表（这是面板"增加员工"入口），装配面据此通告。
	service.publishTeamChanged(mainSessionID)
	return result, nil
}

// agentTeamLibraryPort 是会话端口可选实现的**项目级团队库**读写面。项目作用域由
// 端口实现方从锚定会话解析（与角色注册表同一套解析），application 不解释路径。
type agentTeamLibraryPort interface {
	ReadTeamLibrary(mainSessionID string) (dto.TeamLibrary, error)
	WriteTeamLibrary(mainSessionID string, library dto.TeamLibrary) error
}

// agentTeamLibraryAdapter 把"项目级团队库端口 + 锚定会话"适配成
// agentteam.LibraryPort（锚定会话在构造时绑定，库读写面本身保持无参）。
type agentTeamLibraryAdapter struct {
	port          agentTeamLibraryPort
	mainSessionID string
}

func (adapter agentTeamLibraryAdapter) ReadTeamLibrary() (dto.TeamLibrary, error) {
	return adapter.port.ReadTeamLibrary(adapter.mainSessionID)
}

func (adapter agentTeamLibraryAdapter) WriteTeamLibrary(library dto.TeamLibrary) error {
	return adapter.port.WriteTeamLibrary(adapter.mainSessionID, library)
}

// agentTeamLibrary 构造团队库读写面（锚定会话 = 作用域解析入口）。
func (service *Service) agentTeamLibrary(mainSessionID string) (*agentteam.Library, error) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, errors.New("agent team storage is not assembled")
	}
	port, ok := service.Deps.Sessions.(agentTeamLibraryPort)
	if !ok {
		return nil, errors.New("session port does not expose agent team library storage")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return nil, errors.New("main session ID is required")
	}
	return agentteam.NewLibrary(agentTeamLibraryAdapter{port: port, mainSessionID: mainSessionID})
}

// agentTeamGlobalPort 是会话端口可选实现的**全局母本**（员工库 + 默认顺序）读写面。
// 全局作用域由端口实现方解析（数据根下的 team/），application 不解释路径。
type agentTeamGlobalPort interface {
	ReadEmployeeLibrary(mainSessionID string) (dto.EmployeeLibrary, error)
	WriteEmployeeLibrary(mainSessionID string, library dto.EmployeeLibrary) error
	ReadDefaultOrder(mainSessionID string) (dto.DefaultOrder, error)
	WriteDefaultOrder(mainSessionID string, order dto.DefaultOrder) error
}

// agentTeamGlobalAdapter 把"全局母本端口 + 锚定会话"适配成 agentteam.GlobalPort。
type agentTeamGlobalAdapter struct {
	port          agentTeamGlobalPort
	mainSessionID string
}

func (adapter agentTeamGlobalAdapter) ReadEmployeeLibrary() (dto.EmployeeLibrary, error) {
	return adapter.port.ReadEmployeeLibrary(adapter.mainSessionID)
}

func (adapter agentTeamGlobalAdapter) WriteEmployeeLibrary(library dto.EmployeeLibrary) error {
	return adapter.port.WriteEmployeeLibrary(adapter.mainSessionID, library)
}

func (adapter agentTeamGlobalAdapter) ReadDefaultOrder() (dto.DefaultOrder, error) {
	return adapter.port.ReadDefaultOrder(adapter.mainSessionID)
}

func (adapter agentTeamGlobalAdapter) WriteDefaultOrder(order dto.DefaultOrder) error {
	return adapter.port.WriteDefaultOrder(adapter.mainSessionID, order)
}

// agentTeamGlobal 构造全局母本（员工库 + 默认顺序）读写面。
func (service *Service) agentTeamGlobal(mainSessionID string) (*agentteam.Global, error) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, errors.New("agent team storage is not assembled")
	}
	port, ok := service.Deps.Sessions.(agentTeamGlobalPort)
	if !ok {
		return nil, errors.New("session port does not expose agent team global config storage")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return nil, errors.New("main session ID is required")
	}
	return agentteam.NewGlobal(agentTeamGlobalAdapter{port: port, mainSessionID: mainSessionID})
}

// AgentTeamLibrary 返回项目团队库（团队模板清单）。未建库返回空库（不是错误）。
//
// 这是团队库的**公共读回**面：读回的份就是磁盘上的权威，所以顺手让 `@` 的建议面
// 快照过期（面板 RPC / `@` 空参回执都经这里）——删缓存的代价只是下一次按键多读
// 一次盘，比让建议面停在旧库便宜。失败路径同样清：读不到时缓存里那份更不可信。
func (service *Service) AgentTeamLibrary(mainSessionID string) (dto.TeamLibrary, error) {
	defer service.invalidateTeamLibrarySuggestions()
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	return library.View()
}

// AgentTeamSaveTeam 新增/覆盖一条团队库条目（按 team_id 幂等），返回整份库。
func (service *Service) AgentTeamSaveTeam(mainSessionID string, entry dto.TeamLibraryEntry) (dto.TeamLibrary, error) {
	defer service.invalidateTeamLibrarySuggestions()
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	view, err := library.SaveTeam(entry)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	// 母本 CRUD 也发 team.changed：团队库是面板数据的一部分（团队库表），
	// 写它同样让"面板缓存"过期——不发的旧口径只在会话装配面发声，库的增删改
	// 只能靠发起方自己重取，别的观察者（另一窗口/切回来）会停在旧库。
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamSaveCurrentTeam 把当前会话在编的员工表存成一条团队库条目
// （UI「把当前团队存进团队库」：团队库因此能有用户自己的团队）。
func (service *Service) AgentTeamSaveCurrentTeam(mainSessionID, name, teamID string) (dto.TeamLibrary, error) {
	defer service.invalidateTeamLibrarySuggestions()
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	stored, err := registry.Stored(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	// 库条目要带会话的**实际**发言次序（lifecycle.order_roles），而不是按
	// OrderPriority 推导的次序。
	port, _, err := service.agentTeamPorts()
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	_, orderRoles, err := port.ReadLifecycleOrder(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	entry, err := agentteam.EntryFromRegistry(stored, orderRoles, name, teamID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	view, err := library.SaveTeam(entry)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamDeleteTeam 删除一条团队库条目（幂等）。
func (service *Service) AgentTeamDeleteTeam(mainSessionID, teamID string) (dto.TeamLibrary, error) {
	defer service.invalidateTeamLibrarySuggestions()
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	view, err := library.DeleteTeam(teamID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamMaterializeTeam 把团队库里的一支团队装配到会话：库条目 → TeamSpec →
// 既有工厂（建角色会话 + 写 registry + 写 lifecycle 顺序）。装配后的员工随即
// 出现在员工栏，可以继续单独编辑。
func (service *Service) AgentTeamMaterializeTeam(mainSessionID, teamID string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	entry, err := library.Entry(teamID)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	return service.MaterializeAgentTeam(mainSessionID, agentteam.SpecOfEntry(entry), joinSeq)
}

// ── 全局母本：团队库 / 员工库 / 默认顺序 ──────────────────────────────
//
// 并发口径（用户口径）：母本的**写**落全局文件；**读**在会话侧深拷贝成私有副本。
// 会话内的入职/改序只写会话副本（registry + lifecycle head），不碰全局；只有
// 「确认普及搭配到全局」才会把会话 {员工, 顺序} 回写母本。

// AgentTeamGlobalConfig 读全局母本（团队库 / 员工库 / 默认顺序）与当前会话副本的
// 搭配投影。只读：读母本是深拷贝，不落盘、不隐式迁移旧布局。
//
// 读回团队库的那一步与 AgentTeamLibrary 同口径：让 `@` 的建议面快照过期（这条也是
// 面板刷新母本的入口，用户刚在面板里看到的库就是缓存该重取的那一份）。
func (service *Service) AgentTeamGlobalConfig(mainSessionID string) (dto.TeamGlobalConfig, error) {
	defer service.invalidateTeamLibrarySuggestions()
	global, err := service.agentTeamGlobal(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	config := dto.TeamGlobalConfig{}
	if config.Library, err = library.View(); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	if config.Employees, err = global.Employees(); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	if config.Order, err = global.Order(); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	if config.Composition, err = service.agentTeamComposition(mainSessionID); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	return config, nil
}

// agentTeamComposition 投影"当前会话副本"的搭配：在编员工（不含 user/main）+ 实际
// 发言次序。只读：顺序取 lifecycle head（运行时权威），未编排过时按注册表推导。
func (service *Service) agentTeamComposition(mainSessionID string) (dto.TeamComposition, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamComposition{}, err
	}
	view, err := registry.View(mainSessionID)
	if err != nil {
		return dto.TeamComposition{}, err
	}
	stored, err := registry.Stored(mainSessionID)
	if err != nil {
		return dto.TeamComposition{}, err
	}
	employees := make([]dto.RoleSpec, 0, len(stored.Roles))
	for _, role := range stored.Roles {
		if agentteam.IsBuiltinRole(role.RoleName) {
			continue
		}
		employees = append(employees, role)
	}
	return dto.TeamComposition{
		SessionID:   view.SessionID,
		TeamID:      view.TeamID,
		TeamKind:    view.TeamKind,
		OrderPolicy: view.OrderPolicy,
		OrderRoles:  append([]string(nil), view.OrderRoles...),
		Employees:   employees,
	}, nil
}

// AgentTeamSaveEmployee 新增/覆盖全局员工库里的一个员工（按 role_name 幂等）。
// 这是"库管理"动作，写的是全局母本；会话内入职走 AgentTeamInstantiateRole。
func (service *Service) AgentTeamSaveEmployee(mainSessionID string, role dto.RoleSpec) (dto.EmployeeLibrary, error) {
	global, err := service.agentTeamGlobal(mainSessionID)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	view, err := global.SaveEmployee(role)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamDeleteEmployee 删除全局员工库里的一个员工（幂等）。
func (service *Service) AgentTeamDeleteEmployee(mainSessionID, roleName string) (dto.EmployeeLibrary, error) {
	global, err := service.agentTeamGlobal(mainSessionID)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	view, err := global.DeleteEmployee(roleName)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamSetDefaultOrder 写全局默认顺序（母本发言次序）。
func (service *Service) AgentTeamSetDefaultOrder(mainSessionID, policy string, orderRoles []string) (dto.DefaultOrder, error) {
	global, err := service.agentTeamGlobal(mainSessionID)
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	view, err := global.SetOrder(policy, orderRoles)
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	service.publishTeamChanged(mainSessionID)
	return view, nil
}

// AgentTeamPublishToGlobal 是「确认·普及搭配到全局」：把当前会话副本的
// {在编员工, 发言顺序} 写回全局母本——员工按 role_name 幂等写入员工库、顺序写入
// 默认顺序，并把这份搭配存成一条全局团队库条目。只有这一步会改全局。
func (service *Service) AgentTeamPublishToGlobal(mainSessionID, name, teamID string) (dto.TeamGlobalConfig, error) {
	defer service.invalidateTeamLibrarySuggestions()
	global, err := service.agentTeamGlobal(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	library, err := service.agentTeamLibrary(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	composition, err := service.agentTeamComposition(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	if len(composition.Employees) == 0 {
		return dto.TeamGlobalConfig{}, errors.New("当前会话没有可普及的员工：先在员工栏入职或装配一支团队")
	}
	orderRoles := composition.OrderRoles
	if len(orderRoles) == 0 {
		// 会话还没编排过顺序：用注册表推导的次序，不把空顺序写进母本。
		orderRoles = agentteam.OrderRolesOf(composition.Employees)
	}
	for _, role := range composition.Employees {
		if _, err := global.SaveEmployee(role); err != nil {
			return dto.TeamGlobalConfig{}, err
		}
	}
	// 顺序要在员工之后写：默认顺序的规整会按员工库过滤角色名。
	if _, err := global.SetOrder(composition.OrderPolicy, orderRoles); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	storedRegistry, err := registry.Stored(mainSessionID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	entry, err := agentteam.EntryFromRegistry(storedRegistry, orderRoles, name, teamID)
	if err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	if _, err := library.SaveTeam(entry); err != nil {
		return dto.TeamGlobalConfig{}, err
	}
	// 普及是母本写：发一条 team.changed，让面板（员工库/团队库/默认顺序）热更新。
	service.publishTeamChanged(mainSessionID)
	return service.AgentTeamGlobalConfig(mainSessionID)
}

// AgentTeamRolePrompt 读某个角色登记的提示词（空 = 未登记）。ADVISOR 回合用它
// 取"已装配的员工提示词"；这是只读查询，不建环、不改任何事实。
func (service *Service) AgentTeamRolePrompt(mainSessionID, roleName string) (string, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return "", err
	}
	return registry.PromptFor(mainSessionID, roleName)
}

// AgentTeamRolePlugins 读某个角色登记的**按会话插件装配**（RoleSpec.Plugins；
// nil = 未登记 = 不覆盖：工具面继承宿主当前装配 + 技能目录不注入）。
//
// 与 AgentTeamRolePrompt 同构：只读查询，不建环、不改任何事实。装配根把它注入
// seelebridge（SetRolePluginsProvider），角色回合据此在自己的 ctx 上装配能力面。
func (service *Service) AgentTeamRolePlugins(mainSessionID, roleName string) ([]string, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return nil, err
	}
	return registry.PluginsFor(mainSessionID, roleName)
}

// AgentTeamOptimizeRolePrompt 跑一次有界 LLM 回合优化员工提示词（不写会话消息、
// 不落盘：落盘仍走入职/保存）。未装配提示词优化端口时返回可展示错误。
//
// 上下文补全：请求没带 team_kind/order_roles 时从当前会话补（优化质量依赖
// "这个员工在一支什么队伍里"）；调用方给的提示词原文原样透传。
func (service *Service) AgentTeamOptimizeRolePrompt(ctx context.Context, mainSessionID string, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	if service == nil || service.Deps.RolePrompt == nil {
		return dto.RolePromptOptimizeResult{}, errors.New("角色提示词优化未装配（需要 LLM completer）")
	}
	if strings.TrimSpace(request.RoleName) == "" {
		return dto.RolePromptOptimizeResult{}, errors.New("role_name is required")
	}
	if strings.TrimSpace(request.SystemPrompt) == "" {
		return dto.RolePromptOptimizeResult{}, errors.New("system_prompt is required")
	}
	if strings.TrimSpace(request.TeamKind) == "" {
		if view, err := service.agentTeamRawView(mainSessionID); err == nil {
			request.TeamKind = view.TeamKind
			if len(request.OrderRoles) == 0 {
				request.OrderRoles = append([]string(nil), view.OrderRoles...)
			}
		}
	}
	result, err := service.Deps.RolePrompt.OptimizeRolePrompt(ctx, request)
	if err != nil {
		return dto.RolePromptOptimizeResult{}, err
	}
	if strings.TrimSpace(result.RoleName) == "" {
		result.RoleName = strings.TrimSpace(request.RoleName)
	}
	if strings.TrimSpace(result.Original) == "" {
		result.Original = strings.TrimSpace(request.SystemPrompt)
	}
	return result, nil
}
