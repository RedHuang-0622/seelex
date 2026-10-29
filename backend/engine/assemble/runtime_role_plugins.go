package assemble

// runtime_role_plugins.go — 「teammate 按指定 plugin 装配」的解析面、生效点与读数。
//
// 生态位（能力轴；权限轴在同一条角色回合上另有其人，见 runtime_role_turn.go）：
//
//  1. **解析集合**（rolePluginAssembly）：显式声明（`team_plan members[].plugins`
//     → WorkerRequest.Plugins → roleRoundSpec.Plugins）**替换**集合；没声明就落到
//     "角色自带"（RoleSpec.Plugins，召唤路径经 SetRolePluginsProvider 注入的读面）；
//     两者都空 = **空集 = 不覆盖**（工具面继承宿主当前装配 + 技能目录不注入）。
//  2. **落进本轮 ctx**（seeltools.WithRolePlugins）：工具可见性由 PolicyDeps.PluginFace
//     按 ctx 每轮现算。**不在任何句柄上缓存"当前装配"**——缓存就是并发丢失更新的
//     另一面（两个 teammate 并发回合时，后写的那份会覆盖先跑的那份）。
//  3. **技能目录**：装配集合的技能 name+description 追加进本轮 system prompt（正文
//     永不进目录），并给出**装配读数**（字节 / token 估算 / 黄牌）——回执必须带读数。
//
// 三条写死的边界：
//   - **权限不随插件走**：装配只回答"能用哪些能力包"。权限永远来自
//     ToolsPolicy / PermissionGroups；插件面与权限面相交后**只收窄权限面**
//     （收口在 tools.Policy.Filter 的最后一道）。
//
//     这句"只收窄"要读准对象（2026-10-05，对抗复核 F）：它**只相对权限面成立**，
//     **不相对宿主装配**。相对"继承宿主"（空集）它可以是**放宽**：精选目录里
//     `plugins/default/visibility.md` 的 include/exclude 皆空 ⇒ 一个 inherit-host 的
//     teammate 声明 `plugins:["default"]` 就拿到了**全工具面**，比它不声明时更宽
//     （不声明要过宿主全局激活的那个插件的 include/exclude）。所以这条边界说的是
//     "插件不放宽**权限**"，不是"装配不放宽**工具面**"——后者的真相是：装配集合
//     替换宿主那一份收窄，可以比它宽，也可以比它窄。
//   - **未知插件名显式拒绝**，不是静默忽略：拒绝在编排入口（teamPlanHandler）做，
//     那里手里有插件定义；到执行面才发现名字没了 = 插件被 root 撤销过，按"失灵"
//     处理（见 runtime.go 的 PluginFace 收口），绝不静默放宽。
//   - **重复声明显式拒绝**，不是静默合并：与"未知名/超限"同口径（唯一生效的口径
//     只有一套，见 dto.NormalizePlugins 与 store.ValidateTeamworkPlan 的两处
//     拒绝）。本文件里的规整（normalizePluginNames）是**读侧/传输侧**的规范化，不是
//     声明侧的口径：它面对的是已经过入口校验的事实。
//   - **root 路径不受影响**：主代理仍走全局单选（Manager.active），本文件的集合
//     只进角色回合的 ctx。

import (
	"strings"

	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	teamdomain "github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	"github.com/RedHuang-0622/seelex/kernel/prompt"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// pluginsCatalogTokenWarn 是装配目录段的**黄牌**阈值（token 估算）。
//
// 黄牌**不拒绝装配**：目录段是每轮常驻的 system 字节，超了就把它当读数报出来
// （装配回执里的 yellow/yellow_reason），由 leader 决定要不要拆小。为什么是 6k：
// 外部的经验值是 15k token 起明显拖慢首字（见 docs/research 的插件装配外部调研），
// 6k 是留给"目录之外还有正文注入"的提前量。
const pluginsCatalogTokenWarn = 6000

// pluginsCatalogWindowPercent 是黄牌的第二条判据：目录段 token 估算占当前账号
// 上下文窗口的百分比（2%）。窗口未知（未选账号）时这条不参与判定。
const pluginsCatalogWindowPercent = 2

// roleSkillAssemblyNote 是目录段之后的**一行纠正**：RenderSkillCatalog 的尾句写死
// "The listed set belongs to the active plugin and changes when you switch plugins"
// ——那是**宿主**口径。员工面的集合来自装配声明，且它没有切插件/激活技能的位
// （GroupADM 断位），照抄那句话会让员工拿着错的前提去用能力。
const roleSkillAssemblyNote = "These skills come from this teammate's assembly " +
	"(team_plan members[].plugins); a teammate cannot switch plugins or activate skills."

// 装配回执里的 mode 取值（空集语义必须在回执里**显式写明**，不靠字段缺失暗示）。
const (
	assemblyModeReplace     = "replace"
	assemblyModeInheritHost = "inherit-host"
)

// SetRolePluginsProvider 注入"角色自带的插件装配"读面（RoleSpec.Plugins）；传 nil
// 取消注入（回退"角色自带 = 空集"）。
//
// 与 SetRolePromptProvider 同构：读面只读角色注册表，不建环、不改任何事实；启动期
// 注入一次，重复调用以最后一次为准。
//
// 状态（A08，2026-10-15）：**保留为可选对外读面**。来源（团队角色注册表）已随
// 2026-10-12 J-1a 退场，组合根不再注入；未注入时 `rolePluginsFor` 返回空集 =
// "不覆盖"，与"注册表里没登记"逐字相同（见 rolePluginAssembly 的三层口径）。
// **活动插件来源是另一条**：`team_plan members[].plugins` 的显式声明
// （teamPlanHandlerWorld → WorkerRequest.Plugins → roleRoundSpec.Plugins），它不经
// 本读面、不受退场影响，不可以被一起删。移除本读面须走版本化 API 变更（A08）。
func (r *Runtime) SetRolePluginsProvider(provider func(roleName string) []string) {
	if r == nil {
		return
	}
	r.rolePluginsMu.Lock()
	r.rolePlugins = provider
	r.rolePluginsMu.Unlock()
}

// rolePluginsFor 读某个角色自带的插件装配（未注入读面/未登记 = 空集）。
func (r *Runtime) rolePluginsFor(roleName string) []string {
	if r == nil {
		return nil
	}
	r.rolePluginsMu.RLock()
	provider := r.rolePlugins
	r.rolePluginsMu.RUnlock()
	if provider == nil {
		return nil
	}
	return normalizePluginNames(provider(strings.TrimSpace(roleName)))
}

// rolePluginAssembly 解析"这一轮装配哪些插件"：显式声明替换集合 → 角色自带 →
// 空集（不覆盖）。三层的顺序即契约 6 的"集合"口径（判据在团队域
// [`backend/multiagent/team.ResolvePluginAssembly`](../../multiagent/team/README.md)）。
func (r *Runtime) rolePluginAssembly(spec roleRoundSpec) []string {
	return teamdomain.ResolvePluginAssembly(spec.Plugins, r.rolePluginsFor(spec.RoleName))
}

// roleConversationSystemPrompt 重建“历史会话继续对话”应使用的最新 system prompt：
// 当前角色提示词 + 当前插件装配的技能目录。它与 runRoleRound 共用同一套
// rolePluginAssembly/roleSkillCatalog，不能另拼一份目录口径。
func (r *Runtime) roleConversationSystemPrompt(roleName string, plugins []string) string {
	base := r.roleTurnSystemPrompt(roleName)
	assembly := r.rolePluginAssembly(roleRoundSpec{RoleName: roleName, Plugins: plugins})
	return appendSkillCatalog(base, r.roleSkillCatalog(assembly))
}

// normalizePluginNames 是团队域 `NormalizePluginNames` 的**同身份别名**
// （读侧/传输侧规范化只有一份实现，既有调用点零改动）。
var normalizePluginNames = teamdomain.NormalizePluginNames

// maxPluginsPerTeammate 返回每个 teammate 的插件数上限（配置键
// limits.plugins.per_teammate；未配置/非正 = 出厂默认 3）。
//
// 为什么是配置而不是散在代码里的常量：它是**产品级约束**（一个人身上挂几个能力
// 包是产品决策），与 limits.team.max_teammates 同族。超限的处理口径也一样：**显式
// 拒绝**，不静默截断（截断会把"我声明了 5 个"悄悄变成"装了 3 个"）。
// validateMemberPlugins 是编排入口的两道**显式拒绝**（外加语法规整）：
//
//  1. 语法：去首尾空白、丢弃空项、保序去重（dto.NormalizePlugins，与写入侧同一
//     份口径），并把规整结果写回成员条目——写回去，计划里存的才是"运行时真会用的
//     那一份"；
//  2. 语义：名字必须在插件目录里（defined）。**未知名显式拒绝，不是静默忽略**：
//     静默忽略会让 leader 以为装上了，而员工那头一个能力也没有（要到员工跑完才
//     发现），而且"拒绝"才是可回执的事实；
//  3. 数量：超过 limit（limits.plugins.per_teammate）显式拒绝，不静默截断；
//  4. 重复：显式拒绝，不静默合并——与"未知名"同口径（唯一生效的口径只有一套，
//     见 dto.NormalizePlugins 的说明：修前那里静默去重，于是本函数的语义校验与计划
//     自己写的"不静默"相反，存储层那条重复拒绝根本不可达）。
//
// 抽成函数是为了可测：它只依赖"上限"与"名字是否存在"两个事实，不需要整个
// coordinator / 存储面在场（拒绝口径的回归测试因此是纯函数级的）。
// validateMemberPlugins 是团队域 `ValidateMemberPlugins` 的同身份别名（两道显式拒绝只有一份实现）。
var validateMemberPlugins = teamdomain.ValidateMemberPlugins

func (r *Runtime) maxPluginsPerTeammate() int {
	if r == nil {
		return limit.DefaultPluginsPerTeammate
	}
	if configured := r.limits.Plugins.PerTeammate; configured > 0 {
		return configured
	}
	return limit.DefaultPluginsPerTeammate
}

// pluginFaceJudgement 是团队域装配面裁决类型的**同身份别名**（三种形态的判据只有一份实现，
// 读数与运行面共用它——回执与运行事实因此不可能相反）。
type pluginFaceJudgement = teamdomain.PluginFaceJudgement

// pluginFaceJudgement 解析"这一轮的装配集合"该按哪份定义收窄——**唯一的装配面判据**
// （运行面见 runtime.go 的 PolicyDeps.PluginFace，读数见 assemblyViews）。
//
// 空集 = 不覆盖：这时按**宿主全局激活插件**的定义收窄，与 r.plugins.Filter 同解
// （Filter 就是"用激活插件的这一份定义过一遍"，单元素集合下 visibility.Face 与它逐条等价：
// 未激活 / 未定义 = 空 defs = 一律可见）。
//
// 注意空集那一支读的是**那一刻**的全局激活态：宿主面是一个随时间变的事实，读数只保证
// "与同一判据在同一时刻的结果一致"，不承诺它永远不变。
func (r *Runtime) pluginFaceJudgement(names []string) pluginFaceJudgement {
	if r == nil || r.plugins == nil {
		return pluginFaceJudgement{}
	}
	if len(names) == 0 {
		active := strings.TrimSpace(r.plugins.Active())
		if active == "" {
			return pluginFaceJudgement{}
		}
		defs, missing := r.plugins.DefsFor([]string{active})
		if len(missing) > 0 || len(defs) == 0 {
			return pluginFaceJudgement{}
		}
		return pluginFaceJudgement{Defs: defs}
	}
	defs, missing := r.plugins.DefsFor(names)
	if len(missing) > 0 || len(defs) == 0 {
		return pluginFaceJudgement{Missing: missing, Faulted: true}
	}
	return pluginFaceJudgement{Defs: defs}
}

// skillRegistryFor 取已装配的 skill 目录 actor（未装配 = nil，降级不注入目录）。
func (r *Runtime) skillRegistryFor() *skill.Registry {
	if r == nil {
		return nil
	}
	return r.skills.Load()
}

// roleSkillCatalog 渲染装配集合的技能目录段（纯 name + description；正文永不进
// 目录）。空集、未装配 skill 目录、或这几份目录一条技能都没有 → 返回 ""（system
// 字节逐字不变）。
//
// 复用 prompt_layer.RenderSkillCatalog：主代理侧的目录与员工侧是**同一份口径**
// （排序、每行形状、头尾文案），读数才对得上；另写一份渲染就是第二套字节口径。
func (r *Runtime) roleSkillCatalog(plugins []string) string {
	registry := r.skillRegistryFor()
	if registry == nil || len(plugins) == 0 {
		return ""
	}
	catalog := prompt_layer.RenderSkillCatalog(skillInfosOf(registry.PluginSkillsFor(plugins)), "")
	if catalog == "" {
		return ""
	}
	return catalog + "\n" + roleSkillAssemblyNote
}

// skillInfosOf 把 skill 包的类型投影成目录段消费的只读模型（Name/Description 是
// 目录段的全部输入：Prompt 不投影——不给"顺手带上正文"留口子）。
func skillInfosOf(skills []skill.Skill) []model.SkillInfo {
	infos := make([]model.SkillInfo, 0, len(skills))
	for _, item := range skills {
		infos = append(infos, model.SkillInfo{Name: item.Name, Description: item.Description})
	}
	return infos
}

// rolePluginAssemblyView 是 dto.PluginAssemblyView 的**别名**（`=`，不是新类型）。
//
// 别名而不是"同形的新类型"：回执 JSON（team_plan 的 assemblies[] / team_dispatch 的
// plugin_face）与团队看板的 member.assembly 从此在类型系统上就是**同一个**东西——
// 两份手抄字段的结构体会漂移（改一处漏一处，两个读面对同一个人给出两个形状而两端用例
// 都绿），别名连漂移的语法空间都不存在。
//
// 字段与 tag 的**唯一一份定义**在 contract/dto/teamwork_board.go
// （PluginAssemblyView）；本文件只留"怎么算"（assemblyViews）与 mode 字面量。
type rolePluginAssemblyView = dto.PluginAssemblyView

// assemblyViews 生成逐成员的装配读数（回执用；members 为空时返回空切片）。
//
// 读数走 pluginFaceFor（与运行面同一判据），所以三件事同时成立：
//   - 声明装配的成员：面 = 集合收窄后的工具数；
//   - inherit-host 的成员：面 = **宿主当前装配**收窄后的工具数（真读数，不是 0）；
//   - 声明还在、定义没了：面 = 0 且带失灵说明（**不报满面**）。
func (r *Runtime) assemblyViews(members []storeteamwork.Member) []rolePluginAssemblyView {
	views := make([]rolePluginAssemblyView, 0, len(members))
	for _, member := range members {
		plugins := normalizePluginNames(member.Plugins)
		view := rolePluginAssemblyView{
			Role:        member.Role,
			Mode:        assemblyModeInheritHost,
			Plugins:     plugins,
			PluginCount: len(plugins),
		}
		if len(plugins) > 0 {
			view.Mode = assemblyModeReplace
		}
		// 目录段读数只在**显式装配**时才有值：inherit-host 不注入目录（运行事实）。
		if len(plugins) > 0 {
			if registry := r.skillRegistryFor(); registry != nil {
				skills := registry.PluginSkillsFor(plugins)
				view.SkillCount = len(skills)
				if catalog := r.roleSkillCatalog(plugins); catalog != "" {
					runes := len([]rune(catalog))
					view.SkillCatalogRunes = runes
					// token 估算固定用 runes/4：这是**闸门读数**，不是计费口径——
					// 同一段文本在不同 tokenizer 上有差异，但黄牌只需要一个稳定的量级。
					view.SkillCatalogTokensEst = (runes + 3) / 4
				}
			}
		}
		all := r.AllTools()
		judgement := r.pluginFaceJudgement(plugins)
		view.TotalTools = len(all)
		admitted := 0
		for _, tool := range all {
			if judgement.Visible(tool.Name) {
				admitted++
			}
		}
		view.PluginFaceTools = admitted
		view.PluginFaceFaulted = judgement.Faulted
		view.PluginFaceMissing = judgement.Missing
		view.PluginFaceNote = judgement.FaultNote(plugins)
		view.Yellow, view.YellowReason = r.pluginsCatalogYellow(view.SkillCatalogTokensEst)
		views = append(views, view)
	}
	return views
}

// pluginsCatalogYellow 判黄牌：目录段 token 估算超过 6k，或占当前账号上下文窗口
// 超过 2%。窗口未知时只看第一条。
func (r *Runtime) pluginsCatalogYellow(tokensEst int) (bool, string) {
	if tokensEst <= 0 {
		return false, ""
	}
	if tokensEst > pluginsCatalogTokenWarn {
		return true, "技能目录段超过 6k token 估算（目录每轮常驻 system prompt，不拒绝装配，只报读数）"
	}
	if window := r.ContextWindow(); window > 0 && tokensEst*100 > window*pluginsCatalogWindowPercent {
		return true, "技能目录段超过上下文窗口的 2%（每轮常驻；不拒绝装配，只报读数）"
	}
	return false, ""
}

// appendSkillCatalog 把目录段接到 system prompt 末尾（空目录段 = 原样返回：字节
// 逐字不变；非空才追加，且只追加——不改写已登记的员工提示词）。
func appendSkillCatalog(prompt, catalog string) string {
	if strings.TrimSpace(catalog) == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return catalog
	}
	return strings.TrimRight(prompt, "\n") + "\n\n" + catalog
}
