package sessionstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// mediaTestPNG 生成一张可识别尺寸的真 PNG，避免用假字节掩盖编码问题。
func mediaTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			if x < width/2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
				continue
			}
			img.Set(x, y, color.RGBA{B: 255, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func openMediaTestStore(t *testing.T, settings Settings) (Repository, MediaStore) {
	t.Helper()
	repository, err := Open(context.Background(), Config{
		Backend: BackendJSON, Path: t.TempDir(), SessionStorage: settings,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	media, ok := MediaStoreOf(repository)
	if !ok {
		t.Fatal("jsonRepository does not expose the MediaStore capability")
	}
	return repository, media
}

func mediaTestKey() Key { return Key{ProjectID: "p-media", SessionID: "s-media"} }

// TestMediaStoreKeepsOriginalNameUnderHashFolder 锁定布局契约：
// 目录名 = 内容 hash，文件名 = 原名，head 落在元数据白名单内的 media.json。
func TestMediaStoreKeepsOriginalNameUnderHashFolder(t *testing.T) {
	repository, media := openMediaTestStore(t, Settings{})
	key := mediaTestKey()
	data := mediaTestPNG(t, 16, 8)

	item, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name:     `C:\Users\tester\AppData\Local\Temp\shot-20260913-101530.png`,
		MimeType: "image/png", Kind: "screenshot", Data: data, Width: 16, Height: 8, Scale: 1,
	})
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	if item.Name != "shot-20260913-101530.png" {
		t.Fatalf("name = %q, want the original base name", item.Name)
	}
	layout := repository.(*jsonRepository).layout
	itemPath := filepath.Join(layout.mediaItemDir(key, item.Hash), item.Name)
	stored, err := os.ReadFile(itemPath)
	if err != nil {
		t.Fatalf("media item not found at meta/<hash>/<原名>: %v", err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatal("stored media bytes differ from the input")
	}
	if item.Ref != MediaRefPrefix+item.Hash || MediaRefHash(item.Ref) != item.Hash {
		t.Fatalf("ref = %q, hash = %q", item.Ref, item.Hash)
	}
	if _, err := os.Stat(layout.modulePath(key, moduleMedia)); err != nil {
		t.Fatalf("media head metadata/media.json missing: %v", err)
	}
	head, err := readModuleHeadPayload[mediaHead](layout, key, moduleMedia)
	if err != nil {
		t.Fatalf("read media head: %v", err)
	}
	if len(head.Items) != 1 || head.Items[0].Ref != item.Ref || head.TotalBytes != len(data) {
		t.Fatalf("media head = %+v", head)
	}
}

// TestMediaStoreNeverTruncates 是不变式测试：媒体没有字符软限，
// 无论二进制多长或多「不友好」都必须逐字节往返；超硬限只能整体拒绝。
func TestMediaStoreNeverTruncates(t *testing.T) {
	binary := append([]byte{0x89, 'P', 'N', 'G', 0x00, 0xFF, 0xFE}, bytes.Repeat([]byte{0x00, 0xC3, 0x28}, 300)...)
	_, media := openMediaTestStore(t, Settings{})
	item, err := media.WriteMedia(context.Background(), mediaTestKey(), MediaItem{
		Name: "raw.bin", MimeType: "application/octet-stream", Data: binary,
	})
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	_, stored, err := media.ReadMedia(context.Background(), mediaTestKey(), item.Ref)
	if err != nil {
		t.Fatalf("ReadMedia: %v", err)
	}
	if !bytes.Equal(stored, binary) {
		t.Fatalf("round trip changed bytes: %d -> %d", len(binary), len(stored))
	}
	if item.Bytes != len(binary) {
		t.Fatalf("bytes = %d, want %d", item.Bytes, len(binary))
	}

	// 超硬限：显式拒绝，且不落任何半成品。
	_, limited := openMediaTestStore(t, Settings{MediaMaxItemBytes: 32, MediaSessionQuotaBytes: 64})
	_, err = limited.WriteMedia(context.Background(), mediaTestKey(), MediaItem{
		Name: "big.png", MimeType: "image/png", Data: mediaTestPNG(t, 64, 64),
	})
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("err = %v, want ErrMediaTooLarge", err)
	}
	listed, err := limited.ListMedia(context.Background(), mediaTestKey())
	if err != nil || len(listed) != 0 {
		t.Fatalf("rejected write left media behind: %+v err=%v", listed, err)
	}
}

// TestMediaStoreRejectsByLimitAxis 覆盖三条硬限轴（单件字节 / 像素长边 / 条目数 / 配额）。
func TestMediaStoreRejectsByLimitAxis(t *testing.T) {
	pngBytes := mediaTestPNG(t, 32, 32)
	cases := []struct {
		name     string
		settings Settings
		item     MediaItem
		prepare  func(t *testing.T, media MediaStore)
		wantErr  error
	}{
		{
			name: "invalid item", settings: Settings{},
			item: MediaItem{Name: "", MimeType: "", Data: nil}, wantErr: ErrMediaInvalid,
		},
		{
			name:     "item bytes over limit",
			settings: Settings{MediaMaxItemBytes: 16, MediaSessionQuotaBytes: 64},
			item:     MediaItem{Name: "shot.png", MimeType: "image/png", Data: pngBytes},
			wantErr:  ErrMediaTooLarge,
		},
		{
			name:     "long side over limit",
			settings: Settings{MediaMaxLongSide: 16},
			item:     MediaItem{Name: "shot.png", MimeType: "image/png", Data: pngBytes, Width: 32, Height: 32},
			wantErr:  ErrMediaDimensions,
		},
		{
			name: "item count over limit", settings: Settings{MediaMaxItemsPerSession: 1},
			item: MediaItem{Name: "second.png", MimeType: "image/png", Data: append(append([]byte{}, pngBytes...), 'x')},
			prepare: func(t *testing.T, media MediaStore) {
				t.Helper()
				if _, err := media.WriteMedia(context.Background(), mediaTestKey(), MediaItem{
					Name: "first.png", MimeType: "image/png", Data: pngBytes,
				}); err != nil {
					t.Fatalf("prepare: %v", err)
				}
			},
			wantErr: ErrMediaItemLimit,
		},
		{
			name:     "session quota over limit",
			settings: Settings{MediaMaxItemBytes: 1024, MediaSessionQuotaBytes: 1536},
			item:     MediaItem{Name: "second.png", MimeType: "image/png", Data: bytes.Repeat([]byte{'b'}, 1000)},
			prepare: func(t *testing.T, media MediaStore) {
				t.Helper()
				if _, err := media.WriteMedia(context.Background(), mediaTestKey(), MediaItem{
					Name: "first.png", MimeType: "image/png", Data: bytes.Repeat([]byte{'a'}, 1000),
				}); err != nil {
					t.Fatalf("prepare: %v", err)
				}
			},
			wantErr: ErrMediaQuota,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, media := openMediaTestStore(t, testCase.settings)
			if testCase.prepare != nil {
				testCase.prepare(t, media)
			}
			if _, err := media.WriteMedia(context.Background(), mediaTestKey(), testCase.item); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// TestMediaStoreDeduplicatesByContent 验证内容寻址：同字节只落一份，
// 换名写入记为别名且不重复占配额。
func TestMediaStoreDeduplicatesByContent(t *testing.T) {
	repository, media := openMediaTestStore(t, Settings{})
	key := mediaTestKey()
	data := mediaTestPNG(t, 8, 8)

	first, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "shot-001.png", MimeType: "image/png", Data: data,
	})
	if err != nil {
		t.Fatalf("first WriteMedia: %v", err)
	}
	second, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "shot-002.png", MimeType: "image/png", Data: data,
	})
	if err != nil {
		t.Fatalf("second WriteMedia: %v", err)
	}
	if first.Ref != second.Ref || first.Name != second.Name {
		t.Fatalf("dedup failed: %+v vs %+v", first, second)
	}
	if len(second.Aliases) != 1 || second.Aliases[0] != "shot-002.png" {
		t.Fatalf("aliases = %+v, want [shot-002.png]", second.Aliases)
	}
	entries, err := os.ReadDir(repository.(*jsonRepository).layout.mediaRoot(key))
	if err != nil || len(entries) != 1 {
		t.Fatalf("media dirs = %d err=%v, want 1", len(entries), err)
	}
}

// TestMediaStoreCollectsUnreferenced 验证 GC：引用集内的保留，集合外的删除，
// dryRun 只报告不删除。
func TestMediaStoreCollectsUnreferenced(t *testing.T) {
	_, media := openMediaTestStore(t, Settings{})
	key := mediaTestKey()
	keep, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "keep.png", MimeType: "image/png", Data: mediaTestPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("WriteMedia keep: %v", err)
	}
	drop, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "drop.png", MimeType: "image/png", Data: mediaTestPNG(t, 9, 9),
	})
	if err != nil {
		t.Fatalf("WriteMedia drop: %v", err)
	}
	referenced := ReferencedMediaHashes([]ToolResult{{Multimodal: []MediaRef{keep}}})

	candidates, err := media.CollectMedia(context.Background(), key, referenced, true)
	if err != nil || len(candidates) != 1 || candidates[0] != drop.Hash {
		t.Fatalf("dry run candidates = %+v err=%v, want [%s]", candidates, err, drop.Hash)
	}
	if _, _, err := media.ReadMedia(context.Background(), key, drop.Ref); err != nil {
		t.Fatalf("dry run must not delete: %v", err)
	}
	removed, err := media.CollectMedia(context.Background(), key, referenced, false)
	if err != nil || len(removed) != 1 || removed[0] != drop.Hash {
		t.Fatalf("removed = %+v err=%v, want [%s]", removed, err, drop.Hash)
	}
	if _, _, err := media.ReadMedia(context.Background(), key, keep.Ref); err != nil {
		t.Fatalf("referenced media must survive GC: %v", err)
	}
	if _, _, err := media.ReadMedia(context.Background(), key, drop.Ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced media still readable: %v", err)
	}
}

// TestMediaStoreRejectsBadRef 覆盖非法与缺失引用：必须显式报错，不得静默空结果。
func TestMediaStoreRejectsBadRef(t *testing.T) {
	_, media := openMediaTestStore(t, Settings{})
	cases := []struct {
		ref     string
		wantErr error
	}{
		{ref: "", wantErr: ErrMediaRefInvalid},
		{ref: "blob:deadbeef", wantErr: ErrMediaRefInvalid},
		{ref: MediaRefPrefix, wantErr: ErrMediaRefInvalid},
		{ref: MediaRefPrefix + "0000000000000000000000000000000000000000000000000000000000000000", wantErr: os.ErrNotExist},
	}
	for _, testCase := range cases {
		if _, _, err := media.ReadMedia(context.Background(), mediaTestKey(), testCase.ref); !errors.Is(err, testCase.wantErr) {
			t.Fatalf("ref %q err = %v, want %v", testCase.ref, err, testCase.wantErr)
		}
	}
	if hash := MediaRefHash(" blob:abc "); hash != "" {
		t.Fatalf("MediaRefHash(blob:abc) = %q, want empty", hash)
	}
}

// TestMediaQuotaIsIndependentFromTextBlob 验证分轴配额的独立性：
// 媒体配额打满不影响文本大结果通道，反之文本通道挤满也不影响媒体。
func TestMediaQuotaIsIndependentFromTextBlob(t *testing.T) {
	repository, media := openMediaTestStore(t, Settings{
		MediaMaxItemBytes: 1024, MediaSessionQuotaBytes: 1024,
		BlobSoftLimitChars: 1, BlobHardLimitBytes: 1 << 20, BlobSessionQuotaBytes: 1 << 20,
	})
	key := mediaTestKey()
	if _, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "shot-001.png", MimeType: "image/png", Data: bytes.Repeat([]byte{'a'}, 1000),
	}); err != nil {
		t.Fatalf("first media write: %v", err)
	}
	if _, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "shot-002.png", MimeType: "image/png", Data: bytes.Repeat([]byte{'b'}, 1000),
	}); !errors.Is(err, ErrMediaQuota) {
		t.Fatalf("err = %v, want ErrMediaQuota", err)
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{
		Events: []Event{{Role: "user", Content: "hi", MessageID: "m1"}},
		ToolResults: []ToolResult{{
			Ref: "blob:text-1", Tool: "grep", Content: "text archive", Digest: "d1", Size: 12,
		}},
	}); err != nil {
		t.Fatalf("text channel blocked by media quota: %v", err)
	}
}

// TestMediaRefsRoundTripThroughToolResult 验证多模态引用随工具结果落盘可还原。
func TestMediaRefsRoundTripThroughToolResult(t *testing.T) {
	repository, media := openMediaTestStore(t, Settings{})
	key := mediaTestKey()
	item, err := media.WriteMedia(context.Background(), key, MediaItem{
		Name: "shot.png", MimeType: "image/png", Kind: "screenshot", Data: mediaTestPNG(t, 8, 8), Width: 8, Height: 8,
	})
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{
		Events: []Event{{Role: "user", Content: "看截图", MessageID: "m1"}},
		ToolResults: []ToolResult{{
			Ref: "blob:tool-1", Tool: "screenshot", Content: "captured 8x8",
			Digest: "d1", Size: 13, Multimodal: []MediaRef{item},
		}},
	}); err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}
	results, err := repository.ListToolResults(context.Background(), key)
	if err != nil || len(results) != 1 {
		t.Fatalf("ListToolResults = %+v err=%v", results, err)
	}
	if len(results[0].Multimodal) != 1 || results[0].Multimodal[0].Ref != item.Ref {
		t.Fatalf("multimodal refs lost: %+v", results[0].Multimodal)
	}
	raw, err := json.Marshal(results[0])
	if err != nil || !bytes.Contains(raw, []byte(`"multimodal"`)) {
		t.Fatalf("result json = %s err=%v", raw, err)
	}
	if referenced := ReferencedMediaHashes(results); !referenced[item.Hash] {
		t.Fatalf("referenced set = %+v, want hash %s", referenced, item.Hash)
	}
}
