package computer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ── 滚轮工具面：可滚动面板识别 + 滚轮落点回读 ────────────────
//
// 滚轮与其它输入注入的区别是「它作用于指针下方那个控件」，而页面里通常同时
// 有页面主体、侧栏列表、聊天记录区等多个可滚动面板。只给坐标的滚轮工具会让
// 模型在"点错地方"和"滚了但不知道有没有滚到"之间反复试错，因此这里补两件事：
//
//   - computer_scroll_targets：把"哪些面板能滚、滚到哪了、外面还有多少内容"
//     变成一次只读观测；
//   - computer_scroll 的动作结果回读落点面板的滚前/滚后位置，让"是否还有上下文"
//     这件事在同一个工具结果里就有答案（拿不到时显式标注 unavailable）。

type scrollTargetsArgs struct {
	Window           string `json:"window"`
	IncludeOffscreen bool   `json:"include_offscreen"`
	Limit            int    `json:"limit"`
}

type scrollAxisJSON struct {
	Scrollable bool     `json:"scrollable"`
	Percent    *float64 `json:"percent,omitempty"`
	ViewSize   *float64 `json:"view_size,omitempty"`
	AtStart    bool     `json:"at_start"`
	AtEnd      bool     `json:"at_end"`
	// BeforePercent / Moved 只在滚轮动作的回读里出现：percent 是滚后位置，
	// before_percent 是滚前位置，两者一起回答"这一下滚动了没有"。
	BeforePercent *float64 `json:"before_percent,omitempty"`
	Moved         bool     `json:"moved,omitempty"`
}

type scrollTargetJSON struct {
	Index        int            `json:"index"`
	Name         string         `json:"name,omitempty"`
	ControlType  string         `json:"control_type,omitempty"`
	AutomationID string         `json:"automation_id,omitempty"`
	ClassName    string         `json:"class_name,omitempty"`
	Rect         rectJSON       `json:"rect"`
	Center       pointJSON      `json:"center"`
	Vertical     scrollAxisJSON `json:"vertical"`
	Horizontal   scrollAxisJSON `json:"horizontal"`
}

type scrollTargetsResult struct {
	Window      *windowJSON        `json:"window,omitempty"`
	Total       int                `json:"total"`
	Returned    int                `json:"returned"`
	Targets     []scrollTargetJSON `json:"targets"`
	Note        string             `json:"note"`
	Unavailable []string           `json:"unavailable,omitempty"`
}

// scrollPanelJSON 是滚轮落点上的面板（及其滚前/滚后位置）。
type scrollPanelJSON struct {
	Name        string         `json:"name,omitempty"`
	ControlType string         `json:"control_type,omitempty"`
	Rect        rectJSON       `json:"rect"`
	Vertical    scrollAxisJSON `json:"vertical"`
	Horizontal  scrollAxisJSON `json:"horizontal"`
}

func newScrollAxisJSON(axis ScrollAxis) scrollAxisJSON {
	value := scrollAxisJSON{AtStart: axis.AtStart(), AtEnd: axis.AtEnd()}
	value.Scrollable = axis.Scrollable
	if axis.Scrollable && axis.Percent >= 0 {
		percent := axis.Percent
		value.Percent = &percent
	}
	if axis.Scrollable && axis.ViewSize >= 0 {
		viewSize := axis.ViewSize
		value.ViewSize = &viewSize
	}
	return value
}

func newScrollTargetJSON(index int, target ScrollTarget) scrollTargetJSON {
	return scrollTargetJSON{
		Index:        index,
		Name:         target.Name,
		ControlType:  target.ControlType,
		AutomationID: target.AutomationID,
		ClassName:    target.ClassName,
		Rect:         newRectJSON(target.Rect),
		Center:       newPointJSON(target.Center()),
		Vertical:     newScrollAxisJSON(target.Vertical),
		Horizontal:   newScrollAxisJSON(target.Horizontal),
	}
}

// scrollTargets 是 computer_scroll_targets 的实现：只读枚举窗口里的可滚动面板。
func (t *Tools) scrollTargets(_ context.Context, argsJSON string) (string, error) {
	var args scrollTargetsArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = DefaultScrollTargetLimit
	}
	limit = clampInt(limit, 1, maxScrollTargetLimit)

	window, err := t.resolveScrollWindow(args.Window)
	if err != nil {
		return "", err
	}
	options := ScrollTargetOptions{IncludeOffscreen: args.IncludeOffscreen}
	if window != nil {
		options.WindowHandle = window.Handle
	}
	targets, err := t.ops.scrollTargets(options)
	if err != nil {
		return "", err
	}
	// total 是过滤后的全部面板数，returned 是本次 limit 内的条数：裁剪前后的口径
	// 分开报，模型才知道"还有多少没列出来"。
	all := NormalizeScrollTargets(targets, 0)
	listed := NormalizeScrollTargets(all, limit)

	result := scrollTargetsResult{
		Total:   len(all),
		Targets: make([]scrollTargetJSON, 0, len(listed)),
		Note: "面板按面积从大到小（外层容器在前）；center 可直接作为 computer_scroll 的 x/y。" +
			"vertical.at_end=true 表示该面板已到底、下方没有更多内容；view_size 越小说明屏幕外的内容越多。",
	}
	if window != nil {
		value := newWindowJSON(*window)
		result.Window = &value
	}
	for index, target := range listed {
		result.Targets = append(result.Targets, newScrollTargetJSON(index, target))
	}
	result.Returned = len(result.Targets)
	if len(result.Targets) == 0 {
		result.Note = "该窗口里没有识别到可滚动的面板：可能内容正好铺满（无需滚动），" +
			"也可能这个窗口没有 UI Automation 提供者（自绘界面、游戏等）。" +
			"后者可以先用 computer_keys 发 pgdn/pgup（先 computer_focus 把焦点落到目标窗口），" +
			"或直接 computer_scroll 指定坐标。"
	}
	return encodeResult(result)
}

// resolveScrollWindow 找出查询/滚动目标窗口：给了 match 就按标题子串匹配
// （只读，不聚焦），否则用当前前台窗口。
func (t *Tools) resolveScrollWindow(match string) (*Window, error) {
	needle := strings.ToLower(strings.TrimSpace(match))
	if needle == "" {
		foreground, err := t.ops.foreground()
		if err != nil {
			return nil, err
		}
		return &foreground, nil
	}
	windows, err := t.ops.listWindows()
	if err != nil {
		return nil, err
	}
	var candidate *Window
	for _, win := range windows {
		if !win.Visible || !strings.Contains(strings.ToLower(win.Title), needle) {
			continue
		}
		selected := win
		candidate = &selected
		if !win.Minimized {
			break
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("computer: 未找到标题包含 %q 的可见窗口", match)
	}
	return candidate, nil
}

// largestScrollTarget 选窗口里面积最大的纵向可滚动面板；查不到返回 false，
// 调用方退回窗口中心。
func (t *Tools) largestScrollTarget(handle uintptr) (ScrollTarget, bool) {
	targets, err := t.ops.scrollTargets(ScrollTargetOptions{WindowHandle: handle})
	if err != nil {
		return ScrollTarget{}, false
	}
	return LargestVerticalScrollTarget(targets)
}

// scrollPanelAt 读"这一点上的滚轮会滚到哪个面板"。取不到（没有面板 / UIA 不可用）
// 返回 false：滚轮本身照常执行，只是结果里少一块回读信息。
func (t *Tools) scrollPanelAt(point Point) (ScrollTarget, bool) {
	target, ok, err := t.ops.scrollState(point, scrollReadbackTimeout)
	if err != nil {
		return ScrollTarget{}, false
	}
	return target, ok
}

// scrollPanelResult 组合滚前/滚后两块观测，生成动作结果里的 panel 字段。
func scrollPanelResult(after ScrollTarget, before ScrollTarget, hadBefore bool) scrollPanelJSON {
	panel := scrollPanelJSON{
		Name:        after.Name,
		ControlType: after.ControlType,
		Rect:        newRectJSON(after.Rect),
		Vertical:    newScrollAxisJSON(after.Vertical),
		Horizontal:  newScrollAxisJSON(after.Horizontal),
	}
	// 只有两轴都是同一块面板时，"滚前 versus 滚后"才有意义。
	if !hadBefore || before.Rect != after.Rect {
		return panel
	}
	attachScrollDelta(&panel.Vertical, before.Vertical, after.Vertical)
	attachScrollDelta(&panel.Horizontal, before.Horizontal, after.Horizontal)
	return panel
}

func attachScrollDelta(axis *scrollAxisJSON, before, after ScrollAxis) {
	if !before.Known() || !after.Known() {
		return
	}
	beforePercent := before.Percent
	axis.BeforePercent = &beforePercent
	axis.Moved = beforePercent != after.Percent
}

// scrollNote 用一句人话总结滚轮结果：模型据此决定"继续滚 / 到头了 / 换个面板"。
func scrollNote(panel *scrollPanelJSON, unavailable []string) string {
	if panel == nil {
		if len(unavailable) > 0 {
			return "滚轮已下发，但落点上的面板状态取不到（UI Automation 不可用或无滚动面板）；" +
				"请重新截图确认是否真的滚动了。"
		}
		return "滚轮已下发；请重新截图确认。"
	}
	switch {
	case panel.Vertical.AtEnd:
		return "已滚到该面板底部（vertical.percent≈100）：下方没有更多内容；需要继续找上下文请换别的面板或窗口。"
	case panel.Vertical.AtStart:
		return "已回到该面板顶部（vertical.percent≈0）：上方没有更多内容。"
	case panel.Vertical.Moved:
		return fmt.Sprintf("面板已滚动：vertical.percent %s → %s；如需继续，再发一次 computer_scroll。", formatPercent(panel.Vertical.BeforePercent), formatPercent(panel.Vertical.Percent))
	case panel.Vertical.Scrollable:
		return "该面板可滚动但位置没有变化：可能已经到头，或这次 delta 太小；必要时加大 delta 或先截图确认。"
	default:
		return "落点面板只有横向可滚动（纵向未变化）。"
	}
}

func formatPercent(value *float64) string {
	if value == nil {
		return "未知"
	}
	return fmt.Sprintf("%.1f%%", *value)
}

// scrollReadbackTimeout 是滚轮动作回读的等待上限（短于观测工具的缺省超时：
// 回读失败不该拖慢一次输入动作）。
const scrollReadbackTimeout = 2 * time.Second
