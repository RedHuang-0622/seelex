//go:build gui

package gui

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// TestFrontendOverlayPrefersExternalVendor 钉住瘦身后的资源边界：
// 核心前端仍从内嵌 FS 读取，大 vendor 文件从发行包外部目录覆盖读取。
func TestFrontendOverlayPrefersExternalVendor(t *testing.T) {
	base := fstest.MapFS{
		"index.html":            &fstest.MapFile{Data: []byte("embedded")},
		"vendor/pico.min.css":   &fstest.MapFile{Data: []byte("embedded-pico")},
		"vendor/mermaid/absent": &fstest.MapFile{Data: []byte("embedded-placeholder")},
	}
	external := fstest.MapFS{
		"vendor/mermaid/mermaid.min.js": &fstest.MapFile{Data: []byte("external-mermaid")},
		"vendor/pico.min.css":           &fstest.MapFile{Data: []byte("external-pico")},
	}
	overlay := frontendOverlayFS{base: base, external: external}

	if data, err := fs.ReadFile(overlay, "index.html"); err != nil || string(data) != "embedded" {
		t.Fatalf("core asset = %q/%v, want embedded", data, err)
	}
	if data, err := fs.ReadFile(overlay, "vendor/mermaid/mermaid.min.js"); err != nil || string(data) != "external-mermaid" {
		t.Fatalf("vendor asset = %q/%v, want external", data, err)
	}
	if data, err := fs.ReadFile(overlay, "vendor/pico.min.css"); err != nil || string(data) != "external-pico" {
		t.Fatalf("placeholder vendor asset = %q/%v, want external", data, err)
	}
}

// TestLocateFrontendVendorHonorsPackagedLayout 验证发行目录布局：
// SEELEX_FRONTEND_DIR/vendor/mermaid/mermaid.min.js 是完整性探针。
func TestLocateFrontendVendorHonorsPackagedLayout(t *testing.T) {
	root := t.TempDir()
	mermaid := filepath.Join(root, "vendor", "mermaid")
	if err := os.MkdirAll(mermaid, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mermaid, "mermaid.min.js"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEELEX_FRONTEND_DIR", root)
	if got := locateFrontendVendor(); got != filepath.Clean(root) {
		t.Fatalf("locateFrontendVendor = %q, want %q", got, filepath.Clean(root))
	}
}
