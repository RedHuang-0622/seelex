//go:build lockprobe

package sessionstore

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestProbeCommitCostBreakdown 是 C1/H3 的成本拆解探针（**不是判据**，只打印数字）。
//
// 背景：打点表把 C1 描述为「head 体积随分片数线性增长 + head 每次提交整份重写」，
// 并据此把"整份重写"当成提交基线（1500 行 / 8 KB 负载）的主成本。本探针把提交
// 路径逐环拆开计时，并做**同轮 A/B**（关掉"末分片索引可信"开关 = 退回改动前
// 行为），用于回答两件事：
//
//  1. 提交基线到底花在哪一环（head 发布 vs 分片整片重读 + JSON 解码 vs fsync）；
//
//  2. 「写侧免整片解码」优化到底省了多少（本机打开刚写过的文件会被杀软扫，
//     绝对值随机器漂移，只有同轮 A/B 的差值可信）。
//
//     go test -tags lockprobe ./sessionstore -run TestProbeCommitCostBreakdown -v
func TestProbeCommitCostBreakdown(t *testing.T) {
	const (
		rows    = 1500
		batch   = 100
		payload = 8 * 1024
		rounds  = 20
	)
	root := t.TempDir()
	store := newStoreEngine(root, storageSettings{})
	key := Key{ProjectID: "probe-c1", SessionID: "probe-c1-sess"}

	pad := strings.Repeat("x", payload)
	for start := 0; start < rows; start += batch {
		events := make([]Event, 0, batch)
		for index := start; index < start+batch; index++ {
			events = append(events, Event{
				Seq: uint64(index + 1), Role: "user",
				Content: fmt.Sprintf("row-%d-%s", index, pad), CreatedAt: time.Now().UTC(),
			})
		}
		if _, err := store.messageCommit(key, fmt.Sprintf("seed-%d", start), events); err != nil {
			t.Fatal(err)
		}
	}

	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	lastShard := ""
	if len(head.Shards) > 0 {
		lastShard = filepath.Join(store.messageDir(key), head.Shards[len(head.Shards)-1].Path)
	}
	t.Logf("规模：分片数=%d head=%d 字节；末分片=%d 字节", len(head.Shards),
		fileSize(store.modulePath(key, moduleMessage)), fileSize(lastShard))

	// ---- 环 1：完整提交（包内实现，不含 Router/应用层开销）----
	nextSeq := head.LastSeq
	commitSpent := time.Duration(0)
	for index := 0; index < rounds; index++ {
		nextSeq++
		begin := time.Now()
		if _, err := store.messageCommit(key, fmt.Sprintf("probe-commit-%d", index), []Event{{
			Seq: nextSeq, Role: "user", Content: "probe-row", CreatedAt: time.Now().UTC(),
		}}); err != nil {
			t.Fatal(err)
		}
		commitSpent += time.Since(begin)
	}
	t.Logf("① 完整提交（reap + 分片 append + head 发布）%d 次均值=%v", rounds, commitSpent/rounds)

	// ---- 环 2：单独测 head 发布（publishModuleHead 全流程）----
	payloadHead, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	publishBegin := time.Now()
	for index := 0; index < rounds; index++ {
		if _, err := store.publishModuleHead(key, moduleMessage, fmt.Sprintf("probe-publish-%d", index), payloadHead, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	publishSpent := time.Since(publishBegin)
	t.Logf("② 仅 head 发布（Marshal payload + 校验和 + MarshalIndent + 临时文件 + rename）%d 次均值=%v", rounds, publishSpent/rounds)

	// ---- 环 3：分片写入各阶段（整片重读 + 解码 / 整片 sha256 / append + fsync）----
	readSpent := time.Duration(0)
	hashSpent := time.Duration(0)
	appendSpent := time.Duration(0)
	for index := 0; index < rounds; index++ {
		begin := time.Now()
		if _, err := readMessageRowsFileAt(lastShard); err != nil {
			t.Fatal(err)
		}
		readSpent += time.Since(begin)

		begin = time.Now()
		_ = fileSHA256(lastShard)
		hashSpent += time.Since(begin)

		begin = time.Now()
		if err := appendMessageRowsFile(lastShard, []Event{{Seq: nextSeq + 1, Role: "user", Content: "x"}}); err != nil {
			t.Fatal(err)
		}
		appendSpent += time.Since(begin)
	}
	t.Logf("③ 末分片（%d 行 / %d 字节）：整片重读+解码均值=%v／整片 sha256 均值=%v／append（含 fsync）均值=%v",
		len(head.Shards), fileSize(lastShard), readSpent/rounds, hashSpent/rounds, appendSpent/rounds)

	// ---- 环 5：优化前 / 优化后 A/B（同一次运行内交替）----
	// 同时统计"整片解码次数"（机制面）：优化后应当为 0，优化前为 2/次提交
	// （reap 一次 + 写入器一次）。只看时间会被本机噪声（文件打开 ~1 ms、杀软扫描）
	// 淹没，机制计数才是判据。
	var abDecodes int64
	shardDecodeHook = func() { atomic.AddInt64(&abDecodes, 1) }
	t.Cleanup(func() { shardDecodeHook = nil })
	commitMedian := func(label string, trusted bool) (time.Duration, int64) {
		if trusted {
			shardTailTrustHook = nil
		} else {
			// 强制"索引不可信" = 退回改动前：reap 与写入器各整片读回 + 解码一次。
			shardTailTrustHook = func() bool { return false }
		}
		samples := make([]time.Duration, 0, rounds)
		decodes := int64(0)
		for index := 0; index < rounds; index++ {
			// 每次提交写满一整片（batch 行）：这样"末分片"始终是 ~800 KB 的大片，
			// 慢路径的两处解码才会真的解码一大片（写 1 行会让末分片一直是几十行的
			// 小片，解码成本被噪声淹没）。
			events := make([]Event, 0, batch)
			for row := 0; row < batch; row++ {
				events = append(events, Event{
					Role: "user", Content: fmt.Sprintf("%s-%d-%d-%s", label, index, row, pad),
					CreatedAt: time.Now().UTC(),
				})
			}
			atomic.StoreInt64(&abDecodes, 0)
			begin := time.Now()
			if _, err := store.messageCommit(key, fmt.Sprintf("%s-%d", label, index), events); err != nil {
				t.Fatal(err)
			}
			samples = append(samples, time.Since(begin))
			decodes += atomic.LoadInt64(&abDecodes)
		}
		shardTailTrustHook = nil
		slices.Sort(samples)
		return samples[len(samples)/2], decodes
	}
	_, _ = commitMedian("warm", true) // 热身丢弃
	fastSamples := make([]time.Duration, 0, 3)
	slowSamples := make([]time.Duration, 0, 3)
	var fastDecodes, slowDecodes int64
	for round := 0; round < 3; round++ {
		spent, decodes := commitMedian(fmt.Sprintf("fast-%d", round), true)
		fastSamples = append(fastSamples, spent)
		fastDecodes += decodes
		spent, decodes = commitMedian(fmt.Sprintf("slow-%d", round), false)
		slowSamples = append(slowSamples, spent)
		slowDecodes += decodes
	}
	slices.Sort(fastSamples)
	slices.Sort(slowSamples)
	fast, slow := fastSamples[1], slowSamples[1]
	t.Logf("④ 提交 A/B（各 3 轮 × %d 次取中位数）：优化后=%v ｜ 优化前（强制整片读回）= %v ｜ 时间差=%v ｜ 整片解码次数：优化后=%d 优化前=%d（共 %d 次提交）",
		rounds, fast, slow, fast-slow, fastDecodes, slowDecodes, 3*rounds)

	// ---- 环 7：文件打开/读取原语成本（本机噪声源定位）----
	openSharedSpent := time.Duration(0)
	openDefaultSpent := time.Duration(0)
	statSpent := time.Duration(0)
	readCostSpent := time.Duration(0)
	for index := 0; index < rounds; index++ {
		begin := time.Now()
		file, openErr := openSharedRead(lastShard)
		if openErr != nil {
			t.Fatal(openErr)
		}
		_ = file.Close()
		openSharedSpent += time.Since(begin)

		begin = time.Now()
		plain, plainErr := os.Open(lastShard)
		if plainErr != nil {
			t.Fatal(plainErr)
		}
		_ = plain.Close()
		openDefaultSpent += time.Since(begin)

		begin = time.Now()
		_ = fileSize(lastShard)
		statSpent += time.Since(begin)

		begin = time.Now()
		if _, readErr := readSharedFile(lastShard); readErr != nil {
			t.Fatal(readErr)
		}
		readCostSpent += time.Since(begin)
	}
	t.Logf("⑥ 原语成本（各 %d 次均值，末分片 %d 字节）：openSharedRead=%v／os.Open=%v／os.Stat=%v／readSharedFile（整片读）=%v",
		rounds, fileSize(lastShard), openSharedSpent/rounds, openDefaultSpent/rounds, statSpent/rounds, readCostSpent/rounds)
}
