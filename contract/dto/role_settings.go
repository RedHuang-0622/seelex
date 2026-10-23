package dto

import (
	"sort"
	"strings"
)

// role_settings.go 是「角色设置」的跨层词汇，也是**四级权限向量解析的唯一实现**。
//
// 生态位：一个角色（teammate / 群聊成员）的设置由四层来源叠出来，优先级**高→低**：
//
//	user_allow      用户在会话里对某个角色的显式允许（人说的话最硬）
//	agent_dispatch  派发方在派活时给出的约束（"这次只让它读"）
//	session_default 会话默认（这个会话里员工默认能做什么）
//	global_default  进程全局默认（seele.yaml / limits 派生）
//
// 判据只有一条：**逐标签取首个出现者**——同一个标签在多层出现时，最高优先层的
// 说法说了算；低层既不能覆盖也不能补齐被高层否掉的标签。标签带 `!` 前缀表示
// "这一层明确不给"，它不是"没提到"（三态纪律，与口令写入/清除/保持同款）：
// `!rw` 能压住下层的 `rw`，而"没提到 rw"只会让下层决定。
//
// 为什么这条判据必须在本文件只有一份实现：装配侧（会话加载）、应用层（角色装配）、
// 前端（权限面板预览）都要回答"这个角色到底能做什么"。三处各算一遍必然漂移，且
// 漂移方向是**权限放大**（某一处把"没提到"当成了"允许"）——这是安全问题，不是
// 一致性问题。所以：任何新增来源都只加一层，不加第二个解析函数。

// 权限向量标签：**能力标签**，取值与路由组同源（`PermissionGroupRO` 等，
// 定义仍在 permission.go，这里只是别名，不是第二份字面量）。
//
// 这样选的理由：权限向量要能直接喂给既有的"逐格装配"（`NormalizePermissionGroups`），
// 另造一套能力词表就得写映射，而映射就是第二份判据。新增能力面时先问"它属于哪个
// 路由组"，只有确实不属于任何组时才需要扩词表。
const (
	PermissionTagRO        = PermissionGroupRO        // 读：不改任何共享状态
	PermissionTagRW        = PermissionGroupRW        // 写：项目文件
	PermissionTagRWSession = PermissionGroupRWSession // 写：本会话 transcript
	PermissionTagRWDesktop = PermissionGroupRWDesktop // 写：共享桌面
	PermissionTagCTL       = PermissionGroupCTL       // 控制：叫停 / 派生 / 装载
	PermissionTagADM       = PermissionGroupADM       // 属主：改变能力面本身
)

// PermissionTagDenyPrefix 是"这一层明确不给该标签"的前缀。
const PermissionTagDenyPrefix = "!"

// 四级来源名字（诊断、日志与前端展示用；顺序即优先级，高→低）。
const (
	PermissionLayerUserAllow      = "user_allow"
	PermissionLayerAgentDispatch  = "agent_dispatch"
	PermissionLayerSessionDefault = "session_default"
	PermissionLayerGlobalDefault  = "global_default"
)

// PermissionLayerNames 返回四级来源的规范顺序（高→低）。
func PermissionLayerNames() []string {
	return []string{
		PermissionLayerUserAllow, PermissionLayerAgentDispatch,
		PermissionLayerSessionDefault, PermissionLayerGlobalDefault,
	}
}

// PermissionTags 返回规范标签序（权限面板的"格子顺序"，也是解析结果的展示序）。
func PermissionTags() []string {
	return PermissionGroupNames()
}

// RoleSettings 是一个角色的设置。
//
// 边界：它只描述"这个角色被配成什么样"，不承载运行期状态（激活的插件实例、
// 当前 token 用量都不在这里）。字段与 JSON 字段名是**跨层契约**：前端权限面板、
// 会话级存储、装配侧四面读的都是这一份。
type RoleSettings struct {
	// Account 是该角色使用的账号名（provider 账号；空 = 跟随会话/全局）。
	Account string `json:"account,omitempty"`
	// ContextWindow 是该角色的上下文窗口（token；0 = 跟随会话/全局）。
	ContextWindow int `json:"context_window,omitempty"`
	// PermissionVector 是能力标签集合（见 PermissionTag*；可带 `!` 前缀表示明确不给）。
	PermissionVector []string `json:"permission_vector,omitempty"`
	// DefaultPlugin 是该角色默认装载的插件名（空 = 不预装，走会话默认）。
	DefaultPlugin string `json:"default_plugin,omitempty"`
}

// Empty 报告这份设置是否什么都没配（调用方据此跳过写入/覆盖）。
func (settings RoleSettings) Empty() bool {
	return settings.Account == "" && settings.ContextWindow == 0 &&
		len(settings.PermissionVector) == 0 && settings.DefaultPlugin == ""
}

// RoleSettingsLayers 是四级来源的一份取值。
//
// 用"每层一份 RoleSettings"而不是"每层一个 []string"：Account / ContextWindow /
// DefaultPlugin 也走同一条四级链路（首个非零层说了算），把它们拆到两个结构里会
// 让调用方不得不做两次解析。
type RoleSettingsLayers struct {
	UserAllow      RoleSettings
	AgentDispatch  RoleSettings
	SessionDefault RoleSettings
	GlobalDefault  RoleSettings
}

// Ordered 返回按优先级高→低排列的四层（新切片，顺序即 PermissionLayerNames）。
func (layers RoleSettingsLayers) Ordered() []RoleSettings {
	return []RoleSettings{
		layers.UserAllow, layers.AgentDispatch, layers.SessionDefault, layers.GlobalDefault,
	}
}

// PermissionVectors 返回四层的权限向量（顺序同上）。
func (layers RoleSettingsLayers) PermissionVectors() [][]string {
	ordered := layers.Ordered()
	vectors := make([][]string, 0, len(ordered))
	for _, layer := range ordered {
		vectors = append(vectors, layer.PermissionVector)
	}
	return vectors
}

// ResolvePermissionVector 按四级优先级解析权限向量：**逐标签取首个出现者**，
// 返回最终**允许**的标签集合（高优先层的否定不会出现在结果里）。
//
// 参数顺序即优先级：`layers[0]` 最高。典型调用是
// `ResolvePermissionVector(layers.PermissionVectors()...)`。
//
// 语义边界：
//   - 标签大小写不敏感、前后空白忽略；`!` 前缀可重复且逐次取反（`!!rw` 等价 `rw`）；
//   - 同一个标签在同一层重复出现时，**先出现的那个**说了算（同层内也不允许
//     后写的覆盖先写的，否则"顺序"就又成了一份隐式判据）；
//   - 空层（nil / 空切片）不参与决策，直接跳过；
//   - 返回切片非 nil（无允许项时是空切片），顺序 = 规范标签序（`PermissionTags`），
//     不认识的标签排在已知标签之后并按字典序，保证输出稳定可比对。
func ResolvePermissionVector(layers ...[]string) []string {
	return ResolvePermissionVectorDecision(layers...).Allowed
}

// PermissionVectorDecision 是一次权限向量解析的**完整结论**：允许的标签 +
// 被显式否掉的标签（`!tag` 赢下的那些）。
//
// 为什么要单独说出"被否掉"：判定的执行面要区分三态——**允许**、**没提到**
// （断位：这一族能力不在授权面内，执行面按主体类落成"提权/不可路由"）与
// **显式 deny**（调用者明确说了不给，执行面直接拒绝，不给提权机会）。
// 只返回 Allowed 时后两者会被压成一件事，`!rw` 就只能退化成"没提到"。
type PermissionVectorDecision struct {
	// Allowed 是最终允许的标签（规范序；非 nil，可能为空切片）。
	Allowed []string
	// Denied 是被显式否掉的标签（规范序；非 nil，可能为空切片）。
	Denied []string
}

// ResolvePermissionVectorDecision 是四级优先级解析的**唯一实现**
// （ResolvePermissionVector 是它的 Allowed 投影，不另算一遍）。
func ResolvePermissionVectorDecision(layers ...[]string) PermissionVectorDecision {
	decided := make(map[string]bool, 8)
	order := make([]string, 0, 8)
	for _, layer := range layers {
		for _, raw := range layer {
			tag, allow, ok := parsePermissionTag(raw)
			if !ok {
				continue
			}
			if _, seen := decided[tag]; seen {
				// 高优先层已定：低层既不能覆盖，也不能"再提一次"来翻案。
				continue
			}
			decided[tag] = allow
			order = append(order, tag)
		}
	}
	allowed := make([]string, 0, len(order))
	denied := make([]string, 0, len(order))
	for _, tag := range order {
		if decided[tag] {
			allowed = append(allowed, tag)
			continue
		}
		denied = append(denied, tag)
	}
	sortPermissionTags(allowed)
	sortPermissionTags(denied)
	return PermissionVectorDecision{Allowed: allowed, Denied: denied}
}

// ResolveRoleSettings 按四级优先级解析整份角色设置（高→低参数序）：
// Account / ContextWindow / DefaultPlugin 取**首个非零层**，PermissionVector
// 交给 ResolvePermissionVector（同一份判据，不在这里重算）。
func ResolveRoleSettings(layers ...RoleSettings) RoleSettings {
	resolved := RoleSettings{}
	vectors := make([][]string, 0, len(layers))
	for _, layer := range layers {
		if resolved.Account == "" && layer.Account != "" {
			resolved.Account = layer.Account
		}
		if resolved.ContextWindow == 0 && layer.ContextWindow > 0 {
			resolved.ContextWindow = layer.ContextWindow
		}
		if resolved.DefaultPlugin == "" && layer.DefaultPlugin != "" {
			resolved.DefaultPlugin = layer.DefaultPlugin
		}
		vectors = append(vectors, layer.PermissionVector)
	}
	resolved.PermissionVector = ResolvePermissionVector(vectors...)
	return resolved
}

// NormalizePermissionVector 规整一份**写入侧**的权限向量：逐项取 `解析形态`
// （小写 + 去空白 + 保留单个 `!` 前缀），按首次出现顺序去重。
//
// 与 ResolvePermissionVector 的分工：本函数是写入侧的**校验**（非法形状显式报错，
// 免得一个拼写错误静默变成"没给这项能力"，与 permission.go 写入侧报错同一纪律）；
// ResolvePermissionVector 是读取侧的**解析**（宽容跳过无法识别的项，不阻断会话加载）。
// 两者共用 parsePermissionTag，所以"什么算合法标签"只有一份定义。
//
// 返回：规范化后的标签（可能含 `!` 前缀），以及第一个非法项的原文错误。
func NormalizePermissionVector(vector []string) ([]string, error) {
	if len(vector) == 0 {
		return []string{}, nil
	}
	normalized := make([]string, 0, len(vector))
	seen := make(map[string]struct{}, len(vector))
	for _, raw := range vector {
		tag, allow, ok := parsePermissionTag(raw)
		if !ok {
			return nil, &PermissionVectorError{Entry: raw}
		}
		form := tag
		if !allow {
			form = PermissionTagDenyPrefix + tag
		}
		if _, dup := seen[form]; dup {
			continue
		}
		seen[form] = struct{}{}
		normalized = append(normalized, form)
	}
	return normalized, nil
}

// PermissionVectorError 是权限向量项非法（承载原文，便于前端直接指出填错的那一项）。
type PermissionVectorError struct{ Entry string }

func (err *PermissionVectorError) Error() string {
	if err == nil {
		return "权限向量项非法"
	}
	return "权限向量项 " + err.Entry + " 非法（形如 rw 或 !rw；取值见 dto.PermissionTags）"
}

// parsePermissionTag 把一项原文解析成 (标签, 是否允许)。第二个返回 false 表示这一项
// 不是可识别的标签（空串、内部含空白、只剩 `!`）。
//
// 唯一一份"什么算标签"的判据：解析（宽容）与规整（严格报错）都用它。
func parsePermissionTag(raw string) (string, bool, bool) {
	trimmed := strings.TrimSpace(raw)
	allow := true
	for strings.HasPrefix(trimmed, PermissionTagDenyPrefix) {
		allow = !allow
		trimmed = strings.TrimPrefix(trimmed, PermissionTagDenyPrefix)
	}
	trimmed = strings.ToLower(strings.TrimSpace(trimmed))
	if trimmed == "" || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", false, false
	}
	return trimmed, allow, true
}

// sortPermissionTags 就地把标签按"规范序在前、未知项按字典序在后"排好。
func sortPermissionTags(tags []string) {
	if len(tags) < 2 {
		return
	}
	rank := make(map[string]int, len(PermissionTags()))
	for index, tag := range PermissionTags() {
		rank[tag] = index
	}
	sort.SliceStable(tags, func(i, j int) bool {
		left, leftKnown := rank[tags[i]]
		right, rightKnown := rank[tags[j]]
		switch {
		case leftKnown && rightKnown:
			return left < right
		case leftKnown != rightKnown:
			return leftKnown
		default:
			return tags[i] < tags[j]
		}
	})
}
