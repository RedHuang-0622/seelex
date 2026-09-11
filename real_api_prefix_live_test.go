//go:build manualsmoke

package main

// TestManualSmokeRealAccountPrefixInvariant 是「跨轮前缀不变量 + 工具轮归属」的
// **真实 API 冒烟**：在真实 provider 前挂一台录制反向代理，逐条记录 provider 请求体
// （真实 wire 字节）与响应 usage（含 DeepSeek 的 prompt_cache_hit_tokens），再驱动
// 一个多轮、每轮都带工具调用（含工具轮说明正文 / 工具失败结果）的真实会话，最后：
//
//  1. 断言本次修复管辖的字节在跨轮投影里**逐字节不变**
//     —— assistant(tool_calls) 正文恒为空（§7.4 归零规则）、tool 结果正文不被改写
//     （§7.7 空结果不补占位；§8 发现 2：失败工具结果保存 wire 原文，应用呈现文本
//     只属于视图）；
//  2. 报告相邻请求的消息前缀保持情况（首处分歧的消息下标 / 角色 / 丢失字节）；
//  3. 报告真实 provider 的缓存命中 token（prompt_cache_hit_tokens / miss）。
//
// 硬断言：至少一条 wire 工具消息是失败形状（`{"error": ...}`）、wire 的 tool
// 消息里不出现应用呈现文本（`【模块：…】`）、修复管辖字节无跨轮改写。
//
// 运行：
//
//	$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
//	$env:SEELEX_PREFIX_SMOKE_REPORT = "tmp\prefix_smoke_report.txt"
//	go test -tags manualsmoke . -run 'TestManualSmokeRealAccountPrefixInvariant' `
//	  -count=1 -v -timeout 1200s
//
// 报告写到 SEELEX_PREFIX_SMOKE_REPORT（默认 tmp/prefix_smoke_report.txt，UTF-8），
// 便于在控制台编码不可靠的环境里读完整诊断。
//
// 代理的边界：只做「转发」+「在转发体里补 stream_options.include_usage」（让 provider
// 在流式最后一帧回报 usage，OpenAI 策略的 SSE 解析器本就支持该帧）；不读/不打印/不落盘
// 任何凭据，accounts.yaml 由调用方指向，测试仅把 base_url 改写到本地代理。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── 录制代理 ──────────────────────────────────────────────────

type prefixLiveUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheHitTokens   int `json:"prompt_cache_hit_tokens"`
	CacheMissTokens  int `json:"prompt_cache_miss_tokens"`
}

type prefixLiveRecord struct {
	seq    int
	path   string
	body   []byte // 应用真实发出的请求体（未注入 include_usage 的原始字节）
	status int
	usage  *prefixLiveUsage
}

type prefixLiveProxy struct {
	*httptest.Server
	upstream *url.URL
	client   *http.Client

	mu      sync.Mutex
	records []*prefixLiveRecord
}

func newPrefixLiveProxy(t *testing.T, upstreamBase string) *prefixLiveProxy {
	t.Helper()
	upstream, err := url.Parse(strings.TrimRight(upstreamBase, "/"))
	if err != nil {
		t.Fatalf("parse upstream base_url %q: %v", upstreamBase, err)
	}
	proxy := &prefixLiveProxy{
		upstream: upstream,
		client:   &http.Client{Timeout: 10 * time.Minute},
	}
	proxy.Server = httptest.NewServer(http.HandlerFunc(proxy.serve))
	t.Cleanup(proxy.Close)
	return proxy
}

func (proxy *prefixLiveProxy) serve(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}

	record := &prefixLiveRecord{seq: proxy.reserve(), path: request.URL.Path, body: body}
	defer func() {
		proxy.mu.Lock()
		proxy.records[record.seq-1] = record
		proxy.mu.Unlock()
	}()

	target := *proxy.upstream
	target.Path = strings.TrimRight(proxy.upstream.Path, "/") + request.URL.Path
	target.RawQuery = request.URL.RawQuery

	forward := prefixLiveWithIncludeUsage(body)
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), bytes.NewReader(forward))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	for key, values := range request.Header {
		for _, value := range values {
			upstreamRequest.Header.Add(key, value)
		}
	}
	upstreamRequest.ContentLength = int64(len(forward))

	response, err := proxy.client.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	record.status = response.StatusCode

	flusher, _ := writer.(http.Flusher)
	var captured bytes.Buffer
	buffer := make([]byte, 16*1024)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if captured.Len() < 4<<20 {
				captured.Write(buffer[:read])
			}
			if _, writeErr := writer.Write(buffer[:read]); writeErr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	record.usage = prefixLiveUsageFromEventStream(captured.Bytes())
}

func (proxy *prefixLiveProxy) reserve() int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.records = append(proxy.records, nil)
	return len(proxy.records)
}

func (proxy *prefixLiveProxy) snapshot() []*prefixLiveRecord {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	out := make([]*prefixLiveRecord, 0, len(proxy.records))
	for _, record := range proxy.records {
		if record != nil {
			out = append(out, record)
		}
	}
	return out
}

// prefixLiveWithIncludeUsage 在流式请求体里补 stream_options.include_usage，
// 使 provider 在最后一帧回报 usage（不改动其余字段；解析失败则原样转发）。
func prefixLiveWithIncludeUsage(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	stream, _ := payload["stream"].(bool)
	if !stream {
		return body
	}
	if _, exists := payload["stream_options"]; exists {
		return body
	}
	payload["stream_options"] = map[string]any{"include_usage": true}
	forward, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return forward
}

// prefixLiveUsageFromEventStream 从原始响应字节里取**最后一个**非空 usage
// （OpenAI 兼容 SSE 的 usage-only 帧，或非流式 JSON body）。
func prefixLiveUsageFromEventStream(raw []byte) *prefixLiveUsage {
	last := (*prefixLiveUsage)(nil)
	consider := func(payload string) {
		trimmed := strings.TrimSpace(payload)
		if trimmed == "" || trimmed == "[DONE]" {
			return
		}
		var frame struct {
			Usage *prefixLiveUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(trimmed), &frame); err != nil || frame.Usage == nil {
			return
		}
		last = frame.Usage
	}
	if !bytes.Contains(raw, []byte("data:")) {
		consider(string(raw))
		return last
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		consider(strings.TrimPrefix(line, "data:"))
	}
	return last
}

// ── wire 分析 ─────────────────────────────────────────────────

type prefixLiveMessage struct {
	role       string
	hasTools   bool
	toolCallID string
	content    string
	raw        string
}

// tag 生成紧凑的消息指纹：role(+tc 表示携带工具调用)(内容字节数)。
func (message prefixLiveMessage) tag() string {
	suffix := ""
	if message.hasTools {
		suffix = "+tc"
	}
	marker := ""
	if message.role == "assistant" && message.hasTools && strings.TrimSpace(message.content) != "" {
		marker = "!非空"
	}
	return fmt.Sprintf("%s%s(%d)%s", message.role, suffix, len(message.content), marker)
}

type prefixLivePair struct {
	index        int
	prev         []prefixLiveMessage
	cur          []prefixLiveMessage
	sharedPrefix int
	firstDiff    int
	governedBad  []string
}

func prefixLiveMessages(t *testing.T, body []byte) []prefixLiveMessage {
	t.Helper()
	var payload struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode provider request messages: %v", err)
	}
	out := make([]prefixLiveMessage, 0, len(payload.Messages))
	for _, raw := range payload.Messages {
		var shape struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCalls  []any           `json:"tool_calls"`
			ToolCallID string          `json:"tool_call_id"`
		}
		if err := json.Unmarshal(raw, &shape); err != nil {
			t.Fatalf("decode message: %v", err)
		}
		out = append(out, prefixLiveMessage{
			role:       shape.Role,
			hasTools:   len(shape.ToolCalls) > 0,
			toolCallID: shape.ToolCallID,
			content:    prefixLiveContentText(shape.Content),
			raw:        prefixLiveCompact(raw),
		})
	}
	return out
}

func prefixLiveContentText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return trimmed
}

func prefixLiveCompact(raw json.RawMessage) string {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return buffer.String()
}

// prefixLiveGovernedStability 报告本次修复管辖的两类消息的跨轮字节稳定性：
// assistant(tool_calls)（§7.4 归零）与 tool 结果（§7.7 空结果不补占位）。
func prefixLiveGovernedStability(prev, cur []prefixLiveMessage) []string {
	var problems []string
	limit := len(prev)
	if len(cur) < limit {
		limit = len(cur)
	}
	for index := 0; index < limit; index++ {
		before, after := prev[index], cur[index]
		governed := (before.role == "assistant" && before.hasTools) || before.role == "tool"
		if !governed || before.raw == after.raw {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"msg#%d(%s) 跨轮被改写：prev=%s → cur=%s",
			index, before.tag(), prefixLiveClip(before.raw, 200), prefixLiveClip(after.raw, 200)))
	}
	for index, message := range cur {
		if message.role == "assistant" && message.hasTools && strings.TrimSpace(message.content) != "" {
			problems = append(problems, fmt.Sprintf(
				"msg#%d(%s) 工具轮 assistant 投影正文非空：%s",
				index, message.tag(), prefixLiveClip(message.content, 200)))
		}
	}
	return problems
}

// prefixLiveToolWireFindings 报告 wire 上「工具失败结果」的条数与**应用呈现文本
// 泄漏**：呈现文本（`【模块：工具执行…】`）只属于视图；一旦出现在 tool 消息里，
// 说明记录侧改写了已发出的字节（下一轮重投影分叉的根因，研究文档 §8 发现 2）。
func prefixLiveToolWireFindings(t *testing.T, records []*prefixLiveRecord) (int, []string) {
	t.Helper()
	failed, leaks := 0, []string{}
	for _, record := range records {
		if !strings.Contains(record.path, "chat/completions") {
			continue
		}
		for index, message := range prefixLiveMessages(t, record.body) {
			if message.role != "tool" {
				continue
			}
			if strings.Contains(message.content, prefixLiveModulePrefixMark) {
				leaks = append(leaks, fmt.Sprintf("req#%d msg#%d: %s",
					record.seq, index, prefixLiveClip(message.content, 200)))
			}
			if strings.HasPrefix(strings.TrimSpace(message.content), `{"error":`) {
				failed++
			}
		}
	}
	return failed, leaks
}

func prefixLiveClip(text string, limit int) string {
	flat := strings.ReplaceAll(text, "\n", `\n`)
	if len(flat) <= limit {
		return flat
	}
	return flat[:limit] + "…"
}

func prefixLiveAnalyze(t *testing.T, records []*prefixLiveRecord) ([]prefixLivePair, []string) {
	t.Helper()
	chat := make([]*prefixLiveRecord, 0, len(records))
	for _, record := range records {
		if strings.Contains(record.path, "chat/completions") {
			chat = append(chat, record)
		}
	}
	if len(chat) < 2 {
		t.Fatalf("recorded %d chat completion requests, need >= 2 to test the cross-turn prefix invariant", len(chat))
	}
	pairs := make([]prefixLivePair, 0, len(chat)-1)
	var violations []string
	for index := 1; index < len(chat); index++ {
		prev := prefixLiveMessages(t, chat[index-1].body)
		cur := prefixLiveMessages(t, chat[index].body)
		pair := prefixLivePair{index: index, prev: prev, cur: cur, firstDiff: -1}
		for pair.sharedPrefix < len(prev) && pair.sharedPrefix < len(cur) &&
			prev[pair.sharedPrefix].raw == cur[pair.sharedPrefix].raw {
			pair.sharedPrefix++
		}
		if pair.sharedPrefix < len(prev) {
			pair.firstDiff = pair.sharedPrefix
		}
		pair.governedBad = prefixLiveGovernedStability(prev, cur)
		violations = append(violations, pair.governedBad...)
		pairs = append(pairs, pair)
	}
	return pairs, violations
}

// ── 报告 ──────────────────────────────────────────────────────

const (
	prefixLiveMissingContent   = "Seelex recovery note: the previous message had no text"
	prefixLiveToolCallContent  = "Seelex recovery note: the assistant issued the recorded tool call(s)"
	prefixLiveInterruptedMark  = "Seelex recovery note: interrupted tool call"
	prefixLiveModulePrefixMark = "【模块："
)

func prefixLiveReportPath() string {
	if value := strings.TrimSpace(os.Getenv("SEELEX_PREFIX_SMOKE_REPORT")); value != "" {
		return value
	}
	return filepath.Join("tmp", "prefix_smoke_report.txt")
}

func prefixLiveWriteReport(t *testing.T, sessionID string, records []*prefixLiveRecord, pairs []prefixLivePair, violations []string, failedTools int, presentedLeaks []string) string {
	t.Helper()
	var report strings.Builder
	fmt.Fprintf(&report, "# 真实 API 前缀冒烟报告\n")
	fmt.Fprintf(&report, "session=%s 录制请求=%d\n\n", sessionID, len(records))

	fmt.Fprintf(&report, "## 请求序列（role(+tc)(正文字节数)）\n")
	for _, record := range records {
		if !strings.Contains(record.path, "chat/completions") {
			continue
		}
		messages := prefixLiveMessages(t, record.body)
		tags := make([]string, 0, len(messages))
		for _, message := range messages {
			tags = append(tags, message.tag())
		}
		fmt.Fprintf(&report, "req#%d status=%d n=%d  %s\n", record.seq, record.status, len(messages), strings.Join(tags, " "))
	}

	fmt.Fprintf(&report, "\n## 相邻请求消息前缀\n")
	for _, pair := range pairs {
		if pair.firstDiff < 0 {
			fmt.Fprintf(&report, "req#%d→#%d: 全前缀保持（%d 条）\n", pair.index, pair.index+1, len(pair.prev))
			continue
		}
		prevMessage := pair.prev[pair.firstDiff]
		curMessage := pair.cur[pair.firstDiff]
		fmt.Fprintf(&report, "req#%d→#%d: 共享前缀 %d/%d 条，首处分歧 msg#%d  %s → %s（%+d 字节）\n",
			pair.index, pair.index+1, pair.sharedPrefix, len(pair.prev), pair.firstDiff,
			prevMessage.tag(), curMessage.tag(), len(curMessage.raw)-len(prevMessage.raw))
		fmt.Fprintf(&report, "    prev: %s\n", prefixLiveClip(prevMessage.raw, 700))
		fmt.Fprintf(&report, "    cur : %s\n", prefixLiveClip(curMessage.raw, 700))
	}

	fmt.Fprintf(&report, "\n## 真实 provider usage（前缀缓存观测）\n")
	for _, record := range records {
		if record.usage == nil {
			continue
		}
		usage := record.usage
		ratio := 0.0
		if usage.PromptTokens > 0 {
			ratio = float64(usage.CacheHitTokens) / float64(usage.PromptTokens) * 100
		}
		fmt.Fprintf(&report, "req#%d: prompt=%d hit=%d miss=%d 命中率=%.1f%% completion=%d\n",
			record.seq, usage.PromptTokens, usage.CacheHitTokens, usage.CacheMissTokens, ratio, usage.CompletionTokens)
	}

	fmt.Fprintf(&report, "\n## wire 上的协议占位/呈现文本出现次数\n")
	counts := map[string]int{prefixLiveMissingContent: 0, prefixLiveToolCallContent: 0,
		prefixLiveInterruptedMark: 0, prefixLiveModulePrefixMark: 0}
	for _, record := range records {
		if !strings.Contains(record.path, "chat/completions") {
			continue
		}
		for key := range counts {
			counts[key] += strings.Count(string(record.body), key)
		}
	}
	for _, key := range []string{prefixLiveMissingContent, prefixLiveToolCallContent, prefixLiveInterruptedMark, prefixLiveModulePrefixMark} {
		fmt.Fprintf(&report, "%q: %d\n", key, counts[key])
	}

	fmt.Fprintf(&report, "\n## 违规（修复管辖字节被改写 / 工具轮正文非空）\n")
	if len(violations) == 0 {
		fmt.Fprintf(&report, "无\n")
	}
	for _, violation := range violations {
		fmt.Fprintf(&report, "- %s\n", violation)
	}

	fmt.Fprintf(&report, "\n## 工具失败结果（记录侧必须保存 wire 原文）\n")
	fmt.Fprintf(&report, "wire 上的失败工具消息（`{\"error\": ...}` 形状）=%d\n", failedTools)
	if len(presentedLeaks) == 0 {
		fmt.Fprintf(&report, "应用呈现文本泄漏（`%s` 进入 tool 消息）=0\n", prefixLiveModulePrefixMark)
	}
	for _, leak := range presentedLeaks {
		fmt.Fprintf(&report, "- 呈现文本泄漏: %s\n", leak)
	}

	path := prefixLiveReportPath()
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create report dir: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(report.String()), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	return path
}

// ── 冒烟用例 ──────────────────────────────────────────────────

func TestManualSmokeRealAccountPrefixInvariant(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	raw, err := os.ReadFile(accountsSource)
	if err != nil {
		t.Fatalf("read accounts file: %v", err)
	}
	upstream := prefixLiveUpstreamBaseURL(t, raw)

	proxy := newPrefixLiveProxy(t, upstream)

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	rewritten := bytes.ReplaceAll(raw, []byte(upstream), []byte(proxy.URL))
	if bytes.Equal(rewritten, raw) {
		t.Fatalf("accounts file base_url %q not found for rewrite", upstream)
	}
	if err := os.WriteFile(accountsPath, rewritten, 0o600); err != nil {
		t.Fatalf("write rewritten accounts file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "prefix_probe.txt"), []byte("PREFIX-PROBE-OK\n"), 0o644); err != nil {
		t.Fatalf("write probe file: %v", err)
	}

	harness := newFullChainHarness(t, accountsPath, projectRoot, 60*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	submit := func(prompt string) {
		t.Helper()
		if err := harness.app.Submit(ctx, prompt); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("live turn did not become idle: %v", err)
		}
		if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
			t.Fatalf("live turn failed: %s", snapshot.Chat.Error)
		}
	}

	// 每轮都要求「先说一句 → 再调用工具」：这正是工具轮说明正文（wire 上被丢弃、
	// 只经 onChunk 进视图）与 assistant(tool_calls) 归零同时被触发的形状。
	submit("第一步：先用一句话说明你接下来要做什么，然后调用 read_file 读取 prefix_probe.txt，" +
		"最后只回复文件里的内容，不要解释。")
	for round := 2; round <= 3; round++ {
		submit(fmt.Sprintf("第 %d 轮：同样先写一句说明，再调用 read_file 读取 prefix_probe.txt，"+
			"然后只回复『第%d轮已核对』。", round, round))
	}
	// 强制工具**失败**形状（研究文档 §8 发现 2）：读一个不存在的文件，工具返回
	// 错误；wire 上是框架合成的 `{"error": %q}`，记录侧曾是应用分类呈现文本。
	submit("第 4 轮：调用 read_file 读取 missing_probe.txt，然后如实向用户报告这次调用的结果，不要重试。")

	sessionID := harness.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("live session did not receive a durable session ID")
	}

	records := proxy.snapshot()
	pairs, violations := prefixLiveAnalyze(t, records)
	failedTools, presentedLeaks := prefixLiveToolWireFindings(t, records)

	toolRounds := 0
	for _, record := range records {
		if !strings.Contains(record.path, "chat/completions") {
			continue
		}
		for _, message := range prefixLiveMessages(t, record.body) {
			if message.role == "assistant" && message.hasTools {
				toolRounds++
			}
		}
	}
	reportPath := prefixLiveWriteReport(t, sessionID, records, pairs, violations, failedTools, presentedLeaks)
	t.Logf("report=%s requests=%d tool_round_messages=%d failed_tool_messages=%d violations=%d presented_leaks=%d",
		reportPath, len(records), toolRounds, failedTools, len(violations), len(presentedLeaks))

	if toolRounds == 0 {
		t.Fatal("live session produced no tool-round assistant message: smoke did not exercise the fix's shape")
	}
	if failedTools == 0 {
		t.Fatal("live session produced no failing-tool wire message: 「工具失败结果」这一类分叉未被覆盖")
	}
	if len(presentedLeaks) > 0 {
		t.Errorf("应用呈现文本进入了 wire 的 tool 消息（记录侧改写已发出字节）: %v", presentedLeaks)
	}
	if len(violations) > 0 {
		t.Errorf("prefix invariant violations: %d (see %s)", len(violations), reportPath)
		t.Fatalf("修复管辖的字节在跨轮投影里被改写（%d 处）", len(violations))
	}
}

var prefixLiveBaseURLPattern = regexp.MustCompile(`(?m)^\s*base_url:\s*(\S+)\s*$`)

func prefixLiveUpstreamBaseURL(t *testing.T, accounts []byte) string {
	t.Helper()
	matches := prefixLiveBaseURLPattern.FindAllSubmatch(accounts, -1)
	if len(matches) == 0 {
		t.Fatal("accounts file declares no base_url")
	}
	value := strings.TrimSpace(string(matches[0][1]))
	if !strings.HasPrefix(value, "http") {
		t.Fatalf("accounts file base_url %q is not an http(s) endpoint", value)
	}
	return strings.TrimRight(value, "/")
}
