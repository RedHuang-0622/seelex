package application

import (
	"fmt"
	"runtime"
	"strings"
)

// RenderDiag 构建诊断文本。由 /diag 命令调用。
func RenderDiag(snap Snapshot) string {
	rt := snap.Runtime
	var b strings.Builder

	b.WriteString("  ╔══ System Diagnostics ══╗\n")
	b.WriteString(fmt.Sprintf("  Go: %s  OS: %s/%s  CPU: %d cores\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU()))

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	b.WriteString(fmt.Sprintf("  Goroutines: %d  Mem: %.1f MB (alloc) / %.1f MB (sys)  GC: %d\n",
		runtime.NumGoroutine(), float64(mem.Alloc)/1024/1024, float64(mem.Sys)/1024/1024, mem.NumGC))

	// Engine
	b.WriteString("\n── Engine ──\n")
	b.WriteString(fmt.Sprintf("  Provider: %s  Model: %s  Effort: %s  Tokens: %s\n",
		rt.Provider, rt.Model, rt.Effort, rt.Tokens))
	b.WriteString(fmt.Sprintf("  Session: %s\n", rt.Account))
	if rt.Plan != nil {
		p := rt.Plan
		done := 0
		for _, n := range p.Nodes {
			if n.Status == NodeCompleted || n.Status == NodeSkipped {
				done++
			}
		}
		b.WriteString(fmt.Sprintf("  Plan: %s (%s, %d/%d nodes)\n",
			p.Name, p.Status, done, len(p.Nodes)))
	}

	// Plugins
	b.WriteString("\n── Plugins ──\n")
	active := rt.Plugin
	if active == "" {
		active = "none"
	}
	b.WriteString(fmt.Sprintf("  Active: %s (%d total)\n", active, len(rt.Plugins)))
	for _, p := range rt.Plugins {
		marker := "   "
		if p.Name == rt.Plugin {
			marker = " ★"
		}
		desc := p.Description
		if desc == "" {
			desc = "—"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", marker, p.Name))
		b.WriteString(fmt.Sprintf("       %s\n", desc))
	}

	// Accounts
	b.WriteString(fmt.Sprintf("\n── Accounts ──  %d configured\n", len(rt.Accounts)))
	for _, a := range rt.Accounts {
		status := "✓"
		if a.Disabled {
			status = "✗"
		}
		b.WriteString(fmt.Sprintf("   %s %s\n", status, a.Name))
		b.WriteString(fmt.Sprintf("       %s/%s\n", a.Provider, a.Model))
	}

	// Skills
	b.WriteString(fmt.Sprintf("\n── Skills ──  %d loaded\n", len(rt.Skills)))
	if len(rt.Skills) > 0 {
		names := make([]string, 0, len(rt.Skills))
		for _, sk := range rt.Skills {
			names = append(names, sk.Name)
		}
		const cols = 4
		for i := 0; i < len(names); i += cols {
			end := i + cols
			if end > len(names) {
				end = len(names)
			}
			b.WriteString("  ")
			b.WriteString(strings.Join(names[i:end], "  "))
			b.WriteString("\n")
		}
	}

	b.WriteString("  ╚════════════════════════╝")
	return b.String()
}
