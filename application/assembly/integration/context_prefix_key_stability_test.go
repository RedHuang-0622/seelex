//go:build !extended

package integration

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// prefix_key 稳定性守门用例（I4：前缀有键，且键里不许有"本轮"的东西）。
//
// 三条断言：
//  1. **世界未变 ⇒ 键不变**：连续多轮、只追加对话，键必须一字节不变（否则前缀
//     每轮都"合法失效"，那是把缺陷伪装成预期）。
//  2. **plan 装载/推进 ⇒ 键不变**：plan 帧是**尾部内容**，不是前缀；它若能改变
//     键，就等于承认 plan 会换掉 prefix（与 D1 语义矛盾）。
//  3. **工具集变化 ⇒ 键变**：这是唯一应当让前缀整条失效的开关，且必须显式可见
//     （`Changed` 里出现 `tools`）。
//
// 运行：go test ./application/assembly -run ContextPrefixKeyStability -v -count=1

func TestContextPrefixKeyStability(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	harness.beginTurn("第 1 轮：看一眼。")
	harness.capture(1, 1, "t1.iter1")
	first := probeKeyFor(harness)
	t.Logf("turn1 key: system=%s tools=%s", first.SystemDigest, first.ToolsDigest)

	// ① 只追加对话：键不变。
	harness.beginTurn("第 2 轮：继续。")
	harness.capture(2, 1, "t2.iter1")
	second := probeKeyFor(harness)
	if changed := prefixprobe.DiffKeys(first, second); len(changed) != 0 {
		t.Fatalf("只追加对话不应改变 prefix_key，实际变化：%v", changed)
	}

	// ② plan 装载并推进：键仍不变（plan 是尾部内容）。
	setActivePlan(harness, "第 1 版计划：核对前缀链路", model.NodeRunning)
	harness.beginTurn("第 3 轮：按计划推进。")
	harness.capture(3, 1, "t3.iter1")
	third := probeKeyFor(harness)
	if changed := prefixprobe.DiffKeys(first, third); len(changed) != 0 {
		t.Fatalf("plan 装载不应改变 prefix_key（plan 帧属尾部），实际变化：%v", changed)
	}

	// ③ 工具集变化：键必须变，且归因到 tools。
	harness.tools = append(append([]model.Tool(nil), harness.tools...),
		model.Tool{Name: "team_dispatch", Description: "派活给 teammate"})
	fourth := probeKeyFor(harness)
	changed := prefixprobe.DiffKeys(first, fourth)
	if len(changed) != 1 || changed[0] != "tools" {
		t.Fatalf("工具集变化应只让 tools 格变化，实际：%v", changed)
	}
}

// probeKeyFor 从夹具的**世界状态**构造前缀键。
//
// 只喂与"世界"有关的东西：system prompt、工具签名、项目版本、压缩/记忆纪元、
// left pointer。**绝不喂当前输入或 plan 帧**——那两样是尾部内容，进了键就等于
// 宣称前缀会随每轮变化。
func probeKeyFor(harness *prefixProbeHarness) prefixprobe.Key {
	signatures := make([]string, 0, len(harness.tools))
	for _, tool := range harness.tools {
		signatures = append(signatures, prefixprobe.ToolSignature(tool.Name, tool.Description))
	}
	return prefixprobe.BuildKey(prefixprobe.KeyInput{
		SystemPrompt:   systemPromptFor(harness.service),
		ToolSignatures: signatures,
	})
}
