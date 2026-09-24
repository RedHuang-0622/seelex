//go:build asynclive

// 异步工具「三行账本」的 wire 层真实 API 冒烟（opt-in）。
//
// 要证伪/证实的命题：一条 deferred 工具调用在 OpenAI 格式下必须落成三行
// （req=assistant.tool_calls / ack=tool 回执 / completion=迟到补记），
// 且补记**只能追加在尾部**——任何把 completion 塞回历史中段的写法，
// 要么被 provider 直接拒绝（契约），要么让前缀缓存命中归零（成本）。
//
// 七个探针：
//
//	P1 warm     : system+filler → user → req → ack（写入服务端缓存）
//	P2 A 链路   : P1 全量 + 尾部 user 补记（<seelex-async-result call_id=…>）
//	              期望 200 且命中 ≈ P1 前缀长度
//	P3 中段改写 : 与 P2 同形，但把 filler 中间一行改掉一个字节
//	              期望 200 且命中塌陷 → 这就是 I-16「定稿不可改写」的实测依据
//	P4 B1 契约  : req 在 ack 之前被回答，tool 消息落在若干轮之后（回填语义）
//	              期望 400 → 证明「一次调用一行账」在 wire 层不合法
//	P5 B2 重复  : 同一 call_id 给两条 tool 回执
//	              期望 400 或被忽略 → 决定幂等键要不要落在 message_id
//	P6 Responses: 若 provider 支持 /v1/responses，测迟到 function_call_output
//	              能否被服务端状态接受（有状态链是另一条出路）
//	P7 轮询形态 : ack 之后再追加两次 async_output 取回，每次都是一对相邻的
//	              tool_call/tool_result，历史只在尾部生长。
//	              期望 200 且命中不塌 → 轮询型不破前缀缓存（当前选型的实测依据）
//	              实测 2026-09-24（deepseek-flash）：三次全 200；cached 随前缀
//	              单调增长 3328 → 3456 → 3584 → 3584，ratio 稳定 0.94；
//	              单次轮询的 prompt 成本 +129 / +128（miss 恒 212 = 未缓存的尾部）。
//	              同期对照未变：P3 中段改写塌到 0.25，P4/P5 回填与重复回执仍 400。
//
// 账号只作为不透明文件在本进程内解析，任何输出都不含 key。
//
// 运行：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags asynclive . -run TestAsyncWire -count=1 -v -timeout=10m
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------- 账号加载（只取三个字段，永不打印 key） ----------

type probeAccountEntry struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
}

type probeConfig struct {
	Defaults struct {
		Provider string `yaml:"provider"`
	} `yaml:"defaults"`
	Roles struct {
		Agent    []probeAccountEntry `yaml:"agent"`
		SubAgent []probeAccountEntry `yaml:"subagent"`
	} `yaml:"roles"`
}

// loadProbeAccount 读账号副本，优先 agent[0]，回退 subagent[0]。
func loadProbeAccount(t *testing.T) probeAccountEntry {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if path == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能跑 wire 冒烟")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取账号副本失败: %v", err)
	}
	var cfg probeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析账号失败: %v", err)
	}
	entries := append([]probeAccountEntry{}, cfg.Roles.Agent...)
	entries = append(entries, cfg.Roles.SubAgent...)
	for _, entry := range entries {
		if strings.TrimSpace(entry.BaseURL) != "" && strings.TrimSpace(entry.APIKey) != "" && strings.TrimSpace(entry.Model) != "" {
			if strings.TrimSpace(entry.Provider) == "" {
				entry.Provider = cfg.Defaults.Provider
			}
			t.Logf("使用账号：provider=%s model=%s host=%s", entry.Provider, entry.Model, hostOf(entry.BaseURL))
			return entry
		}
	}
	t.Skip("账号里没有可用的 agent/subagent 条目（需要 base_url + api_key + model）")
	return probeAccountEntry{}
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "(无法解析)"
	}
	return parsed.Host
}

// ---------- wire 结构（OpenAI Chat Completions） ----------

type wireCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []wireCall `json:"tool_calls,omitempty"`
}

func user(content string) wireMessage { return wireMessage{Role: "user", Content: content} }
func tool(callID, content string) wireMessage {
	return wireMessage{Role: "tool", Content: content, ToolCallID: callID}
}

// reasoningProbe 是确定性思考文本。thinking 模式下 provider 要求历史里的
// assistant 轮把 reasoning_content 原样回传，否则整条请求 400。
const reasoningProbe = "用户要一条长命令，先登记调用并给出受理回执，不等待执行完成。"

func assist(content string) wireMessage {
	return wireMessage{Role: "assistant", Content: content, ReasoningContent: reasoningProbe}
}

func requestWithToolCall(id, name, args string) wireMessage {
	message := wireMessage{Role: "assistant", Content: "", ReasoningContent: reasoningProbe}
	call := wireCall{ID: id, Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = args
	message.ToolCalls = []wireCall{call}
	return message
}

// ---------- 稳定前缀 ----------

// probeNonce 是一次运行专属的盐：provider 侧缓存 TTL 长于本进程，若不隔离，
// 「中段改写」在第二次运行时命中的是上一次自己写进去的缓存（实测翻车过一次），
// 测出来的是缓存复用而不是塌陷。放在前缀最开头 → 同一运行内所有变体仍共享前缀。
var probeNonce = func() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "nonce-unavailable"
	}
	return hex.EncodeToString(buf)
}()

// stableFiller 是确定性长填充：provider 侧前缀缓存通常要求共享前缀 ≥1024 token，
// 中文按 ≈1 token/字 估，80 段约 4.5k token。第 25 段是「中段改写」的靶子。
func stableFiller(tampered bool) string {
	var builder strings.Builder
	builder.WriteString("探测运行 nonce=" + probeNonce + "\n")
	builder.WriteString("你是一个只做算术的助手。以下是稳定参考段落，用于构造可缓存前缀：\n")
	for index := 1; index <= 80; index++ {
		line := fmt.Sprintf("段落 %03d：异步工具协议规定一次调用留下受理回执与完成补记两类事件，"+
			"两者都必须以追加方式写入，历史中段的字节永不受写，因此缓存前缀单调增长。\n", index)
		if tampered && index == 25 {
			line = strings.Replace(line, "永不受写", "允许回写", 1)
		}
		builder.WriteString(line)
	}
	return builder.String()
}

const (
	probeCallID = "call_probe_1"
	probeAck    = "已受理，异步执行中。call_id=" + probeCallID + " 输出落点=async://probe/out.log，结果稍后作为独立事件补记。"
)

// 轮询形态的两次取回。载荷按 seelebridge/tools/async_exec.go renderPolled 的实际
// 字段形状手写：确定性、不含时间戳与耗时——这些字节会永久留在可缓存前缀里。
const (
	pollCallID1  = "call_poll_1"
	pollCallID2  = "call_poll_2"
	pollArgs     = `{"handle":"a1","wait_ms":5000}`
	pollProgress = `{"status":"progress","handle":"a1","state":"running","exit_code":-1,` +
		`"log_path":"/tmp/seelex-async-a1.log","output":"building 1/2\n",` +
		`"hint":"仍在运行。需要结果就再调一次 async_output(handle)。"}`
	pollFinished = `{"status":"finished","handle":"a1","state":"done","exit_code":0,` +
		`"log_path":"/tmp/seelex-async-a1.log","output":"building 2/2\ndone\n",` +
		`"hint":"命令已结束；output 为其输出的未交付部分。"}`
)

// pollRound 是一对完整的取回：assistant 发起 async_output 调用 + 紧邻其后的 tool 回执。
func pollRound(callID, args, result string) []wireMessage {
	return []wireMessage{requestWithToolCall(callID, "async_output", args), tool(callID, result)}
}

// asyncCompletionMessage 是 A 链路的补记：role=user、自带 call_id 信封、正文只给摘要+句柄。
func asyncCompletionMessage() string {
	return "<seelex-async-result call_id=\"" + probeCallID + "\" origin_round=\"1\" state=\"completed\">\n" +
		"后台命令已结束，退出码 0。正文 2048 字节未入本消息，可用 read_tool_result(ref=\"async://probe/out.log\") 分页读回。\n" +
		"</seelex-async-result>\n请只回答：异步结果是否已经拿到？回答「是」或「否」。"
}

// ---------- HTTP ----------

type probeResult struct {
	label      string
	status     int
	usage      map[string]any
	errSnippet string
	sha        string
	cachedTok  int
	promptTok  int
}

// postJSON 发一次非流式请求，返回状态、usage 与错误片段（脱敏）。
func postJSON(ctx context.Context, t *testing.T, account probeAccountEntry, endpoint string, body map[string]any, label string) probeResult {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("%s 序列化失败: %v", label, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("%s 构造请求失败: %v", label, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+account.APIKey)

	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s 请求失败: %v", label, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))

	result := probeResult{label: label, status: response.StatusCode, sha: fmt.Sprintf("%x", sha256.Sum256(payload))[:12]}
	sum := map[string]any{}
	if json.Unmarshal(raw, &sum) == nil {
		if usage, ok := sum["usage"].(map[string]any); ok {
			result.usage = usage
			result.cachedTok, result.promptTok = readCacheCounters(usage)
		}
	}
	if response.StatusCode >= 400 {
		text := redact(string(raw), account.APIKey)
		if len(text) > 320 {
			text = text[:320] + "…"
		}
		result.errSnippet = text
	}
	t.Logf("[%s] http=%d 耗时=%s prompt_tokens=%d cached_tokens=%d sha=%s%s",
		label, response.StatusCode, time.Since(started).Round(time.Millisecond),
		result.promptTok, result.cachedTok, result.sha, usageDump(result.usage))
	if result.errSnippet != "" {
		t.Logf("[%s] 错误体=%s", label, result.errSnippet)
	}
	return result
}

func usageDump(usage map[string]any) string {
	if usage == nil {
		return " usage=缺失"
	}
	details, _ := json.Marshal(usage)
	return " usage=" + string(details)
}

// readCacheCounters 兼容各家 OpenAI 兼容层的缓存字段命名。
// 同一家可能同时给两个别名且数值相同（DeepSeek），因此只取第一个命中的来源，
// 相加会把命中数翻倍。
func readCacheCounters(usage map[string]any) (cached int, prompt int) {
	prompt = intOf(usage["prompt_tokens"]) + intOf(usage["input_tokens"])
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		if value := intOf(details["cached_tokens"]); value > 0 {
			return value, prompt
		}
	}
	for _, key := range []string{"prompt_cache_hit_tokens", "cache_read_input_tokens", "cached_prompt_tokens"} {
		if value := intOf(usage[key]); value > 0 {
			return value, prompt
		}
	}
	return 0, prompt
}

func intOf(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func redact(text, secret string) string {
	if strings.TrimSpace(secret) == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "<redacted>")
}

// ---------- 探针 ----------

func TestAsyncWireLiveProbe(t *testing.T) {
	account := loadProbeAccount(t)
	chatEndpoint := completionsEndpoint(account.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	body := func(messages []wireMessage) map[string]any {
		return map[string]any{
			"model":       account.Model,
			"messages":    messages,
			"max_tokens":  16,
			"temperature": 0,
			"stream":      false,
		}
	}

	base := func(tampered bool) []wireMessage {
		return []wireMessage{
			{Role: "system", Content: stableFiller(tampered)},
			user("跑一条长命令，先别等它。"),
			requestWithToolCall(probeCallID, "bash", `{"command":"sleep 3; echo done","background":true}`),
			tool(probeCallID, probeAck),
		}
	}

	// P1：写入服务端缓存（同一份请求发两次，第二次才是可信的命中读数）。
	warm := body(base(false))
	postJSON(ctx, t, account, chatEndpoint, warm, "P1-warm-a")
	p1 := postJSON(ctx, t, account, chatEndpoint, warm, "P1-warm-b")

	// P2：A 链路——补记追加在尾部，历史中段字节不变。
	messagesA := append(append([]wireMessage{}, base(false)...),
		assist("命令在后台跑，我先记着。"),
		user(asyncCompletionMessage()),
	)
	p2 := postJSON(ctx, t, account, chatEndpoint, body(messagesA), "P2-A链路尾部补记")

	// P3：中段改写——只改 filler 第 25 行的两个字。
	messagesMid := append([]wireMessage{}, messagesA...)
	messagesMid[0] = wireMessage{Role: "system", Content: stableFiller(true)}
	p3 := postJSON(ctx, t, account, chatEndpoint, body(messagesMid), "P3-中段改写")

	// P4：B1 契约——req 未即时回答，tool 消息落在两轮之后（回填语义）。
	messagesB1 := []wireMessage{
		{Role: "system", Content: stableFiller(false)},
		user("跑一条长命令，先别等它。"),
		requestWithToolCall(probeCallID, "bash", `{"command":"sleep 3; echo done","background":true}`),
		user("另外插一句：1+1 等于几？"),
		assist("2"),
		tool(probeCallID, "迟到结果：exit=0，stdout=done"),
		user("请只回答：拿到结果了吗？回答「是」或「否」。"),
	}
	p4 := postJSON(ctx, t, account, chatEndpoint, body(messagesB1), "P4-B1迟到tool回填")

	// P5：B2 重复——同一 call_id 两条 tool 回执。
	messagesB2 := append(append([]wireMessage{}, base(false)...),
		tool(probeCallID, "重复回执：同一 call_id 的第二次回答"),
		user("请只回答：1+1 等于几？"),
	)
	p5 := postJSON(ctx, t, account, chatEndpoint, body(messagesB2), "P5-B2重复回执")

	// P7：轮询形态。前四行与 P1 字节完全相同（同一段 filler、同一条派发、同一份
	// 受理回执），此后每次取回都是一对相邻的 tool_call/tool_result，历史只在尾部
	// 生长。要测的是：与 P2 在 ack 之后分叉的前缀还能不能命中，以及逐轮追加时
	// 命中量会不会倒退。
	poll1 := append(append([]wireMessage{}, base(false)...), pollRound(pollCallID1, pollArgs, pollProgress)...)
	p7a := postJSON(ctx, t, account, chatEndpoint, body(poll1), "P7-取回1")
	poll2 := append(append([]wireMessage{}, poll1...), pollRound(pollCallID2, pollArgs, pollFinished)...)
	p7b := postJSON(ctx, t, account, chatEndpoint, body(poll2), "P7-取回2")
	p7c := postJSON(ctx, t, account, chatEndpoint,
		body(append(append([]wireMessage{}, poll2...), user("请只回答：后台命令的结果拿到了吗？回答「是」或「否」。"))),
		"P7-取回完追问")

	// 结论表（判据：A 合法且命中≈前缀；中段改写命中塌陷；B1/B2 被拒）。
	if p1.status != 200 {
		t.Fatalf("基线请求未通过（http=%d），命中数据无效：先修 wire 形态再谈缓存", p1.status)
	}
	ratio := func(r probeResult) float64 {
		if r.promptTok == 0 {
			return 0
		}
		return float64(r.cachedTok) / float64(r.promptTok)
	}
	t.Logf("==== 判据汇总（prompt/cached/命中率）====")
	t.Logf("P1 基线      ：http=%d prompt=%d cached=%d ratio=%.2f", p1.status, p1.promptTok, p1.cachedTok, ratio(p1))
	t.Logf("P2 A 链路    ：http=%d prompt=%d cached=%d ratio=%.2f", p2.status, p2.promptTok, p2.cachedTok, ratio(p2))
	t.Logf("P3 中段改写  ：http=%d prompt=%d cached=%d ratio=%.2f", p3.status, p3.promptTok, p3.cachedTok, ratio(p3))

	if p2.status != 200 {
		t.Errorf("A 链路（补记追加在尾部）被拒：http=%d", p2.status)
	}
	switch {
	case p1.promptTok == 0 || p2.promptTok == 0:
		t.Log("inconclusive：provider 未回报缓存计数，命中判据未验证（合法性已验证）")
	case ratio(p2) < 0.5:
		t.Errorf("A 链路命中过低：ratio=%.2f（基线 %.2f）——尾部追加本不该破缓存", ratio(p2), ratio(p1))
	default:
		t.Logf("成立：A 链路（ack 定稿 + 尾部补记）命中 ratio=%.2f", ratio(p2))
	}
	if ratio(p3) >= ratio(p2) && ratio(p2) > 0 {
		t.Errorf("中段改写未导致塌陷（P3 ratio=%.2f ≥ P2 ratio=%.2f）——I-16 的实测依据不成立，需重看 provider 缓存语义", ratio(p3), ratio(p2))
	} else {
		t.Logf("成立：中段改写字母级差异即让改写点之后缓存作废（P3 ratio=%.2f < P2 ratio=%.2f）", ratio(p3), ratio(p2))
	}
	if p4.status < 400 {
		t.Errorf("B1（tool 结果回填到若干轮之后）被该 provider 接受（http=%d）——回填形态并非非法，A/B 判据需重判", p4.status)
	} else {
		t.Logf("成立：B1 迟到回填非法（http=%d）", p4.status)
	}
	if p5.status < 400 {
		t.Logf("注意：B2 同 call_id 重复回执被接受（http=%d），幂等键必须自己兜住", p5.status)
	} else {
		t.Logf("成立：B2 重复回执非法（http=%d），补记绝不能再用 role=tool", p5.status)
	}

	// P7 判据：轮询形态合法（每对都紧邻）、分叉前缀仍命中、命中量随追加不减。
	t.Logf("==== P7 轮询形态（prompt/cached/命中率/单轮增量）====")
	for _, r := range []probeResult{p7a, p7b, p7c} {
		t.Logf("%-14s http=%d prompt=%d cached=%d ratio=%.2f", r.label, r.status, r.promptTok, r.cachedTok, ratio(r))
		if r.status != 200 {
			t.Errorf("%s 被拒（http=%d）——tool_call 与 tool 回执已严格相邻，若仍被拒说明问题不在配对", r.label, r.status)
		}
	}
	t.Logf("轮询单次 token 成本：+%d → +%d（P1 prompt=%d）",
		p7a.promptTok-p1.promptTok, p7b.promptTok-p7a.promptTok, p1.promptTok)
	switch {
	case p1.cachedTok == 0 || p7a.cachedTok == 0:
		t.Log("P7 inconclusive：provider 未回报缓存计数，轮询是否保前缀未验证（合法性已验证）")
	case p7a.cachedTok*10 < p1.cachedTok*9:
		t.Errorf("P7 前缀命中塌陷：首轮 cached=%d < P1 cached=%d 的九成——与 P2 仅在 ack 之后分叉，不该作废已定稿前缀",
			p7a.cachedTok, p1.cachedTok)
	case p7c.cachedTok < p7a.cachedTok:
		t.Errorf("P7 命中倒退：追加两轮后 cached=%d < 首轮 cached=%d——尾部生长不应作废前缀",
			p7c.cachedTok, p7a.cachedTok)
	default:
		t.Logf("成立：轮询不破前缀（P1 cached=%d → 三轮 cached=%d/%d/%d）",
			p1.cachedTok, p7a.cachedTok, p7b.cachedTok, p7c.cachedTok)
	}

	// P6：Responses API（有状态链）——多数兼容层不支持，支持才测。
	if responsesEndpoint, ok := responsesEndpointFor(account.BaseURL); ok {
		probeResponsesAPI(ctx, t, account, responsesEndpoint)
	} else {
		t.Log("P6 Responses：该 base_url 不含可推断的 /v1/responses，跳过（有状态链未验证）")
	}
}

// probeResponsesAPI 测有状态链上「迟到的 function_call_output」是否被接受。
func probeResponsesAPI(ctx context.Context, t *testing.T, account probeAccountEntry, endpoint string) {
	input := []map[string]any{
		{"role": "system", "content": stableFiller(false)},
		{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "跑长命令，先别等。"}}},
		{"type": "function_call", "call_id": probeCallID, "name": "bash", "arguments": `{"command":"sleep 3","background":true}`},
		{"type": "function_call_output", "call_id": probeCallID, "output": probeAck},
		{"role": "user", "content": []map[string]any{{"type": "input_text", "text": asyncCompletionMessage()}}},
	}
	first := postJSON(ctx, t, account, endpoint, map[string]any{
		"model": account.Model, "input": input, "max_output_tokens": 16, "store": true,
	}, "P6-responses-1")
	if first.status != 200 {
		t.Logf("P6 Responses：首轮即被拒（http=%d），有状态链在本 provider 上不可用", first.status)
		return
	}
	// 迟到补投：只发新的 function_call_output（同 call_id）+ 追问，看服务端是否接受。
	late := []map[string]any{
		{"type": "function_call_output", "call_id": probeCallID, "output": "迟到结果：exit=0"},
		{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "请只回答：是 或 否"}}},
	}
	postJSON(ctx, t, account, endpoint, map[string]any{
		"model": account.Model, "input": late, "max_output_tokens": 16, "store": true,
	}, "P6-responses-2迟到补投")
}

// completionsEndpoint 把 base_url 归一到 …/v1/chat/completions。
func completionsEndpoint(base string) string {
	return joinEndpoint(base, "/chat/completions")
}

func responsesEndpointFor(base string) (string, bool) {
	if strings.Contains(strings.ToLower(base), "openai.") || strings.Contains(strings.ToLower(hostOf(base)), "openai.") {
		return joinEndpoint(base, "/responses"), true
	}
	// 非 OpenAI 官方域也可能支持；只在 base 明显是 v1 根时尝试。
	if strings.HasSuffix(strings.TrimRight(base, "/"), "/v1") {
		return joinEndpoint(base, "/responses"), true
	}
	return "", false
}

func joinEndpoint(base, suffix string) string {
	trimmed := strings.TrimRight(base, "/")
	lower := strings.ToLower(trimmed)
	for _, cut := range []string{"/chat/completions", "/responses", "/completions"} {
		if strings.HasSuffix(lower, cut) {
			trimmed = trimmed[:len(trimmed)-len(cut)]
		}
	}
	return trimmed + suffix
}
