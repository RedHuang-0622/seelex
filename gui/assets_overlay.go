package gui

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// frontendAssets 返回前端资源根：内嵌核心 + 可选的外部 vendor 目录。
// 外部目录保留离线大组件，避免把约 5.7 MiB 的 mermaid/pdf/xterm 载荷塞进
// TUI/GUI 共用之外的 GUI 可执行文件。找不到时仍启动，相关预览按前端既有
// 降级分支显示“组件未加载”。
func frontendAssets() (fs.FS, error) {
	base, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		return nil, err
	}
	root := locateFrontendVendor()
	if root == "" {
		log.Printf("GUI vendor assets not found; mermaid/pdf/docx/terminal vendor features may be unavailable")
		return base, nil
	}
	return frontendOverlayFS{base: base, external: os.DirFS(root)}, nil
}

// locateFrontendVendor 按发行目录、显式环境变量、开发仓库祖先目录查找
// 包含 vendor/ 的前端根目录。
func locateFrontendVendor() string {
	candidates := make([]string, 0, 8)
	if dir := strings.TrimSpace(os.Getenv("SEELEX_FRONTEND_DIR")); dir != "" {
		candidates = append(candidates, dir)
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(dir, "frontend", "dist"),
			filepath.Join(dir, "frontend"),
		)
		for parent := dir; ; parent = filepath.Dir(parent) {
			candidates = append(candidates, filepath.Join(parent, "gui", "frontend", "dist"))
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
		}
	}
	if workdir, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(workdir, "gui", "frontend", "dist"),
			filepath.Join(workdir, "frontend", "dist"),
		)
	}
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if info, err := os.Stat(filepath.Join(candidate, "vendor", "mermaid", "mermaid.min.js")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// frontendOverlayFS 让 vendor/ 优先读外部目录，其余文件继续走内嵌 FS。
type frontendOverlayFS struct {
	base     fs.FS
	external fs.FS
}

var (
	_ fs.FS         = frontendOverlayFS{}
	_ fs.ReadDirFS  = frontendOverlayFS{}
	_ fs.ReadFileFS = frontendOverlayFS{}
	_ fs.StatFS     = frontendOverlayFS{}
)

func (overlay frontendOverlayFS) Open(name string) (fs.File, error) {
	if isVendorAsset(name) {
		if file, err := overlay.external.Open(name); err == nil {
			return file, nil
		}
	}
	return overlay.base.Open(name)
}

func (overlay frontendOverlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if isVendorAsset(name) || name == "vendor" {
		return fs.ReadDir(overlay.external, name)
	}
	return fs.ReadDir(overlay.base, name)
}

func (overlay frontendOverlayFS) ReadFile(name string) ([]byte, error) {
	if isVendorAsset(name) {
		if data, err := fs.ReadFile(overlay.external, name); err == nil {
			return data, nil
		}
	}
	return fs.ReadFile(overlay.base, name)
}

func (overlay frontendOverlayFS) Stat(name string) (fs.FileInfo, error) {
	if isVendorAsset(name) {
		if info, err := fs.Stat(overlay.external, name); err == nil {
			return info, nil
		}
	}
	return fs.Stat(overlay.base, name)
}

func isVendorAsset(name string) bool {
	return name == "vendor" || strings.HasPrefix(name, "vendor/")
}
