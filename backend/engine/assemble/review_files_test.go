package assemble

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// fakeReviewFilesPort 记录落点收到的清单并回一份可控收据。
type fakeReviewFilesPort struct {
	calls int
	got   []dto.ReviewFileRef
	out   dto.ReviewRequest
	err   error
}

func (port *fakeReviewFilesPort) RequestFileReview(_ context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	port.calls++
	port.got = files
	if port.err != nil {
		return dto.ReviewRequest{}, port.err
	}
	if port.out.RequestID == "" {
		port.out = dto.ReviewRequest{RequestID: "review-1", Files: files}
	}
	return port.out, nil
}

func dispatchShowFiles(t *testing.T, port ReviewFilesPort, args string) (string, error) {
	t.Helper()
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	RegisterReviewFilesTool(runtime, port)
	return runtime.Agent().DirectDispatch(context.Background(), "show_files", args)
}

// TestShowFilesBatchCapIsAnExplicitError：一次调用 = 一次批处理召唤，超限**显式
// 报错**（不静默截断、不越过落点）。
func TestShowFilesBatchCapIsAnExplicitError(t *testing.T) {
	port := &fakeReviewFilesPort{}
	files := make([]map[string]string, 0, dto.MaxReviewFilesPerRequest+1)
	for index := 0; index <= dto.MaxReviewFilesPerRequest; index++ {
		files = append(files, map[string]string{"path": "docs/a.md"})
	}
	encoded, _ := json.Marshal(map[string]any{"files": files})
	_, err := dispatchShowFiles(t, port, string(encoded))
	if err == nil {
		t.Fatal("11 份文件必须报错（上限 10），实际放行")
	}
	if !strings.Contains(err.Error(), "10") {
		t.Fatalf("超限报错必须说清上限：%v", err)
	}
	if port.calls != 0 {
		t.Fatalf("超限请求不得到达落点（calls=%d）", port.calls)
	}
}

// TestShowFilesAtCapReachesPort：恰好 10 份是允许的（上限是闭区间）。
func TestShowFilesAtCapReachesPort(t *testing.T) {
	port := &fakeReviewFilesPort{}
	files := make([]dto.ReviewFileRef, 0, dto.MaxReviewFilesPerRequest)
	for index := 0; index < dto.MaxReviewFilesPerRequest; index++ {
		files = append(files, dto.ReviewFileRef{Path: "docs/a.md", Note: "n"})
	}
	encoded, _ := json.Marshal(map[string]any{"files": files})
	if _, err := dispatchShowFiles(t, port, string(encoded)); err != nil {
		t.Fatalf("恰好 %d 份应当放行：%v", dto.MaxReviewFilesPerRequest, err)
	}
	if port.calls != 1 || len(port.got) != dto.MaxReviewFilesPerRequest {
		t.Fatalf("落点收到 calls=%d files=%d", port.calls, len(port.got))
	}
}

// TestShowFilesRejectsEmptyList：空清单是形状错误（"一次调用 = 一次批处理召唤"）。
func TestShowFilesRejectsEmptyList(t *testing.T) {
	port := &fakeReviewFilesPort{}
	if _, err := dispatchShowFiles(t, port, `{"files":[]}`); err == nil {
		t.Fatal("空 files 必须报错")
	}
	if _, err := dispatchShowFiles(t, port, `{}`); err == nil {
		t.Fatal("缺 files 必须报错")
	}
	if port.calls != 0 {
		t.Fatalf("形状错误不得到达落点（calls=%d）", port.calls)
	}
}

// TestShowFilesReceiptReportsRequestedOpenedAndSkipped：返回值必须回契约（工具结果
// 进对话，被后续回合读到）——"请求了几份 / 哪几份进去 / 哪几份没进去、为什么"。
func TestShowFilesReceiptReportsRequestedOpenedAndSkipped(t *testing.T) {
	port := &fakeReviewFilesPort{out: dto.ReviewRequest{
		RequestID: "review-7",
		Files:     []dto.ReviewFileRef{{Path: "docs/a.md", Note: "先看这里", StartLine: 3, EndLine: 9}},
		Skipped:   []dto.ReviewSkip{{Path: "config/accounts.yaml", Reason: dto.ReviewSkipSensitive}},
	}}
	result, err := dispatchShowFiles(t, port, `{"files":[{"path":"docs/a.md","note":"先看这里","start_line":3,"end_line":9},{"path":"config/accounts.yaml"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Requested int                 `json:"requested"`
		Opened    int                 `json:"opened"`
		RequestID string              `json:"request_id"`
		Files     []dto.ReviewFileRef `json:"files"`
		Skipped   []dto.ReviewSkip    `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(result), &receipt); err != nil {
		t.Fatalf("收据必须是合法 JSON：%v（原文 %q）", err, result)
	}
	if receipt.Requested != 2 || receipt.Opened != 1 || receipt.RequestID != "review-7" {
		t.Fatalf("收据计数不对：%+v", receipt)
	}
	if len(receipt.Files) != 1 || receipt.Files[0].StartLine != 3 || receipt.Files[0].EndLine != 9 {
		t.Fatalf("收据必须回列出被接受的清单：%+v", receipt.Files)
	}
	if len(receipt.Skipped) != 1 || receipt.Skipped[0].Reason != dto.ReviewSkipSensitive {
		t.Fatalf("收据必须回列出跳过原因：%+v", receipt.Skipped)
	}
}

// TestShowFilesPropagatesPortError：落点失败原样上抛（例如工作区未绑定），不吞成
// "看起来都开好了"。
func TestShowFilesPropagatesPortError(t *testing.T) {
	cause := errors.New("worktree: no workspace bound to current session")
	port := &fakeReviewFilesPort{err: cause}
	_, err := dispatchShowFiles(t, port, `{"files":[{"path":"docs/a.md"}]}`)
	if err == nil || !strings.Contains(err.Error(), "no workspace bound") {
		t.Fatalf("落点错误必须上抛，实得 %v", err)
	}
}
