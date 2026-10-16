package computer

import (
	"errors"
	"testing"
	"time"
)

// 测试用标志位（与 Windows 的鼠标事件常量同值；本文件不带 build tag，
// 让点击序列的语义在任何平台都能跑）。
const (
	testDownFlags uint32 = 0x0002
	testUpFlags   uint32 = 0x0004
)

func TestRunClickSequencePairsDownWithUp(t *testing.T) {
	var calls []uint32
	inject := func(flags uint32, _, _ uintptr, _ uint32) error {
		calls = append(calls, flags)
		return nil
	}
	waits := 0
	if err := runClickSequence(inject, testDownFlags, testUpFlags, 10, 20, 2, 30*time.Millisecond,
		func(time.Duration) { waits++ }); err != nil {
		t.Fatalf("runClickSequence: %v", err)
	}
	want := []uint32{testDownFlags, testUpFlags, testDownFlags, testUpFlags}
	if len(calls) != len(want) {
		t.Fatalf("注入序列 = %#v, want %#v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("注入序列 = %#v, want %#v", calls, want)
		}
	}
	if waits != 1 {
		t.Fatalf("两次点击之间的等待次数 = %d, want 1", waits)
	}
}

func TestRunClickSequenceReleasesAfterFailedDown(t *testing.T) {
	var calls []uint32
	inject := func(flags uint32, _, _ uintptr, _ uint32) error {
		calls = append(calls, flags)
		if flags == testDownFlags {
			return errors.New("inject down failed")
		}
		return nil
	}
	err := runClickSequence(inject, testDownFlags, testUpFlags, 0, 0, 1, time.Millisecond, func(time.Duration) {})
	if err == nil {
		t.Fatalf("按下失败必须返回错误")
	}
	if len(calls) != 2 || calls[1] != testUpFlags {
		t.Fatalf("按下失败后必须补一次释放（否则鼠标卡在按下），实际序列 = %#v", calls)
	}
}

func TestRunClickSequenceReportsFailedUp(t *testing.T) {
	var calls []uint32
	inject := func(flags uint32, _, _ uintptr, _ uint32) error {
		calls = append(calls, flags)
		if flags == testUpFlags {
			return errors.New("inject up failed")
		}
		return nil
	}
	if err := runClickSequence(inject, testDownFlags, testUpFlags, 0, 0, 1, time.Millisecond, func(time.Duration) {}); err == nil {
		t.Fatalf("释放失败必须返回错误")
	}
	if len(calls) != 2 {
		t.Fatalf("释放失败时不应重试按下，实际序列 = %#v", calls)
	}
}

func TestRunClickSequenceTreatsZeroClicksAsOne(t *testing.T) {
	var calls []uint32
	inject := func(flags uint32, _, _ uintptr, _ uint32) error {
		calls = append(calls, flags)
		return nil
	}
	if err := runClickSequence(inject, testDownFlags, testUpFlags, 0, 0, 0, time.Millisecond, func(time.Duration) {}); err != nil {
		t.Fatalf("runClickSequence(clicks=0): %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("clicks<1 应视为一次点击，实际序列 = %#v", calls)
	}
}
