// Command computer-use-mcp 通过 MCP stdio 协议暴露桌面 computer use 工具：
// 截图、鼠标、键盘、窗口枚举与聚焦。
//
// 典型用法是在 Codex 配置中注册为 MCP server：
//
//	codex mcp add computer -- G:\path\to\computer-use-mcp.exe
//
// 服务端只做输入注入与截屏，不做任何业务判断；安全边界由调用方（模型与用户审批）
// 负责，详见 seelebridge/tools/computer/README.md。
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

const (
	serverName    = "computer-use"
	serverVersion = "0.1.0"
	maxLineBytes  = 32 << 20
	defaultWidth  = 1600
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func textResult(format string, args ...any) toolResult {
	return toolResult{Content: []toolContent{{Type: "text", Text: fmt.Sprintf(format, args...)}}}
}

func errorResult(err error) toolResult {
	return toolResult{
		IsError: true,
		Content: []toolContent{{Type: "text", Text: "computer-use error: " + err.Error()}},
	}
}

type regionArg struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (r *regionArg) rect() *computer.Rect {
	if r == nil {
		return nil
	}
	return &computer.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
}

type pointArg struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func shotDir() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "codex-computer-use", "shots")
	}
	return filepath.Join(os.TempDir(), "codex-computer-use", "shots")
}

// captureAndEncode 截屏、落盘并生成 MCP 内容块。
func captureAndEncode(region *computer.Rect, maxWidth int, savePath string, inline bool, cursor *computer.Point) ([]toolContent, error) {
	capture, err := computer.CaptureShot(computer.ScreenshotOptions{Region: region, MaxWidth: maxWidth})
	if err != nil {
		return nil, err
	}
	if savePath == "" {
		savePath = filepath.Join(shotDir(), fmt.Sprintf("shot-%s.png", time.Now().Format("20060102-150405.000")))
	}
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return nil, fmt.Errorf("computer: 创建截图目录失败: %w", err)
	}
	file, err := os.Create(savePath)
	if err != nil {
		return nil, fmt.Errorf("computer: 创建截图文件失败: %w", err)
	}
	defer file.Close()
	if err := png.Encode(file, capture.Image); err != nil {
		return nil, fmt.Errorf("computer: 编码 PNG 失败: %w", err)
	}
	bounds := capture.Image.Bounds()
	point := capture.Cursor
	if cursor != nil {
		point = *cursor
	}
	summary := fmt.Sprintf(
		"screenshot saved: %s\nvirtual region=%s image=%dx%d scale=%.3f cursor=(%d,%d)\n坐标一律使用虚拟桌面物理像素；image 尺寸只影响观察，不影响坐标。",
		savePath, capture.Region, bounds.Dx(), bounds.Dy(), capture.Scale, point.X, point.Y,
	)
	content := []toolContent{{Type: "text", Text: summary}}
	if !inline {
		return content, nil
	}
	var buf strings.Builder
	buf.Grow(bounds.Dx() * bounds.Dy())
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	if err := png.Encode(encoder, capture.Image); err != nil {
		return content, nil
	}
	if err := encoder.Close(); err != nil {
		return content, nil
	}
	content = append(content, toolContent{Type: "image", Data: buf.String(), MimeType: "image/png"})
	return content, nil
}

func inlineImage(flag *bool) bool {
	if flag == nil {
		return true
	}
	return *flag
}

func main() {
	computer.EnableDPIAwareness()
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 64*1024), maxLineBytes)
	writer := bufio.NewWriter(os.Stdout)
	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "无法解析 JSON-RPC 消息"}})
			continue
		}
		if len(req.ID) == 0 {
			// 客户端通知，无需响应。
			continue
		}
		writeResponse(writer, handle(req))
	}
}

func writeResponse(writer *bufio.Writer, resp rpcResponse) {
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		return
	}
	writer.Write(encoded)
	writer.WriteByte('\n')
	writer.Flush()
}

func handle(req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		protocol := "2024-11-05"
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err == nil && params.ProtocolVersion != "" {
				protocol = params.ProtocolVersion
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": protocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefinitions()}
	case "tools/call":
		result, err := callTool(req.Params)
		if err != nil {
			resp.Result = errorResult(err)
			break
		}
		resp.Result = result
	default:
		resp.Error = &rpcError{Code: -32601, Message: "不支持的方法: " + req.Method}
	}
	return resp
}

func callTool(params json.RawMessage) (toolResult, error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return toolResult{}, fmt.Errorf("参数非法: %w", err)
	}
	switch call.Name {
	case "virtual_screen":
		rect, err := computer.VirtualScreen()
		if err != nil {
			return toolResult{}, err
		}
		cursor, _ := computer.CursorPosition()
		return textResult("virtual screen=%s cursor=(%d,%d)", rect, cursor.X, cursor.Y), nil

	case "screenshot":
		var args struct {
			Region   *regionArg `json:"region"`
			MaxWidth int        `json:"max_width"`
			SavePath string     `json:"save_path"`
			Inline   *bool      `json:"inline_image"`
		}
		if len(call.Arguments) > 0 {
			if err := json.Unmarshal(call.Arguments, &args); err != nil {
				return toolResult{}, fmt.Errorf("screenshot 参数非法: %w", err)
			}
		}
		maxWidth := args.MaxWidth
		if maxWidth == 0 {
			maxWidth = defaultWidth
		}
		if maxWidth < 0 {
			maxWidth = 0
		}
		content, err := captureAndEncode(args.Region.rect(), maxWidth, args.SavePath, inlineImage(args.Inline), nil)
		if err != nil {
			return toolResult{}, err
		}
		return toolResult{Content: content}, nil

	case "cursor_position":
		cursor, err := computer.CursorPosition()
		if err != nil {
			return toolResult{}, err
		}
		return textResult("cursor=(%d,%d)", cursor.X, cursor.Y), nil

	case "move_mouse":
		var args struct {
			pointArg
			WithScreenshot bool `json:"with_screenshot"`
			MaxWidth       int  `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("move_mouse 参数非法: %w", err)
		}
		if err := computer.MoveMouse(computer.Point{X: args.X, Y: args.Y}); err != nil {
			return toolResult{}, err
		}
		return actionResult(fmt.Sprintf("moved cursor to (%d,%d)", args.X, args.Y), args.WithScreenshot, args.MaxWidth)

	case "click":
		var args struct {
			pointArg
			Button         string `json:"button"`
			Clicks         int    `json:"clicks"`
			IntervalMS     int    `json:"interval_ms"`
			WithScreenshot bool   `json:"with_screenshot"`
			MaxWidth       int    `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("click 参数非法: %w", err)
		}
		opts := computer.ClickOptions{
			Button: args.Button,
			Clicks: args.Clicks,
		}
		if args.IntervalMS > 0 {
			opts.Interval = time.Duration(args.IntervalMS) * time.Millisecond
		}
		if err := computer.Click(computer.Point{X: args.X, Y: args.Y}, opts); err != nil {
			return toolResult{}, err
		}
		summary := fmt.Sprintf("clicked %s x%d at (%d,%d)", buttonName(args.Button), maxInt(args.Clicks, 1), args.X, args.Y)
		return actionResult(summary, args.WithScreenshot, args.MaxWidth)

	case "drag":
		var args struct {
			FromX          int  `json:"from_x"`
			FromY          int  `json:"from_y"`
			ToX            int  `json:"to_x"`
			ToY            int  `json:"to_y"`
			DurationMS     int  `json:"duration_ms"`
			WithScreenshot bool `json:"with_screenshot"`
			MaxWidth       int  `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("drag 参数非法: %w", err)
		}
		duration := time.Duration(args.DurationMS) * time.Millisecond
		if duration <= 0 {
			duration = 300 * time.Millisecond
		}
		if err := computer.Drag(
			computer.Point{X: args.FromX, Y: args.FromY},
			computer.Point{X: args.ToX, Y: args.ToY},
			duration,
		); err != nil {
			return toolResult{}, err
		}
		summary := fmt.Sprintf("dragged (%d,%d) -> (%d,%d)", args.FromX, args.FromY, args.ToX, args.ToY)
		return actionResult(summary, args.WithScreenshot, args.MaxWidth)

	case "scroll":
		var args struct {
			X              int  `json:"x"`
			Y              int  `json:"y"`
			Delta          int  `json:"delta"`
			WithScreenshot bool `json:"with_screenshot"`
			MaxWidth       int  `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("scroll 参数非法: %w", err)
		}
		if args.Delta == 0 {
			args.Delta = -120
		}
		if err := computer.Scroll(computer.Point{X: args.X, Y: args.Y}, args.Delta); err != nil {
			return toolResult{}, err
		}
		return actionResult(fmt.Sprintf("scrolled delta=%d at (%d,%d)", args.Delta, args.X, args.Y), args.WithScreenshot, args.MaxWidth)

	case "type_text":
		var args struct {
			Text           string `json:"text"`
			WithScreenshot bool   `json:"with_screenshot"`
			MaxWidth       int    `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("type_text 参数非法: %w", err)
		}
		if err := computer.TypeText(args.Text); err != nil {
			return toolResult{}, err
		}
		return actionResult(fmt.Sprintf("typed %d 字符", len([]rune(args.Text))), args.WithScreenshot, args.MaxWidth)

	case "press_keys":
		var args struct {
			Keys           string `json:"keys"`
			Times          int    `json:"times"`
			WithScreenshot bool   `json:"with_screenshot"`
			MaxWidth       int    `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("press_keys 参数非法: %w", err)
		}
		if err := computer.PressKeys(args.Keys, args.Times); err != nil {
			return toolResult{}, err
		}
		return actionResult(fmt.Sprintf("pressed %s x%d", args.Keys, maxInt(args.Times, 1)), args.WithScreenshot, args.MaxWidth)

	case "list_windows":
		windows, err := computer.ListWindows()
		if err != nil {
			return toolResult{}, err
		}
		if len(windows) == 0 {
			return textResult("没有可见的顶层窗口"), nil
		}
		var builder strings.Builder
		builder.WriteString("可见窗口：\n")
		for _, win := range windows {
			state := "normal"
			if win.Minimized {
				state = "minimized"
			} else if !win.Visible {
				state = "hidden"
			}
			fmt.Fprintf(&builder, "- %s [%s] handle=0x%x\n", win.Title, state, win.Handle)
			if win.Rect.Width > 0 && win.Rect.Height > 0 {
				fmt.Fprintf(&builder, "  rect=%s 中心=(%d,%d)\n", win.Rect, win.Rect.Center().X, win.Rect.Center().Y)
			}
		}
		return toolResult{Content: []toolContent{{Type: "text", Text: builder.String()}}}, nil

	case "foreground_window":
		win, err := computer.ForegroundWindow()
		if err != nil {
			return toolResult{}, err
		}
		return textResult("前台窗口: %q rect=%s handle=0x%x", win.Title, win.Rect, win.Handle), nil

	case "focus_window":
		var args struct {
			Title          string `json:"title"`
			WithScreenshot bool   `json:"with_screenshot"`
			MaxWidth       int    `json:"max_width"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("focus_window 参数非法: %w", err)
		}
		win, err := computer.FocusWindow(args.Title)
		if err != nil {
			return toolResult{}, err
		}
		summary := fmt.Sprintf("focused %q rect=%s 中心=(%d,%d)", win.Title, win.Rect, win.Rect.Center().X, win.Rect.Center().Y)
		return actionResult(summary, args.WithScreenshot, args.MaxWidth)

	case "sleep":
		var args struct {
			Milliseconds int `json:"milliseconds"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return toolResult{}, fmt.Errorf("sleep 参数非法: %w", err)
		}
		ms := args.Milliseconds
		if ms < 0 {
			ms = 0
		}
		if ms > 30000 {
			ms = 30000
		}
		computer.Sleep(ms)
		return textResult("slept %dms", ms), nil

	default:
		return toolResult{}, fmt.Errorf("未知工具 %q", call.Name)
	}
}

func actionResult(summary string, withScreenshot bool, maxWidth int) (toolResult, error) {
	if !withScreenshot {
		return textResult("%s", summary), nil
	}
	if maxWidth == 0 {
		maxWidth = defaultWidth
	}
	computer.Sleep(150)
	content, err := captureAndEncode(nil, maxWidth, "", true, nil)
	if err != nil {
		return textResult("%s（截图失败: %v）", summary, err), nil
	}
	content = append([]toolContent{{Type: "text", Text: summary}}, content...)
	return toolResult{Content: content}, nil
}

func buttonName(button string) string {
	if button == "" {
		return "left"
	}
	return button
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func obj(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func toolDefinitions() []toolDef {
	regionSchema := map[string]any{
		"type":        "object",
		"description": "可选：截图区域（虚拟桌面物理像素）",
		"properties": map[string]any{
			"x":      map[string]any{"type": "integer"},
			"y":      map[string]any{"type": "integer"},
			"width":  map[string]any{"type": "integer"},
			"height": map[string]any{"type": "integer"},
		},
	}
	screenshotExtras := map[string]any{
		"with_screenshot": map[string]any{
			"type":        "boolean",
			"description": "为 true 时在动作后附带一张新截图",
		},
		"max_width": map[string]any{
			"type":        "integer",
			"description": "截图返回的最大宽度（默认 1600，0 表示不缩放）",
		},
	}
	withProps := func(base map[string]any) map[string]any {
		merged := map[string]any{}
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range screenshotExtras {
			merged[k] = v
		}
		return merged
	}
	return []toolDef{
		{
			Name:        "virtual_screen",
			Description: "返回虚拟桌面（多显示器并集）矩形与当前光标位置。",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "screenshot",
			Description: "截取屏幕并保存为 PNG，同时以图像内容返回供模型查看。坐标使用虚拟桌面物理像素。",
			InputSchema: obj(map[string]any{
				"region":       regionSchema,
				"max_width":    map[string]any{"type": "integer", "description": "返回图像最大宽度，默认 1600，0 表示原尺寸"},
				"save_path":    map[string]any{"type": "string", "description": "可选：PNG 保存路径，默认落在 %LOCALAPPDATA%\\codex-computer-use\\shots"},
				"inline_image": map[string]any{"type": "boolean", "description": "是否内联返回图像，默认 true；仅需文件路径时可设为 false"},
			}),
		},
		{
			Name:        "cursor_position",
			Description: "返回当前光标坐标。",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "move_mouse",
			Description: "把鼠标移动到指定坐标（虚拟桌面物理像素）。",
			InputSchema: obj(withProps(map[string]any{
				"x": map[string]any{"type": "integer"},
				"y": map[string]any{"type": "integer"},
			}), "x", "y"),
		},
		{
			Name:        "click",
			Description: "在指定坐标点击鼠标。按钮默认 left，clicks=2 表示双击。",
			InputSchema: obj(withProps(map[string]any{
				"x":           map[string]any{"type": "integer"},
				"y":           map[string]any{"type": "integer"},
				"button":      map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}},
				"clicks":      map[string]any{"type": "integer"},
				"interval_ms": map[string]any{"type": "integer", "description": "多次点击间隔，默认 60ms"},
			}), "x", "y"),
		},
		{
			Name:        "drag",
			Description: "按住左键从起点拖到终点，用于框选、拖拽滑块或窗口。",
			InputSchema: obj(withProps(map[string]any{
				"from_x":      map[string]any{"type": "integer"},
				"from_y":      map[string]any{"type": "integer"},
				"to_x":        map[string]any{"type": "integer"},
				"to_y":        map[string]any{"type": "integer"},
				"duration_ms": map[string]any{"type": "integer", "description": "拖拽时长，默认 300ms"},
			}), "from_x", "from_y", "to_x", "to_y"),
		},
		{
			Name:        "scroll",
			Description: "在指定坐标滚动滚轮。delta 为 120 的倍数，正数向上、负数向下。",
			InputSchema: obj(withProps(map[string]any{
				"x":     map[string]any{"type": "integer"},
				"y":     map[string]any{"type": "integer"},
				"delta": map[string]any{"type": "integer", "description": "默认 -120（向下滚一格）"},
			}), "x", "y"),
		},
		{
			Name:        "type_text",
			Description: "以 Unicode 方式输入文本，支持中文与 emoji；输入前请先用 click 聚焦目标控件。",
			InputSchema: obj(withProps(map[string]any{
				"text": map[string]any{"type": "string"},
			}), "text"),
		},
		{
			Name:        "press_keys",
			Description: "发送按键组合，如 enter、tab、esc、ctrl+c、alt+f4、shift+tab；times 为重复次数。",
			InputSchema: obj(withProps(map[string]any{
				"keys":  map[string]any{"type": "string"},
				"times": map[string]any{"type": "integer"},
			}), "keys"),
		},
		{
			Name:        "list_windows",
			Description: "列出有标题的顶层窗口及其矩形、状态与句柄。",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "foreground_window",
			Description: "返回当前前台窗口（标题、矩形、句柄），用于确认键盘输入会落到哪个窗口。",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "focus_window",
			Description: "按标题子串（大小写不敏感）查找窗口并置前；窗口最小化时会先还原。",
			InputSchema: obj(withProps(map[string]any{
				"title": map[string]any{"type": "string"},
			}), "title"),
		},
		{
			Name:        "sleep",
			Description: "等待指定毫秒数，最多 30000ms，用于等待界面稳定。",
			InputSchema: obj(map[string]any{
				"milliseconds": map[string]any{"type": "integer"},
			}, "milliseconds"),
		},
	}
}
