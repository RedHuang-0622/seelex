package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// applyTestLimits 在测试内改一项 limits 配置并在结束恢复（进程内生效配置）。
func applyTestLimits(t *testing.T, mutate func(*seelexctx.Limits)) {
	t.Helper()
	previous := limits.Get()
	applied := previous
	mutate(&applied)
	ApplyLimits(applied)
	t.Cleanup(func() { ApplyLimits(previous) })
}

// TestValidateRetainWindowStartupCheck：《压缩四区模型》的配置校验——保护区下限
// （context_retain_floor_percent × 预算）高于保留上限 window.retain_tokens 是非法
// 组合，启动期必须报错（静默取小会把"用户写错了"吞成"配置看起来生效"）。
func TestValidateRetainWindowStartupCheck(t *testing.T) {
	applyTestLimits(t, func(applied *seelexctx.Limits) { applied.ContextRetainFloorPercent = 90 })

	const windowTokens = 200_000
	budget := task_context.ContextBudgetFor(runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: windowTokens, output: 8_192})
	if budget.Budget <= 0 {
		t.Fatalf("测试夹具预算不可用：%+v", budget)
	}
	floor := seelexctx.RetainFloorTokens(90, budget.Budget)
	if floor <= 0 {
		t.Fatalf("比例下限应大于 0：%d", floor)
	}
	// 保留上限写小 → 非法组合。
	if err := ValidateRetainWindow(WindowConfig{RetainTokens: floor / 2}, limits.Get(), windowTokens); err == nil {
		t.Fatalf("floor(%d) > retain_tokens(%d) 必须报错", floor, floor/2)
	}
	// 保留上限足够大 → 合法。
	if err := ValidateRetainWindow(WindowConfig{RetainTokens: floor * 2}, limits.Get(), windowTokens); err != nil {
		t.Fatalf("合法组合被拒：%v", err)
	}
	// retain_tokens 未配置（0 → 运行时回退账号窗口）不构成非法组合。
	if err := ValidateRetainWindow(WindowConfig{}, limits.Get(), windowTokens); err != nil {
		t.Fatalf("未配置保留上限时不应报错：%v", err)
	}
	// 账号未声明窗口 → 该维无从计算，不误报。
	if err := ValidateRetainWindow(WindowConfig{}, limits.Get(), 0); err != nil {
		t.Fatalf("窗口未知时不应报错：%v", err)
	}
}

// TestCompactRetainFloorRaisesProtectedWindow：保护区下限生效——同样的 transcript
// 与 window 配置，配了 context_retain_floor_percent 后保护区明显变大（挡住 min 的
// 坏方向：占比窗口过小把保留区压到失忆），且保留轮数不超过总轮数。
func TestCompactRetainFloorRaisesProtectedWindow(t *testing.T) {
	const (
		rounds      = 16
		answerChars = 32_000
	)
	applyWindowConfig(t, WindowConfig{Ratio: 0.05})

	// 基线：无下限（0 = 未配置）→ 占比窗口把保留区压到只够 1 轮。
	applyTestLimits(t, func(applied *seelexctx.Limits) { applied.ContextRetainFloorPercent = 0 })
	baseService, baseEngine, baseSession := compactTestService(t, "task-floor-base")
	baseMarks := appendWindowRounds(t, baseService, "task-floor-base", rounds, answerChars)
	compactNow(t, baseService, baseSession)
	baseKept := retainedRounds(baseEngine.History(), baseMarks)

	// 干预：下限 50% 预算 → 保留区被抬高。
	applyTestLimits(t, func(applied *seelexctx.Limits) { applied.ContextRetainFloorPercent = 50 })
	flooredService, flooredEngine, flooredSession := compactTestService(t, "task-floor-raised")
	flooredMarks := appendWindowRounds(t, flooredService, "task-floor-raised", rounds, answerChars)
	compactNow(t, flooredService, flooredSession)
	flooredKept := retainedRounds(flooredEngine.History(), flooredMarks)

	if baseKept < 1 {
		t.Fatalf("基线保留轮数 = %d，want >= 1（至少保 1 个完整协议单元）", baseKept)
	}
	if flooredKept <= baseKept {
		t.Fatalf("下限未抬高保护区：base=%d floored=%d（rounds=%d）", baseKept, flooredKept, rounds)
	}
	if flooredKept > rounds {
		t.Fatalf("保留轮数 %d 超过总轮数 %d", flooredKept, rounds)
	}
	t.Logf("保留轮数 base=%d floored=%d（下限 50%% 预算）", baseKept, flooredKept)
}

// TestCompactFrameBodyCarriesZoneLayout：四区显式化的**报表落点**——压缩记录指向的
// 帧正文里必须能回读到四区（分区 + 各区 token 数与来源）与保留窗口决策，读者据此
// 把"判据说超了"与"哪个区占了多少"对上。
func TestCompactFrameBodyCarriesZoneLayout(t *testing.T) {
	applyWindowConfig(t, WindowConfig{RetainTokens: 9_000, Ratio: 0.7})
	service, _, sessionID := compactTestService(t, "task-zone")
	appendWindowRounds(t, service, "task-zone", 16, 32_000)

	record := compactNow(t, service, sessionID)
	if record.FrameRef == "" {
		t.Fatalf("压缩记录没有 frame_ref，四区报表不可回读：%+v", record)
	}
	page, err := service.ReadToolResultHandler(context.Background(),
		fmt.Sprintf(`{"result_ref":%q,"offset":0,"limit":8000}`, record.FrameRef))
	if err != nil {
		t.Fatalf("回读帧正文失败：%v", err)
	}
	var decoded struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(page), &decoded); err != nil {
		t.Fatalf("帧正文分页不是 JSON：%v", err)
	}
	for _, want := range []string{
		"## Context zones (四区)",
		"- stable_prefix:",
		"- folded:",
		"- protected_window:",
		"- 保留窗口: ",
		"retained=",
		"cap=9000",
	} {
		if !strings.Contains(decoded.Content, want) {
			t.Fatalf("帧正文缺少 %q：\n%s", want, decoded.Content)
		}
	}
}
