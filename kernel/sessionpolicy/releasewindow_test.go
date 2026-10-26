package switching

import (
	"errors"
	"strings"
	"testing"
)

// 本文件钉「会话驻留窗口」可调面的次序与文案（M5-R6-release-window 接收者转移）：
// 先校验 → 再落盘 → 最后改内存；没装配落盘口 = 只本次运行有效。

type releaseWindowRecorder struct {
	calls    []string
	validate error
	persist  error
	applied  int
	applyErr error
	current  int
}

func (rec *releaseWindowRecorder) deps(withPersist bool) ReleaseWindowDeps {
	deps := ReleaseWindowDeps{
		Validate: func(int) error {
			rec.calls = append(rec.calls, "validate")
			return rec.validate
		},
		Apply: func(window int) (int, error) {
			rec.calls = append(rec.calls, "apply")
			if rec.applyErr != nil {
				return 0, rec.applyErr
			}
			rec.applied = window
			return window, nil
		},
		Current: func() int { return rec.current },
	}
	if withPersist {
		deps.Persist = func(int) error {
			rec.calls = append(rec.calls, "persist")
			return rec.persist
		}
	}
	return deps
}

func TestSetReleaseWindowOrderAndFailureStops(t *testing.T) {
	rec := &releaseWindowRecorder{}
	applied, err := SetReleaseWindow(rec.deps(true), 3)
	if err != nil || applied != 3 {
		t.Fatalf("应返回生效值，got %d, %v", applied, err)
	}
	if strings.Join(rec.calls, ",") != "validate,persist,apply" {
		t.Fatalf("次序 = %v, want validate,persist,apply", rec.calls)
	}

	// 校验失败：一个字节都不改（不写盘、不动内存）。
	rec = &releaseWindowRecorder{validate: errors.New("bad window")}
	if _, err := SetReleaseWindow(rec.deps(true), -1); err == nil || err.Error() != "bad window" {
		t.Fatalf("校验错误应原样返回，got %v", err)
	}
	if strings.Join(rec.calls, ",") != "validate" {
		t.Fatalf("校验失败后不得落盘/生效，got %v", rec.calls)
	}

	// 落盘失败：文案保留，且不动内存（返回值与"重启后的值"一致）。
	rec = &releaseWindowRecorder{persist: errors.New("disk full")}
	_, err = SetReleaseWindow(rec.deps(true), 2)
	if err == nil || err.Error() != "保存会话驻留窗口失败: disk full" {
		t.Fatalf("落盘失败文案必须保留，got %v", err)
	}
	if strings.Join(rec.calls, ",") != "validate,persist" {
		t.Fatalf("落盘失败不得改内存，got %v", rec.calls)
	}
}

func TestSetReleaseWindowWithoutPersistStillApplies(t *testing.T) {
	rec := &releaseWindowRecorder{}
	applied, err := SetReleaseWindow(rec.deps(false), 4)
	if err != nil || applied != 4 {
		t.Fatalf("没装配落盘口不算失败，got %d, %v", applied, err)
	}
	if strings.Join(rec.calls, ",") != "validate,apply" {
		t.Fatalf("次序 = %v, want validate,apply", rec.calls)
	}
	// 装配缺陷（没有 apply 口）必须响亮失败，而不是静默"成功"。
	if _, err := SetReleaseWindow(ReleaseWindowDeps{Validate: func(int) error { return nil }}, 1); err == nil ||
		!strings.Contains(err.Error(), "apply port is not assembled") {
		t.Fatalf("缺 apply 口应显式报错，got %v", err)
	}
}

func TestReleaseWindowReadsCurrent(t *testing.T) {
	rec := &releaseWindowRecorder{current: 2}
	if got := ReleaseWindow(rec.deps(false)); got != 2 {
		t.Fatalf("应读当前生效值，got %d", got)
	}
	if got := ReleaseWindow(ReleaseWindowDeps{}); got != 0 {
		t.Fatalf("没装配读数口时返回 0，got %d", got)
	}
}
