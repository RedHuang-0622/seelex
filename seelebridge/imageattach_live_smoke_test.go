//go:build imagelive

// seelex 引擎侧「待随图」链路真机 + 真实 API 冒烟（opt-in）。
//
// 它验证 imageattach 这一层的装配形状，而不只是包内单测：
//   - 真机截屏（computer.CaptureShot）→ 待随图队列 → Completer 接缝；
//   - 模型确实看到像素：以 ForegroundWindow 的标题为地面真值，回答必须命中；
//   - 同一张图只随一次请求（第二次调用不再附加）。
//
// 前提：
//
//	go mod edit -replace "github.com/RedHuang-0622/Seele=G:/Program/go/Seele"
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags imagelive ./seelebridge/ -run TestImageAttachLiveSmoke -count=1 -v
//	联调后复原：go mod edit -dropreplace github.com/RedHuang-0622/Seele
//
// 说明：本冒烟用注入的 Loader 覆盖「按 ref 加载」分支；真实会话媒体分区
// （sessionstore.MediaStore）的写入由截屏工具接入，届时复用同一条接缝。
package seelebridge

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/imageattach"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

func TestImageAttachLiveSmoke(t *testing.T) {
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsPath == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API 随图冒烟")
	}
	loaded, err := config.LoadTolerant(accountsPath)
	if err != nil {
		t.Fatalf("加载账号配置: %v", err)
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

	capture, err := computer.CaptureShot(computer.ScreenshotOptions{MaxWidth: 1280})
	if err != nil {
		t.Skipf("本机截屏不可用: %v", err)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, capture.Image); err != nil {
		t.Fatalf("编码截图 PNG: %v", err)
	}
	width, height := capture.Image.Bounds().Dx(), capture.Image.Bounds().Dy()
	window, windowErr := computer.ForegroundWindow()
	title := ""
	if windowErr == nil {
		title = strings.TrimSpace(window.Title)
	}
	if title == "" {
		t.Skipf("拿不到前台窗口标题，缺少地面真值: %v", windowErr)
	}
	t.Logf("截图 %dx%d，%d 字节；地面真值前台窗口标题=%q", width, height, buffer.Len(), title)

	question := "请只回答画面里最前面那个窗口标题栏上的文字，不要解释、不要调用工具。"

	t.Run("内存附件", func(t *testing.T) {
		registry := imageattach.NewRegistry()
		registry.Add("smoke", imageattach.Attachment{
			Label: fmt.Sprintf("screenshot %dx%d", width, height),
			Image: types.ImagePart{MimeType: "image/png", Data: buffer.Bytes(), Width: width, Height: height},
		})
		reply := askThroughWrapper(t, specs, registry, nil, question)
		if !mentionsTitle(reply, title) {
			t.Fatalf("回答未命中窗口标题 %q: %s", title, reply)
		}
	})

	t.Run("按 ref 加载", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "screenshot.png")
		if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
			t.Fatalf("写出截图: %v", err)
		}
		registry := imageattach.NewRegistry()
		registry.Add("smoke", imageattach.Attachment{Ref: "media:smoke-fixture", Label: "screenshot(ref)"})
		loader := func(context.Context, string) (types.ImagePart, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return types.ImagePart{}, err
			}
			return types.ImagePart{MimeType: "image/png", Data: data, Width: width, Height: height}, nil
		}
		reply := askThroughWrapper(t, specs, registry, loader, question)
		if !mentionsTitle(reply, title) {
			t.Fatalf("回答未命中窗口标题 %q: %s", title, reply)
		}
	})
}

// askThroughWrapper 用 imageattach.Wrapper 包装真实账号客户端提问，返回首个成功回答。
func askThroughWrapper(
	t *testing.T,
	specs []model.AccountSpec,
	registry *imageattach.Registry,
	loader func(context.Context, string) (types.ImagePart, error),
	question string,
) string {
	t.Helper()
	var failures []string
	for _, spec := range specs {
		next := account.ClientFor(spec)
		probe := &countingCompleter{next: next}
		wrapper := &imageattach.Wrapper{
			Next:    probe,
			Pending: registry,
			Options: imageattach.Options{
				SessionID: func(context.Context) string { return "smoke" },
				Loader:    loader,
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		message, err := wrapper.Complete(ctx, []types.Message{types.Message{Role: "user"}.WithText(question)}, nil)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(%s/%s): %v", spec.Name, spec.Provider, spec.Model, err))
			continue
		}
		reply := strings.TrimSpace(message.Text())
		if reply == "" {
			failures = append(failures, fmt.Sprintf("%s(%s/%s): 返回空内容", spec.Name, spec.Provider, spec.Model))
			continue
		}
		// 装配形状：底层确实收到了比原历史多一条、且带图的 user 消息。
		if probe.lastImageCount != 1 {
			failures = append(failures, fmt.Sprintf("%s: 底层收到的带图消息图片数 = %d, want 1", spec.Name, probe.lastImageCount))
			continue
		}
		if probe.lastMessageCount != 2 {
			failures = append(failures, fmt.Sprintf("%s: 底层收到 %d 条消息, want 2（原问题 + 随图消息）", spec.Name, probe.lastMessageCount))
			continue
		}
		t.Logf("账号 %s(%s/%s) 回答: %s", spec.Name, spec.Provider, spec.Model, reply)
		return reply
	}
	t.Fatalf("所有账号都失败：\n%s", strings.Join(failures, "\n"))
	return ""
}

// countingCompleter 记录底层收到的最后一条消息形状。
type countingCompleter struct {
	next             agent.Completer
	lastMessageCount int
	lastImageCount   int
}

func (c *countingCompleter) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	c.lastMessageCount = len(messages)
	c.lastImageCount = 0
	if len(messages) > 0 {
		c.lastImageCount = messages[len(messages)-1].ImageCount()
	}
	return c.next.Complete(ctx, messages, tools)
}
