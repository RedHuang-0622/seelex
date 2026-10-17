package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/multiagent/resume"
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/store"
)

func stringPointer(value string) *string { return &value }

func resumeRecord(nodeID, sessionID, status string) store.NodeSessionRecord {
	record := store.NodeSessionRecord{
		SchemaVersion: store.NodeSessionSchemaVersion,
		NodeID:        nodeID, SessionID: sessionID, Status: status,
		Goal: "实现渲染件", UpdatedAt: time.Unix(1700000000, 0),
	}
	record.Unit.Kind = string(workunit.KindSubagent)
	return record
}

// TestLocateResumeUnitsStatusDerivation 钉住"残留记录 → 恢复单元"的判定口径：
// 在跑 ⇒ active（要续跑）；判失败 ⇒ failed；其余 ⇒ done；**已有结论一律按终结**
// （结论唯一：残留记录只是半同步残留）。
func TestLocateResumeUnitsStatusDerivation(t *testing.T) {
	records := []store.NodeSessionRecord{
		resumeRecord("n-running", "sess-1", "running"),
		resumeRecord("n-queued", "sess-2", "queued"),
		resumeRecord("n-failed", "sess-3", "failed"),
		resumeRecord("n-done", "sess-4", "done"),
		resumeRecord("n-concluded", "sess-5", "running"), // 在跑但已有结论
		resumeRecord("n-unknown-status", "sess-6", "??"), // 认不得的词不判死
	}
	units := LocateResumeUnits("sess-main", records, map[string]struct{}{"n-concluded": {}})
	if len(units) != len(records) {
		t.Fatalf("单元数 = %d, want %d", len(units), len(records))
	}
	byKey := map[string]resume.Unit{}
	for _, unit := range units {
		byKey[unit.Key] = unit
		if unit.Kind != ResumeKind || unit.ParentSessionID != "sess-main" || unit.Goal != "实现渲染件" {
			t.Fatalf("单元字段必须带全（kind/父会话/目标）：%+v", unit)
		}
	}
	want := map[string]resume.UnitStatus{
		"n-running":        resume.UnitActive,
		"n-queued":         resume.UnitActive,
		"n-failed":         resume.UnitFailed,
		"n-done":           resume.UnitDone,
		"n-concluded":      resume.UnitDone,
		"n-unknown-status": resume.UnitDone,
	}
	for key, status := range want {
		if byKey[key].Status != status {
			t.Fatalf("%s 的状态 = %v, want %v", key, byKey[key].Status, status)
		}
	}
}

// TestResumeUnitKeyFallsBackToSession 钉住幂等键口径：节点 id 优先，缺了退到会话号；
// 两个都没有 = 没有可归位的身份（调用方跳过，不造出一个空键单元）。
func TestResumeUnitKeyFallsBackToSession(t *testing.T) {
	if got := ResumeUnitKey(workunit.Progress{NodeID: "n1", SessionID: "s1"}); got != "n1" {
		t.Fatalf("键 = %q, want n1", got)
	}
	if got := ResumeUnitKey(workunit.Progress{SessionID: "s1"}); got != "s1" {
		t.Fatalf("没有节点 id 时退到会话号：%q", got)
	}
	if got := ResumeUnitKey(workunit.Progress{}); got != "" {
		t.Fatalf("没有身份时回空键：%q", got)
	}
	records := []store.NodeSessionRecord{{SessionID: "", NodeID: ""}}
	if units := LocateResumeUnits("sess-main", records, nil); len(units) != 0 {
		t.Fatalf("没有身份的记录不该进恢复批次：%+v", units)
	}
}

// TestRecordTerminalPredicates 钉住两个谓词的分工：`RecordIsFailed` 只认 failed；
// `RecordIsTerminal` 认 done/failed；**认不得的词两个都不是**（不判死、不当终结）。
func TestRecordTerminalPredicates(t *testing.T) {
	cases := map[string]struct {
		status   string
		failed   bool
		terminal bool
	}{
		"running": {status: "running"},
		"queued":  {status: "queued"},
		"failed":  {status: "failed", failed: true, terminal: true},
		"done":    {status: "done", terminal: true},
		"未知词":     {status: "??"},
		"空":       {status: ""},
	}
	for name, want := range cases {
		record := resumeRecord("n1", "s1", want.status)
		if got := RecordIsFailed(record); got != want.failed {
			t.Fatalf("%s：RecordIsFailed = %v, want %v", name, got, want.failed)
		}
		if got := RecordIsTerminal(record); got != want.terminal {
			t.Fatalf("%s：RecordIsTerminal = %v, want %v", name, got, want.terminal)
		}
	}
}

// TestSceneOfPrefersSummaryAndFallsBackToLastStage：现场投影——摘要取记录自带的那份，
// 为空时回退到阶段日志最后一条预览（"之前做到哪"），载荷是记录本身的 JSON。
func TestSceneOfPrefersSummaryAndFallsBackToLastStage(t *testing.T) {
	record := resumeRecord("n1", "s1", "running")
	record.Summary = "已写完解析"
	record.StagesJSON = workunit.EncodeStages([]workunit.Stage{
		{Stage: "plan", Preview: "拆任务"},
		{Stage: "impl", Preview: "写解析"},
	})
	scene, err := SceneOf(record)
	if err != nil {
		t.Fatalf("SceneOf: %v", err)
	}
	if scene.Summary != "已写完解析" {
		t.Fatalf("摘要应取记录自带的那份：%q", scene.Summary)
	}
	var decoded store.NodeSessionRecord
	if err := json.Unmarshal(scene.Payload, &decoded); err != nil || decoded.NodeID != "n1" {
		t.Fatalf("载荷应当是记录本身：%v %+v", err, decoded)
	}

	record.Summary = "   "
	scene, err = SceneOf(record)
	if err != nil {
		t.Fatalf("SceneOf: %v", err)
	}
	if scene.Summary != "impl: 写解析" {
		t.Fatalf("摘要为空时应回退最后一条阶段预览：%q", scene.Summary)
	}
	if got := LastStagePreview(resumeRecord("n1", "s1", "running")); got != "" {
		t.Fatalf("没有阶段日志时预览为空：%q", got)
	}
}

// TestResumeNoteCombinesContractBodyWithFacts：恢复说明 = 契约正文（`workunit.RecoveryNote`）
// + 契约不收的两项追加事实（最后已知进展 / 回灌历史长度），且都**有界**。
func TestResumeNoteCombinesContractBodyWithFacts(t *testing.T) {
	record := resumeRecord("n1", "s1", "running")
	record.History = []types.Message{{Role: "user", Content: stringPointer("做吧")}}
	note := ResumeNote(record, "impl: 写解析")
	if !strings.HasPrefix(note, workunit.RecoveryNotePrefix) {
		t.Fatalf("正文必须以契约前缀族开头：%q", note)
	}
	if !strings.Contains(note, "last known progress: impl: 写解析") {
		t.Fatalf("追加事实要带上最后已知进展：%q", note)
	}
	if !strings.Contains(note, "recovered messages: 1") {
		t.Fatalf("追加事实要带上回灌历史长度：%q", note)
	}
	// 没有进展与历史时只留契约正文（追加事实为空串）。
	plain := ResumeNote(resumeRecord("n2", "s2", "running"), "  ")
	if strings.Contains(plain, "last known progress") || strings.Contains(plain, "recovered messages") {
		t.Fatalf("没有事实时不该凭空追加：%q", plain)
	}
}
