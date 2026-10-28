package artifact

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

func testStore(t *testing.T, settings storeroot.Settings) (*Store, storelayout.Scope) {
	t.Helper()
	root := t.TempDir()
	layout := storelayout.New(root)
	return New(layout, settings), storelayout.Scope{ProjectID: "project", SessionID: "session"}
}

func TestMediaRoundTripDedupAndPaths(t *testing.T) {
	store, scope := testStore(t, storeroot.Settings{})
	data := []byte("image-bytes")
	first, err := store.WriteMedia(scope, MediaItem{
		Name: `C:\shots\screen.png`, MimeType: "image/png", Kind: "screenshot", Data: data, Width: 8, Height: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "screen.png" || first.Hash != MediaHash(data) {
		t.Fatalf("media ref=%+v", first)
	}
	second, err := store.WriteMedia(scope, MediaItem{Name: "alias.png", MimeType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if second.Hash != first.Hash || len(second.Aliases) != 1 || second.Aliases[0] != "alias.png" {
		t.Fatalf("dedup ref=%+v", second)
	}
	read, payload, err := store.ReadMedia(scope, first.Ref)
	if err != nil || read.Hash != first.Hash || !bytes.Equal(payload, data) {
		t.Fatalf("read=%+v payload=%q err=%v", read, payload, err)
	}
	if _, err := os.Stat(filepath.Join(store.MediaRoot(scope), first.Hash, "meta.json")); err != nil {
		t.Fatalf("media meta path changed: %v", err)
	}
	// MediaPath 是「点开缩略图，用系统默认查看器看原图」的唯一取路径入口：
	// 它必须指向真实落盘文件，且非法 ref 不能编出一条路径来。
	path, err := store.MediaPath(scope, first.Ref)
	if err != nil {
		t.Fatalf("MediaPath: %v", err)
	}
	if stored, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(stored, data) {
		t.Fatalf("MediaPath 指向的不是原图: %q err=%v", path, readErr)
	}
	if _, err := store.MediaPath(scope, "media:"); err == nil {
		t.Fatal("非法 ref 必须显式失败")
	}
	if _, err := store.MediaPath(scope, "media:deadbeef"); err == nil {
		t.Fatal("不存在的 ref 必须显式失败，不能编出一条路径")
	}
	head, err := store.ReadMediaHead(scope)
	if err != nil || len(head.Items) != 1 || head.TotalBytes != len(data) {
		t.Fatalf("media head=%+v err=%v", head, err)
	}
}

func TestMediaLimitsAndGC(t *testing.T) {
	store, scope := testStore(t, storeroot.Settings{
		MediaMaxItemBytes: 4, MediaSessionQuotaBytes: 8, MediaMaxItemsPerSession: 2, MediaMaxLongSide: 10,
	})
	if _, err := store.WriteMedia(scope, MediaItem{Name: "big", MimeType: "image/png", Data: []byte("12345")}); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("size err=%v", err)
	}
	keep, err := store.WriteMedia(scope, MediaItem{Name: "keep", MimeType: "image/png", Data: []byte("aaaa")})
	if err != nil {
		t.Fatal(err)
	}
	drop, err := store.WriteMedia(scope, MediaItem{Name: "drop", MimeType: "image/png", Data: []byte("bbbb")})
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{keep.Hash: true}
	candidates, err := store.CollectMedia(scope, refs, true)
	if err != nil || len(candidates) != 1 || candidates[0] != drop.Hash {
		t.Fatalf("dry-run=%v err=%v", candidates, err)
	}
	if _, _, err := store.ReadMedia(scope, drop.Ref); err != nil {
		t.Fatalf("dry-run deleted media: %v", err)
	}
	if _, err := store.CollectMedia(scope, refs, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadMedia(scope, drop.Ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced media remains: %v", err)
	}
}

func TestBlobToolResultAndGC(t *testing.T) {
	store, scope := testStore(t, storeroot.Settings{
		BlobSoftLimitChars: 3, BlobHardLimitBytes: 16, BlobSessionQuotaBytes: 64,
	})
	if _, err := store.WriteBlob(scope, "bash", "12345678901234567"); !errors.Is(err, ErrBigToolResultTooLarge) {
		t.Fatalf("hard limit err=%v", err)
	}
	blob, err := store.WriteBlob(scope, "bash", "abcdefg")
	if err != nil {
		t.Fatal(err)
	}
	if !blob.Truncated || blob.Content != "abc" {
		t.Fatalf("blob=%+v", blob)
	}
	content, err := store.ReadBlob(scope, blob.Hash)
	if err != nil || content != "abc" {
		t.Fatalf("read=%q err=%v", content, err)
	}
	if err := store.WriteToolResult(scope, "result:1", []byte(`{"ref":"result:1"}`)); err != nil {
		t.Fatal(err)
	}
	payload, err := store.ReadToolResult(scope, "result:1")
	if err != nil || string(payload) != `{"ref":"result:1"}` {
		t.Fatalf("tool result=%s err=%v", payload, err)
	}
	removed, err := store.CollectBlobs(scope, nil, false)
	if err != nil || len(removed) != 1 || removed[0] != blob.Hash {
		t.Fatalf("blob GC=%v err=%v", removed, err)
	}
}

func TestSearchIndexBuildAndQuery(t *testing.T) {
	store, scope := testStore(t, storeroot.Settings{})
	rows := []SearchRow{
		{Seq: 1, Content: "Alpha 项目计划"},
		{Seq: 2, Content: "Beta 搜索实现", ReasoningContent: "keyword"},
	}
	if err := store.PutSearch(scope, rows, 2, 1, "Alpha 摘要"); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(scope, "Alpha")
	if err != nil || len(hits) == 0 {
		t.Fatalf("Alpha hits=%+v err=%v", hits, err)
	}
	hits, err = store.Search(scope, "Beta")
	if err != nil || len(hits) == 0 || hits[0].SummaryOnly {
		t.Fatalf("Beta hits=%+v err=%v", hits, err)
	}
	if _, err := store.Search(scope, "missing"); err != nil {
		t.Fatalf("no-hit query must not error: %v", err)
	}
}
