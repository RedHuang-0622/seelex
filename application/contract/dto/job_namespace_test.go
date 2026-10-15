package dto

import "testing"

// TestJobNamespaceOfMatchesTokenPlusSeparator 钉住句柄文法里最容易错的一处：
// `bt` 是 `bteam` 的前缀，识别**必须把分隔符一起匹配**，否则团队的 `bteam-3` 会被判成
// 后台工具作业的句柄——两张表的命名空间又重叠了（2026-10-15 审计 D1 要修的就是这个）。
func TestJobNamespaceOfMatchesTokenPlusSeparator(t *testing.T) {
	cases := []struct {
		handle string
		want   JobNamespace
		known  bool
	}{
		{"bt-3", JobNamespaceToolJob, true},
		{"bs-7", JobNamespaceSubagent, true},
		{"bteam-3", JobNamespaceTeamwork, true},
		{"bteam-10", JobNamespaceTeamwork, true},
		// 旧形态与外来字符串：不认得就是**不认得**，调用方据此显式报错，而不是猜。
		{"a5", "", false},
		{"bteam", "", false},
		{"bt", "", false},
		{"bteam3", "", false},
		{"xbt-3", "", false},
		{"", "", false},
	}
	for _, testCase := range cases {
		namespace, known := JobNamespaceOf(testCase.handle)
		if known != testCase.known || namespace != testCase.want {
			t.Fatalf("JobNamespaceOf(%q) = (%q, %v)，want (%q, %v)",
				testCase.handle, namespace, known, testCase.want, testCase.known)
		}
	}
}

// TestJobHandleRoundTripsThroughItsNamespace 钉住"铸造与识别是同一份文法"：凡铸造出来的
// 句柄都必须被识别回同一个命名空间（两张表各自铸造，判据只有这一处）。
func TestJobHandleRoundTripsThroughItsNamespace(t *testing.T) {
	for _, namespace := range JobNamespaces {
		for _, seq := range []int{1, 9, 10, 100} {
			handle := JobHandle(namespace, seq)
			got, known := JobNamespaceOf(handle)
			if !known || got != namespace {
				t.Fatalf("JobHandle(%q, %d) = %q，识别回 (%q, %v)", namespace, seq, handle, got, known)
			}
		}
	}
}

// TestJobNamespacesAreDisjoint 钉住整套命名空间互不相交：任何两个 token 铸造出来的句柄
// 不可能撞名（这正是"跨表静默命中"不可能再发生的前提）。
func TestJobNamespacesAreDisjoint(t *testing.T) {
	seen := map[string]JobNamespace{}
	for _, namespace := range JobNamespaces {
		for _, seq := range []int{1, 2, 3, 7, 10, 12} {
			handle := JobHandle(namespace, seq)
			if other, taken := seen[handle]; taken {
				t.Fatalf("句柄 %q 同时属于 %q 与 %q", handle, other, namespace)
			}
			seen[handle] = namespace
		}
	}
}
