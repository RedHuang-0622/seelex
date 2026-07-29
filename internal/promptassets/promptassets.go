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

//go:embed assets/system/*.md assets/effort/*.md assets/plan/*.md
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

func SystemIdentity() string { return read("assets/system/identity.md") }

func SystemInstructions() string { return read("assets/system/instructions.md") }

func Effort(level string) string { return read("assets/effort/" + level + ".md") }

func PlanPreflight(data PlanData) string { return render("assets/plan/preflight.md", data) }

func PlanReplan(data PlanData) string { return render("assets/plan/replan.md", data) }

func read(name string) string {
	content, err := files.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("prompt asset %q: %v", name, err))
	}
	return strings.TrimSpace(string(content))
}

func render(name string, data PlanData) string {
	parsed, err := template.New(name).Option("missingkey=error").Parse(read(name))
	if err != nil {
		panic(fmt.Sprintf("parse prompt asset %q: %v", name, err))
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		panic(fmt.Sprintf("render prompt asset %q: %v", name, err))
	}
	return strings.TrimSpace(output.String())
}
