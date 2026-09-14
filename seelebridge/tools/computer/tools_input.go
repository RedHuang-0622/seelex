package computer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ── 输入类工具：点击 / 移动 / 拖拽 / 滚轮 / 文本 / 组合键 / 等待 ──
//
// 这些动作有真实副作用（会改变用户桌面），因此：
//   - 坐标一律是虚拟桌面物理像素（进程启动即声明 Per-Monitor V2 DPI 感知）；
//   - 参数越界显式报错，不静默改写用户意图（clicks、times、delta 都有硬上限）；
//   - 对子代理不可见（见 seelebridge/tools/policy.go）：并行子代理共享同一块
//     桌面，同时注入输入会互相打断。

type pointArg struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// actionResult 是所有输入类工具的统一结果形状：只说做了什么、在哪里做的，
// 不回显整段输入文本（会话记录里已经有参数）。
type actionResult struct {
	Action       string      `json:"action"`
	Point        *pointJSON  `json:"point,omitempty"`
	From         *pointJSON  `json:"from,omitempty"`
	To           *pointJSON  `json:"to,omitempty"`
	Button       string      `json:"button,omitempty"`
	Clicks       int         `json:"clicks,omitempty"`
	Delta        int         `json:"delta,omitempty"`
	Chars        int         `json:"chars,omitempty"`
	Keys         string      `json:"keys,omitempty"`
	Times        int         `json:"times,omitempty"`
	Milliseconds int         `json:"milliseconds,omitempty"`
	Window       *windowJSON `json:"window,omitempty"`
	Note         string      `json:"note,omitempty"`
}

type clickArgs struct {
	X          *int   `json:"x"`
	Y          *int   `json:"y"`
	Button     string `json:"button"`
	Clicks     int    `json:"clicks"`
	IntervalMS int    `json:"interval_ms"`
	Window     string `json:"window"`
}

func (t *Tools) click(_ context.Context, argsJSON string) (string, error) {
	var args clickArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	button := strings.ToLower(strings.TrimSpace(args.Button))
	if button == "" {
		button = "left"
	}
	switch button {
	case "left", "right", "middle":
	default:
		return "", fmt.Errorf("computer: button 只支持 left/right/middle，得到 %q", args.Button)
	}
	clicks := args.Clicks
	if clicks == 0 {
		clicks = 1
	}
	if clicks < 1 || clicks > 3 {
		return "", fmt.Errorf("computer: clicks 只支持 1-3，得到 %d", args.Clicks)
	}
	point, window, err := t.resolveTarget(args.X, args.Y, args.Window)
	if err != nil {
		return "", err
	}
	if err := t.ops.click(point, ClickOptions{
		Button:   button,
		Clicks:   clicks,
		Interval: time.Duration(clampInt(args.IntervalMS, 0, 2000)) * time.Millisecond,
	}); err != nil {
		return "", err
	}
	value := newPointJSON(point)
	result := actionResult{Action: "click", Point: &value, Button: button, Clicks: clicks}
	if window != nil {
		focused := newWindowJSON(*window)
		result.Window = &focused
	}
	return encodeResult(result)
}

type moveArgs struct {
	X *int `json:"x"`
	Y *int `json:"y"`
}

func (t *Tools) move(_ context.Context, argsJSON string) (string, error) {
	var args moveArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.X == nil || args.Y == nil {
		return "", errors.New("computer: 需要 x 与 y")
	}
	point := Point{X: *args.X, Y: *args.Y}
	if err := t.ops.moveMouse(point); err != nil {
		return "", err
	}
	value := newPointJSON(point)
	return encodeResult(actionResult{Action: "move", Point: &value})
}

type dragArgs struct {
	From       *pointArg `json:"from"`
	To         *pointArg `json:"to"`
	DurationMS int       `json:"duration_ms"`
}

func (t *Tools) drag(_ context.Context, argsJSON string) (string, error) {
	var args dragArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.From == nil || args.To == nil {
		return "", errors.New("computer: 需要 from 与 to（各含 x/y）")
	}
	from := Point{X: args.From.X, Y: args.From.Y}
	to := Point{X: args.To.X, Y: args.To.Y}
	duration := time.Duration(clampInt(args.DurationMS, 0, 10000)) * time.Millisecond
	if args.DurationMS == 0 {
		duration = 300 * time.Millisecond
	}
	if err := t.ops.drag(from, to, duration); err != nil {
		return "", err
	}
	fromValue, toValue := newPointJSON(from), newPointJSON(to)
	return encodeResult(actionResult{Action: "drag", From: &fromValue, To: &toValue})
}

type scrollArgs struct {
	X     *int `json:"x"`
	Y     *int `json:"y"`
	Delta int  `json:"delta"`
}

func (t *Tools) scroll(_ context.Context, argsJSON string) (string, error) {
	var args scrollArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.Delta == 0 {
		return "", errors.New("computer: delta 需要非零（120 = 一格，正数向上）")
	}
	if args.Delta > maxScrollDelta || args.Delta < -maxScrollDelta {
		return "", fmt.Errorf("computer: delta 绝对值上限 %d，得到 %d", maxScrollDelta, args.Delta)
	}
	point, err := t.resolvePoint(args.X, args.Y)
	if err != nil {
		return "", err
	}
	if err := t.ops.scroll(point, args.Delta); err != nil {
		return "", err
	}
	value := newPointJSON(point)
	return encodeResult(actionResult{Action: "scroll", Point: &value, Delta: args.Delta})
}

type typeArgs struct {
	Text string `json:"text"`
}

func (t *Tools) typeText(_ context.Context, argsJSON string) (string, error) {
	var args typeArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.Text == "" {
		return "", errors.New("computer: text 不能为空")
	}
	if count := utf8.RuneCountInString(args.Text); count > maxTypeChars {
		return "", fmt.Errorf("computer: text 长度上限 %d 字符，得到 %d", maxTypeChars, count)
	}
	if err := t.ops.typeText(args.Text); err != nil {
		return "", err
	}
	return encodeResult(actionResult{
		Action: "type", Chars: utf8.RuneCountInString(args.Text),
		Note: "文本已注入当前焦点控件；若字符未出现，先用 computer_focus/computer_click 把焦点落到目标控件。",
	})
}

type keysArgs struct {
	Keys  string `json:"keys"`
	Times int    `json:"times"`
}

func (t *Tools) keys(_ context.Context, argsJSON string) (string, error) {
	var args keysArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	combo := strings.TrimSpace(args.Keys)
	if combo == "" {
		return "", errors.New("computer: keys 不能为空（例如 ctrl+shift+t、enter）")
	}
	times := args.Times
	if times == 0 {
		times = 1
	}
	if times < 1 || times > 10 {
		return "", fmt.Errorf("computer: times 只支持 1-10，得到 %d", args.Times)
	}
	if err := t.ops.pressKeys(combo, times); err != nil {
		return "", err
	}
	return encodeResult(actionResult{Action: "keys", Keys: combo, Times: times})
}

type waitArgs struct {
	Milliseconds int `json:"milliseconds"`
}

func (t *Tools) wait(_ context.Context, argsJSON string) (string, error) {
	var args waitArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.Milliseconds < 0 {
		return "", errors.New("computer: milliseconds 不能为负")
	}
	ms := clampInt(args.Milliseconds, 0, maxWaitMilliseconds)
	t.ops.sleep(time.Duration(ms) * time.Millisecond)
	return encodeResult(actionResult{
		Action: "wait", Milliseconds: ms,
		Note: "等待结束；重新截图确认状态，而不是假设动作已经生效。",
	})
}

// resolveTarget 解析输入动作的目标坐标：显式 x/y 优先；只有 window 时先聚焦
// 再取窗口中心；两者都没有则显式报错（不猜"当前鼠标位置"做破坏性动作）。
func (t *Tools) resolveTarget(x, y *int, window string) (Point, *Window, error) {
	var focused *Window
	if match := strings.TrimSpace(window); match != "" {
		win, err := t.ops.focusWindow(match)
		if err != nil {
			return Point{}, nil, err
		}
		focused = &win
	}
	switch {
	case x != nil && y != nil:
		return Point{X: *x, Y: *y}, focused, nil
	case x != nil || y != nil:
		return Point{}, nil, errors.New("computer: x 与 y 需要同时给出")
	case focused != nil:
		return focused.Rect.Center(), focused, nil
	default:
		return Point{}, nil, errors.New("computer: 需要 x/y，或给出 window 以操作窗口中心")
	}
}

// resolvePoint 解析滚轮落点：显式 x/y 优先，否则用当前光标位置（滚轮作用于
// 指针下方的控件，缺省落到指针处符合直觉）。
func (t *Tools) resolvePoint(x, y *int) (Point, error) {
	switch {
	case x != nil && y != nil:
		return Point{X: *x, Y: *y}, nil
	case x != nil || y != nil:
		return Point{}, errors.New("computer: x 与 y 需要同时给出")
	default:
		return t.ops.cursor()
	}
}
