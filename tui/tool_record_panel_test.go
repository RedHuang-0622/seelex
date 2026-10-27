package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// tool_record_panel_test.go — 工具记录的 TUI 只读面（2026-10-15）。
//
// 口径：与 GUI「状态 → 工具记录」**同源**（同一份工具台账读数，终端不再折一遍
// 统计）；关着计数 / 没记过 / 未装配读面的宿主**整块退场**（不留空壳）；
// 面板仍只读（开关是 GUI 设置的事）。

// toolRecordFakeApp 在 fakeApp 之上补工具记录读面。
type toolRecordFakeApp struct {
	*fakeApp
	board application.ToolRecordBoard
}

func (app *toolRecordFakeApp) Board() application.ToolRecordBoard { return app.board }

// deps 在 fakeApp 的端口上再绑定工具记录读面。
func (app *toolRecordFakeApp) deps() Dependencies {
	deps := app.fakeApp.deps()
	deps.ToolRecord = app
	return deps
}

func TestToolRecordPanelRendersSuccessFailureAndTopReason(t *testing.T) {
	app := &toolRecordFakeApp{
		fakeApp: newFakeApp(),
		board: application.ToolRecordBoard{
			Enabled: true, Total: 12, Success: 9, Failure: 3,
			Tools: []application.ToolRecordStat{
				{
					Tool: "bash", Success: 8, Failure: 2, Total: 10,
					LastAt:  time.Date(2026, 10, 15, 21, 10, 0, 0, time.UTC),
					Reasons: []application.ToolRecordReason{{Reason: "timeout", Count: 2}},
				},
				{Tool: "read_file", Success: 1, Failure: 1, Total: 2,
					Reasons: []application.ToolRecordReason{{Reason: "not-found", Count: 1}}},
			},
		},
	}
	model := NewModel(app.deps())
	model.width = 160

	rendered := strings.Join(model.toolRecordLines(), "\n")
	for _, want := range []string{"工具记录", "12", "成功 9", "失败 3", "bash", "8", "2", "主因 超时×2", "read_file", "目标不存在"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("看板缺少 %q：\n%s", want, rendered)
		}
	}
}

// 关着计数 / 没记过 / 未装配读面 → 整块不出现。
func TestToolRecordPanelStaysSilentWhenDisabledOrUnwired(t *testing.T) {
	disabled := &toolRecordFakeApp{fakeApp: newFakeApp(), board: application.ToolRecordBoard{Total: 3, Success: 3}}
	if lines := NewModel(disabled.deps()).toolRecordLines(); len(lines) != 0 {
		t.Fatalf("计数关着时不该出现看板：%v", lines)
	}
	empty := &toolRecordFakeApp{fakeApp: newFakeApp(), board: application.ToolRecordBoard{Enabled: true}}
	if lines := NewModel(empty.deps()).toolRecordLines(); len(lines) != 0 {
		t.Fatalf("没记过时不该出现空看板：%v", lines)
	}
	if lines := NewModel(newFakeApp().deps()).toolRecordLines(); len(lines) != 0 {
		t.Fatalf("未装配读面时必须整块退场：%v", lines)
	}
}

// 台账落盘失败要显示成告警（读数照记，但"写不进去"必须看得见）。
func TestToolRecordPanelSurfacesLedgerError(t *testing.T) {
	app := &toolRecordFakeApp{
		fakeApp: newFakeApp(),
		board: application.ToolRecordBoard{
			Enabled: true, Total: 1, Success: 1,
			Tools:       []application.ToolRecordStat{{Tool: "bash", Success: 1, Total: 1}},
			LedgerError: "open tool-record.jsonl: access denied",
		},
	}
	model := NewModel(app.deps())
	model.width = 160
	rendered := strings.Join(model.toolRecordLines(), "\n")
	if !strings.Contains(rendered, "台账落盘失败") || !strings.Contains(rendered, "access denied") {
		t.Fatalf("台账落盘失败必须显示告警：\n%s", rendered)
	}
}
