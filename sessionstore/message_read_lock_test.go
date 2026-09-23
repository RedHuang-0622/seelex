package sessionstore

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// C2（锁热点）验收：message 读路径不再与写者共用 messageMu。
//
// 两条测试针对同一根因的不同侧面：
//  1. 结构性证明（不靠耗时预算）：读者被钉在"锁外解码"里时，提交照样完成
//     ——读路径若回到锁内解码，提交会一直等读者，本测试超时失败；
//  2. 无锁快照的代价边界：读者拿着**过期快照**时并发淘汰删掉了它引用的
//     分片，读必须以新快照重试一次并给出淘汰后的真相，而不是报错或返回
//     幽灵行。
//
// 对照的时序验收（读造成的额外等待 ≤ 预算）在 lock_hotspot_test.go
// （原 redprobe 文件转常规）。

// buildMessageRows 提交 rows 行（seq 从 1 起，按 step 分批），返回 message key。
func buildMessageRows(t *testing.T, store *storeEngine, key Key, rows, step int) {
	t.Helper()
	seq := 0
	for start := 0; start < rows; start += step {
		batch := make([]Event, 0, step)
		for index := start; index < start+step && index < rows; index++ {
			seq++
			batch = append(batch, messageRow(0, fmt.Sprintf("m-%d", seq), "user",
				EventKindUserInput, fmt.Sprintf("row-%d", seq)))
		}
		if _, err := store.messageCommit(key, fmt.Sprintf("commit-%d", start), batch); err != nil {
			t.Fatalf("commit batch %d: %v", start, err)
		}
	}
}

// pinFirstReader 把**第一个**进入"锁外解码"的读者钉住：返回的 release 关闭后
// 它才继续；entered 关闭表示读者已停在解码里。钩子只生效一次（后续读者直通），
// 因此不会被重试路径或其它读点二次阻塞。
func pinFirstReader(t *testing.T) (entered chan struct{}, release func()) {
	t.Helper()
	enteredCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var once sync.Once
	publishedReadHook = func() {
		once.Do(func() {
			close(enteredCh)
			<-releaseCh
		})
	}
	t.Cleanup(func() { publishedReadHook = nil })
	var releaseOnce sync.Once
	return enteredCh, func() { releaseOnce.Do(func() { close(releaseCh) }) }
}

// TestMessageReadDecodeOutsideWriterLock 证明写者不排在读者后面：
// 读者停在解码中 ⇒ 提交仍必须完成（否则说明读路径仍持 messageMu）。
func TestMessageReadDecodeOutsideWriterLock(t *testing.T) {
	store, key := messageFixture(t, 100)
	buildMessageRows(t, store, key, 100, 10)

	entered, release := pinFirstReader(t)
	defer release()

	readDone := make(chan error, 1)
	go func() {
		rows, err := store.readAllRows(key)
		if err == nil && len(rows) < 100 {
			err = fmt.Errorf("readAllRows = %d rows, want >= 100", len(rows))
		}
		readDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("读者未能进入锁外解码（钩子未被调用）")
	}

	commitDone := make(chan error, 1)
	go func() {
		_, err := store.messageCommit(key, "commit-tail",
			[]Event{messageRow(0, "m-tail", "user", EventKindUserInput, "tail")})
		commitDone <- err
	}()
	select {
	case err := <-commitDone:
		if err != nil {
			t.Fatalf("并发提交失败: %v", err)
		}
	case <-time.After(10 * time.Second):
		release()
		<-readDone
		t.Fatal("RED: 读者解码期间提交迟迟不完成——读路径仍在 messageMu 内解码（C2 回归）")
	}

	release()
	if err := <-readDone; err != nil {
		t.Fatalf("并发读失败: %v", err)
	}
	// 读者手里的快照早于上面那次提交：允许少看见尾行，但绝不能多看见
	// （多看见 = 读到了写者的未发布/半成品行）。
	if rows, err := store.readAllRows(key); err != nil || len(rows) != 101 {
		t.Fatalf("提交后 readAllRows = %d rows (%v), want 101", len(rows), err)
	}
}

// TestMessageReadStaleSnapshotConvergesAfterEviction 钉住无锁快照的**可见性契约**：
// 读者拿着过期快照（其引用的分片正被并发淘汰/重写）时，读必须收敛到淘汰后的
// 真相——不报假损坏、不返回已淘汰行、不返回半成品行。
func TestMessageReadStaleSnapshotConvergesAfterEviction(t *testing.T) {
	store, key := messageFixture(t, 10)
	buildMessageRows(t, store, key, 30, 10) // 3 片：1..10 / 11..20 / 21..30

	entered, release := pinFirstReader(t)
	defer release()

	type result struct {
		rows []Event
		err  error
	}
	readDone := make(chan result, 1)
	go func() {
		rows, err := store.readAllRows(key)
		readDone <- result{rows: rows, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("读者未能进入锁外解码（钩子未被调用）")
	}

	// 读者钉住时淘汰前缀 1..20：旧分片文件被删、head 换成只剩 21..30 的新片。
	if _, err := store.lRUDelete(key, 20, true); err != nil {
		t.Fatalf("lru delete: %v", err)
	}
	release()

	select {
	case got := <-readDone:
		if got.err != nil {
			t.Fatalf("过期快照读没有收敛（既不是淘汰后真相也不是显式失败）: %v", got.err)
		}
		if len(got.rows) != 10 {
			t.Fatalf("淘汰后读回 %d 行, want 10", len(got.rows))
		}
		for _, row := range got.rows {
			if row.Seq <= 20 {
				t.Fatalf("读回已淘汰行 seq=%d（快照未收敛）", row.Seq)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("过期快照读未返回")
	}
}

// TestWithPublishedHeadRetriesOnceAfterDrift 钉住重试契约：解码撞上并发清理
// （坐标已漂移）时以新快照重试一次并成功。
func TestWithPublishedHeadRetriesOnceAfterDrift(t *testing.T) {
	store, key := messageFixture(t, 10)
	buildMessageRows(t, store, key, 10, 10)

	calls := 0
	rows, err := store.withPublishedHead(key, func(head messageHead) ([]Event, error) {
		calls++
		if calls == 1 {
			// 模拟"读到一半分片被并发清理"：期间提交推进发布点（坐标漂移）。
			if _, commitErr := store.messageCommit(key, "commit-2",
				[]Event{messageRow(0, "m-11", "user", EventKindUserInput, "row-11")}); commitErr != nil {
				t.Fatalf("并发提交: %v", commitErr)
			}
			return nil, fmt.Errorf("shard vanished mid-read")
		}
		return store.decodePublishedRows(key, head, 1, 0)
	})
	if err != nil {
		t.Fatalf("漂移后应重试一次并成功: %v", err)
	}
	if calls != 2 {
		t.Fatalf("decode 调用次数 = %d, want 2", calls)
	}
	if len(rows) != 11 || rows[len(rows)-1].Seq != 11 {
		t.Fatalf("重试结果 %d 行（末 %d）, want 11 行（末 11）", len(rows), rows[len(rows)-1].Seq)
	}
}

// TestWithPublishedHeadReportsErrorWithoutDrift 反向护栏：坐标没漂移时错误
// 原样上抛（不得用重试掩盖真损坏），且不重试。
func TestWithPublishedHeadReportsErrorWithoutDrift(t *testing.T) {
	store, key := messageFixture(t, 10)
	buildMessageRows(t, store, key, 10, 10)

	calls := 0
	_, err := store.withPublishedHead(key, func(messageHead) ([]Event, error) {
		calls++
		return nil, fmt.Errorf("corrupt shard")
	})
	if err == nil || !strings.Contains(err.Error(), "corrupt shard") {
		t.Fatalf("err = %v, want 原样上抛 corrupt shard", err)
	}
	if calls != 1 {
		t.Fatalf("decode 调用次数 = %d, want 1（未漂移不重试）", calls)
	}
}
