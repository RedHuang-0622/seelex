package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 窗口材质的取值：**责任链在前端确认**（gui/frontend/dist/window-treatment.js
// 的 resolveWindowTreatment），这里只认落盘的那一个词。
const (
	// WindowTreatmentOpaque 是默认：窗口不透明，跑现有一切皮肤。
	WindowTreatmentOpaque = "opaque"
	// WindowTreatmentAcrylic 让窗口走亚克力背景——毛玻璃皮肤才透得出来。
	WindowTreatmentAcrylic = "acrylic"
)

// windowAppearance 是外观设置文件（gui-appearance.json）的正文。皮肤 id 只作可读
// 性留档（出问题时能看出"这行是什么时候被哪套皮肤写的"），真正参与判定的只有
// window_treatment 一个词。
type windowAppearance struct {
	Skin            string `json:"skin"`
	WindowTreatment string `json:"window_treatment"`
}

func normalizeWindowTreatment(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), WindowTreatmentAcrylic) {
		return WindowTreatmentAcrylic
	}
	return WindowTreatmentOpaque
}

// readWindowAppearance 读外观设置。路径为空 / 文件不存在 / 内容损坏一律回落到默认
// （不透明）——外观设置永远不该阻断启动，这也是它和账号/插件配置的区别。
func readWindowAppearance(path string) windowAppearance {
	fallback := windowAppearance{WindowTreatment: WindowTreatmentOpaque}
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var parsed windowAppearance
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fallback
	}
	parsed.WindowTreatment = normalizeWindowTreatment(parsed.WindowTreatment)
	return parsed
}

func writeWindowAppearance(path string, appearance windowAppearance) error {
	if strings.TrimSpace(path) == "" {
		return nil // 宿主没给落盘路径：设置只在内存里活一轮，不算失败
	}
	appearance.WindowTreatment = normalizeWindowTreatment(appearance.WindowTreatment)
	payload, err := json.MarshalIndent(appearance, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

// windowWantsTranslucency 就是 run_wails 启动期读的那一行：这份外观设置要不要把
// 窗口开成透明/亚克力。它在建窗**之前**被调用，因此只依赖已落盘的文件。
func windowWantsTranslucency(path string) bool {
	return readWindowAppearance(path).WindowTreatment == WindowTreatmentAcrylic
}
