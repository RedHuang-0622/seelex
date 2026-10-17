package stablehash

import "testing"

// 已知向量钉住算法：这些值变了，node 会话号/账号绑定/权限主体名都会失配。
func TestFNV1a32KnownVectors(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"", 0x811c9dc5},
		{"a", 0xe40c292c},
		{"foobar", 0xbf9cf968},
	}
	for _, testCase := range cases {
		if got := FNV1a32(testCase.in); got != testCase.want {
			t.Errorf("FNV1a32(%q) = %#x, want %#x", testCase.in, got, testCase.want)
		}
	}
}
