package seelexctx

// limits_project_board_test.go — 项目看板开关块（limits.project_board，M3）的解析口径。
//
// 单独成文件而不是塞进 limits_test.go：这一块是"能力开关 + 触发面 + 去抖窗"三格，
// 与压缩预算/比例校验那些是不同族的旋钮，放在一起只会让两边都更难读。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLimitsProjectBoardBlock：解析与负值拒绝。
//
// 口径（与 async_exec 同族）：整块缺失 = 关；块在但没写 enabled = 关（bool 零值就是
// "没开"——这一块的缺省从来不是"开"，所以不需要像压缩摘要那样区分"没写"与"写了
// false"）。三格里**没有** max_rows：单次追加的行数上限唯一判据在存储面
// （越界显式报错，不静默截断），配置里再放一个就是对同一次追加的第二份判据。
func TestLimitsProjectBoardBlock(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.yaml")
	if err := os.WriteFile(missing, []byte("limits:\n  evidence_chars: 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	full, err := LoadLimits(missing)
	if err != nil {
		t.Fatalf("LoadLimits: %v", err)
	}
	if full.ProjectBoard.Enabled {
		t.Fatalf("缺 project_board 块时必须为关，得到 %+v", full.ProjectBoard)
	}

	configured := filepath.Join(dir, "configured.yaml")
	if err := os.WriteFile(configured, []byte(
		"limits:\n  project_board:\n    enabled: true\n    hook: \"task_complete\"\n    flush_idle_ms: 250\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := LoadLimits(configured)
	if err != nil {
		t.Fatalf("LoadLimits: %v", err)
	}
	if !parsed.ProjectBoard.Enabled || parsed.ProjectBoard.Hook != "task_complete" || parsed.ProjectBoard.FlushIdleMS != 250 {
		t.Fatalf("project_board 解析结果 = %+v，希望 enabled=true / hook=task_complete / flush_idle_ms=250",
			parsed.ProjectBoard)
	}

	// 负值显式报错：0 = 未配置（用同步器缺省），负数既不是"默认"也不是"禁用"，
	// 两种解读都会让配置看不出来。
	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("limits:\n  project_board:\n    flush_idle_ms: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimits(invalid); err == nil {
		t.Fatal("负的 flush_idle_ms 必须显式报错")
	}
}

// TestShippedSeelexYamlProjectBoardOff：出厂档默认关，且块内没有 max_rows。
//
// 为什么单独钉这条：出厂档是"缺省语义"的现场（与 async_exec 的用例同族）。看板是一层
// "要人喊才做"的能力，出厂打开会让每次工具完成都多一次取活体投影；而 max_rows 一旦
// 出现在出厂档里，就成了对存储面硬上限的第二份判据。
func TestShippedSeelexYamlProjectBoardOff(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatalf("读出厂配置: %v", err)
	}
	limits, err := LoadLimits(filepath.Join("..", "config", "seelex.yaml"))
	if err != nil {
		t.Fatalf("LoadLimits(出厂档): %v", err)
	}
	if limits.ProjectBoard.Enabled {
		t.Fatal("出厂档必须 enabled: false（关就是关）")
	}
	if got := limits.ProjectBoard.Hook; got != "todo_list" {
		t.Fatalf("出厂档 hook = %q，希望显式 todo_list（缺省档写出来，配置一眼可读）", got)
	}
	if got := limits.ProjectBoard.FlushIdleMS; got != 2000 {
		t.Fatalf("出厂档 flush_idle_ms = %d，希望 2000", got)
	}
	if strings.Contains(string(content), "max_rows:") {
		t.Fatal("出厂档里不该出现 max_rows 键（单次追加上限的唯一判据在存储面）")
	}
}
