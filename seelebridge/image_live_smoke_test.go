//go:build imagelive

// seelex × 本地 Seele（引擎侧消息级图片）真实 API 联调冒烟（opt-in）。
//
// 它验证的是「引擎侧多模态链路」而不只是包内单测：
//   - 真机截屏 / 合成图 → types.Message.Images → provider content parts 投影；
//   - provider 真的看到了像素：合成图按随机主色顺序出题，真机截图拿前台窗口
//     标题作为地面真值，两者都必须命中；
//   - 真机 computer use 原语与引擎侧调用在同一链路里协作。
//
// 前提（默认构建不受影响）：
//
//	go mod edit -replace=github.com/RedHuang-0622/Seele=G:/Program/go/Seele
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags imagelive ./seelebridge/ -run TestEngineImageLiveSmoke -count=1 -v
//
// 联调结束后复原：go mod edit -dropreplace=github.com/RedHuang-0622/Seele
package seelebridge

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

func TestEngineImageLiveSmoke(t *testing.T) {
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsPath == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API 图片冒烟")
	}
	loaded, err := config.LoadTolerant(accountsPath)
	if err != nil {
		t.Fatalf("加载账号配置: %v", err)
	}
	if len(loaded.Specs) == 0 {
		t.Fatal("账号配置为空")
	}

	specs := loaded.Specs
	if only := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNT")); only != "" {
		var filtered []model.AccountSpec
		for _, spec := range specs {
			if spec.Name == only {
				filtered = append(filtered, spec)
			}
		}
		if len(filtered) == 0 {
			t.Fatalf("SEELEX_SMOKE_ACCOUNT=%q 未在账号配置中找到", only)
		}
		specs = filtered
	}

	t.Run("合成图主色顺序", func(t *testing.T) {
		palette := []struct {
			label string
			words []string
			fill  color.RGBA
		}{
			{label: "红", words: []string{"红", "red"}, fill: color.RGBA{R: 255, A: 255}},
			{label: "绿", words: []string{"绿", "green"}, fill: color.RGBA{G: 255, A: 255}},
			{label: "蓝", words: []string{"蓝", "blue"}, fill: color.RGBA{B: 255, A: 255}},
			{label: "黄", words: []string{"黄", "yellow"}, fill: color.RGBA{R: 255, G: 255, A: 255}},
		}
		// 随机排列：能背出「红绿蓝」的瞎猜答不出这一组顺序。
		rand.New(rand.NewSource(time.Now().UnixNano())).Shuffle(len(palette), func(i, j int) {
			palette[i], palette[j] = palette[j], palette[i]
		})
		labels := make([]string, 0, len(palette))
		fills := make([]color.RGBA, 0, len(palette))
		for _, entry := range palette {
			labels = append(labels, entry.label)
			fills = append(fills, entry.fill)
		}
		pngBytes, width, height := syntheticStripes(fills)
		t.Logf("合成图 %dx%d，自左向右期望顺序: %s", width, height, strings.Join(labels, "→"))

		question := "这是一张只有四个纯色色块的图。请从左到右依次读出每个色块的颜色，只回答四个颜色词，不要解释。"
		reply := askWithImage(t, specs, question, pngBytes, width, height)
		assertColorOrder(t, reply, palette)
	})

	t.Run("真机截图的前台窗口标题", func(t *testing.T) {
		capture, err := computer.CaptureShot(computer.ScreenshotOptions{MaxWidth: 1280})
		if err != nil {
			t.Skipf("本机截屏不可用: %v", err)
		}
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, capture.Image); err != nil {
			t.Fatalf("编码截图 PNG: %v", err)
		}
		window, windowErr := computer.ForegroundWindow()
		title := ""
		if windowErr == nil {
			title = strings.TrimSpace(window.Title)
		}
		if title == "" {
			t.Skipf("拿不到前台窗口标题，缺少地面真值: %v", windowErr)
		}
		width, height := capture.Image.Bounds().Dx(), capture.Image.Bounds().Dy()
		t.Logf("截图 %dx%d，%d 字节；地面真值前台窗口标题=%q", width, height, buffer.Len(), title)

		question := "这是一张屏幕截图。请只回答画面里最前面那个窗口标题栏上的文字，不要解释、不要调用工具。"
		reply := askWithImage(t, specs, question, buffer.Bytes(), width, height)
		if !mentionsTitle(reply, title) {
			t.Fatalf("回答未命中窗口标题 %q: %s", title, reply)
		}
	})
}

// askWithImage 用引擎侧消息模型（types.Message.Images）向真实 provider 提问，
// 逐个账号尝试并返回第一个非空回答；全部失败时把每个账号的原因一并报出。
func askWithImage(t *testing.T, specs []model.AccountSpec, question string, pngBytes []byte, width, height int) string {
	t.Helper()
	message := types.Message{Role: "user"}.
		WithText(question).
		WithImages(types.ImagePart{
			MimeType: "image/png",
			Data:     pngBytes,
			Width:    width,
			Height:   height,
			Name:     "smoke.png",
		})
	var failures []string
	for _, spec := range specs {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		reply, err := account.ClientFor(spec).Complete(ctx, []types.Message{message}, nil)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(%s/%s): %v", spec.Name, spec.Provider, spec.Model, err))
			continue
		}
		text := strings.TrimSpace(reply.Text())
		if text == "" {
			failures = append(failures, fmt.Sprintf("%s(%s/%s): 返回空内容", spec.Name, spec.Provider, spec.Model))
			continue
		}
		t.Logf("账号 %s(%s/%s) 回答: %s", spec.Name, spec.Provider, spec.Model, text)
		return text
	}
	t.Fatalf("所有账号都失败：\n%s", strings.Join(failures, "\n"))
	return ""
}

// syntheticStripes 生成一条等宽色带图，返回 PNG 字节与尺寸。
func syntheticStripes(fills []color.RGBA) ([]byte, int, int) {
	const (
		blockWidth  = 240
		blockHeight = 240
	)
	img := image.NewRGBA(image.Rect(0, 0, blockWidth*len(fills), blockHeight))
	for index, fill := range fills {
		for y := 0; y < blockHeight; y++ {
			for x := 0; x < blockWidth; x++ {
				img.Set(index*blockWidth+x, y, fill)
			}
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		panic(err)
	}
	return buffer.Bytes(), img.Bounds().Dx(), img.Bounds().Dy()
}

// assertColorOrder 断言回答里四个颜色词的出现顺序与出题顺序一致。
func assertColorOrder(t *testing.T, reply string, palette []struct {
	label string
	words []string
	fill  color.RGBA
}) {
	t.Helper()
	normalized := strings.ToLower(reply)
	positions := make([]int, 0, len(palette))
	labels := make([]string, 0, len(palette))
	for _, entry := range palette {
		position := -1
		for _, word := range entry.words {
			if index := strings.Index(normalized, word); index >= 0 && (position < 0 || index < position) {
				position = index
			}
		}
		if position < 0 {
			t.Fatalf("回答缺少颜色 %s：%s", entry.label, reply)
		}
		positions = append(positions, position)
		labels = append(labels, entry.label)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] >= positions[i] {
			t.Fatalf("颜色顺序不符（期望 %s）：%s", strings.Join(labels, "→"), reply)
		}
	}
}

// mentionsTitle 判断回答是否提到窗口标题：先整体匹配，再退化到最长前缀片段。
func mentionsTitle(reply, title string) bool {
	normalize := func(source string) string {
		var builder strings.Builder
		for _, r := range strings.ToLower(source) {
			switch r {
			case ' ', '\t', '\n', '\r', '"', '\'', '「', '」', '“', '”', '：', ':', '—', '-':
				continue
			}
			builder.WriteRune(r)
		}
		return builder.String()
	}
	normalizedReply := normalize(reply)
	runes := []rune(normalize(title))
	for size := len(runes); size >= 4; size-- {
		if strings.Contains(normalizedReply, string(runes[:size])) {
			return true
		}
	}
	return false
}
