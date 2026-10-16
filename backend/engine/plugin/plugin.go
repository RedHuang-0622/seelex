// Package plugin 承载 Runtime 的插件可见性执行面：插件不再控制 holder，
// 而是作为 bridge.WithVisibilityPolicy 的输入——激活插件时按
// include/exclude 过滤每次请求的可见工具集。域内不依赖 seelebridge 根包。
//
// 边界（解耦方案 §02.4 方案 A 轻量版）：本包只是可见性投影缓存，不是
// 插件定义的事实源——事实源在顶层 plugin.Manager（manifest/skills/MCP
// 全量契约），本包 defs 由 root 经 ToolBackend 单点推送（Define/Undefine/
// Activate），写路径只有 root 一个入口；**全局激活态仍保持单选**。
//
// 按会话装配（2026-10-05 落地）：角色会话（teammate）的能力面是**按会话的集合**——
// 集合由 team_plan members[].plugins / RoleSpec.Plugins 声明，经 ctx 落到
// VisibleName/Face（多插件 include 取并集、exclude 取并集硬拆），与权限面相交后
// 只收窄、永不放宽。它是 root 那条全局激活态的**只读侧路**：本包的 active 不被它
// 读写，switch_plugin 仍是唯一的全局开关。见
// docs/devlog/2026-10-05-teammate-plugin-assembly-impl.md。
package plugin

import (
	"fmt"
	"path"
	"sort"
	"sync"

	"github.com/RedHuang-0622/Seele/types"
)

// Def 是插件可见性配置（include/exclude 快照）。
type Def struct {
	Name        string
	Description string
	Include     []string
	Exclude     []string
}

// Manager 是插件可见性状态的 actor 资源（自带锁）。defs 是 root
// plugin.Manager 定义的投影缓存，本包不解释 manifest/skills/MCP。
type Manager struct {
	mu     sync.RWMutex
	defs   map[string]Def
	active string
	// unassembled 是"本进程**未定义**的名字怎么判 / 怎么说"的判决函数，由产品侧在
	// 启动期注入一次（nil = 未注入，回落既有口径）。
	//
	// 为什么是注入而不是本包自己判：判定要做的事是"这个名字在精选目录里是什么"——
	// 那是 `plugins/curated.yaml` 的事实，本包不解析 YAML、不读文件。本包只掌握另一半
	// 事实（本进程定义了哪些名字，见 Defined/Names），两半在**装配入口**（team_plan）
	// 合起来判；判决函数由能同时看见目录的那一侧给。
	unassembled func(name string, installed []string) error
}

// NewManager 构造插件可见性管理器。
func NewManager() *Manager {
	return &Manager{defs: make(map[string]Def)}
}

// Define 定义或替换一个插件的可见性快照。
func (m *Manager) Define(name, description string, include, exclude []string) error {
	if name == "" {
		return fmt.Errorf("seelebridge: plugin name is empty")
	}
	m.mu.Lock()
	m.defs[name] = Def{
		Name: name, Description: description,
		Include: append([]string(nil), include...),
		Exclude: append([]string(nil), exclude...),
	}
	m.mu.Unlock()
	return nil
}

// Undefine 删除插件定义；若其为当前激活插件则一并停用。
func (m *Manager) Undefine(name string) {
	m.mu.Lock()
	delete(m.defs, name)
	if m.active == name {
		m.active = ""
	}
	m.mu.Unlock()
}

// Activate 激活插件（未定义返回显式错误）。
func (m *Manager) Activate(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defs[name]; !ok {
		return fmt.Errorf("seelebridge: plugin %q is not defined", name)
	}
	m.active = name
	return nil
}

// Deactivate 停用当前插件。
func (m *Manager) Deactivate() {
	m.mu.Lock()
	m.active = ""
	m.mu.Unlock()
}

// Active 返回当前激活插件名。
func (m *Manager) Active() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// Defined 报告插件是否已定义（按会话装配的**语义**校验用：未知名显式拒绝，不
// 静默忽略）。只读，不改激活态——Activate 也能"问"这个问题，但它带副作用。
func (m *Manager) Defined(name string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	_, ok := m.defs[name]
	m.mu.RUnlock()
	return ok
}

// Names 返回本进程已定义插件名（排序）。"已装的是哪些"是拒绝文案的一半事实——
// 写错名字的人要靠这份名单自己看出是错别字。
func (m *Manager) Names() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	names := make([]string, 0, len(m.defs))
	for name := range m.defs {
		names = append(names, name)
	}
	m.mu.RUnlock()
	sort.Strings(names)
	return names
}

// SetUnassembledReason 注入"未定义的名字怎么判 / 怎么说"（产品侧启动期一次；nil =
// 取消注入，回落既有口径）。注入即免审批地接管了**拒绝文案**，判定边界写在本包外：
// 它只被调用在"名字确实未定义"时（见 UnassembledReason 的调用点）。
func (m *Manager) SetUnassembledReason(fn func(name string, installed []string) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.unassembled = fn
	m.mu.Unlock()
}

// HasUnassembledReason 报告是否注入了判决函数（未注入的调用方不必为每个名字多跑一趟）。
func (m *Manager) HasUnassembledReason() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.unassembled != nil
}

// UnassembledReason 问一句"这个未定义的名字是什么"，返回 nil = 放行（判决函数认为
// 它可装配），非 nil = 显式拒绝的文案。
//
// 未注入判决函数时返回 nil：那正是"库里为真、运行期不成立"的那一半——本包不假装
// 知道精选目录，回落既有口径（装配入口的 validateMemberPlugins 报"未定义"）。
func (m *Manager) UnassembledReason(name string) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	fn := m.unassembled
	m.mu.RUnlock()
	if fn == nil {
		return nil
	}
	// 先把已装名单取出来再调判决函数：判决文案要写"已装的是哪些"，而本包持锁调用
	// 外部函数是自找死锁（判决函数可能回头读 Manager）。
	return fn(name, m.Names())
}

// DefsFor 取这些插件名的可见性快照（按入参顺序），并把本进程**未定义**的名字
// 列在 missing 里：调用方据此裁决"显式拒绝"还是"失灵"（见 seelebridge 的
// PluginFace 收口）。只读，不改激活态。
func (m *Manager) DefsFor(names []string) (defs []Def, missing []string) {
	if m == nil {
		return nil, append([]string(nil), names...)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	defs = make([]Def, 0, len(names))
	for _, name := range names {
		def, ok := m.defs[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		defs = append(defs, def)
	}
	return defs, missing
}

// Filter 按激活插件的 include/exclude 过滤可见工具集；未激活/未知插件
// 原样返回（插件过滤不生效）。
func (m *Manager) Filter(tools []types.Tool) []types.Tool {
	m.mu.RLock()
	active := m.active
	def, ok := m.defs[active]
	m.mu.RUnlock()
	if !ok || active == "" {
		return tools
	}
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Function.Name
		if len(def.Include) > 0 && !matchesAnyPattern(name, def.Include) {
			continue
		}
		if matchesAnyPattern(name, def.Exclude) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// VisibleName 报告一个工具名在**装配集合**（多个插件定义）下是否可见。
//
// 语义（多插件是 Filter 单插件口径的自然推广，单元素集合与 Filter 完全同解）：
//   - **exclude 是硬否决**：任一装配插件的 exclude 命中它 → 不可见（exclude 取
//     并集后硬拆）；
//   - **include 是准入并集**：至少一个装配插件接纳它（没写 include 的插件不设
//     准入 = 接纳）→ 可见。
//
// defs 为空 = 没装配插件：**一律可见**（不装配就不改变工具面）。
//
// 导出的是"按名字判"这一步（工具面过滤与装配读数共用同一判据）——读数如果另写
// 一套，读数就会和实际过滤结果不是一回事。
func VisibleName(defs []Def, name string) bool {
	if len(defs) == 0 {
		return true
	}
	admitted := false
	for _, def := range defs {
		if matchesAnyPattern(name, def.Exclude) {
			return false
		}
		if len(def.Include) == 0 || matchesAnyPattern(name, def.Include) {
			admitted = true
		}
	}
	return admitted
}

// Face 按**装配集合**过滤工具面（include 取并集、exclude 取并集硬拆，见
// VisibleName）。纯函数：无状态、无 I/O、同输入恒同输出——按会话装配每轮现算，
// 绝不在 Manager 上缓存"某会话的装配"（那是并发丢失更新的另一面）。
//
// 与 Filter 的关系：Filter 读的是**全局激活态**（root 路径，单插件），Face 读的是
// 调用方给的集合（角色会话路径）。两条路互不写：本函数不改任何字段。
func Face(defs []Def, tools []types.Tool) []types.Tool {
	if len(defs) == 0 {
		return tools
	}
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		if !VisibleName(defs, tool.Function.Name) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchToolPattern(pattern, name) {
			return true
		}
	}
	return false
}

// matchToolPattern 支持 "*" 通配的简单模式匹配（与旧 holder 插件过滤语义一致）。
func matchToolPattern(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	ok, err := path.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return ok
}
