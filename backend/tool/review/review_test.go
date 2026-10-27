package review

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 本文件钉**判据本身**（分类表 / 去重键）与**宿主契约**（全被跳过不写宿主、无会话不发布）。
// 门面级集成用例（裁决通道、快照投影、工作区切换清理、超限文案）仍在 `application/assembly`，
// 打的是 Service → 快照这条端到端路径，不重复这里的判据。

func TestClassifyReadError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		expect string
	}{
		{"nil", nil, ""},
		{"敏感文件名", errors.New("read worktree file: sensitive name: .env"), dto.ReviewSkipSensitive},
		{"忽略目录", errors.New("read: ignored directory .git"), dto.ReviewSkipSandbox},
		{"越界", errors.New("read: path escapes workspace root"), dto.ReviewSkipSandbox},
		{"绝对路径", errors.New("read: absolute path not allowed"), dto.ReviewSkipSandbox},
		{"符号链接", errors.New("read: symbolic link is not followed"), dto.ReviewSkipSandbox},
		{"目录", errors.New("read: target is a directory"), dto.ReviewSkipSandbox},
		{"缺路径", errors.New("read: path required"), dto.ReviewSkipSandbox},
		{"未知文案按缺失", errors.New("read: no such file"), dto.ReviewSkipMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReadError(tc.err); got != tc.expect {
				t.Fatalf("ClassifyReadError(%v) = %q, want %q", tc.err, got, tc.expect)
			}
		})
	}
}

func TestNormalizeKey(t *testing.T) {
	if got := NormalizeKey(`docs\a\b.md`); got != "docs/a/b.md" {
		t.Fatalf("分隔符应归一，got %q", got)
	}
	if NormalizeKey("docs/a.md") == NormalizeKey("docs/A.md") {
		t.Fatal("本函数只管字面：大小写不同不算同一份（路径语义在 workspace 域）")
	}
}

// fakeWorkspaceFile 是 contract.WorkspaceFilePort 的最小结构满足实现。
type fakeWorkspaceFile struct {
	contents map[string]dto.FileContent
	errs     map[string]error
	reads    []string
}

func (port *fakeWorkspaceFile) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	port.reads = append(port.reads, root+":"+relPath)
	if err := port.errs[relPath]; err != nil {
		return dto.FileContent{}, err
	}
	content, ok := port.contents[relPath]
	if !ok {
		return dto.FileContent{}, errors.New("no such file")
	}
	return content, nil
}

func (port *fakeWorkspaceFile) GitCommitFileContent(string, string, string, int64) (dto.FileContent, error) {
	return dto.FileContent{}, errors.New("not implemented")
}

var _ contract.WorkspaceFilePort = (*fakeWorkspaceFile)(nil)

// fakeHost 记录宿主调用，供断言"什么时候写了宿主、什么时候没写"。
type fakeHost struct {
	port          *fakeWorkspaceFile
	workspaceErr  error
	sessionInCtx  string
	viewSessionID string
	setCalls      []string
	clears        []string
	publishes     []string
	nextID        string
}

func newFakeHost(port *fakeWorkspaceFile) *fakeHost {
	return &fakeHost{port: port, sessionInCtx: "session-ctx", nextID: "review-1-1"}
}

func (host *fakeHost) WorkspaceFile() (contract.WorkspaceFilePort, string, error) {
	if host.workspaceErr != nil {
		return nil, "", host.workspaceErr
	}
	if host.port == nil {
		return nil, "", errors.New("no workspace")
	}
	return host.port, "/root", nil
}

func (host *fakeHost) NextRequestID() string { return host.nextID }

func (host *fakeHost) SessionIDFromContext(context.Context) string { return host.sessionInCtx }

func (host *fakeHost) ViewSessionID() string { return host.viewSessionID }

func (host *fakeHost) SetReviewRequest(sessionID string, request *dto.ReviewRequest) uint64 {
	host.setCalls = append(host.setCalls, sessionID)
	return 42
}

func (host *fakeHost) ClearReviewRequest(sessionID string) {
	host.clears = append(host.clears, sessionID)
}

func (host *fakeHost) PublishSnapshotChanged(sessionID string, revision uint64) {
	host.publishes = append(host.publishes, sessionID)
}

// TestRequestWritesHostOnlyWhenSomethingPassed：全被跳过 → 不写宿主、不发布；
// 有通过 → 写一次 + 发布一次，且收据如实列出被跳过项。
func TestRequestWritesHostOnlyWhenSomethingPassed(t *testing.T) {
	port := &fakeWorkspaceFile{
		contents: map[string]dto.FileContent{
			"docs/ok.md": {Path: "docs/ok.md", TextLike: true},
			"docs/bin":   {Path: "docs/bin", TextLike: false},
		},
		errs: map[string]error{"docs/secret": errors.New("sensitive name: .env")},
	}
	host := newFakeHost(port)
	requests := NewRequests(host)

	receipt, err := requests.Request(context.Background(), []dto.ReviewFileRef{
		{Path: "docs/ok.md"},
		{Path: "docs/ok.md"},    // 重复
		{Path: " docs\\ok.md "}, // 归一后仍是重复
		{Path: "docs/bin"},      // 二进制
		{Path: "docs/secret"},   // 敏感
		{Path: "  "},            // 空路径
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Files) != 1 || receipt.Files[0].Path != "docs/ok.md" {
		t.Fatalf("只该接受一份，got %+v", receipt.Files)
	}
	if receipt.RequestID != "review-1-1" {
		t.Fatalf("收据应带召唤 ID，got %q", receipt.RequestID)
	}
	reasons := map[string]string{}
	for _, skipped := range receipt.Skipped {
		reasons[skipped.Path] = skipped.Reason
	}
	if reasons["docs/bin"] != dto.ReviewSkipBinary || reasons["docs/secret"] != dto.ReviewSkipSensitive ||
		reasons["  "] != dto.ReviewSkipMissing {
		t.Fatalf("跳过原因应如实分类，got %+v", receipt.Skipped)
	}
	if len(host.setCalls) != 1 || host.setCalls[0] != "session-ctx" {
		t.Fatalf("应写一次宿主（会话取 ctx），got %v", host.setCalls)
	}
	if len(host.publishes) != 1 || host.publishes[0] != "session-ctx" {
		t.Fatalf("应发布一次快照事件，got %v", host.publishes)
	}
	// 敏感/二进制/重复都不该再读第二遍（去重在读取之前）。
	if len(port.reads) != 3 {
		t.Fatalf("应只读三份（去重在前），got %v", port.reads)
	}
}

func TestRequestKeepsHostUntouchedWhenAllSkipped(t *testing.T) {
	port := &fakeWorkspaceFile{errs: map[string]error{"docs/a": errors.New("no such file")}}
	host := newFakeHost(port)

	if _, err := NewRequests(host).Request(context.Background(), []dto.ReviewFileRef{{Path: "docs/a"}}); err != nil {
		t.Fatal(err)
	}
	if len(host.setCalls) != 0 || len(host.publishes) != 0 {
		t.Fatalf("全被跳过时不得碰既有请求（否则会把用户开着的抽屉关掉）：%v / %v", host.setCalls, host.publishes)
	}
}

func TestRequestWithoutSessionDoesNotPublish(t *testing.T) {
	port := &fakeWorkspaceFile{contents: map[string]dto.FileContent{"docs/ok.md": {Path: "docs/ok.md", TextLike: true}}}
	host := newFakeHost(port)
	host.sessionInCtx = ""
	host.viewSessionID = ""

	if _, err := NewRequests(host).Request(context.Background(), []dto.ReviewFileRef{{Path: "docs/ok.md"}}); err != nil {
		t.Fatal(err)
	}
	if len(host.setCalls) != 0 || len(host.publishes) != 0 {
		t.Fatalf("没有会话归属时不该写请求也不该发布：%v / %v", host.setCalls, host.publishes)
	}
}

// TestRequestFallsBackToViewSession：ctx 里没有会话时按视图会话归属。
func TestRequestFallsBackToViewSession(t *testing.T) {
	port := &fakeWorkspaceFile{contents: map[string]dto.FileContent{"docs/ok.md": {Path: "docs/ok.md", TextLike: true}}}
	host := newFakeHost(port)
	host.sessionInCtx = ""
	host.viewSessionID = "view-1"

	if _, err := NewRequests(host).Request(context.Background(), []dto.ReviewFileRef{{Path: "docs/ok.md"}}); err != nil {
		t.Fatal(err)
	}
	if len(host.setCalls) != 1 || host.setCalls[0] != "view-1" {
		t.Fatalf("应回退视图会话，got %v", host.setCalls)
	}
}

// TestRequestWorkspaceFailureBubbles：workspace 解析失败要显式冒泡（带 show_files 前缀）。
func TestRequestWorkspaceFailureBubbles(t *testing.T) {
	host := newFakeHost(nil)
	host.workspaceErr = errors.New("worktree: workspace backend does not support file preview")

	_, err := NewRequests(host).Request(context.Background(), []dto.ReviewFileRef{{Path: "docs/a.md"}})
	if err == nil || err.Error() != "show_files: worktree: workspace backend does not support file preview" {
		t.Fatalf("文案必须保留，got %v", err)
	}
}

// TestClearUsesViewSessionWhenBlank：Clear 的 sessionID 为空时按视图会话；空视图会话是 no-op。
func TestClearUsesViewSessionWhenBlank(t *testing.T) {
	host := newFakeHost(nil)
	host.viewSessionID = "view-1"
	requests := NewRequests(host)

	requests.Clear(" ")
	if len(host.clears) != 1 || host.clears[0] != "view-1" {
		t.Fatalf("空 sessionID 应按视图会话清，got %v", host.clears)
	}
	host.viewSessionID = ""
	requests.Clear("")
	if len(host.clears) != 1 {
		t.Fatalf("没有会话归属时不该调用宿主，got %v", host.clears)
	}
}
