package computer

import (
	"sort"
	"time"
)

// ── 可滚动面板（scroll target）────────────────────────────────
//
// 只注入滚轮的模型有两处结构性缺口：
//
//  1. 截图只含视口内的像素，"屏幕外还有没有上下文"既不在画面里，也不在窗口
//     列表里；
//  2. 滚轮作用于**指针下方的控件**，而一个页面里往往同时存在页面主体、侧栏
//     列表、聊天记录区等多个面板——点错地方就滚错东西。
//
// 本文件定义平台无关的观测类型与纯函数（过滤 / 排序 / 点命中 / 轴状态派生），
// Windows 侧由 uia_windows.go 用 UI Automation 填充，非 Windows 侧返回
// ErrUnsupported 桩。这样"面板怎么筛、怎么排、怎么判边界"在任何平台都能单测，
// 平台实现只剩系统调用。

// UnknownScrollValue 是 UI Automation 在「该轴不可滚动 / 无值」时报告的值
// （UIA ScrollPattern 约定：不可滚动时 percent 与 viewSize 均为 -1）。
const UnknownScrollValue = -1

// DefaultScrollTargetLimit 是 computer_scroll_targets 缺省返回的面板条数上限。
const DefaultScrollTargetLimit = 12

// maxScrollTargetLimit 是调用方可以要求的条数上限：面板是给人看的观测结果，
// 一次返回几十条只会淹没有效信息。
const maxScrollTargetLimit = 40

// maxScrollTargetResults 是原语层的安全上限：无论调用方要多少，一次查询最多
// 带这么多条结果回来（单测与真机都用同一口径）。
const maxScrollTargetResults = 64

// DefaultScrollTargetTimeout 是一次 UI Automation 查询的缺省超时。UIA 是跨进程
// 调用，目标进程卡住时调用方会被拖住，因此查询必须有上限。这个上限要**大于**
// 内部兜底遍历的预算（`scrollTargetWalkTimeout` 3s），否则懒构建的可访问性树还没
// 走完，外层就先把整次查询掐掉了。
const DefaultScrollTargetTimeout = 5 * time.Second

// ScrollAxis 是一个面板在某条轴（纵 / 横）上的滚动状态。
type ScrollAxis struct {
	// Scrollable 报告该轴当前是否可以滚动（内容溢出视口）。
	Scrollable bool
	// Percent 是当前滚动位置（0-100，0 = 起始端，100 = 末端）；
	// 不可滚动或不可知时为 UnknownScrollValue。
	Percent float64
	// ViewSize 是视口占内容的比例（%）：100 表示内容刚好铺满视口，
	// 数值越小说明屏幕外的上下文越多。不可知时为 UnknownScrollValue。
	ViewSize float64
}

// Known 报告该轴是否带回了可用的位置数值。
func (a ScrollAxis) Known() bool {
	return a.Scrollable && a.Percent >= 0
}

// AtStart 报告该轴是否停在起始端（纵向 = 页面顶部 / 横向 = 最左）。
// 只有真正可滚动时才成立：不可滚动不是"已经在顶部"。
func (a ScrollAxis) AtStart() bool {
	return a.Known() && a.Percent < 0.5
}

// AtEnd 报告该轴是否停在末端（纵向 = 页面底部 / 横向 = 最右）。
func (a ScrollAxis) AtEnd() bool {
	return a.Known() && a.Percent > 99.5
}

// ScrollTarget 是一个「可以用滚轮操作、并借此看到更多上下文」的面板。
type ScrollTarget struct {
	// Name 是控件可读名（UIA Name）。网页里的滚动容器经常没有名字，允许为空。
	Name string
	// ControlType 是本地化控件类型（如 "pane" / "document" / "list"），
	// 用于让模型分辨"这是页面主体还是侧栏"。
	ControlType string
	// AutomationID / ClassName 是技术标识，用于区分同名的兄弟面板。
	AutomationID string
	ClassName    string
	// Rect 是面板矩形，坐标系与鼠标注入一致（虚拟桌面物理像素）。
	Rect Rect
	// Vertical / Horizontal 是两条轴的滚动状态。
	Vertical   ScrollAxis
	Horizontal ScrollAxis
}

// Scrollable 报告该面板是否至少有一条轴可以滚动。
func (t ScrollTarget) Scrollable() bool {
	return t.Vertical.Scrollable || t.Horizontal.Scrollable
}

// Area 返回面板面积（像素），用于"外层面板在前"的排序与最内层命中判定。
func (t ScrollTarget) Area() int {
	if t.Rect.Width <= 0 || t.Rect.Height <= 0 {
		return 0
	}
	return t.Rect.Width * t.Rect.Height
}

// Center 返回面板中心点：滚轮要落在面板内部，中心是最稳的落点。
func (t ScrollTarget) Center() Point {
	return t.Rect.Center()
}

// Contains 报告点是否落在面板矩形内。
func (t ScrollTarget) Contains(p Point) bool {
	return t.Rect.Contains(p.X, p.Y)
}

// ScrollTargetOptions 描述一次「找出页面里哪些面板可以滚轮操作」的查询。
type ScrollTargetOptions struct {
	// WindowHandle 限定查询范围（该窗口的子树）；0 表示当前前台窗口。
	WindowHandle uintptr
	// IncludeOffscreen 为 true 时保留 UI Automation 标记为离屏的面板
	//（缺省丢弃：离屏面板滚了也看不见）。
	IncludeOffscreen bool
	// Timeout 是整次查询的上限；0 用 DefaultScrollTargetTimeout。
	Timeout time.Duration
}

// NormalizeScrollTargets 过滤无效面板（零/负面积、两轴都不可滚动）、按面积降序
// 排列（外层容器在前，模型先看到页面主干再看到内层列表）并限流。
//
// 纯函数：任何平台都能单测，并且是"哪些面板算可滚轮操作"的唯一判据。
func NormalizeScrollTargets(targets []ScrollTarget, limit int) []ScrollTarget {
	filtered := make([]ScrollTarget, 0, len(targets))
	for _, target := range targets {
		if !target.Scrollable() {
			continue
		}
		if target.Rect.Width <= 0 || target.Rect.Height <= 0 {
			continue
		}
		filtered = append(filtered, target)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if left.Area() != right.Area() {
			return left.Area() > right.Area()
		}
		if left.Rect.Y != right.Rect.Y {
			return left.Rect.Y < right.Rect.Y
		}
		if left.Rect.X != right.Rect.X {
			return left.Rect.X < right.Rect.X
		}
		return left.Name < right.Name
	})
	// limit <= 0 表示"调用方自己再裁剪"：原语层仍保留安全上限。
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	if len(filtered) > maxScrollTargetResults {
		filtered = filtered[:maxScrollTargetResults]
	}
	return filtered
}

// PickScrollTargetAtPoint 返回包含该点、面积最小的可滚动面板：滚轮作用于指针
// 下方的控件，因此"这一点会滚到谁"的答案就是最内层命中。
//
// 纯函数；无命中返回 false（此时滚轮落点是"没有面板接住"的普通区域）。
func PickScrollTargetAtPoint(targets []ScrollTarget, p Point) (ScrollTarget, bool) {
	var best ScrollTarget
	found := false
	for _, target := range targets {
		if !target.Scrollable() || !target.Contains(p) {
			continue
		}
		if !found || target.Area() < best.Area() {
			best, found = target, true
		}
	}
	return best, found
}

// LargestVerticalScrollTarget 返回面积最大的纵向可滚动面板：调用方只给了窗口、
// 没给坐标时用它挑滚轮落点（页面主干一般就是最大的那块）。
func LargestVerticalScrollTarget(targets []ScrollTarget) (ScrollTarget, bool) {
	var best ScrollTarget
	found := false
	for _, target := range targets {
		if !target.Vertical.Scrollable {
			continue
		}
		if !found || target.Area() > best.Area() {
			best, found = target, true
		}
	}
	return best, found
}
