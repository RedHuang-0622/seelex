package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"sort"
	"strings"
	"sync"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// RegistryState 包装 framework tools.Registry：内联工具 provider 由
// AddInline 累积维护，注册表快照在每次增删后重建。
type RegistryState struct {
	Registry *frameworktools.Registry
	inline   *InlineProvider
	// undeclared 是"注册了但没有簇属声明"的工具名（打点 K-0 的读面）：名字不在
	// 路由组表里 ⇒ DeclaredToolMeta 派生不出 Groups。它不是错误（动态 MCP /
	// 第三方插件工具本来就无从声明），但**必须是可观测的**——一张看得见的清单
	// 才拦得住"新工具忘了分封、于是静默降级成按名字路由/默认审批"。
	undeclaredMu sync.Mutex
	undeclared   map[string]bool
}

// NewRegistryState 构造带超时/权限门/传输选择/事件与诊断中间件的工具注册表。
//
// 事件、路径门、权限门与传输选择走 MetaMiddleware（权限判定要吃工具自带的簇属
// ToolMeta），诊断留在普通链上。框架先套普通中间件、再按顺序套 Meta 中间件，因此
// 实际嵌套是：
//
//	事件 → **执行面决策** → 路径门 → 权限门 → **建连（传输选择）** → 诊断 → handler
//
// 三处前移各自的理由：
//   - 路径门在权限门之前（P1，越界路径直接拒，不先弹一次没用的审批）；
//   - 执行面决策在路径门**之前**（决策段）：路径门要按面取根，而"这次走不走远端"
//     在旧链序里要等权限门之后才知道——于是会话固定到 SSH 的调用被本地根误拒。决策
//     段只读（配置 + 探针缓存），绝不建连，因此前移不改"被拒的调用不许对外握手"；
//   - 传输选择（建连段）仍在权限门**之后**、诊断之前：越权/被拒的调用不白连一次
//     远端，而诊断看到的是已经选完的那次调用。
//
// 决策段与建连段是**同一份中间件装在两处**（transportMiddleware），靠 ctx 段标记
// 区分本次是哪一段（见 transport_decision.go 的 TransportDecisionStage/DialStage）：
// 这样装配根的签名不变，而链序拿到了它需要的第三个身份。transport 为 nil 时链上
// 逐字等于旧顺序。
func NewRegistryState(timeout time.Duration, permission *PermissionGate, approvalTimeout time.Duration, pathGate frameworktools.MetaMiddleware, transportMiddleware frameworktools.MetaMiddleware, eventMiddleware, diagnosticMiddleware frameworktools.Middleware) *RegistryState {
	return &RegistryState{Registry: frameworktools.NewRegistry(
		frameworktools.WithCallTimeout(timeout),
		frameworktools.WithMiddleware(diagnosticMiddleware),
		frameworktools.WithMetaMiddleware(
			asMetaMiddleware(eventMiddleware),
			TransportDecisionStage(transportMiddleware),
			pathGate,
			permission.Middleware(approvalTimeout),
			TransportDialStage(transportMiddleware),
		),
	)}
}

// asMetaMiddleware 把不吃簇属的普通中间件提升为 MetaMiddleware（忽略 meta）。
func asMetaMiddleware(mw frameworktools.Middleware) frameworktools.MetaMiddleware {
	if mw == nil {
		return nil
	}
	return func(name string, _ frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return mw(name, next)
	}
}

// AddInline 注册一个普通产品工具（等价旧 holder.RegisterInline，重名覆盖）。
//
// 簇属（ToolMeta）在这里**无条件填上**：声明来自 DeclaredToolMeta（路由组表是
// 单一事实源，见 tool_meta.go）。填表是打点 K-0 的第一步——在此之前 Meta 恒为
// nil，权限判定只能退回按名字路由，声明的能力面（control 类仅 root 可路由、
// 资源限定）全部读不到。
func (s *RegistryState) AddInline(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if s == nil || s.Registry == nil {
		return
	}
	if s.inline == nil {
		s.inline = &InlineProvider{}
		_ = s.Registry.Register(s.inline)
	}
	meta := policy.DeclaredToolMeta(name)
	if len(meta.Groups) == 0 {
		s.noteUndeclared(name)
	}
	s.inline.upsert(frameworktools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name: name, Description: description, Parameters: inputSchema,
			},
		},
		Handler: frameworktools.HandlerFunc(handler),
		Meta:    &meta,
	})
	// 重建快照使新工具立即可见（注册表只读锁调度，快照重建线程安全）。
	_ = s.Registry.Unregister(s.inline.ProviderName())
	_ = s.Registry.Register(s.inline)
}

// noteUndeclared 记下"没有簇属声明"的工具名（幂等）。
func (s *RegistryState) noteUndeclared(name string) {
	s.undeclaredMu.Lock()
	defer s.undeclaredMu.Unlock()
	if s.undeclared == nil {
		s.undeclared = map[string]bool{}
	}
	s.undeclared[name] = true
}

// UndeclaredTools 返回没有簇属声明的工具名（稳定排序，供用例/诊断读取）。
//
// 判据（打点 K-0）：**静态工具面必须为空**——seelex 自己注册的每个工具都该在
// 路由组表里分封；非空即意味着"这个名字没有权限策略、也没有并发分类"。
// 动态第三方工具（MCP / 插件）不在这条判据内：它们的名字在装配期不存在，
// 执行口径仍是"未分封 = 按框架默认走审批"（既有语义，未改）。
func (s *RegistryState) UndeclaredTools() []string {
	if s == nil {
		return nil
	}
	s.undeclaredMu.Lock()
	defer s.undeclaredMu.Unlock()
	names := make([]string, 0, len(s.undeclared))
	for name := range s.undeclared {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InlineMetas 返回已注册内联工具的簇属（名字 → 声明），供用例断言"全量填表"。
func (s *RegistryState) InlineMetas() map[string]frameworktools.ToolMeta {
	if s == nil || s.inline == nil {
		return nil
	}
	metas := map[string]frameworktools.ToolMeta{}
	for _, entry := range s.inline.Tools() {
		if entry.Meta == nil {
			metas[entry.Definition.Function.Name] = frameworktools.ToolMeta{}
			continue
		}
		metas[entry.Definition.Function.Name] = *entry.Meta
	}
	return metas
}

// FindTool 按名称在注册表中查找工具（plan 工具面读取）。
func (s *RegistryState) FindTool(name string) (types.Tool, bool) {
	if s == nil || s.Registry == nil {
		return types.Tool{}, false
	}
	for _, tool := range s.Registry.Tools() {
		if tool.Function.Name == name {
			return tool, true
		}
	}
	return types.Tool{}, false
}

// InlineProvider 累积 RegisterTool 注册的普通产品工具。
// framework tools.Registry 不允许同名工具重复，AddInline 在添加前按名称去重。
type InlineProvider struct {
	mu      sync.Mutex
	entries []frameworktools.ToolEntry
}

// ProviderName 返回内联 provider 名称。
func (p *InlineProvider) ProviderName() string { return "seelex-inline" }

// Tools 返回全部内联工具（只读拷贝）。
func (p *InlineProvider) Tools() []frameworktools.ToolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]frameworktools.ToolEntry(nil), p.entries...)
}

func (p *InlineProvider) upsert(entry frameworktools.ToolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.entries {
		if p.entries[index].Definition.Function.Name == entry.Definition.Function.Name {
			p.entries[index] = entry
			return
		}
	}
	p.entries = append(p.entries, entry)
}

// PermissionGate 是权限门控的可变状态：middleware 在注册表构造时闭包捕获
// 它，Set/SetPermissionTier*（兼容壳 SetFullAccess*）运行时原子更新。
//
// 权限档位（tier，full 档 = 旧的全权）是**会话级**的用户决定（application 侧
// 归属进 SessionUnit，G4）：执行面必须按工具调度 ctx 的会话归属解析，绝不能把
// 它放进程级上——进程级只有一个格子有两个真实后果：
//   - 污染：A 会话点全权 → B 会话的工具调用被静默放行（B 未同意）；
//   - 失灵：B 会话的 chat 起点同步（syncFullAccessFor）会把 A 的全权关掉，
//     表现为「点了全权仍弹审批/仍被拒」——多会话并行下必现（单飞时代只是
//     侥幸没暴露）。
type PermissionGate struct {
	mu      sync.RWMutex
	checker *toolspermission.PermissionChecker
	handler toolspermission.ApprovalHandler
	// cfg 是最近一次 Set 的权限配置快照：主体类策略（Enforcer）用它做路由与
	// 分封判断，保证"谁有权限"只有一份事实（分组表 + 主体授权表）。
	cfg toolspermission.PermissionConfig
	// RoleSessionPolicy 是"角色会话（员工）→ ToolsPolicy"的解析器，由组合根
	// （main.go，读角色注册表）注入；nil = 不做员工主体识别（全部按 root 判）。
	// 它只被非子代理调用使用，结果按 rolePolicyCacheTTL 缓存。
	RoleSessionPolicy func(sessionID string) (string, bool)
	// rolePolicyCache 缓存 RoleSessionPolicy 的结果（含未命中），由 mu 保护。
	rolePolicyCache map[string]rolePolicyCacheEntry
	// RoleSessionOwner 把"角色会话（员工/评审者）"折算成**宿主主会话**：越权提权的
	// 审批按宿主主会话归属呈现，复用现有审批面板（视图单格 + 目录 awaiting_approval
	// + 会话快照），而不是落在一个没有面板的角色会话号上——那会让"需要人类点头的
	// 工具"永远等不到人点，只能等到超时被拒（= 继承全量工具的员工的越权面事实上
	// 没有 sudo 口令可用）。
	//
	// 由组合根（main.go，读角色注册表反查索引）注入；nil = 审批按调用会话原样归属。
	// 它只影响**审批呈现归属**，不影响主体判定（判定读 SessionFromContext 的原始
	// 会话号），因此"审批在哪个面板弹"与"按谁判"是两件互不干扰的事。
	RoleSessionOwner func(sessionID string) (string, bool)
	// sessionTier 是会话级**权限档位**选择（键 = 会话 ID，值 = 档位 id）。空串键是
	// **进程级默认**（CLI -permission / 未做会话级选择的回退面）；未选择的会话回退
	// 进程默认，不继承别的会话的档位。
	//
	// 档位是**一份权限向量预设**（见 policy.TierVector）：执行面把它展开成"组 → 位"
	// 后自己判（详见 Enforce）。因此这里只剩一个字符串选择，不再有"按档位预构建的
	// 规则表"——一份判据、一份表。
	sessionTier map[string]string
	// RolePermissionVector 是 (会话, 角色) → **生效权限向量**的读面，由组合根注入
	// （实现方是 application/core 的 RolePermissionVector：登记优先、缺失按四级链路
	// 解析）。nil = 没接上读面，员工侧按 ctx 身份自带的格子/档位派生（不落共享表）。
	//
	// 它取代了旧的"运行时把派生授权写回 cfg.Subjects 并重建 checker"那条路：
	// 判定所需的格位在一次调用的局部量里算出来，共享表只读。
	RolePermissionVector rolePermissionVectorResolver
	// groupNames 是**当前配置**里存在的路由组名集合（rebuildLocked 时重建）。
	// 用途见 sanitizeMeta：工具声明的簇若不在本次配置里，声明必须作废——否则
	// checker 的 DecideForMeta 查不到组、所需位退化成 0，判定结果是**放行**。
	groupNames map[string]bool
	// SessionFromContext 从工具调度 ctx 提取会话归属（seelebridge 根包
	// 注入 seelebridge 会话路由键；nil = 权限审批保持进程级空归属回退）。
	SessionFromContext func(ctx context.Context) string
	// employeePermissions 是装配期分配过的员工权限（主键 = 员工主体 emp_<角色>）。
	//
	// 它是**显式分配的唯一存储**，只读地参与判定：Enforce 先查这里（显式分配优先），
	// 查不到才按 ctx 身份（显式格子 → 档位）派生。**绝不写回 cfg.Subjects**——
	// 共享表写回 + 重建 checker 是旧的第二条判据（见 permission_policy.go 的
	// employeeVector 注释）。
	employeePermissions map[toolspermission.Subject]EmployeePermission
}

// Set 装配权限配置与审批处理器。checker 只承载 base 规则：档位不再复制规则表
// （向量预设见 policy.TierVector），因此 Set 只是"换表 + 重建 checker"，不再
// 需要把装配期的员工分配重放进共享表（那条重放路径已收掉：显式分配存在
// employeePermissions 里，判定时只读）。
func (state *PermissionGate) Set(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	state.mu.Lock()
	state.handler = handler
	state.cfg = cfg
	state.rebuildLocked()
	state.mu.Unlock()
}

// rebuildLocked 重建 base checker 与组名集合（调用方持锁）。
//
// 这是 checker 装配的**唯一**落点：cfg 一变就整体重建。旧的按档位预构建
// （tierCheckers）已随"档位 = 向量预设"退场——档位不再是一张规则表，而是
// 执行面判定的一个输入（PermissionTierFor → policy.TierVector）。
func (state *PermissionGate) rebuildLocked() {
	state.checker = toolspermission.NewPermissionChecker(state.cfg)
	state.groupNames = make(map[string]bool, len(state.cfg.Groups))
	for _, group := range state.cfg.Groups {
		state.groupNames[group.Name] = true
	}
}

// SetEmployeePermissions 把**装配期分配的员工权限**并入权责表：每个员工得到自己的
// 主体条目 emp_<角色名>——与用户（root）的权限同一张表、同一种形状（主体 × 路由组 × 位）。
//
// 与三档 ToolsPolicy 的关系：档位只是"装配时没单独分配"的默认值（EmployeePermission
// 的 Groups 为空时按 Policy 派生）；显式分组位永远优先。这样"给某个员工开一格能力"
// 不必再新增档位或改判定代码——改的就是那条主体条目。
//
// 校验：角色名为空 / 档位不是可分配口径（空、full、未识别）而没有显式分组位 → 报错。
// 装配路径宁可显式失败，也不要静默写进一条无意义的授权（那会变成"看起来分配了、
// 其实按宿主默认判"）。
func (state *PermissionGate) SetEmployeePermissions(permissions []EmployeePermission) error {
	prepared := make(map[toolspermission.Subject]EmployeePermission, len(permissions))
	for _, permission := range permissions {
		subject, ok := EmployeeSubject(permission.RoleName)
		if !ok {
			return fmt.Errorf("员工权限分配缺角色名（收到 %+v）", permission)
		}
		if _, err := permission.Grant(); err != nil {
			return err
		}
		prepared[subject] = permission
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(prepared) == 0 {
		return nil
	}
	if state.employeePermissions == nil {
		state.employeePermissions = make(map[toolspermission.Subject]EmployeePermission, len(prepared))
	}
	for subject, permission := range prepared {
		if _, err := permission.Grant(); err != nil {
			return err
		}
		// 只记分配事实（显式分配的唯一存储）。**不写 cfg.Subjects、不 rebuild**：
		// 判定所需的格位在 Enforce 里按这条记录现算（见 permission_policy.go 的
		// employeeVector），共享主体表因此只剩配置那一份来源。
		state.employeePermissions[subject] = permission
	}
	return nil
}

// EmployeePermissions 返回装配期分配过的员工权限（快照，供巡检/诊断读取）。
func (state *PermissionGate) EmployeePermissions() []EmployeePermission {
	if state == nil {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	permissions := make([]EmployeePermission, 0, len(state.employeePermissions))
	for _, permission := range state.employeePermissions {
		permissions = append(permissions, permission)
	}
	return permissions
}

// SetRolePermissionVectorResolver 注入 (会话, 角色) → **生效权限向量**的读面
// （application/core 的 RolePermissionVector：登记优先、缺失按四级链路解析）。
//
// 注入后，员工角色会话里的工具调用按这个向量判定（见 Enforce 的四条判据）；
// 传 nil 关闭读面，员工侧只按 ctx 身份自带的格子/档位派生。
func (state *PermissionGate) SetRolePermissionVectorResolver(resolver func(sessionID, role string) ([]string, bool, error)) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.RolePermissionVector = resolver
	state.mu.Unlock()
}

// SetRoleSessionPolicyResolver 注入"角色会话 → 员工权限口径（ToolsPolicy）"解析器。
// 注入后，员工角色会话里的工具调用按 ToolsPolicy 判成 emp_ro / emp_rw 主体：
// 位齐 → 组默认/规则，位缺（违权）→ 执行选择页面提权。传 nil 关闭员工主体识别。
func (state *PermissionGate) SetRoleSessionPolicyResolver(resolver func(sessionID string) (string, bool)) {
	state.mu.Lock()
	state.RoleSessionPolicy = resolver
	state.rolePolicyCache = nil // 解析器换人 → 缓存作废
	state.mu.Unlock()
}

// SetRoleSessionOwnerResolver 注入"角色会话（员工/评审者）→ 宿主主会话"的读面：
// 越权提权的审批按宿主主会话归属呈现在**现有审批面板**上（不新建面板、不改 UI）。
//
// 为什么必须折算（不是锦上添花）：角色会话（`goal-a2a-pm` / `advisor:<main>`）不是
// 用户视图里的会话，application 侧的审批归属（observeInteraction）只把"视图会话
// （或空归属）"的待批镜像进单格 Interaction——角色会话的待批既进不了单格，也没有
// 对应的会话单元承载 awaiting_approval，于是对宿主**完全不可见**，只能等审批超时
// 被拒。折算到宿主主会话后，面板/目录/会话快照三条既有读面原样复用。
//
// 传 nil = 审批按调用会话原样归属（旧行为；未接反查索引的宿主/桩不因此改变语义）。
func (state *PermissionGate) SetRoleSessionOwnerResolver(resolver func(sessionID string) (string, bool)) {
	state.mu.Lock()
	state.RoleSessionOwner = resolver
	state.mu.Unlock()
}

// approvalSessionFor 把调用会话折算成**审批呈现归属**：注入了读面且命中时用宿主
// 主会话，否则原样返回调用会话——"查不到归属"要知道这不是"没有会话"，按会话归属
// 的既有行为必须保持（后台会话待批仍按自己的会话归属，不冒充别人的）。
func (state *PermissionGate) approvalSessionFor(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	state.mu.RLock()
	resolver := state.RoleSessionOwner
	state.mu.RUnlock()
	if resolver == nil {
		return sessionID
	}
	owner, ok := resolver(sessionID)
	if !ok || strings.TrimSpace(owner) == "" {
		return sessionID
	}
	return strings.TrimSpace(owner)
}

// SetPermissionTierFor 设置指定会话的**权限档位**（空会话 ID = 进程级默认）。
// 只影响该会话自己的工具调度，不触碰其它会话的档位。未识别的档位 id 报错且
// **不改变**当前档位（写入侧显式失败，绝不把一次拼错静默落成 manual/full）。
func (state *PermissionGate) SetPermissionTierFor(sessionID, tier string) error {
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		return err
	}
	state.mu.Lock()
	if state.sessionTier == nil {
		state.sessionTier = make(map[string]string)
	}
	state.sessionTier[sessionID] = normalized
	state.mu.Unlock()
	return nil
}

// PermissionTierFor 返回指定会话生效的档位（会话级选择优先，未选择回退进程
// 默认；与 middleware 同源解析，供探针/诊断读取）。
func (state *PermissionGate) PermissionTierFor(sessionID string) string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.effectiveTierLocked(sessionID)
}

// PermissionTier 返回进程级默认档位（装配期捕获面；未做会话级选择的回退值）。
func (state *PermissionGate) PermissionTier() string {
	return state.PermissionTierFor("")
}

// SetFullAccess 是**兼容壳**：true → full 档、false → manual 档（进程级默认）。
func (state *PermissionGate) SetFullAccess(on bool) {
	_ = state.SetPermissionTierFor("", dto.PermissionTierFromFullAccess(on))
}

// SetFullAccessFor 是**兼容壳**：true → full 档、false → manual 档（指定会话）。
func (state *PermissionGate) SetFullAccessFor(sessionID string, on bool) {
	_ = state.SetPermissionTierFor(sessionID, dto.PermissionTierFromFullAccess(on))
}

// FullAccess 返回进程级默认是否 full 档（兼容读面）。
func (state *PermissionGate) FullAccess() bool {
	return state.FullAccessFor("")
}

// FullAccessFor 返回指定会话生效的档位是否等价旧 full_access（兼容读面；会话级
// 选择优先，未选择回退进程默认）。
func (state *PermissionGate) FullAccessFor(sessionID string) bool {
	return dto.PermissionTierIsFullAccess(state.PermissionTierFor(sessionID))
}

// effectiveTierLocked 解析生效的档位（调用方持锁）：会话级选择优先，未选择回退
// 进程默认，都没有则 manual。空字符串值（未选择）与"没有条目"是同一件事。
func (state *PermissionGate) effectiveTierLocked(sessionID string) string {
	if tier, ok := state.sessionTier[sessionID]; ok && tier != "" {
		return tier
	}
	if tier, ok := state.sessionTier[""]; ok && tier != "" {
		return tier
	}
	return dto.PermissionTierManual
}

// checkerForLocked 选 checker（调用方持锁）：**一律 base 表**。
//
// 档位不再复制规则表（旧实现的 tierCheckers 已删除）：档位是向量预设，由
// Enforce 用 policy.TierCells 展开后判定；规则层只剩一份（deny/ask 的最细粒度
// 覆盖），因此这里没有"按档位选表"这回事了。
func (state *PermissionGate) checkerForLocked(_ string, _ policy.SubjectClass) *toolspermission.PermissionChecker {
	return state.checker
}

// Middleware 把一次工具调用接到框架 permission.Gate 上：**判定完全交给框架**
// （工具自带簇属 → 位/组路由 → 规则），harness 只提供三件东西——授权表
// （checker）、执行选择页面（ApprovalHandler）、会话级提权（Enforce）。
//
// 会话归属与**主体**随调度 ctx 透出：会话 ID 走 WithSessionID（审批请求据此
// 路由回正确会话视图），主体走 WithEngine（框架据此查授权表）。主体由"谁在
// 调用"解析（见 permission_policy.go）：子代理 = sub、员工角色会话 = emp_ro /
// emp_rw、其余 = root。会话 ID 在写进 ctx 前先按 RoleSessionOwner 折算成宿主
// 主会话（角色会话没有自己的审批面板）。
//
// 全权短路同样按调用 ctx 的会话归属解析（A 会话的全权不替 B 会话放行；B 的
// 起点同步也关不掉 A）：全权是用户的显式决定，不允许任何"还没轮到规则"的
// 时序把它降级成询问或拒绝，也不允许它越会话传播。
func (state *PermissionGate) Middleware(approvalTimeout time.Duration) frameworktools.MetaMiddleware {
	return func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			sessionID := state.sessionFromContext(ctx)
			class := state.classFor(ctx)
			// 簇属声明先过守卫：声明的簇不在本次配置的组表里 ⇒ 声明作废，判定退回
			// 按名字路由（详见 sanitizeMeta）。这一步必须在 Enforce/Decide **之前**，
			// 因为两个入口吃的都是这份 meta。用局部量：闭包参数被同一工具的多路
			// 并发调用共享，就地改写会成数据竞争（sanitize 幂等，但仍不该共享写）。
			declared := state.sanitizeMeta(meta)
			// 主体解析：员工执行面在 ctx 里带了角色名 → 落到这个员工自己的主体
			// （emp_<角色>）；其余落回主体类对应的共享主体。判定表只有一份，
			// 因此"给员工分配了什么权限"与"调用时按什么判"是同一条路径。
			subject := state.resolveEmployeeSubject(ctx, class)
			ctx = withSubjectClass(ctx, class)
			ctx = toolspermission.WithEngine(ctx, toolspermission.Engine(subject))
			// 会话归属只供**审批呈现**使用：角色会话（员工/评审者）折算到宿主主会话
			// （见 RoleSessionOwner），因此越权提权弹在宿主的面板上，而不是一个没有
			// 面板的角色会话号上。判定读的是 SessionFromContext 的原始会话（telemetry
			// 键），所以"按谁判"与"审批弹在哪"互不干扰。
			if approvalSessionID := state.approvalSessionFor(sessionID); approvalSessionID != "" {
				ctx = toolspermission.WithSessionID(ctx, approvalSessionID)
			}
			if err := state.gate(approvalTimeout, class, sessionID).Decide(ctx, name, declared, policyArgsFor(name, argsJSON)); err != nil {
				return "", err
			}
			return next.Execute(ctx, argsJSON)
		})
	}
}

// policyArgsFor 给判定换一份"模式匹配用参数"。命令行类工具（bash）的规则写的是
// **命令模式**（"git *" / "rm -rf /*"），而工具入参是 `{"command":"..."}` 的 JSON：
// 直接拿整串 JSON 去匹配模式，等于规则里的 patterns 全部失效（只留下"匹配不上"
// 的兜底规则）。这里把命令本体抽出来交给 checker，让 args 级能力白名单真正生效；
// 真正传给工具的仍是原始 argsJSON。
func policyArgsFor(name, argsJSON string) string {
	switch name {
	case "bash", "bash_bg":
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err == nil {
			if command := strings.TrimSpace(input.Command); command != "" {
				return command
			}
		}
	}
	return argsJSON
}

// gate 组装框架判定器：checker 与审批处理器在调用瞬间快照，因此 Set 重建
// checker 不会让在途调用落在旧表或 nil 上。
//
// checker 的选择是"主体类 + 会话档位向量"的函数：root 按本会话 TierVector /
// TierCells 展开组位，sub/员工一律用 base 表。这让档位只改主 agent 的公开能力
// 授予面，不改任何主体的"有没有位"。
//
// DenyWithoutPrompt：seelex 的 deny 是用户显式配置的拒绝，维持"直接拒绝"的
// 语义（返回可 errors.Is 归类的英文错误：策略拒绝 / 不在该 engine 的命名
// 空间）；只有 ask（无命中规则）才呈现执行选择页面。框架默认的"拒绝也走选择
// 页面"是另一套产品语义，要采纳时去掉这个开关即可。
//
// 子代理（sub 主体）没有人类在环：Approval 置 nil，框架的 ask 分支直接返回
// 拒绝（`approve` 里 Approval==nil → DenialError），位缺（违权）本来就走
// denyOrPrompt 的拒绝分支——即"违权操作直接拒绝"，绝不挂起等一个不会有人回答
// 的选择页面。
func (state *PermissionGate) gate(approvalTimeout time.Duration, class policy.SubjectClass, sessionID string) *toolspermission.Gate {
	state.mu.RLock()
	// checker 按"主体类 + 会话档位"选：root 用本会话档位的规则表（edit/auto 剪掉
	// 若干 ask），sub/员工一律用 base 表（档位不改变任何主体的位）。checker 与审批
	// 处理器在调用瞬间快照，因此 Set 重建 checker 不会让在途调用落在旧表或 nil 上。
	checker, handler := state.checkerForLocked(sessionID, class), state.handler
	state.mu.RUnlock()
	if class == policy.SubjectClassSub {
		handler = nil
	}
	return &toolspermission.Gate{
		Checker:           checker,
		Approval:          handler,
		Enforcer:          state,
		Timeout:           approvalTimeout, // limits.approval_timeout；<=0 时框架取默认
		DenyWithoutPrompt: true,
	}
}

// Enforce 实现框架 permission.BitEnforcer：在**位/组/规则之前**用权限向量接管判定。
//
// 判据（唯一一条，三态，见 policy.PermissionVectorCells）：
//
//  1. 显式 deny 规则永远硬（危险命令）→ 直接拒绝，任何主体、任何档位都不例外；
//  2. 显式 `!tag`（用户/装配方说了不给这一族）→ 直接拒绝，不给提权机会；
//  3. 断位（向量里没有这一族）→ 按主体类落成：子代理交回框架（不可路由/拒绝）、
//     员工与 root 走执行选择页面提权（人类 sudo 口令）；
//  4. 已授权 → 员工/子代理直接放行（赋权即免审批）；root 在其档位**不是 full** 时
//     仍让规则层的 ask 求值（write_file / 命令黑名单这类"最细粒度覆盖"照旧生效），
//     full 档只受第 1 条约束。
//
// 向量从哪来（permission_policy.go 的 callVector）：root = 会话档位的预设
// （policy.TierVector）；员工 = (会话, 角色) 的生效向量（登记优先、缺失走四级链路；
// 读面没接上时按 ctx 身份自带的格子/档位派生）。两者都是**这一层唯一的方向**：
// 显式分配与运行时派生都不写共享主体表（旧的第二条判据已收掉）。
func (state *PermissionGate) Enforce(ctx context.Context, subject toolspermission.Subject, meta frameworktools.ToolMeta, name, argsJSON string) (toolspermission.Action, bool) {
	class := state.classFor(ctx)
	cfg := state.configSnapshot()
	if class == policy.SubjectClassRoot && state.FullAccessFor(state.sessionFromContext(ctx)) {
		// full 档：**免审批**（不再问规则层），但危险命令的 deny 仍然硬拦——这是本轮
		// 相对旧实现（full = BypassAll，连 deny 一起绕过）的收紧。
		if result, _ := state.decideFor(subject, name, meta, argsJSON); result == toolspermission.ResultDeny {
			return toolspermission.ActionDeny, true
		}
		return toolspermission.ActionAllow, true
	}
	if !state.policyReady(cfg) {
		return "", false
	}
	if class == policy.SubjectClassSub {
		// 子代理没有"角色登记"这一维，授权面仍由配置的主体表 + 位判定表达：
		// 显式 deny 硬拦、位齐即授权（没有人类可问）、断位交回框架落成不可路由。
		if result, _ := state.decideFor(subject, name, meta, argsJSON); result == toolspermission.ResultDeny {
			return toolspermission.ActionDeny, true
		}
		if _, routed := policy.RoutePermissionGroup(cfg.Groups, name); !routed {
			return "", false
		}
		if !state.visibleFor(subject, name, meta) {
			return "", false
		}
		return toolspermission.ActionAllow, true
	}
	cells, tier, ok := state.callVector(ctx)
	if !ok {
		return "", false
	}
	// 1) 显式 deny 规则（危险命令）：最硬的一条，先判。
	if result, _ := state.decideFor(subject, name, meta, argsJSON); result == toolspermission.ResultDeny {
		return toolspermission.ActionDeny, true
	}
	group, routed := policy.RoutePermissionGroup(cfg.Groups, name)
	// 2) 显式 `!tag`：调用方明确说了"这一族不给" → **不给规则层兜底的机会**，
	//    直接走人类口令（root / 员工）；子代理没有人类可问 → 拒绝。
	if routed && cells.ExplicitlyDenied(group.Name) {
		if class == policy.SubjectClassSub {
			return toolspermission.ActionDeny, true
		}
		return toolspermission.ActionAsk, true
	}
	// 3) 断位：向量里没有这一族（或这个工具在本次配置里没有分封）。
	if !routed || !cells.Covers(group.Name, group.Mode) {
		switch class {
		case policy.SubjectClassSub:
			// 子代理没有人类在环：交回框架，由它落成"不可路由/拒绝"。
			return "", false
		case policy.SubjectClassRoot:
			// root 的向量外调用交回框架：规则层（ask/allow/deny 的最细粒度覆盖）照旧
			// 说话。向量在这条路上只做**放宽**（授予的组免审），从不收窄主代理今天
			// 走规则层能得到的东西——否则"默认档下一次 git status 也要点一次审批"。
			return "", false
		default:
			return toolspermission.ActionAsk, true
		}
	}
	// 4) 已授权。
	if class == policy.SubjectClassSub {
		// 子代理沿用既有口径：位齐即授权（没有人类可问，规则 ask 不落页面）。
		return toolspermission.ActionAllow, true
	}
	// root：向量授予即免审批（档位的语义就是"这一族不再打断我"）。规则层只剩两种
	// 声音——第 1 条的 deny（危险命令，硬拦）与**向量外**调用的 ask（第 3 条回落
	// 框架），因此这里不再求值规则 ask：那是旧"按档位剪规则"的实现细节，剪法一变
	// 判据就分叉。
	_ = tier
	return toolspermission.ActionAllow, true
}

func (state *PermissionGate) sessionFromContext(ctx context.Context) string {
	if state == nil {
		return ""
	}
	state.mu.RLock()
	resolver := state.SessionFromContext
	state.mu.RUnlock()
	if resolver == nil {
		return ""
	}
	return resolver(ctx)
}
