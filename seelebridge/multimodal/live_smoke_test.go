package multimodal

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestLiveMultimodalImageSmoke 是真机多模态冒烟（opt-in）：
//
//	生成左红右蓝 PNG → 落会话媒体分区（meta/<hash>/<原名>）→ 读回 → 编码
//	content parts → 真实 provider 带图请求 → 断言模型答出两种颜色。
//
// 断言刻意选「文字提示里没有答案」的颜色词：模型答出 red/blue 只能来自真正
// 送达的图片字节；链路任一段静默丢图（空回答、只发文本、被截断）都会失败。
//
// 运行（只把路径交给加载器，不读取打印 config/accounts.yaml 内容）：
//
//	$env:SEELEX_LIVE_SMOKE='1'
//	$env:SEELEX_ACCOUNTS_PATH='G:\Program\go\seelex\config\accounts.yaml'
//	go test ./seelebridge/multimodal/ -run TestLiveMultimodalImageSmoke -count=1 -v -timeout 300s
func TestLiveMultimodalImageSmoke(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 and SEELEX_ACCOUNTS_PATH to run the live multimodal smoke test")
	}
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_ACCOUNTS_PATH"))
	if accountsPath == "" {
		t.Skip("set SEELEX_ACCOUNTS_PATH to an accounts.yaml path to run the live multimodal smoke test")
	}
	accounts, err := config.Load(accountsPath)
	if err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	spec, err := model.ResolveAccountSpec(accounts.Specs, model.RoleAgent)
	if err != nil {
		t.Fatalf("resolve agent account: %v", err)
	}
	t.Logf("live target: model=%s base_url=%s provider=%s", spec.Model, spec.BaseURL, spec.Provider)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// 1) 会话媒体分区：真落盘、真读回（不绕过存储层直接喂内存字节）。
	storePath := t.TempDir()
	repository, err := sessionstore.Open(ctx, sessionstore.Config{
		Backend: sessionstore.BackendJSON, Path: storePath,
	})
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	media, ok := sessionstore.MediaStoreOf(repository)
	if !ok {
		t.Fatal("session store does not expose the media partition")
	}
	key := sessionstore.Key{ProjectID: "p-live-multimodal", SessionID: "s-live-multimodal"}
	pngBytes := liveSplitPNG(t, 128, 128)
	name := "prompt-smoke-" + time.Now().UTC().Format("20060102-150405.000") + ".png"
	item, err := media.WriteMedia(ctx, key, sessionstore.MediaItem{
		Name: name, MimeType: "image/png", Kind: "screenshot",
		Data: pngBytes, Width: 128, Height: 128, Scale: 1,
	})
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	listed, err := media.ListMedia(ctx, key)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListMedia = %+v err=%v", listed, err)
	}
	if listed[0].Name != name {
		t.Fatalf("stored name = %q, want original name %q", listed[0].Name, name)
	}
	onDisk, parent := findStoredFile(t, storePath, name)
	hash := strings.TrimPrefix(item.Ref, sessionstore.MediaRefPrefix)
	if parent != hash {
		t.Fatalf("media folder = %q, want content hash %q (path %s)", parent, hash, onDisk)
	}
	t.Logf("media: ref=%s name=%s bytes=%d", item.Ref, onDisk, item.Bytes)

	source, err := LoadImage(ctx, media, key, item.Ref)
	if err != nil {
		t.Fatalf("LoadImage: %v", err)
	}
	if !bytes.Equal(source.Data, pngBytes) {
		t.Fatalf("round trip changed image bytes: %d -> %d", len(pngBytes), len(source.Data))
	}

	// 2) 真实带图请求：提示里不出现颜色词，答案只能来自图片。
	prompt := "这是一张测试图片，左半部分和右半部分各是一种纯色。" +
		"只回答这两种颜色的英文单词，用空格分隔，不要解释，不要标点。"
	answer, err := CompleteWithImages(ctx, Config{
		BaseURL: spec.BaseURL, APIKey: spec.APIKey, Model: spec.Model,
		MaxTokens: 512, Temperature: 0, Timeout: 3 * time.Minute,
	}, prompt, []ImageSource{source})
	if err != nil {
		t.Fatalf("live multimodal request failed: %v", err)
	}
	t.Logf("answer: model=%s finish=%s content=%q reasoning=%q prompt_tokens=%d completion_tokens=%d request_bytes=%d",
		answer.Model, answer.FinishReason, answer.Text, answer.Reasoning,
		answer.PromptTokens, answer.CompletionTokens, answer.RequestBytes)

	lowered := strings.ToLower(answer.FinalText())
	if lowered == "" {
		t.Fatalf("live answer is empty (finish=%s, prompt_tokens=%d): nothing came back to assert on",
			answer.FinishReason, answer.PromptTokens)
	}
	for _, want := range []string{"red", "blue"} {
		if !strings.Contains(lowered, want) {
			t.Fatalf("live answer %q does not mention %q: the image bytes did not reach the model", answer.FinalText(), want)
		}
	}
	if answer.PromptTokens <= 0 {
		t.Fatalf("prompt_tokens = %d: the provider reported no input tokens", answer.PromptTokens)
	}
}

// findStoredFile 在数据根里定位落盘文件，返回绝对路径与其父目录名（应为内容 hash）。
func findStoredFile(t *testing.T, root, fileName string) (string, string) {
	t.Helper()
	found := ""
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == fileName {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk store root: %v", err)
	}
	if found == "" {
		t.Fatalf("stored media file %q not found under %s", fileName, root)
	}
	return found, filepath.Base(filepath.Dir(found))
}

// liveSplitPNG 生成左红右蓝的 PNG（纯色块，任何视觉模型都能判色）。
func liveSplitPNG(t *testing.T, width, height int) []byte {
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
