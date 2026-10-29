package prefixprobe

import "testing"

func TestDigest(t *testing.T) {
	if got := Digest(""); got != "" {
		t.Fatalf("空串应返回空串（区分「未提供」与「空内容」），got %q", got)
	}
	first, second := Digest("abc"), Digest("abc")
	if first != second || len(first) != 16 {
		t.Fatalf("digest 不稳定或长度不对：%q vs %q", first, second)
	}
	if Digest("abc") == Digest("abd") {
		t.Fatalf("不同内容不应撞摘要")
	}
}

func TestBuildKey_ToolOrderIsIrrelevant(t *testing.T) {
	base := KeyInput{
		SystemPrompt:   "system",
		ProjectVersion: "v1",
		MemoryEpoch:    3,
		CompactSeq:     3,
		ToolsEpoch:     1,
		RetainedFrom:   7,
	}
	one := base
	one.ToolSignatures = []string{ToolSignature("read_file", "读文件"), ToolSignature("bash", "跑命令")}
	two := base
	two.ToolSignatures = []string{ToolSignature("bash", "跑命令"), ToolSignature("read_file", "读文件")}

	if BuildKey(one) != BuildKey(two) {
		t.Fatalf("工具顺序不应影响键（否则 map 迭代抖动会废掉前缀）")
	}
}

func TestBuildKey_SlotsAreIndependent(t *testing.T) {
	base := KeyInput{
		SystemPrompt: "system", ProjectVersion: "v1",
		MemoryEpoch: 1, CompactSeq: 1, ToolsEpoch: 1,
		ToolSignatures: []string{ToolSignature("read_file", "d")},
		RetainedFrom:   0,
	}
	baseline := BuildKey(base)

	cases := []struct {
		name   string
		mutate func(*KeyInput)
		want   []string
	}{
		{name: "系统提示词变化", mutate: func(k *KeyInput) { k.SystemPrompt = "system2" }, want: []string{"system_digest"}},
		{name: "项目版本变化", mutate: func(k *KeyInput) { k.ProjectVersion = "v2" }, want: []string{"project_version"}},
		{name: "memory 纪元推进", mutate: func(k *KeyInput) { k.MemoryEpoch = 2 }, want: []string{"memory_epoch"}},
		{name: "压缩推进", mutate: func(k *KeyInput) { k.CompactSeq = 2 }, want: []string{"compact_seq"}},
		{name: "工具纪元推进", mutate: func(k *KeyInput) { k.ToolsEpoch = 2 }, want: []string{"tools"}},
		{name: "工具集变化", mutate: func(k *KeyInput) {
			k.ToolSignatures = []string{ToolSignature("bash", "d")}
		}, want: []string{"tools"}},
		{name: "left pointer 移动", mutate: func(k *KeyInput) { k.RetainedFrom = 99 }, want: []string{"retained_from"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.ToolSignatures = append([]string(nil), base.ToolSignatures...)
			tc.mutate(&input)
			got := DiffKeys(baseline, BuildKey(input))
			if len(got) != len(tc.want) {
				t.Fatalf("Changed=%v, want %v", got, tc.want)
			}
			for index := range tc.want {
				if got[index] != tc.want[index] {
					t.Fatalf("Changed=%v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestBuildKey_QueryIndependent 是 I4 的守门用例：同一世界状态、不同 query
// 必须得到同一把键。若哪天有人把 query 塞进 KeyInput，这条会立刻红。
func TestBuildKey_QueryIndependent(t *testing.T) {
	// 两轮只差"当前输入"，而当前输入不属于 KeyInput——这正是设计意图。
	first := BuildKey(KeyInput{SystemPrompt: "s", ProjectVersion: "v", MemoryEpoch: 1, CompactSeq: 1, ToolsEpoch: 1})
	second := BuildKey(KeyInput{SystemPrompt: "s", ProjectVersion: "v", MemoryEpoch: 1, CompactSeq: 1, ToolsEpoch: 1})
	if first != second {
		t.Fatalf("同一世界状态的键必须相同：%+v vs %+v", first, second)
	}
}
