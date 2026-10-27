package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadWindowAppearanceFallsBackToOpaque(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cases := []struct {
		name string
		path string
	}{
		{name: "空路径", path: ""},
		{name: "文件不存在", path: filepath.Join(dir, "nope.json")},
	}
	for _, tc := range cases {
		if got := readWindowAppearance(tc.path); got.WindowTreatment != WindowTreatmentOpaque {
			t.Fatalf("%s: 期望回落不透明，得到 %q", tc.name, got.WindowTreatment)
		}
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readWindowAppearance(broken); got.WindowTreatment != WindowTreatmentOpaque {
		t.Fatalf("损坏文件应回落不透明，得到 %q", got.WindowTreatment)
	}
}

func TestWindowAppearanceRoundTripsTranslucency(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "gui-appearance.json")

	if windowWantsTranslucency(path) {
		t.Fatal("未写文件时不该要透明窗口")
	}
	if err := writeWindowAppearance(path, windowAppearance{Skin: "glass", WindowTreatment: WindowTreatmentAcrylic}); err != nil {
		t.Fatal(err)
	}
	if !windowWantsTranslucency(path) {
		t.Fatal("写了 acrylic 之后建窗期应读到透明窗口")
	}

	// 落盘正文是自解释的 JSON：皮肤 id 留档，判据只有一个词。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed windowAppearance
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("落盘内容不是 JSON: %v", err)
	}
	if parsed.Skin != "glass" || parsed.WindowTreatment != WindowTreatmentAcrylic {
		t.Fatalf("落盘内容不符: %+v", parsed)
	}

	// 换回纯色皮肤：词归一成 opaque，窗口下次不透明。
	if err := writeWindowAppearance(path, windowAppearance{Skin: "qoder", WindowTreatment: "OPAQUE"}); err != nil {
		t.Fatal(err)
	}
	if windowWantsTranslucency(path) {
		t.Fatal("换回纯色皮肤后不该再要透明窗口")
	}
}

// SetWindowAppearance 是前端唯一入口：它必须把**未知材质词**归一成默认，而不是原样
// 落盘（否则前端一句笔误就能把窗口锁进一个 Go 不认识的词）。
func TestBridgeSetWindowAppearanceNormalizesTreatment(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "gui-appearance.json")
	bridge, err := NewBridge(newFakeApplication().deps(), Options{Title: "Seelex Test", AppearancePath: path})
	if err != nil {
		t.Fatal(err)
	}

	if err := bridge.SetWindowAppearance("glass", "acrylic"); err != nil {
		t.Fatal(err)
	}
	if !windowWantsTranslucency(path) {
		t.Fatal("glass/acrylic 应落成透明窗口")
	}

	if err := bridge.SetWindowAppearance("qoder", "definitely-not-a-treatment"); err != nil {
		t.Fatal(err)
	}
	if got := readWindowAppearance(path); got.WindowTreatment != WindowTreatmentOpaque || got.Skin != "qoder" {
		t.Fatalf("未知材质词应归一为 opaque 且保留皮肤: %+v", got)
	}
}

// 宿主没给落盘路径（浏览器预览 / 未接线）：写是 no-op，读回不透明，都不报错。
func TestBridgeSetWindowAppearanceWithoutPathIsNoop(t *testing.T) {
	t.Parallel()
	bridge, err := NewBridge(newFakeApplication().deps(), Options{Title: "Seelex Test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.SetWindowAppearance("glass", "acrylic"); err != nil {
		t.Fatalf("无落盘路径应静默成功: %v", err)
	}
	if windowWantsTranslucency("") {
		t.Fatal("空路径永远不该要透明窗口")
	}
}
