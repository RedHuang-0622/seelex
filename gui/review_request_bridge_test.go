package gui

import (
	"encoding/json"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestBridgeSnapshotCarriesReviewRequest 是 J-2 的 **GUI 面 bridge 断言**：待审阅
// 请求的落点是快照只读段（可重放），所以 GUI 必须能在 Snapshot() 里拿到它，且 wire
// 字段名与前端契约一致。
//
// 为什么值得单独钉一次字段名：Go 侧改名编译照样通过，JS 侧只是静默 undefined——
// 前端 D2 的「apply review request」会因此表现成"agent 叫了文件但抽屉毫无反应"，
// 而两边各自的单测都绿。这里按 JSON 键名钉住跨语言那一半。
func TestBridgeSnapshotCarriesReviewRequest(t *testing.T) {
	fake := newFakeApplication()
	fake.snapshot.ReviewRequest = &dto.ReviewRequest{
		RequestID: "review-1",
		Files:     []dto.ReviewFileRef{{Path: "docs/readme.md", Note: "看这里", StartLine: 2, EndLine: 4}},
		Skipped:   []dto.ReviewSkip{{Path: "config/accounts.yaml", Reason: dto.ReviewSkipSensitive}},
	}
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := bridge.Snapshot()
	if snapshot.ReviewRequest == nil || snapshot.ReviewRequest.RequestID != "review-1" {
		t.Fatalf("Bridge.Snapshot() 未携带待审阅请求：%+v", snapshot.ReviewRequest)
	}
	if len(snapshot.ReviewRequest.Files) != 1 || snapshot.ReviewRequest.Files[0].Path != "docs/readme.md" {
		t.Fatalf("请求清单在 bridge 面上被改写：%+v", snapshot.ReviewRequest.Files)
	}

	encoded, err := json.Marshal(snapshot.ReviewRequest)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request_id", "files", "requested_at"} {
		if _, ok := wire[key]; !ok {
			t.Fatalf("wire 契约缺字段 %q：%s", key, encoded)
		}
	}
	files, ok := wire["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files 的 wire 形状不对：%s", encoded)
	}
	file, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("files[0] 不是对象：%s", encoded)
	}
	for _, key := range []string{"path", "note", "start_line", "end_line"} {
		if _, ok := file[key]; !ok {
			t.Fatalf("file 的 wire 契约缺字段 %q：%s", key, encoded)
		}
	}
	skipped, ok := wire["skipped"].([]any)
	if !ok || len(skipped) != 1 {
		t.Fatalf("skipped 的 wire 形状不对：%s", encoded)
	}
	skip, _ := skipped[0].(map[string]any)
	if skip["reason"] != dto.ReviewSkipSensitive {
		t.Fatalf("跳过原因词表必须原样上线：%s", encoded)
	}
}
