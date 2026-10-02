//go:build computerlive

// computer use 真实 API 冒烟（opt-in，走**完整应用链路**）：
//
//	app.Submit（真实 provider）→ 模型自己决定调用 computer_screenshot
//	  → 截图落会话媒体分区 → 随下一次请求送入模型 → 回答必须命中真实前台窗口标题
//
// 三条证据链各自独立：
//
//  1. 媒体分区出现 `screenshot-*.png`（只有 computer_screenshot 会写它）→ 工具
//     真的被模型调用了，而不是纸上谈兵；
//  2. **出站 provider 请求里带了图像内容**（本地记录代理转发出站请求，检查
//     请求体里的 `image_url` data URL）→ 画面真的作为图像送进了模型请求；
//  3. 回答命中地面真值（computer.ForegroundWindow 独立读出的前台窗口标题）→
//     模型确实给出了答案（**注意：这条本身不构成"看过图"的证据**，见下）。
//
// 为什么必须有证据 2：`computer_screenshot` 的工具结果文本里**已经**包含
// `foreground.title`，模型完全可以只读这段文本就"命中标题"而从未看过像素。
// 2026-09-16 对照实验（真实 provider、无任何图像内容、仅工具结果文本）：
// 纯文本请求同样返回 "Seelex"，与地面真值逐字相同。因此"回答命中"只能说明
// 链路通，不能说明画面到达；唯一硬证据是出站请求体里确实有图像。
//
// ⚠️ 本冒烟会把**当前屏幕画面**连同提问发给 provider（这正是它的目的），
// 也会把账号配置原样复制到临时目录（不读取、不打印其内容）。只在知情且接受
// 的前提下运行；它会截屏，但**不注入任何键鼠输入**。
//
// 运行：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags computerlive . -run TestComputerUseLiveSmoke -count=1 -v -timeout=15m
//
// 关闭 computer use（SEELEX_COMPUTER_USE=off）时本冒烟会失败——那正是它要
// 证明的能力，不要在这个用例上关掉。
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

func TestComputerUseLiveSmoke(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API computer use 冒烟")
	}
	if !computer.Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	// 地面真值：真实前台窗口标题。注意它是"回合产出"的对照物，不是"看过图"的证据
	// ——该标题同时出现在工具结果文本里（见文件头），区分能力靠证据二的出站请求。
	foreground, err := computer.ForegroundWindow()
	if err != nil {
		t.Skipf("本机没有可观测的前台窗口: %v", err)
	}
	marker := longestTitleRun(foreground.Title)
	if marker == "" {
		t.Skipf("前台窗口标题没有可用作地面真值的片段: %q", foreground.Title)
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyAccountsOpaque(t, accountsSource, accountsPath)
	// 出站证据面：把账号副本里的 base_url 改指向本地记录代理，代理再原样转发到
	// 真实端点。「画面有没有作为图像进请求」由请求体本身判定，不依赖模型行为；
	// 读到的 base_url 只作为转发目标，密钥等字段不解析、不打印。
	originalBase := readAccountsBaseURL(t, accountsPath)
	recorder, providerProxy := newProviderRecordingProxy(t, originalBase)
	defer providerProxy.Close()
	rewriteAccountsBaseURL(t, accountsPath, originalBase, providerProxy.URL)
	harness := newFullChainHarness(t, accountsPath, projectRoot, 60*time.Second)
	defer harness.app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	prompt := "请调用 computer_screenshot 工具截取当前屏幕，然后只回答前台窗口标题的原文" +
		"（照抄标题里出现的内容，不要解释、不要猜测、不要加引号）。"
	if err := harness.app.Submit(ctx, prompt); err != nil {
		t.Fatalf("提交真实回合: %v", err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("真实回合未回到 idle: %v", err)
	}
	snapshot := harness.app.Snapshot()
	if snapshot.Chat.Error != "" {
		t.Fatalf("真实回合失败: %s", snapshot.Chat.Error)
	}
	sessionID := snapshot.Session.ID
	storeRoot := filepath.Join(projectRoot, "sessions")

	// 证据一：工具结果里出现截图媒体引用（工具确实被调用），且该引用在磁盘上
	// 真实存在（落进会话媒体分区）。引用来自会话记录，因此不依赖存储布局假设。
	refs := screenshotMediaRefs(snapshot.Conversation)
	if len(refs) == 0 {
		t.Fatalf("会话媒体分区没有截图（模型没有真正调用 computer_screenshot）；session=%s store=%s\nvisible_tools=%v\n对话末尾：%s",
			sessionID, storeRoot, visibleToolNames(snapshot.Runtime.VisibleTools), describeTail(snapshot.Conversation, 4))
	}
	for _, ref := range refs {
		// 存储后端目录名带后端后缀（如 sessions-json），因此从工程根按内容 hash
		// 递归定位，不假设后端目录名。
		if !mediaRefOnDisk(t, projectRoot, ref) {
			t.Fatalf("截图媒体引用 %s 在磁盘上找不到（root=%s）\n存储树：%s", ref, projectRoot, describeStoreTree(t, projectRoot))
		}
	}
	// 证据二：出站 provider 请求里确实带了图像内容（画面真的送达模型）。
	// 这是本冒烟唯一的"看过图"硬证据：模型回答只证明链路通，不证明画面到达。
	withImage, withoutImage, imageBytes := recorder.summary()
	if withImage+withoutImage == 0 {
		t.Fatalf("本地记录代理没有看到任何 provider 请求：base_url 改写或转发链路失效")
	}
	if withImage == 0 {
		t.Fatalf("没有任何出站请求携带图像内容（requests=%d）：画面没有送进模型请求", withImage+withoutImage)
	}
	if withoutImage == 0 {
		t.Fatalf("所有 %d 次请求都带图像，记录器没有区分能力（判定疑似恒真）", withImage)
	}
	if imageBytes < 8<<10 {
		t.Fatalf("请求里的图像只有 %d 字节，不像是真实桌面截图", imageBytes)
	}
	t.Logf("出站 provider 请求：带图 %d 次 / 不带图 %d 次，最大随图 %d 字节（真实转发到 %s）",
		withImage, withoutImage, imageBytes, originalBase)

	// 证据三：回答命中地面真值（模型确实给了答案）。
	// 注意：这条**不能**单独证明"看过图"——工具结果文本里已含 foreground.title，
	// 只读文本也能命中（见文件头对照实验）；它只用于证明回合产出正常、摘要是活字。
	answer := latestVisibleAssistant(snapshot.Conversation)
	if !strings.Contains(normalizeTitleText(answer), normalizeTitleText(marker)) {
		t.Fatalf("回答未命中前台窗口标题：answer=%q ground_truth=%q（截图引用 %v）",
			answer, foreground.Title, refs)
	}
	t.Logf("真实 API + computer use 冒烟通过：session=%s 截图引用=%v 命中标题片段=%q（前台窗口 %q）",
		sessionID, refs, marker, foreground.Title)
}

// describeStoreTree 列出临时目录下与媒体相关的路径（诊断：定位媒体实际落点）。
func describeStoreTree(t *testing.T, root string) string {
	t.Helper()
	var builder strings.Builder
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if depth := strings.Count(relative, string(os.PathSeparator)); depth > 5 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if entry.IsDir() && name != "meta" && name != "sessions" && name != "runtime" &&
			!strings.HasPrefix(name, "project-") && !strings.HasPrefix(name, "session-") {
			return nil
		}
		if entry.IsDir() && name != "meta" && !strings.HasPrefix(name, "project-") && !strings.HasPrefix(name, "session-") &&
			name != "sessions" && name != "runtime" {
			return nil
		}
		if entry.IsDir() || strings.Contains(name, "screenshot") || name == "meta.json" {
			builder.WriteString("\n  " + relative)
		}
		return nil
	})
	if builder.Len() == 0 {
		return "(没有任何项目/会话/meta 目录)"
	}
	return builder.String()
}

// visibleToolNames 列出本回合模型可见的工具名（诊断用：先排除"工具没进可见面"）。
func visibleToolNames(tools []model.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// describeTail 摘出末尾几条可见消息（角色 + 首行截断），失败诊断用，不含工具原始结果。
func describeTail(conversation []model.Message, limit int) string {
	if limit <= 0 || len(conversation) == 0 {
		return "(空)"
	}
	start := len(conversation) - limit
	if start < 0 {
		start = 0
	}
	var builder strings.Builder
	for _, message := range conversation[start:] {
		content := strings.Join(strings.Fields(message.Content), " ")
		if runes := []rune(content); len(runes) > 120 {
			content = string(runes[:120]) + "…"
		}
		builder.WriteString("\n  [" + message.Role + "/" + message.Kind + "] " + content)
	}
	return builder.String()
}

// copyAccountsOpaque 原样复制账号配置（不解析、不打印内容；与其它冒烟同一口径）。
func copyAccountsOpaque(t *testing.T, source, destination string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("打开账号配置: %v", err)
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatalf("创建临时账号配置: %v", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("复制账号配置: %v", err)
	}
}

// screenshotMediaRefs 从会话记录里收集 computer_screenshot 工具结果中的
// `media:<sha256>` 引用（工具结果 JSON 里的 ref 字段）。
func screenshotMediaRefs(conversation []model.Message) []string {
	seen := map[string]bool{}
	refs := []string{}
	for _, message := range conversation {
		content := message.Content
		for index := 0; ; {
			at := strings.Index(content[index:], "\"ref\":\"media:")
			if at < 0 {
				break
			}
			at += index + len("\"ref\":\"media:")
			end := strings.IndexByte(content[at:], '"')
			if end < 0 {
				break
			}
			hash := content[at : at+end]
			if len(hash) == 64 && !seen[hash] {
				if _, err := hex.DecodeString(hash); err == nil {
					seen[hash] = true
					refs = append(refs, "media:"+hash)
				}
			}
			index = at + end
		}
	}
	return refs
}

// mediaRefOnDisk 校验媒体引用在会话存储里真实落盘：按内容 hash 目录定位
// （目录名即 sha256），要求其中至少有一个非空文件。
func mediaRefOnDisk(t *testing.T, storeRoot, ref string) bool {
	t.Helper()
	hash := strings.TrimPrefix(ref, "media:")
	found := false
	_ = filepath.WalkDir(storeRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !entry.IsDir() || entry.Name() != hash {
			return nil
		}
		children, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil
		}
		for _, child := range children {
			if info, statErr := child.Info(); statErr == nil && !info.IsDir() && info.Size() > 0 {
				found = true
				break
			}
		}
		return nil
	})
	return found
}

// latestVisibleAssistant 取最后一条非空 assistant 正文。
func latestVisibleAssistant(conversation []model.Message) string {
	for index := len(conversation) - 1; index >= 0; index-- {
		message := conversation[index]
		if strings.EqualFold(message.Role, "assistant") && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}

// normalizeTitleText 压缩空白与标点（标题里的连接符/空格在不同渲染下可能不同）。
func normalizeTitleText(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

// longestTitleRun 取标题里最长的连续非空白片段（≥6 字符）作为地面真值标记，
// 避免整串标题因标点/空格差异造成假阴性。
func longestTitleRun(title string) string {
	best := ""
	for _, run := range strings.FieldsFunc(title, func(r rune) bool { return unicode.IsSpace(r) }) {
		if len([]rune(run)) > len([]rune(best)) {
			best = run
		}
	}
	if len([]rune(best)) < 6 {
		return ""
	}
	return best
}

// ── 出站证据：provider 请求记录代理 ──────────────────────────────
//
// 只做一件事：把 Seelex 发往 provider 的请求原样转发到真实端点，并在转发前
// 检查请求体里是否带 `data:image/...;base64,...`（即随图）。判定完全落在
// 出站流量上，不依赖模型行为、不解析密钥、不打印任何请求内容。

// providerRecording 累计出站请求的判别结果。
type providerRecording struct {
	mu        sync.Mutex
	withImage int
	plain     int
	maxBytes  int
}

func (r *providerRecording) summary() (withImage, plain, maxImageBytes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.withImage, r.plain, r.maxBytes
}

// imageDataURLPattern 命中请求体里的随图 data URL（图片本体是 base64 段）。
var imageDataURLPattern = regexp.MustCompile(`data:(image/[A-Za-z0-9.+-]+);base64,([A-Za-z0-9+/=]+)`)

func (r *providerRecording) record(body []byte) {
	matches := imageDataURLPattern.FindAllSubmatch(body, -1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(matches) == 0 {
		r.plain++
		return
	}
	r.withImage++
	for _, match := range matches {
		if size := len(match[2]) / 4 * 3; size > r.maxBytes {
			r.maxBytes = size
		}
	}
}

// newProviderRecordingProxy 起本地反向代理：转发到 targetBase 并记录随图情况。
func newProviderRecordingProxy(t *testing.T, targetBase string) (*providerRecording, *httptest.Server) {
	t.Helper()
	target, err := url.Parse(strings.TrimSpace(targetBase))
	if err != nil || target.Host == "" {
		t.Fatalf("账号配置里的 base_url 无法解析为转发目标: %q", targetBase)
	}
	recording := &providerRecording{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		if readErr == nil {
			recording.record(body)
		}
		director(request)
		request.Host = target.Host // 避免把 127.0.0.1 的 Host 带给真实端点
	}
	return recording, httptest.NewServer(proxy)
}

// readAccountsBaseURL 取账号配置里第一个 base_url（仅作转发目标，不触碰其它字段）。
func readAccountsBaseURL(t *testing.T, path string) string {
	t.Helper()
	for _, line := range strings.Split(readAccountsFileText(t, path), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "base_url:")
		if !ok {
			continue
		}
		if value := strings.TrimSpace(rest); value != "" {
			return value
		}
	}
	t.Fatalf("账号配置里没有 base_url：无法把出站请求改指向记录代理")
	return ""
}

// rewriteAccountsBaseURL 只把账号副本里的 base_url 字段改写为记录代理地址。
func rewriteAccountsBaseURL(t *testing.T, path, original, replacement string) {
	t.Helper()
	updated := strings.ReplaceAll(readAccountsFileText(t, path), original, replacement)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("写入账号副本: %v", err)
	}
}

func readAccountsFileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	return string(data)
}
