// Package promptassets owns versioned, embedded prompt resources shared by
// application and runtime packages. Prompt text belongs in assets, while Go
// code supplies only runtime facts used by the templates.
package promptassets

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed assets/system/*.md assets/effort/*.md assets/plan/*.md assets/subagent/*.md
var files embed.FS

// PlanData is the runtime policy projection available to a planning template.
// It deliberately contains constraints, not user input or account data.
type PlanData struct {
	Effort       string
	NodeLimit    string
	Topology     string
	Concurrency  string
	Verification string
}

// SubagentData 是子代理章程模板的运行时事实（goal/预算/节点 ID 来自节点
// 输入与 effort 调节；Evidence 为父代理证据的预渲染文本，空 = 无证据段）。
type SubagentData struct {
	Goal            string
	NodeID          string
	MaxLoops        int
	MaxOutputTokens int
	Evidence        string
}

func SystemIdentity() string { return read("assets/system/identity.md") }

func SystemInstructions() string { return read("assets/system/instructions.md") }

func Effort(level string) string { return read("assets/effort/" + level + ".md") }

func PlanPreflight(data PlanData) string { return render("assets/plan/preflight.md", data) }

func PlanReplan(data PlanData) string { return render("assets/plan/replan.md", data) }

// SubagentCharter 渲染子代理章程（Claude Code 风格结构化提示词：
// Role/Context/Task/Investigation/Constraints/Verification）。提示词正文
// 在 assets/subagent/charter.md，Go 侧只提供运行时事实。
func SubagentCharter(data SubagentData) string {
	return render("assets/subagent/charter.md", data)
}

// Validate loads every production prompt and executes each template once.
// Application construction calls this before any prompt is consumed, turning
// an invalid embedded asset into a normal startup error instead of a panic in
// a request or constructor path.
func Validate() error {
	for _, name := range []string{
		"assets/system/identity.md",
		"assets/system/instructions.md",
		"assets/effort/lite.md",
		"assets/effort/medium.md",
		"assets/effort/high.md",
		"assets/effort/max.md",
	} {
		if _, err := readAsset(name); err != nil {
			return err
		}
	}
	for _, name := range []string{"assets/plan/preflight.md", "assets/plan/replan.md"} {
		if _, err := renderAsset(name, PlanData{}); err != nil {
			return err
		}
	}
	if _, err := renderAsset("assets/subagent/charter.md", SubagentData{}); err != nil {
		return err
	}
	return nil
}

func read(name string) string {
	content, _ := readAsset(name)
	return content
}

func render(name string, data any) string {
	output, _ := renderAsset(name, data)
	return output
}

func readAsset(name string) (string, error) {
	content, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompt asset %q: %w", name, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func renderAsset(name string, data any) (string, error) {
	content, err := readAsset(name)
	if err != nil {
		return "", err
	}
	parsed, err := template.New(name).Option("missingkey=error").Parse(content)
	if err != nil {
		return "", fmt.Errorf("parse prompt asset %q: %w", name, err)
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		return "", fmt.Errorf("render prompt asset %q: %w", name, err)
	}
	return strings.TrimSpace(output.String()), nil
}
