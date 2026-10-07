// Package promptassets owns versioned, embedded prompt resources shared by
// application and runtime packages. Prompt text belongs in assets, while Go
// code supplies only runtime facts used by the templates.
//
// 外部覆盖层（本次启用）：内嵌 assets/ 仍是**默认数据**（单二进制可部署、提示词
// 与代码同版本），但启动期可以把它指向一个外部目录（默认 config/prompt/，见
// main.resolvePromptDir）。解析口径与 internal/bootseed 一致：
//
//  1. 存在即读：目录里同名文件优先——用户改过的就是它；逐文件回退，缺失的那份
//     用内嵌默认，所以只覆盖一份也能用。
//  2. 空文件视为"未配置"：回退内嵌，而不是把对应段落抹成空（一次误清空不该让
//     system 段消失）。
//  3. 目录本身缺失时由启动期用内嵌默认词初始化（bootseed），之后只读用户那份。
//
// SetDir 必须早于任何提示词消费；未设置（测试、无处落盘）时全部走内嵌。
package promptassets

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
)

//go:embed assets/system/*.md assets/effort/*.md assets/plan/*.md assets/subagent/*.md
var files embed.FS

// Entry 是提示词资产目录在外部目录里的入口文件（相对目录，用 / 分隔）：
// 责任链"目录算不算存在"的判据与落盘后的入口都取它。
const Entry = "system/instructions.md"

var (
	dirMu sync.RWMutex
	dir   string
)

// SetDir 设置外部覆盖目录（config/prompt）。空串 = 全部走内嵌。
// 调用方拿不到可用目录时直接传 ""，不必特殊分支。
func SetDir(path string) {
	dirMu.Lock()
	defer dirMu.Unlock()
	dir = strings.TrimSpace(path)
}

// Dir 返回当前外部覆盖目录（"" = 全内嵌）。
func Dir() string {
	dirMu.RLock()
	defer dirMu.RUnlock()
	return dir
}

// DefaultFS 返回内嵌默认提示词的只读资源集，**根就是提示词目录本身**
// （路径形如 system/instructions.md）：启动期落盘时按同一组相对路径写出去。
func DefaultFS() fs.FS {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		// assets 子树由 //go:embed 保证存在；真出错时退化成整棵资源根，
		// 至少不让启动期 panic。
		return files
	}
	return sub
}

// DefaultFiles 返回内嵌默认提示词的相对路径清单（用 / 分隔，已排序）。
// 启动期落盘用它枚举默认数据包的文件。
func DefaultFiles() []string {
	paths := make([]string, 0, 16)
	_ = fs.WalkDir(files, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		paths = append(paths, strings.TrimPrefix(filepath.ToSlash(path), "assets/"))
		return nil
	})
	sort.Strings(paths)
	return paths
}

// DirOf 从"入口文件的绝对路径"反推提示词资产根目录（= SetDir 的入参）。
// Entry 形如 system/instructions.md，所以命中时 bootseed 给的 Root 是入口所在
// 目录（…/config/prompt/system），要按 Entry 的层级再上溯。
func DirOf(entryPath string) string {
	root := filepath.Dir(entryPath)
	for i := strings.Count(Entry, "/"); i > 0; i-- {
		root = filepath.Dir(root)
	}
	return root
}

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

// Effort 返回**统一的那一份** effort 工作纪律。
//
// 2026-10 归一：这里原本按档位返回四份近乎同构的变体（lite/medium/high/max），
// 各自复述"本档该做多少事"。那是**第二份事实**——档位真正的差别（思考强度、
// loop 次数、plan 策略）全在运行时被机器执行（reasoning_effort / SetMaxLoops /
// dto.PlanPolicy），提示词再复述一遍只会与它们分叉；而档位名一旦写进提示词，
// 模型就会按"我是 lite"自我设限，与"effort 只调思考强度与循环预算"的原意相悖。
//
// 所以四个档位共用这一份，档位差异一律由运行时表达。
func Effort() string { return read("assets/effort/system.md") }

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
// a request or constructor path. 它读的是**解析后**的那份：外部目录覆盖了
// 哪几份，校验的就是哪几份——用户改坏的提示词在装配期就报出来，而不是等到
// 某个回合才以怪异行为显现。
func Validate() error {
	for _, name := range []string{
		"assets/system/identity.md",
		"assets/system/instructions.md",
		"assets/effort/system.md",
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
	if content, ok := readOverride(name); ok {
		return content, nil
	}
	content, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompt asset %q: %w", name, err)
	}
	return strings.TrimSpace(string(content)), nil
}

// readOverride 按外部覆盖目录解析资产：name 形如 assets/system/identity.md，
// 目录内按去掉 assets/ 前缀后的相对路径查找（config/prompt/system/identity.md）。
// 命中且非空才算覆盖。
func readOverride(name string) (string, bool) {
	root := Dir()
	if root == "" {
		return "", false
	}
	rel := strings.TrimPrefix(filepath.ToSlash(name), "assets/")
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", false
	}
	return content, true
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
