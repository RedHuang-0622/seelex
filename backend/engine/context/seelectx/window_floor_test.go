package seelexctx

import (
	"strings"
	"testing"
)

// TestRetainFloorTokens：保护区下限的**比例部分** = 比例 × 预算；未配置（0）或预算
// 不可用（<= 0）时为 0 —— 此时下限只剩「至少 1 个完整协议单元」这条兜底。
func TestRetainFloorTokens(t *testing.T) {
	cases := []struct {
		name    string
		percent int
		budget  int
		want    int
	}{
		{name: "未配置", percent: 0, budget: 100_000, want: 0},
		{name: "负比例按未配置", percent: -5, budget: 100_000, want: 0},
		{name: "预算不可用", percent: 25, budget: 0, want: 0},
		{name: "常规", percent: 25, budget: 100_000, want: 25_000},
		{name: "整预算", percent: 100, budget: 174_488, want: 174_488},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RetainFloorTokens(testCase.percent, testCase.budget); got != testCase.want {
				t.Fatalf("RetainFloorTokens(%d, %d) = %d, want %d",
					testCase.percent, testCase.budget, got, testCase.want)
			}
		})
	}
}

// TestRetainedContextTokensWithFloor：保留区 = clamp(min(token1, token2), floor, token1)。
// 下限只在「min 把保护区压得比 floor 还小」时抬高结果；上限始终是 token1（未配置
// retain_tokens 时回退账号窗口 → 再夹到全量上下文）。
func TestRetainedContextTokensWithFloor(t *testing.T) {
	cases := []struct {
		name         string
		all          int
		retainTokens int
		fallback     int
		ratio        float64
		floor        int
		want         int
		wantReason   string
	}{
		{
			name: "未配下限 → 与 min 口径逐位一致", all: 100_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, floor: 0, want: 70_000,
			wantReason: "min(200000, 70000) 不变",
		},
		{
			name: "下限低于 min → 不抬高", all: 100_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, floor: 50_000, want: 70_000,
			wantReason: "floor 50k < min 70k",
		},
		{
			name: "下限高于 min → 抬到下限", all: 100_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, floor: 90_000, want: 90_000,
			wantReason: "挡住 min 的坏方向（占比窗口过小）",
		},
		{
			name: "下限超过全量上下文 → 夹到全量", all: 100_000,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, floor: 150_000, want: 100_000,
			wantReason: "保留区不得超过全量上下文",
		},
		{
			name: "token1 上限更高时下限不得越过保留上限", all: 100_000,
			retainTokens: 60_000, fallback: 200_000, ratio: 0.7, floor: 80_000, want: 60_000,
			wantReason: "floor 80k > token1 60k → 夹到 token1（该组合在启动期已报错）",
		},
		{
			name: "下限生效但全量上下文更小", all: 40_000,
			retainTokens: 100_000, fallback: 200_000, ratio: 0.7, floor: 30_000, want: 30_000,
			wantReason: "min(100000, 28000)=28000 → floor 30k 抬高",
		},
		{
			name: "全量上下文不可用 → 不决策", all: 0,
			retainTokens: 0, fallback: 200_000, ratio: 0.7, floor: 50_000, want: 0,
			wantReason: "调用方保持原目标",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := WindowConfig{RetainTokens: testCase.retainTokens, Ratio: testCase.ratio}
			got := config.RetainedContextTokensWithFloor(testCase.all, testCase.fallback, testCase.floor)
			if got != testCase.want {
				t.Fatalf("RetainedContextTokensWithFloor(all=%d, fallback=%d, floor=%d) = %d, want %d (%s)",
					testCase.all, testCase.fallback, testCase.floor, got, testCase.want, testCase.wantReason)
			}
			if got > testCase.all {
				t.Fatalf("保留前缀 %d 超过全量上下文 %d", got, testCase.all)
			}
			if testCase.retainTokens > 0 && got > testCase.retainTokens && testCase.all > testCase.retainTokens {
				t.Fatalf("保留前缀 %d 越过配置上限 retain_tokens=%d", got, testCase.retainTokens)
			}
		})
	}
}

// TestValidateRetainWindow：floor > retain_tokens 是**非法组合**，必须报错（不许静默
// 取小）；retain_tokens 未配置（0 → 运行时回退账号窗口）时不构成非法组合。
func TestValidateRetainWindow(t *testing.T) {
	cases := []struct {
		name         string
		retainTokens int
		floor        int
		wantErr      bool
	}{
		{name: "未配下限", retainTokens: 60_000, floor: 0},
		{name: "retain_tokens 未配置 → 无上限可冲突", retainTokens: 0, floor: 999_999},
		{name: "下限低于上限", retainTokens: 60_000, floor: 50_000},
		{name: "下限等于上限", retainTokens: 60_000, floor: 60_000},
		{name: "下限高于上限 → 报错", retainTokens: 60_000, floor: 60_001, wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := WindowConfig{RetainTokens: testCase.retainTokens}
			err := config.ValidateRetainWindow(testCase.floor)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("floor=%d > retain_tokens=%d 必须报错，得到 nil", testCase.floor, testCase.retainTokens)
				}
				if !strings.Contains(err.Error(), "retain_tokens") {
					t.Fatalf("错误信息必须点名冲突的两个旋钮，得到 %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("合法组合被拒：%v", err)
			}
		})
	}
}
