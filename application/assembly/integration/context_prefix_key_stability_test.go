//go:build !extended

package integration

import (
	"context"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
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

	// ③ 可用性变化：catalog 冻结，键不变，只追加哨兵。
	harness.tools = append(append([]model.Tool(nil), harness.tools...),
		model.Tool{Name: "team_dispatch", Description: "派活给 teammate"})
	harness.runtime.visibleTools = append([]model.Tool(nil), harness.tools...)
	harness.beginTurn("第 4 轮：重新装配工具目录。")
	afterAvailability := harness.service.components.tasks.ContextEpochStateFor(harness.session)
	if afterAvailability.ToolsEpoch != 1 || len(afterAvailability.ToolsUpdates) < 2 {
		t.Fatalf("可用性变化应只追加 sentinel、不 bump epoch：epoch=%d updates=%d",
			afterAvailability.ToolsEpoch, len(afterAvailability.ToolsUpdates))
	}
	fourth := probeKeyFor(harness)
	if changed := prefixprobe.DiffKeys(first, fourth); len(changed) != 0 {
		t.Fatalf("工具可用性变化不得改变冻结 catalog 的键，实际：%v", changed)
	}

	// ④ catalog 本身变化：显式 bump tools_epoch，键只归因到 tools。
	epochs := harness.service.components.tasks.ContextEpochStateFor(harness.session)
	catalog := append([]model.ToolCatalogEntry(nil), epochs.ToolCatalog...)
	catalog = append(catalog, model.ToolCatalogEntry{Name: "team_dispatch", Description: "派活给 teammate"})
	if changed, epoch := harness.service.components.tasks.ReplaceToolCatalogFor(harness.session, catalog); !changed || epoch <= epochs.ToolsEpoch {
		t.Fatalf("显式 catalog 变化应 bump epoch：changed=%t epoch=%d before=%d", changed, epoch, epochs.ToolsEpoch)
	}
	harness.beginTurn("第 5 轮：工具 catalog 发生变化。")
	afterCatalog := harness.service.components.tasks.ContextEpochStateFor(harness.session)
	if afterCatalog.ToolsEpoch != 2 {
		t.Fatalf("显式 catalog 变化后 epoch=%d want=2", afterCatalog.ToolsEpoch)
	}
	fifth := probeKeyFor(harness)
	changed := prefixprobe.DiffKeys(first, fifth)
	if len(changed) != 1 || changed[0] != "tools" {
		t.Fatalf("catalog 变化应只让 tools 格变化，实际：%v", changed)
	}
}

func TestContextL1ReusesTruthDeltaAndColdRebuildMatches(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	harness.beginTurn("第 1 轮：建立 L1。")
	firstKey := probeKeyFor(harness)
	stats, ok := harness.service.components.context.PrefixL1StatsFor(harness.session)
	if !ok || stats.Rebuilt != 1 || stats.Hits != 0 {
		t.Fatalf("首轮应重建 L1，实际 ok=%t stats=%+v", ok, stats)
	}

	harness.streamText("第 1 轮答复。")
	harness.llmIteration("第 1 轮答复。", "", nil)
	harness.finishTurn("第 1 轮答复。")

	harness.beginTurn("第 2 轮：只追加。")
	secondKey := probeKeyFor(harness)
	if changed := prefixprobe.DiffKeys(firstKey, secondKey); len(changed) != 0 {
		t.Fatalf("同一 world 只追加 truth 不应改变键：%v", changed)
	}
	stats, ok = harness.service.components.context.PrefixL1StatsFor(harness.session)
	if !ok || stats.Hits != 1 || stats.ReusedEvents == 0 {
		t.Fatalf("第二轮应只追加新增 truth 事件，实际 ok=%t stats=%+v", ok, stats)
	}
	projection := harness.service.components.view.CollectRuntimeProjectionFor(context.Background(), harness.session)
	cache := projection.Runtime.ContextCache
	if cache == nil || !cache.Available || cache.Hits != 1 || cache.ReusedChars <= 0 ||
		cache.PrefixKey.SystemDigest == "" {
		t.Fatalf("Snapshot projection missing L1 cache stats: %+v", cache)
	}
	liveCache := harness.service.components.frontendSnapshot.Snapshot().Runtime.ContextCache
	if liveCache == nil || liveCache.Hits != 1 || liveCache.ReusedChars <= 0 {
		t.Fatalf("assembly did not publish L1 cache immediately: %+v", liveCache)
	}
	before := append([]contract.EngineMessage(nil), harness.engine.History()...)

	// 冷加载等价：丢弃派生 L1，不丢 truth；同一请求重装配必须逐字节相同。
	harness.service.components.tasks.ForgetContextL1For(harness.session)
	if _, err := harness.service.components.context.PrepareExecutionContextFor(
		harness.session, prefixProbeRequestID, "第 2 轮：只追加。",
	); err != nil {
		t.Fatalf("冷重建装配: %v", err)
	}
	after := harness.engine.History()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("冷重建前缀与热 L1 不一致：before=%d messages, after=%d messages", len(before), len(after))
	}
	coldStats, ok := harness.service.components.context.PrefixL1StatsFor(harness.session)
	if !ok || coldStats.Rebuilt != 1 || coldStats.Hits != 0 {
		t.Fatalf("冷加载应显式重建而非复用：ok=%t stats=%+v", ok, coldStats)
	}
}

// probeKeyFor 从夹具的**世界状态**构造前缀键。
//
// 只喂与"世界"有关的东西：system prompt、工具签名、项目版本、压缩/记忆纪元、
// left pointer。**绝不喂当前输入或 plan 帧**——那两样是尾部内容，进了键就等于
// 宣称前缀会随每轮变化。
func probeKeyFor(harness *prefixProbeHarness) prefixprobe.Key {
	key, ok := harness.service.components.context.PrefixKeyFor(harness.session)
	if !ok {
		harness.t.Fatal("装配出口没有登记真 prefix_key")
	}
	return key
}
