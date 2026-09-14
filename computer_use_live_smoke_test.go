//go:build computerlive

// computer use 真实 API 冒烟（opt-in，走**完整应用链路**）：
//
//	app.Submit（真实 provider）→ 模型自己决定调用 computer_screenshot
//	  → 截图落会话媒体分区 → 随下一次请求送入模型 → 回答必须命中真实前台窗口标题
//
// 两条证据链各自独立：
//
//  1. 媒体分区出现 `screenshot-*.png`（只有 computer_screenshot 会写它）→ 工具
//     真的被模型调用了，而不是纸上谈兵；
//  2. 回答命中地面真值（computer.ForegroundWindow 独立读出的前台窗口标题）→
//     画面确实进了模型请求，模型是「看图回答」而不是猜。
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
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// 地面真值：真实前台窗口标题（模型只能从画面里读出来）。
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
	// 证据二：回答命中地面真值（画面确实送达模型）。
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

// normalizeTitleText 折叠空白与标点（标题里的连接符/空格在不同渲染下可能不同）。
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
