package tokens

import (
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestCount_Empty(t *testing.T) {
	if got := Count(""); got != 0 {
		t.Fatalf("Count(\"\") = %d, want 0", got)
	}
}

func TestCount_ASCII(t *testing.T) {
	if got := Count("abcd"); got != 1 {
		t.Fatalf("Count(abcd) = %d, want 1", got)
	}
	if got := Count("abcdefgh"); got != 2 {
		t.Fatalf("Count(abcdefgh) = %d, want 2", got)
	}
}

func TestCount_CJK(t *testing.T) {
	if got := Count("中文"); got != 2 {
		t.Fatalf("Count(中文) = %d, want 2", got)
	}
	if got := Count("中文测试文本"); got != 6 {
		t.Fatalf("Count(中文测试文本) = %d, want 6", got)
	}
}

func TestCount_Mixed(t *testing.T) {
	// 2 CJK + ceil(5/4)=2 ASCII + 1 空格 → 5（偏保守）。
	if got := Count("你好 world"); got != 5 {
		t.Fatalf("Count(你好 world) = %d, want 5", got)
	}
}

func TestCount_Symbols(t *testing.T) {
	if got := Count("..."); got != 2 {
		t.Fatalf("Count(...) = %d, want 2", got)
	}
	if got := Count("{}"); got != 1 {
		t.Fatalf("Count({}) = %d, want 1", got)
	}
}

func TestCount_NonASCIILetterConservative(t *testing.T) {
	// 西里尔字母不按 ASCII 4 字符/token 计，按 1 字符/token 保守计。
	if got := Count("привет"); got < 5 {
		t.Fatalf("Count(привет) = %d, want >= 5", got)
	}
}

func TestCountMessage(t *testing.T) {
	content := "你好"
	msg := types.Message{Role: "user", Content: &content}
	got := CountMessage(msg)
	if got < 4+2 {
		t.Fatalf("CountMessage = %d, want >= role(4)+content(2)", got)
	}
}

func TestCountHistory(t *testing.T) {
	a, b := "第一", "second"
	history := []types.Message{
		{Role: "user", Content: &a},
		{Role: "assistant", Content: &b},
	}
	single := CountMessage(history[0]) + CountMessage(history[1])
	if got := CountHistory(history); got != single {
		t.Fatalf("CountHistory = %d, want %d", got, single)
	}
}
