package workunit

import (
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
	"strings"
	"sync"
	"testing"
)

func unitRecord(nodeID, kind string) storeworkunit.Record {
	record := storeworkunit.Record{NodeID: nodeID}
	record.Unit.Kind = kind
	return record
}

// TestKindOfReadsTheRecordsOwnIdentity：身份判据的顺序是判据的一部分——
// ① 记录写明的身份优先；② 老记录缺身份时按**团队事实**（现场名单）兜底；
// ③ 认不得的身份词按既有链（子代理）归类，并把词交回调用方留痕（不静默归类）。
func TestKindOfReadsTheRecordsOwnIdentity(t *testing.T) {
	scenes := map[string]bool{"n-team": true}

	if kind, unknown := KindOf(unitRecord("n1", string(KindTeammate)), scenes); kind != KindTeammate || unknown != "" {
		t.Fatalf("写明的身份必须被采信：kind=%q unknown=%q", kind, unknown)
	}
	if kind, unknown := KindOf(unitRecord("n1", string(KindSubagent)), scenes); kind != KindSubagent || unknown != "" {
		t.Fatalf("写明的身份必须被采信：kind=%q unknown=%q", kind, unknown)
	}
	// 老记录（没有身份那一格）+ 落在团队现场名单里 ⇒ teammate。
	if kind, _ := KindOf(unitRecord("n-team", ""), scenes); kind != KindTeammate {
		t.Fatalf("现场名单里的老记录必须按 teammate 恢复，得到 %q", kind)
	}
	if kind, _ := KindOf(unitRecord("n-other", "  "), scenes); kind != KindSubagent {
		t.Fatalf("名单外的老记录按子代理恢复，得到 %q", kind)
	}
	// 认不得的身份词：按子代理归类 + 把词交回（调用方留痕），绝不静默当成"新的某层"。
	kind, unknown := KindOf(unitRecord("n9", "remote"), scenes)
	if kind != KindSubagent || unknown != "remote" {
		t.Fatalf("未知身份词应回 (subagent, remote)，得到 (%q, %q)", kind, unknown)
	}
}

// stubStrategy 是恢复策略替身：记下自己收到的批次。
type stubStrategy struct {
	kind     Kind
	restored [][]string
}

func (s *stubStrategy) Kind() Kind { return s.kind }

func (s *stubStrategy) Restore(records []storeworkunit.Record) {
	nodeIDs := make([]string, 0, len(records))
	for _, record := range records {
		nodeIDs = append(nodeIDs, record.NodeID)
	}
	s.restored = append(s.restored, nodeIDs)
}

// TestRestoreUnitsDispatchesByIdentityAndKeepsOrder：分派按身份切开、只把**非空批次**交给
// 对应策略、策略次序原样保持，且每批都留一行痕（诊断面）。
func TestRestoreUnitsDispatchesByIdentityAndKeepsOrder(t *testing.T) {
	subagents := &stubStrategy{kind: KindSubagent}
	teammates := &stubStrategy{kind: KindTeammate}
	var logs []string

	RestoreUnits("sess-1", []storeworkunit.Record{
		unitRecord("n1", string(KindSubagent)),
		unitRecord("n2", string(KindTeammate)),
		unitRecord("n3", "weird"),
		unitRecord("n4", ""),
	}, RestoreDeps{
		SceneNodes: func(sessionID string) []string {
			if sessionID != "sess-1" {
				t.Fatalf("现场名单读数的会话号 = %q", sessionID)
			}
			return []string{"n2"}
		},
		Strategies: []RecoveryStrategy{teammates, subagents},
		Log:        func(format string, args ...any) { logs = append(logs, format) },
	})

	if len(teammates.restored) != 1 || strings.Join(teammates.restored[0], ",") != "n2" {
		t.Fatalf("teammate 批次 = %v, want [n2]", teammates.restored)
	}
	if len(subagents.restored) != 1 || strings.Join(subagents.restored[0], ",") != "n1,n3,n4" {
		t.Fatalf("subagent 批次 = %v, want [n1 n3 n4]（未知身份词归子代理）", subagents.restored)
	}
	// 两条留痕：未知身份词 + 两个策略的批次（共 3 条）。
	if len(logs) != 3 {
		t.Fatalf("留痕条数 = %d, want 3：%v", len(logs), logs)
	}

	// 空批不惊动策略；空输入与空策略表都是幂等的无事路径。
	idle := &stubStrategy{kind: KindSubagent}
	RestoreUnits("sess-1", nil, RestoreDeps{Strategies: []RecoveryStrategy{idle}})
	RestoreUnits("sess-1", []storeworkunit.Record{unitRecord("n1", "")}, RestoreDeps{})
	if len(idle.restored) != 0 {
		t.Fatalf("空输入不该触发恢复：%v", idle.restored)
	}
}

// TestRestoreUnitsWithoutSceneIndex：没有现场名单（未装配计划/账本）时，缺身份的老记录一律
// 按子代理算（与迁移前一致：没有团队事实就不做 teammate 兜底）。
func TestRestoreUnitsWithoutSceneIndex(t *testing.T) {
	subagents := &stubStrategy{kind: KindSubagent}
	RestoreUnits("sess-1", []storeworkunit.Record{unitRecord("n1", "")}, RestoreDeps{
		Strategies: []RecoveryStrategy{subagents},
	})
	if len(subagents.restored) != 1 || subagents.restored[0][0] != "n1" {
		t.Fatalf("没有现场名单时老记录应按子代理恢复：%v", subagents.restored)
	}
}

// TestResumeNotesIsReadOnceAndKeyScoped：恢复说明的容器语义——空键/空说明不记、
// `Take` 读完即消、`Peek` 非消费读、`Clear` 丢弃未读、零值与 nil 接收者都安全。
func TestResumeNotesIsReadOnceAndKeyScoped(t *testing.T) {
	var notes ResumeNotes
	notes.Set("  ", "x")
	notes.Set("node-1", "   ")
	notes.Set("node-1", "  中断后续跑  ")
	if got := notes.Peek("node-1"); got != "中断后续跑" {
		t.Fatalf("Peek = %q（应当 trim 且不清掉）", got)
	}
	if got := notes.Peek("node-1"); got != "中断后续跑" {
		t.Fatalf("Peek 是消费外读法，第二次也应读得到：%q", got)
	}
	if got := notes.Take("node-1"); got != "中断后续跑" {
		t.Fatalf("Take = %q", got)
	}
	if got := notes.Take("node-1"); got != "" {
		t.Fatalf("Take 读完即消，第二次应为空：%q", got)
	}
	if got := notes.Peek("other"); got != "" {
		t.Fatalf("键隔离：%q", got)
	}
	notes.Set("node-2", "说明")
	notes.Clear("node-2")
	if got := notes.Peek("node-2"); got != "" {
		t.Fatalf("Clear 应当丢掉未读说明：%q", got)
	}

	var nilNotes *ResumeNotes
	nilNotes.Set("k", "v")
	if got := nilNotes.Take("k"); got != "" {
		t.Fatalf("nil 接收者应安全：%q", got)
	}
}

// TestResumeNotesIsConcurrencySafe：容器自持锁，并发读写不产生数据竞争（本用例供 `-race` 使用）。
func TestResumeNotesIsConcurrencySafe(t *testing.T) {
	var notes ResumeNotes
	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := "k"
			for round := 0; round < 100; round++ {
				notes.Set(key, "说明")
				_ = notes.Peek(key)
				_ = notes.Take(key)
				notes.Clear(key)
			}
		}(index)
	}
	wg.Wait()
}
