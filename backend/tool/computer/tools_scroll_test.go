package computer

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── 滚轮工具层单测（原语注入假实现，任何平台都能跑）──────────

type scrollTargetsPayload struct {
	Window struct {
		Handle string `json:"handle"`
		Title  string `json:"title"`
	} `json:"window"`
	Total    int `json:"total"`
	Returned int `json:"returned"`
	Targets  []struct {
		Index       int    `json:"index"`
		Name        string `json:"name"`
		ControlType string `json:"control_type"`
		Rect        struct {
			X      int `json:"x"`
			Y      int `json:"y"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"rect"`
		Center struct {
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"center"`
		Vertical struct {
			Scrollable bool     `json:"scrollable"`
			Percent    *float64 `json:"percent"`
			ViewSize   *float64 `json:"view_size"`
			AtStart    bool     `json:"at_start"`
			AtEnd      bool     `json:"at_end"`
		} `json:"vertical"`
		Horizontal struct {
			Scrollable bool `json:"scrollable"`
		} `json:"horizontal"`
	} `json:"targets"`
	Note        string   `json:"note"`
	Unavailable []string `json:"unavailable"`
}

type scrollPayload struct {
	Action      string   `json:"action"`
	Delta       int      `json:"delta"`
	Unavailable []string `json:"unavailable"`
	Note        string   `json:"note"`
	Window      *struct {
		Title string `json:"title"`
	} `json:"window"`
	Panel *struct {
		Name     string `json:"name"`
		Vertical struct {
			Scrollable    bool     `json:"scrollable"`
			Percent       *float64 `json:"percent"`
			BeforePercent *float64 `json:"before_percent"`
			Moved         bool     `json:"moved"`
			AtStart       bool     `json:"at_start"`
			AtEnd         bool     `json:"at_end"`
		} `json:"vertical"`
	} `json:"panel"`
}

func scrollTargetFixture(name string, x, y, width, height int, scrollable bool, percent float64) ScrollTarget {
	target := ScrollTarget{
		Name:        name,
		ControlType: "pane",
		ClassName:   name + "-panel",
		Rect:        Rect{X: x, Y: y, Width: width, Height: height},
		Vertical:    ScrollAxis{Scrollable: scrollable, Percent: percent, ViewSize: 50},
	}
	if !scrollable {
		target.Vertical.Percent, target.Vertical.ViewSize = UnknownScrollValue, UnknownScrollValue
	}
	return target
}

func TestScrollTargetsListsPanelsOfMatchedWindow(t *testing.T) {
	h := newHarness(t)
	h.recorder.scrollTargets = []ScrollTarget{
		scrollTargetFixture("body", 0, 0, 900, 700, true, 40),
		scrollTargetFixture("sidebar", 10, 10, 200, 300, true, 0),
		scrollTargetFixture("static", 0, 0, 500, 500, false, UnknownScrollValue),
	}
	result := h.call(t, ToolScrollTargets, `{"window":"Notes","limit":1}`)

	if len(h.recorder.scrollTargetQueries) != 1 {
		t.Fatalf("面板查询次数 = %d, want 1", len(h.recorder.scrollTargetQueries))
	}
	if got := h.recorder.scrollTargetQueries[0].WindowHandle; got != 0x2 {
		t.Fatalf("查询窗口句柄 = 0x%X, want 0x2（按标题匹配到的窗口）", got)
	}
	if len(h.recorder.focusCalls) != 0 {
		t.Fatalf("computer_scroll_targets 必须只读，实际聚焦了 %v", h.recorder.focusCalls)
	}

	var payload scrollTargetsPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("解析结果: %v (%s)", err, result)
	}
	if payload.Window.Title != "Notes - Editor" {
		t.Fatalf("窗口 = %q, want 匹配到的标题", payload.Window.Title)
	}
	if payload.Total != 2 || payload.Returned != 1 {
		t.Fatalf("total/returned = %d/%d, want 2/1（过滤掉不可滚动面板后 2 个，limit=1 只列 1 个）",
			payload.Total, payload.Returned)
	}
	top := payload.Targets[0]
	if top.Name != "body" {
		t.Fatalf("第一条应是面积最大的面板，得到 %q", top.Name)
	}
	if top.Center.X != 450 || top.Center.Y != 350 {
		t.Fatalf("center = (%d,%d), want (450,350)", top.Center.X, top.Center.Y)
	}
	if top.Vertical.Percent == nil || *top.Vertical.Percent != 40 {
		t.Fatalf("vertical.percent = %v, want 40", top.Vertical.Percent)
	}
	if top.Vertical.AtEnd || !top.Vertical.Scrollable {
		t.Fatalf("vertical 状态 = %+v", top.Vertical)
	}
}

func TestScrollTargetsDefaultsToForegroundWindowAndExplainsEmptyResult(t *testing.T) {
	h := newHarness(t)
	result := h.call(t, ToolScrollTargets, `{}`)
	if len(h.recorder.scrollTargetQueries) != 1 {
		t.Fatalf("面板查询次数 = %d, want 1", len(h.recorder.scrollTargetQueries))
	}
	if got := h.recorder.scrollTargetQueries[0].WindowHandle; got != 0xAA {
		t.Fatalf("缺省应查前台窗口 0xAA，得到 0x%X", got)
	}
	var payload scrollTargetsPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("解析结果: %v (%s)", err, result)
	}
	if payload.Total != 0 || len(payload.Targets) != 0 {
		t.Fatalf("无面板时应返回空列表，得到 %+v", payload)
	}
	if !strings.Contains(payload.Note, "pgdn") {
		t.Fatalf("空结果要给出回退办法（键盘滚动），得到 %q", payload.Note)
	}
}

func TestScrollTargetsUnknownWindowAndPrimitiveFailure(t *testing.T) {
	h := newHarness(t)
	h.callErr(t, ToolScrollTargets, `{"window":"不存在的窗口"}`)
	if len(h.recorder.scrollTargetQueries) != 0 {
		t.Fatal("窗口没匹配上时不该查询面板")
	}

	h = newHarness(t)
	h.recorder.scrollTargetsErr = ErrUnsupported
	if err := h.callErr(t, ToolScrollTargets, `{}`); !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("原语错误应透传，得到 %v", err)
	}
}

func TestScrollReportsPanelBeforeAfter(t *testing.T) {
	h := newHarness(t)
	before := scrollTargetFixture("messages", 100, 100, 400, 400, true, 12.5)
	after := before
	after.Vertical.Percent = 37.5
	call := 0
	h.recorder.scrollStateFn = func(Point) (ScrollTarget, bool, error) {
		call++
		if call == 1 {
			return before, true, nil
		}
		return after, true, nil
	}

	result := h.call(t, ToolScroll, `{"x":100,"y":100,"delta":-360}`)
	var payload scrollPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("解析结果: %v (%s)", err, result)
	}
	if payload.Panel == nil {
		t.Fatalf("结果应带落点面板: %s", result)
	}
	if !payload.Panel.Vertical.Moved {
		t.Fatalf("滚前 12.5 → 滚后 37.5 应判定为已移动: %+v", payload.Panel.Vertical)
	}
	if payload.Panel.Vertical.BeforePercent == nil || *payload.Panel.Vertical.BeforePercent != 12.5 {
		t.Fatalf("before_percent = %v, want 12.5", payload.Panel.Vertical.BeforePercent)
	}
	if payload.Panel.Vertical.Percent == nil || *payload.Panel.Vertical.Percent != 37.5 {
		t.Fatalf("percent = %v, want 37.5", payload.Panel.Vertical.Percent)
	}
	if !strings.Contains(payload.Note, "37.5") {
		t.Fatalf("note 应说明滚后位置，得到 %q", payload.Note)
	}
}

func TestScrollMarksUnavailableWhenPanelUnknown(t *testing.T) {
	h := newHarness(t)
	result := h.call(t, ToolScroll, `{"x":5,"y":5,"delta":120}`)
	var payload scrollPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("解析结果: %v (%s)", err, result)
	}
	if payload.Panel != nil {
		t.Fatalf("取不到面板时不该编造 panel: %s", result)
	}
	if len(payload.Unavailable) != 1 || payload.Unavailable[0] != "scroll_panel" {
		t.Fatalf("unavailable = %v, want [scroll_panel]", payload.Unavailable)
	}
	if !strings.Contains(payload.Note, "重新截图") {
		t.Fatalf("note 应提示重新截图确认，得到 %q", payload.Note)
	}
}

func TestScrollWithWindowTargetsLargestPanel(t *testing.T) {
	h := newHarness(t)
	h.recorder.scrollTargets = []ScrollTarget{
		scrollTargetFixture("sidebar", 1000, 0, 100, 600, true, 0),
		scrollTargetFixture("body", 0, 0, 900, 700, true, 0),
	}
	h.call(t, ToolScroll, `{"window":"Notes","delta":-120}`)

	if len(h.recorder.focusCalls) != 1 || h.recorder.focusCalls[0] != "Notes" {
		t.Fatalf("给了 window 应先聚焦，得到 %v", h.recorder.focusCalls)
	}
	if len(h.recorder.scrollCalls) != 1 {
		t.Fatalf("滚轮调用 = %+v", h.recorder.scrollCalls)
	}
	if got := h.recorder.scrollCalls[0].point; got != (Point{X: 450, Y: 350}) {
		t.Fatalf("缺省落点应为最大面板中心 (450,350)，得到 %+v", got)
	}
}
