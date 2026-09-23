package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

// newTempPNG 在临时目录写一张 PNG，返回路径。
func newTempPNG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: uint8(x % 256), B: uint8(y % 256), A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("写入测试 PNG 失败: %v", err)
	}
	return path
}

// callViewImage 以 MCP 参数形状调用 view_image。
func callViewImage(t *testing.T, arguments map[string]any) (toolResult, error) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": "view_image", "arguments": arguments})
	if err != nil {
		t.Fatalf("编码参数失败: %v", err)
	}
	return callTool(params)
}

func TestViewImageToolReturnsInlineImage(t *testing.T) {
	path := newTempPNG(t, t.TempDir(), "shot.png", 320, 240)

	result, err := callViewImage(t, map[string]any{"path": path})
	if err != nil {
		t.Fatalf("view_image 失败: %v", err)
	}
	if result.IsError {
		t.Fatalf("view_image 不该返回 isError")
	}
	if len(result.Content) != 2 {
		t.Fatalf("content 块数 = %d, want 2（文本 + 图像）", len(result.Content))
	}
	if result.Content[0].Type != "text" || !strings.Contains(result.Content[0].Text, path) {
		t.Fatalf("首个文本块应带路径，实际 %q", result.Content[0].Text)
	}
	imageBlock := result.Content[1]
	if imageBlock.Type != "image" || imageBlock.MimeType != "image/png" {
		t.Fatalf("图像块 = %+v, want type=image mimeType=image/png", imageBlock)
	}
	decoded, err := base64.StdEncoding.DecodeString(imageBlock.Data)
	if err != nil {
		t.Fatalf("图像块不是合法 base64: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("图像块不是可解码图片: %v", err)
	}
	if format != "png" || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 240 {
		t.Fatalf("图像块 = %s %v, want png 320x240", format, img.Bounds())
	}
}

func TestViewImageToolHonoursMaxWidthAndInlineFlag(t *testing.T) {
	path := newTempPNG(t, t.TempDir(), "shot.png", 800, 400)

	result, err := callViewImage(t, map[string]any{"path": path, "max_width": 100})
	if err != nil {
		t.Fatalf("view_image 失败: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Content[1].Data)
	if err != nil {
		t.Fatalf("图像块不是合法 base64: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("图像块不是可解码图片: %v", err)
	}
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 50 {
		t.Fatalf("降采样尺寸 = %v, want 100x50", img.Bounds())
	}

	textOnly, err := callViewImage(t, map[string]any{"path": path, "inline_image": false})
	if err != nil {
		t.Fatalf("view_image(inline_image=false) 失败: %v", err)
	}
	if len(textOnly.Content) != 1 || textOnly.Content[0].Type != "text" {
		t.Fatalf("inline_image=false 时应只回文本，实际 %+v", textOnly.Content)
	}
}

func TestViewImageToolReportsInvalidPath(t *testing.T) {
	cases := []struct {
		name      string
		arguments map[string]any
		target    error
	}{
		{name: "缺少 path", arguments: map[string]any{}},
		{
			name:      "文件不存在",
			arguments: map[string]any{"path": filepath.Join(t.TempDir(), "missing.png")},
			target:    computer.ErrImageNotFound,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := callViewImage(t, testCase.arguments)
			if err == nil {
				t.Fatalf("期望报错，实际成功")
			}
			if testCase.target != nil && !errors.Is(err, testCase.target) {
				t.Fatalf("err = %v, want errors.Is(_, %v)", err, testCase.target)
			}
		})
	}
}

func TestToolDefinitionsExposeViewTools(t *testing.T) {
	definitions := toolDefinitions()
	byName := map[string]toolDef{}
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Description) == "" {
			t.Fatalf("工具 %s 缺少 description", definition.Name)
		}
		byName[definition.Name] = definition
	}
	for _, name := range []string{"view_screen", "view_image", "screenshot", "click", "scroll", "scroll_targets"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("tools/list 缺少 %s", name)
		}
	}
	if description := byName["view_screen"].Description; !strings.Contains(description, "当前页面状态") {
		t.Fatalf("view_screen 的描述未点明「当前页面状态」：%q", description)
	}
	if description := byName["scroll_targets"].Description; !strings.Contains(description, "滚轮") {
		t.Fatalf("scroll_targets 的描述未点明滚轮面板：%q", description)
	}
}

// TestDescribeScrollTargetsRendersPanels 覆盖面板清单的文本渲染：名字缺失时
// 退回类名，边界与视口占比都要写进文本。不碰真实桌面。
func TestDescribeScrollTargetsRendersPanels(t *testing.T) {
	targets := []computer.ScrollTarget{
		{
			Name:        "messages",
			ControlType: "pane",
			Rect:        computer.Rect{X: 100, Y: 200, Width: 400, Height: 300},
			Vertical:    computer.ScrollAxis{Scrollable: true, Percent: 100, ViewSize: 40},
		},
		{
			ClassName: "unnamed-container",
			Rect:      computer.Rect{X: 0, Y: 0, Width: 200, Height: 200},
			Vertical:  computer.ScrollAxis{Scrollable: true, Percent: 0, ViewSize: 80},
		},
		{
			Name:       "static",
			Rect:       computer.Rect{X: 0, Y: 0, Width: 900, Height: 900},
			Horizontal: computer.ScrollAxis{Scrollable: true, Percent: 12, ViewSize: 70},
		},
	}
	text := describeScrollTargets(targets, 2)
	if !strings.Contains(text, "messages") || !strings.Contains(text, "中心=(300,350)") {
		t.Fatalf("清单缺少面板名与中心坐标：%q", text)
	}
	if !strings.Contains(text, "已在末端") {
		t.Fatalf("清单应标出已到底的面板：%q", text)
	}
	if strings.Contains(text, "unnamed-container") {
		t.Fatalf("limit=2 时只保留面积最大的两条：%q", text)
	}

	if empty := describeScrollTargets(nil, 0); !strings.Contains(empty, "pgdn") {
		t.Fatalf("空结果应给出键盘滚动回退：%q", empty)
	}
}

func TestDescribeScreenStateIsSelfDescribing(t *testing.T) {
	state := describeScreenState()
	if !strings.Contains(state, "当前页面状态") {
		t.Fatalf("状态文本缺少标题：%q", state)
	}
	for _, section := range []string{"虚拟桌面", "光标", "前台窗口"} {
		if !strings.Contains(state, section) {
			t.Fatalf("状态文本缺少 %s 段：%q", section, state)
		}
	}
}
