package accountwindow

import "testing"

// TestPresetsAreAscendingAndValid：挡位表升序、不重复，且每一挡都过自己的校验。
// 这张表是**下发面**（前端只渲染它，不自己写死一份），所以顺序与合法性都是契约。
func TestPresetsAreAscendingAndValid(t *testing.T) {
	presets := Presets()
	if len(presets) == 0 {
		t.Fatal("挡位表不能为空")
	}
	for index, preset := range presets {
		if err := Validate(preset); err != nil {
			t.Fatalf("挡位 %d 通不过校验：%v", preset, err)
		}
		if index > 0 && preset <= presets[index-1] {
			t.Fatalf("挡位必须升序且不重复：%v", presets)
		}
	}
}

// TestValidateRejectsNonPositive：0 不是"合法窗口"（0 = 清掉该账号的覆盖，由调用方
// 在进这里之前处置），负数与 0 一样在边界被拒。
func TestValidateRejectsNonPositive(t *testing.T) {
	for _, window := range []int{0, -1} {
		if err := Validate(window); err == nil {
			t.Fatalf("window=%d 必须被拒（0 = 清除该账号的覆盖，不是合法窗口）", window)
		}
	}
}

// TestValidateAcceptsBoundaryAndRejectsBeyond：上界本身合法，超过上界被拒——
// 上界是防手滑（多打一个 0），不是模型真实窗口。
func TestValidateAcceptsBoundaryAndRejectsBeyond(t *testing.T) {
	if err := Validate(MaxWindow); err != nil {
		t.Fatalf("上界本身应当合法：%v", err)
	}
	if err := Validate(MaxWindow + 1); err == nil {
		t.Fatal("超过上界必须被拒")
	}
}
