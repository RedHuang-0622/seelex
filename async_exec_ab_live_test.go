//go:build manualsmoke

package main

// 轮询型后台命令切片的 §8.3 五指标 A/B（真实 API，opt-in，不进 CI）。
//
// 两臂只差 limits.async_exec.enabled——装配、账号、模型、任务素材逐字段相同：
//
//	A 臂 = 开关关：bash schema 里没有 background、async_output 未注册，长命令只能在
//	        工具调用里同步阻塞，整轮被这一条命令钉住；
//	B 臂 = 开关开：background=true 秒回受理回执（不含输出），趁命令在跑做别的活，
//	        再用 async_output(handle, wait_ms) 轮询取回增量。
//
// 判据在跑之前写死（规格 §8.3 + 台账 §10.2，跑完不得回头改阈值）：
//
//	1 任务总墙钟 ≤ A            —— 异步的全部价值；不降就是白做
//	2 逐轮 cached/prompt ≥ A−2pp —— 轮询是否破前缀
//	3 任务总 prompt ≤ A×1.10     —— 信封重复的容忍度
//	4 结果真进上下文 每任务 ≥1 次 —— 轮询型最坏失效＝模型根本不取
//	5 进程表归零 + 输出目录有界   —— 孤儿进程与泄漏
//	6 前缀结构与兑现率（J11）     —— 探针算的 tail-growth 必须一次不破，且 B 臂累计兑现率
//	                              Σcached(r)/Σprompt(r−1) ≥ A−2pp；任何一次被"无进展预算"
//	                              掐死（T4 的形状）都直接判不成立
//
// 指标 1、5、6 不过 = 判「不成立」，整个切片保持单 commit 直接 git revert，不调阈值。
//
// **前缀怎么算**（不靠 provider 报的数自证）：中间件代理留住每条请求的真实字节体，
// 逐对相邻请求取最长公共字节前缀 lcp；`lcp == len(上一条)` 即"历史只在尾部生长"。
// 于是可命中上限 = 上一条的 prompt tokens（纯尾部生长时），与 provider 报的 cached 并排
// 成"兑现率"。字节口径的复用率（Σlcp/Σbody）与 token 口径的兑现率同表列出——一个是
// 我们算的结构，一个是它报的账面，两者背离就是问题所在。
//
// 任务 T4 是"安静长命令"：整条命令中途一行都不输出，脚本要求 B 臂恰好轮询 6 次，
// 6 次取回的载荷逐字节相同。这正是 S2 之前会被"无进展预算"掐死的形状；步数写死是为了
// 不让模型随机性决定回合形状（两臂、每次重复都走同一条脚本）。
//
// 指标 4 的判据只看 wire，不看模型说了什么：标记 SENT-* 只存在于命令输出里，所以它
// 必须出现在 role=tool 消息中，且出现在 async_output 调用**之后**的那次请求里才算数
// ——受理回执本身不含输出，模型抄不到。T3 以「不取回结果」为设计目标，其指标 4 降级
// 为「async_output 的返回体进入后续请求 ≥1 次」，这一点在报告里单列。
//
// 运行（校准：只跑 T1、1 次、命令 6 秒）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	$env:SEELEX_ASYNC_AB_TASKS='T1'; $env:SEELEX_ASYNC_AB_REPEATS='1'; $env:SEELEX_ASYNC_AB_SECONDS='6'
//	go test -tags manualsmoke . -run TestManualSmokeAsyncAB -count=1 -v -timeout=30m
//
// 全量：删掉那三个变量（默认 T1,T2,T3 × 各 3 次 × 命令 20s）。原始数据落
// SEELEX_ASYNC_AB_REPORT（默认 _logs/async_ab_raw.txt）。账号只作为不透明文件在本
// 进程内改写 base_url，任何输出都不含 key。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	abArmA = "A" // 基线：开关关（同步阻塞）
	abArmB = "B" // 切片：开关开（受理回执 + 轮询）

	// abGrace 是"命令自己的期限"之外的宽限：过了这个点还活着的 sleep 就是孤儿。
	abGrace = 10 * time.Second
	// abTurnBudget 是单轮预算：命令 20s + 若干工具回合 + provider 抖动。
	abTurnBudget = 6 * time.Minute
)

type abConfig struct {
	seconds int
	repeats int
	tasks   []string
}

func abConfigFromEnv() abConfig {
	cfg := abConfig{seconds: 20, repeats: 3, tasks: []string{"T1", "T2", "T3", "T4"}}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SEELEX_ASYNC_AB_SECONDS"))); err == nil && value > 0 {
		cfg.seconds = value
	}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SEELEX_ASYNC_AB_REPEATS"))); err == nil && value > 0 {
		cfg.repeats = value
	}
	if value := strings.TrimSpace(os.Getenv("SEELEX_ASYNC_AB_TASKS")); value != "" {
		tasks := make([]string, 0, 4)
		for _, part := range strings.Split(value, ",") {
			if task := strings.ToUpper(strings.TrimSpace(part)); task != "" {
				tasks = append(tasks, task)
			}
		}
		cfg.tasks = tasks
	}
	return cfg
}

// abRound 是一次 provider 请求的缓存观测（逐轮并排表的数据源）。
//
// body/lcp/tail 是**探针自己算出来的结构事实**（不依赖 provider 口径）：lcp 为该请求体
// 与同回合上一条请求体的最长公共字节前缀，tail=true 表示上一条请求体整条就是这一条的
// 前缀，即"历史只在尾部生长"。缓存命中该不该高，先看这条结构判据，再看 provider 报的数。
type abRound struct {
	prompt     int
	cached     int
	completion int
	body       int
	lcp        int
	tail       bool
}

type abSample struct {
	arm       string
	task      string
	rep       int
	wall      time.Duration
	requests  int
	rounds    []abRound
	prompt    int
	cached    int
	uncached  int  // 真实边际：命中的前缀按轮重复计入 prompt，只有 miss 才是新增成本
	polls     int  // 含 async_output 调用的请求数
	inContext bool // 命令输出标记在调用之后的请求里进过 tool 消息
	dirs      int  // 输出目录增量
	logBytes  int64
	leftover  int // 进程表增量（sleep.exe）
	// cacheable/cacheRealized 从第二条请求起累计：上一条的 prompt tokens 是这一条
	// 理论上可命中的上限（纯尾部生长时），realized 是 provider 真报的命中 tokens。
	cacheable       int
	cacheRealized   int
	tailBroken      int
	promptBodyBytes int
	lcpBodyBytes    int
	failure         string
}

// utilization = 兑现率：真实命中 ÷ 可命中上限。跨臂单位一致，所以两臂可直接比。
func (s abSample) utilization() float64 {
	if s.cacheable <= 0 {
		return 0
	}
	return float64(s.cacheRealized) / float64(s.cacheable)
}

func (s abSample) ratio() float64 {
	if s.prompt <= 0 {
		return 0
	}
	return float64(s.cached) / float64(s.prompt)
}

// abPrompt 生成任务指令。两臂的任务、素材、提问完全一致，只差执行形状——
// 形状才是被测变量，别让措辞混进结果里。
func abPrompt(arm, taskID, command, sentinel string) string {
	const tail = "只调用上面点名的工具，不要编造任何命令输出。"
	sync := "用 bash 执行命令 `" + command + "`（工具调用会阻塞到命令结束），拿到输出后再继续。"
	async := "用 bash 的 background=true 派发命令 `" + command + "`（该次调用的返回只是受理回执，不含输出）。派发成功后先干别的活，" +
		"之后用 async_output(handle, wait_ms=5000) 取回输出；若返回仍在运行，就再调用一次 async_output。"
	ask := "最后回答三件事：命令输出里的标记是什么、a.txt 的第一行是什么、b.txt 的第一行是什么。"

	switch taskID {
	case "T2":
		write := "用 write_file 把 notes.txt 写成一行 ok。"
		if arm == abArmA {
			return sync + write + "最后回答两件事：命令输出里的标记是什么、notes.txt 的内容是什么。" + tail
		}
		return async + write + "然后再取回命令输出。" + "最后回答两件事：命令输出里的标记是什么、notes.txt 的内容是什么。" + tail
	case "T3":
		// 中途终止：B 能在命令还在跑时就收尾，A 结构上做不到（这正是差异所在）。
		read := "用 read_file 读取 b.txt。"
		if arm == abArmA {
			return sync + read + "最后回答两件事：b.txt 的第一行是什么、命令输出里的标记是什么。" + tail
		}
		return "用 bash 的 background=true 派发命令 `" + command + "`，然后调用 async_output(handle, wait_ms=1000) 一次；" +
			"不管命令是否结束都不要再等、不要再取回。" + read +
			"最后只回答 b.txt 的第一行，并按 async_output 的返回如实说明命令此刻是否已经结束。" + tail
	case "T4":
		// 安静长命令：整条命令中途**一行都不输出**，直到最后才 echo 标记。B 臂被要求
		// 恰好轮询 6 次——6 次取回的载荷逐字节相同（载荷必须确定性，否则每轮白烧缓存），
		// 这正是"无进展预算"会误杀的形状（S2 的实测位）。步数写死，是为了不让模型的
		// 随机性决定回合形状：两臂、每次重复都走同一条脚本。
		if arm == abArmA {
			return "用 bash 执行命令 `" + command + "`（工具调用会阻塞到命令结束）。" +
				"然后回答：命令输出里的标记是什么。" + tail
		}
		return "严格按序执行，不增不减：(1) 用 bash 的 background=true 派发命令 `" + command +
			"`，description 写「安静等一条长命令跑完」；(2) 对上一步返回的 handle 调用 async_output(handle, wait_ms=2000) " +
			"**恰好 6 次**；(3) 6 次之后，若某次返回里出现了标记就把标记原样回答出来，没有出现就回答「未取得」并说明命令仍在运行。" +
			"不要派发第二条命令，不要用别的工具。"
	default: // T1：长命令并行读代码
		read := "用 read_file 依次读取 a.txt 与 b.txt。"
		if arm == abArmA {
			return sync + read + ask + tail
		}
		return async + read + "然后再取回命令输出。" + ask + tail
	}
}

// abMessagesSpan 取出请求体里 "messages" 数组的**元素区**字节（去掉外层方括号）。
//
// 两个刻意的选择：
//   - 不用 json.Unmarshal：结构判据要的是**序列化后**的字节序一致（provider 缓存正是按
//     渲染后的 token 序做前缀匹配的），解一遍再重排反而会掩盖真实分叉点；
//   - 去掉外层 `]`：上一条的结尾方括号在下一条里变成逗号，留着它 LCP 必然差一字节，
//     "尾部生长"就永远判不过——那是采集器的错，不是链路的错（第一次校准实测就是这 1 字节）。
//
// 比较只在 messages 区做：请求体里 tools 排在其后，整条字节的 LCP 会在"上一条的
// messages 结尾 / tools 开头"处停下，与历史有没有被改写无关，纯是字段序。
func abMessagesSpan(body []byte) []byte {
	marker := []byte("\"messages\":[")
	start := bytes.Index(body, marker)
	if start < 0 {
		return nil
	}
	open := start + len(marker) - 1 // 指向 '['
	depth, inString, escaped := 0, false, false
	for index := open; index < len(body); index++ {
		char := body[index]
		switch {
		case inString && escaped:
			escaped = false
		case inString && char == 0x5C: // '\\'：JSON 字符串里的转义符，必须单独判，否则 " 会被误当作闭合
			escaped = true
		case char == '"':
			inString = !inString
		case inString:
		case char == '[' || char == '{':
			depth++
		case char == ']' || char == '}':
			depth--
			if depth == 0 {
				return body[open+1 : index] // 去掉外层 [ ]
			}
		}
	}
	return nil
}

// abCommonPrefixBytes 返回两段字节的公共前缀长度（探针的结构判据）。
func abCommonPrefixBytes(left, right []byte) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	count := 0
	for count < limit && left[count] == right[count] {
		count++
	}
	return count
}

// abRunTurn 跑一次完整回合并采集五项指标。每个样本一个独立 projectRoot/harness/代理，
// 用例通过 subtest 结束时统一回收（t.Cleanup）。
func abRunTurn(t *testing.T, accountsSource, upstream, taskID, arm string, rep, seconds int, nonce string) abSample {
	t.Helper()
	requireGitBash(t)
	sample := abSample{arm: arm, task: taskID, rep: rep}

	raw, err := os.ReadFile(accountsSource)
	if err != nil {
		t.Fatalf("read accounts file: %v", err)
	}
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
	for name, content := range map[string]string{
		"a.txt": "A-FIRST-LINE-" + nonce + "\n",
		"b.txt": "B-FIRST-LINE-" + nonce + "\n",
	} {
		if err := os.WriteFile(filepath.Join(projectRoot, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	// 标记既是"只有取回命令输出才知道的东西"（指标 4），也顺带充当运行级一次性盐：
	// provider 缓存 TTL 长于进程，不隔离的话第二轮会命中上一轮自己写进去的缓存。
	sentinel := fmt.Sprintf("SENT-%s-%s-%d", taskID, nonce, time.Now().UnixNano()%100000)
	command := fmt.Sprintf("sleep %d; echo %s", seconds, sentinel)

	dirsBefore, bytesBefore := abAsyncTempFootprint()
	sleepBefore := abSleepProcesses()

	harness := newFullChainHarnessWithLimits(t, accountsPath, projectRoot, 90*time.Second, true,
		seelexctx.Limits{AsyncExec: seelexctx.AsyncExecLimits{Enabled: arm == abArmB}})
	ctx, cancel := context.WithTimeout(context.Background(), abTurnBudget)
	defer cancel()

	start := time.Now()
	if err := harness.app.Submit(ctx, abPrompt(arm, taskID, command, sentinel)); err != nil {
		sample.failure = fmt.Sprintf("submit: %v", err)
	} else if err := harness.app.WaitForIdle(ctx); err != nil {
		sample.failure = fmt.Sprintf("idle: %v", err)
	} else if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
		sample.failure = snapshot.Chat.Error
	}
	sample.wall = time.Since(start)

	// 过了命令自己的期限再查进程表：还没到期的进程不算孤儿。
	if wait := time.Until(start.Add(time.Duration(seconds)*time.Second + abGrace)); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}

	callIdx, outIdx := -1, -1
	receipts := 0
	var prevBody []byte
	prevPrompt := 0
	for index, record := range proxy.snapshot() {
		if !strings.Contains(record.path, "chat/completions") {
			continue
		}
		sample.requests++
		if dir := strings.TrimSpace(os.Getenv("SEELEX_ASYNC_AB_DUMPDIR")); dir != "" {
			// 只用于本地诊断"两请求从第几个字节开始分叉"，默认关（正文含素材，不外发）。
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s_%s_req%02d.json", taskID, arm, sample.requests)), record.body, 0o600)
		}
		round := abRound{body: len(record.body)}
		if record.usage != nil {
			round.prompt, round.cached, round.completion = record.usage.PromptTokens, record.usage.CacheHitTokens, record.usage.CompletionTokens
			sample.prompt += record.usage.PromptTokens
			sample.cached += record.usage.CacheHitTokens
			sample.uncached += record.usage.CacheMissTokens
		}
		// 结构判据只能算在 **messages 数组**上：请求体里 tools 排在 messages 之后，
		// 整条字节的 LCP 会在"上一条的 messages 结尾 / tools 开头"处停下，那与历史有没有
		// 被改写无关，纯是序列化字段序。所以取 messages 片段自己比。
		messagesSpan := abMessagesSpan(record.body)
		if len(prevBody) > 0 {
			round.lcp = abCommonPrefixBytes(prevBody, messagesSpan)
			round.tail = round.lcp == len(prevBody)
			if !round.tail {
				sample.tailBroken++
			}
			// 分子分母必须同口径：都取 messages 元素区。第一版把分子算成 messages 区、
			// 分母算成整条请求体（含 tools，每请求恒定 ~16KB），"复用率"被系统性压低且
			// 绝对值不可解读——两臂同偏见还能比，但那是运气不是设计。
			sample.promptBodyBytes += len(messagesSpan)
			sample.lcpBodyBytes += round.lcp
			if prevPrompt > 0 && record.usage != nil {
				sample.cacheable += prevPrompt
				sample.cacheRealized += record.usage.CacheHitTokens
			}
		}
		if record.usage != nil {
			prevPrompt = record.usage.PromptTokens
			sample.rounds = append(sample.rounds, round)
		}
		prevBody = messagesSpan
		messages := prefixLiveMessages(t, record.body)
		roundReceipts := abAsyncReceipts(messages)
		for _, message := range messages {
			if message.role == "assistant" && message.hasTools && strings.Contains(message.raw, `"async_output"`) && callIdx < 0 {
				callIdx = index
			}
			if message.role == "tool" && strings.Contains(message.raw, sentinel) && outIdx < 0 {
				outIdx = index
			}
		}
		// 每对问答都会随历史重放到后续请求里，所以取"最全的那一条请求"的条数：
		// 第一条是派发回执，其余每条都是一次 async_output 取回。
		if roundReceipts > receipts {
			receipts = roundReceipts
		}
	}
	if receipts > 1 {
		sample.polls = receipts - 1
	}
	if outIdx >= 0 {
		// A 臂没有 async_output：同步 bash 的输出进入后续请求即等价事实。
		sample.inContext = arm == abArmA || outIdx > callIdx
	}
	dirsAfter, bytesAfter := abAsyncTempFootprint()
	sample.dirs = dirsAfter - dirsBefore
	sample.logBytes = bytesAfter - bytesBefore
	sample.leftover = abSleepProcesses() - sleepBefore
	return sample
}

// abAsyncReceipts 数一条请求里有几条 tool 消息的正文是异步载荷（解出来含 handle）。
//
// 只能在 content 上判，不能在 raw 上判：raw 是 wire 原文（正文字符串里全是 `\"`），
// 而 content 已被 prefixLiveContentText 解过一层，就是工具返回的那段 JSON 文本。
func abAsyncReceipts(messages []prefixLiveMessage) int {
	count := 0
	for _, message := range messages {
		if message.role != "tool" || message.content == "" {
			continue
		}
		var payload struct {
			Handle string `json:"handle"`
		}
		if err := json.Unmarshal([]byte(message.content), &payload); err != nil {
			continue
		}
		if payload.Handle != "" {
			count++
		}
	}
	return count
}

// abAsyncTempFootprint 统计后台输出目录的个数与总字节：这是"目录有界"的实测面。
func abAsyncTempFootprint() (int, int64) {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return 0, 0
	}
	temp := os.TempDir()
	dirs, total := 0, int64(0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "seelex-async-") {
			continue
		}
		dirs++
		_ = filepath.WalkDir(filepath.Join(temp, entry.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, statErr := d.Info(); statErr == nil {
				total += info.Size()
			}
			return nil
		})
	}
	return dirs, total
}

// abSleepProcesses 数一下当前存活的 sleep.exe（git-bash 的样本命令就是它）。
// 返回 -1 表示本机拿不到进程表，指标 5 只能靠目录那一半。
func abSleepProcesses() int {
	if runtime.GOOS != "windows" {
		return -1
	}
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq sleep.exe", "/NH").Output()
	if err != nil {
		return -1
	}
	return strings.Count(string(out), "sleep.exe")
}

// ── 汇总与判据 ────────────────────────────────────────────────────────────

type abGate struct {
	name   string
	detail string
	pass   bool
	// hard = true 时不过即判"不成立"（§8.3：指标 1 或 5）。
	hard bool
}

// abEfficiencyTask 标出"用来比墙钟与总 prompt"的任务集合。T4 不在其中（它的回合形状
// 是脚本故意做重的，专测无进展预算）。判据归属在跑之前定死，不是看到结果后再分。
func abEfficiencyTask(taskID string) bool {
	switch taskID {
	case "T1", "T2", "T3":
		return true
	default:
		return false
	}
}

func abMedian(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]int64(nil), values...)
	sort.Slice(copied, func(i, j int) bool { return copied[i] < copied[j] })
	return copied[len(copied)/2]
}

func abOK(arm, task string, samples []abSample) []abSample {
	out := make([]abSample, 0, len(samples))
	for _, sample := range samples {
		if sample.arm == arm && sample.task == task && sample.failure == "" && sample.requests > 0 {
			out = append(out, sample)
		}
	}
	return out
}

func abWallMS(s []abSample) []int64 {
	out := make([]int64, 0, len(s))
	for _, sample := range s {
		out = append(out, sample.wall.Milliseconds())
	}
	return out
}

func abPromptTotals(s []abSample) []int64 {
	out := make([]int64, 0, len(s))
	for _, sample := range s {
		out = append(out, int64(sample.prompt))
	}
	return out
}

func abRatioSum(s []abSample) (float64, int, int) {
	cached, prompt := 0, 0
	for _, sample := range s {
		cached += sample.cached
		prompt += sample.prompt
	}
	if prompt == 0 {
		return 0, cached, prompt
	}
	return float64(cached) / float64(prompt), cached, prompt
}

// abJudge 按 §8.3 事先写死的门槛算五条判据。
func abJudge(cfg abConfig, samples []abSample) ([]abGate, []string) {
	var notes []string
	var wallsA, wallsB, promptsA, promptsB []int64
	cachedA, promptA, cachedB, promptB := 0, 0, 0, 0
	leftoverWorst, dirsWorst := 0, 0
	incomplete := 0

	for _, task := range cfg.tasks {
		a, b := abOK(abArmA, task, samples), abOK(abArmB, task, samples)
		if len(a) == 0 || len(b) == 0 {
			notes = append(notes, fmt.Sprintf("%s 有效样本不足（A=%d B=%d）——该任务的判据不计入", task, len(a), len(b)))
			incomplete++
			continue
		}
		// 墙钟与总 prompt 只在"效率任务"上判：T4 的脚本要求就是**故意**串行轮询 6 次
		// （它测的是安静轮询会不会被预算掐死，不是省不省时间），拿它比墙钟/prompt 总量
		// 是把一条为测 A 而设计的任务塞进 B 的判据里。
		if abEfficiencyTask(task) {
			wallsA = append(wallsA, abMedian(abWallMS(a)))
			wallsB = append(wallsB, abMedian(abWallMS(b)))
			promptsA = append(promptsA, abMedian(abPromptTotals(a)))
			promptsB = append(promptsB, abMedian(abPromptTotals(b)))
		}
		_, ca, pa := abRatioSum(a)
		_, cb, pb := abRatioSum(b)
		cachedA += ca
		promptA += pa
		cachedB += cb
		promptB += pb

		fetched := 0
		for _, sample := range b {
			if sample.task == "T3" || sample.task == "T4" {
				// 这两条任务的设计目标不是"取回标记"（T4 就是要看安静轮询会不会被掐死），
				// 所以按"确实取回过"判，而不是按输出进没进上下文判。
				if sample.polls >= 1 {
					fetched++
				}
				continue
			}
			if sample.inContext {
				fetched++
			}
		}
		if fetched == 0 {
			notes = append(notes, fmt.Sprintf("%s/B 没有任何一次取回结果进入上下文", task))
		}
	}

	for _, sample := range samples {
		if sample.leftover > leftoverWorst {
			leftoverWorst = sample.leftover
		}
		if sample.dirs > dirsWorst {
			dirsWorst = sample.dirs
		}
	}

	applicable := len(wallsB) > 0 && len(wallsB) == len(wallsA)
	gate1 := applicable
	for i := range wallsB {
		if wallsB[i] > wallsA[i] {
			gate1 = false
		}
	}
	var sumA, sumB int64
	for _, v := range wallsA {
		sumA += v
	}
	for _, v := range wallsB {
		sumB += v
	}

	ratioA, ratioB := 0.0, 0.0
	if promptA > 0 {
		ratioA = float64(cachedA) / float64(promptA)
	}
	if promptB > 0 {
		ratioB = float64(cachedB) / float64(promptB)
	}

	gate3 := len(promptsB) > 0
	for i := range promptsB {
		if float64(promptsB[i]) > float64(promptsA[i])*1.10 {
			gate3 = false
		}
	}
	uncachedA, uncachedB := 0, 0
	for _, sample := range samples {
		if sample.failure != "" || sample.requests == 0 {
			continue
		}
		if sample.arm == abArmA {
			uncachedA += sample.uncached
		} else {
			uncachedB += sample.uncached
		}
	}

	gate4 := true
	for _, task := range cfg.tasks {
		found := false
		for _, sample := range abOK(abArmB, task, samples) {
			if task == "T3" || task == "T4" {
				found = found || sample.polls >= 1
			} else {
				found = found || sample.inContext
			}
		}
		gate4 = gate4 && found
	}

	gate5 := leftoverWorst <= 0 && dirsWorst <= 1
	if abSleepProcesses() < 0 {
		notes = append(notes, "本机无法读取进程表（tasklist 不可用），指标 5 只判输出目录那一半")
	}

	// 判据 6（台账 J11 的可操作化）：**结构**先于**账面**。
	//   结构 = 探针算的 tail-growth：上一条请求体整条是这一条的前缀 ⇒ 历史只在尾部生长，
	//          中段没有改写过（改写一次，缓存就塌一次，P3 实测 0.25）。
	//   账面 = 兑现率 Σcached(r) / Σprompt(r−1)：纯尾部生长时上一轮的全部 prompt 就是这一轮
	//          可命中的上限，兑现率两臂单位一致，可直接比。
	// T4 另加一条硬事实：安静长命令被脚本要求轮询满 6 次，任何一次被"无进展预算"掐死
	// 都直接判不成立（这就是 S2 修的洞，跑前写死）。
	brokenA, brokenB, cacheableA, realizedA, cacheableB, realizedB := 0, 0, 0, 0, 0, 0
	var t4Polls []int64
	aborted := 0
	for _, sample := range samples {
		// 预算掐死会把原因留在 failure 里，必须在"跳过不完整样本"之前数。
		if strings.Contains(sample.failure, "no observable progress") {
			aborted++
		}
		if sample.failure != "" || sample.requests == 0 {
			continue
		}
		if sample.arm == abArmA {
			brokenA += sample.tailBroken
			cacheableA += sample.cacheable
			realizedA += sample.cacheRealized
		} else {
			brokenB += sample.tailBroken
			cacheableB += sample.cacheable
			realizedB += sample.cacheRealized
		}
		if sample.task == "T4" {
			t4Polls = append(t4Polls, int64(sample.polls))
		}
	}
	utilA, utilB := 0.0, 0.0
	if cacheableA > 0 {
		utilA = float64(realizedA) / float64(cacheableA)
	}
	if cacheableB > 0 {
		utilB = float64(realizedB) / float64(cacheableB)
	}
	gate6 := brokenA == 0 && brokenB == 0 && cacheableB > 0 && utilB >= utilA-0.02 && aborted == 0
	if len(t4Polls) > 0 {
		if medPolls := abMedian(t4Polls); medPolls < 5 {
			notes = append(notes, fmt.Sprintf("T4 轮询次数中位=%d（脚本要求 6 次）——采集器或模型没按步骤走，该任务的兑现率不作数", medPolls))
		}
	}

	return []abGate{
		{name: "1 墙钟 B ≤ A", detail: abScopeNote(applicable, fmt.Sprintf("逐任务中位(ms) A=%v B=%v；合计 A=%d B=%d（Δ%+.1f%%）",
			wallsA, wallsB, sumA, sumB, abPercent(sumA, sumB))), pass: gate1, hard: true},
		{name: "2 缓存比率 B ≥ A−2pp", detail: fmt.Sprintf("A=%.3f（%d/%d） B=%.3f（%d/%d）",
			ratioA, cachedA, promptA, ratioB, cachedB, promptB), pass: ratioB >= ratioA-0.02 && len(samples) > 0, hard: false},
		{name: "3 总 prompt B ≤ A×1.10", detail: abScopeNote(applicable, fmt.Sprintf("逐任务中位 A=%v B=%v；未命中边际合计 A=%d B=%d（只判 T1/T2/T3）",
			promptsA, promptsB, uncachedA, uncachedB)), pass: gate3 || !applicable, hard: false},
		{name: "4 结果真进上下文", detail: fmt.Sprintf("每任务 B 臂 ≥1 次（T3 按取回回执计）；有效任务数 %d，样本不完整 %d",
			len(cfg.tasks), incomplete), pass: gate4 && len(cfg.tasks) > 0, hard: false},
		{name: "5 进程归零/目录有界", detail: fmt.Sprintf("进程表增量最大 %d、输出目录增量最大 %d/次、日志字节增量见逐行",
			leftoverWorst, dirsWorst), pass: gate5, hard: true},
		{name: "6 前缀结构与兑现率", detail: fmt.Sprintf("尾部生长破坏次数 A=%d B=%d（必须 0）；兑现率 A=%.3f（%d/%d） B=%.3f（%d/%d）；被无进展预算掐死的样本=%d",
			brokenA, brokenB, utilA, realizedA, cacheableA, utilB, realizedB, cacheableB, aborted),
			pass: gate6, hard: true},
	}, notes
}

// abScopeNote 在"本轮没跑效率任务"时把判据标成不适用（校准轮只跑 T4 是这种情形）。
// 判据不因为"没数据"被当成通过或不通过，而是明写不适用——免得空表读出假结论。
func abScopeNote(applicable bool, detail string) string {
	if applicable {
		return detail
	}
	return "本轮未跑效率任务（T1/T2/T3），判据不适用｜" + detail
}

func abPercent(a, b int64) float64 {
	if a == 0 {
		return 0
	}
	return float64(a-b) / float64(a) * 100
}

// abBit 把布尔判据打成 1/0，便于逐轮表里直接数破坏次数。
func abBit(value bool) int {
	if value {
		return 1
	}
	return 0
}

func abReportPath() string {
	if value := strings.TrimSpace(os.Getenv("SEELEX_ASYNC_AB_REPORT")); value != "" {
		return value
	}
	return filepath.Join("_logs", "async_ab_raw.txt")
}

func abWriteReport(t *testing.T, cfg abConfig, samples []abSample, gates []abGate, notes []string) string {
	t.Helper()
	path := abReportPath()
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	var report strings.Builder
	fmt.Fprintf(&report, "# §8.3 轮询型后台命令 A/B 原始数据（%s）\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&report, "命令时长=%ds 每臂每任务重复=%d 次 任务=%s 开关唯一变量=limits.async_exec.enabled\n\n",
		cfg.seconds, cfg.repeats, strings.Join(cfg.tasks, ","))

	fmt.Fprintf(&report, "## 逐次原始样本（ratio=cached/prompt，util=兑现率 cached(r)/prompt(r−1)，in_ctx=输出进入后续上下文）\n")
	for _, sample := range samples {
		line := fmt.Sprintf("task=%-3s arm=%s rep=%d wall_ms=%-7d reqs=%-3d prompt=%-7d cached=%-7d uncached=%-6d ratio=%.3f util=%.3f polls=%-2d in_ctx=%-5v tail_break=%d dirs=%+d log_bytes=%+d leftover=%d %s",
			sample.task, sample.arm, sample.rep, sample.wall.Milliseconds(), sample.requests, sample.prompt, sample.cached,
			sample.uncached, sample.ratio(), sample.utilization(), sample.polls, sample.inContext, sample.tailBroken,
			sample.dirs, sample.logBytes, sample.leftover, abFailureSuffix(sample))
		fmt.Fprintf(&report, "%s\n", strings.TrimRight(line, " \t"))
		rounds := make([]string, 0, len(sample.rounds))
		for i, round := range sample.rounds {
			ratio := 0.0
			if round.prompt > 0 {
				ratio = float64(round.cached) / float64(round.prompt)
			}
			// lcp/tail 只在有前一条请求时可算（第一条恒为 0/-），因此带 lcp 的轮才打出来。
			prefix := ""
			if i > 0 {
				prefix = fmt.Sprintf(" lcp=%d tail=%d", round.lcp, abBit(round.tail))
			}
			rounds = append(rounds, fmt.Sprintf("r%d=%d/%.2f out=%d body=%d%s", i+1, round.prompt, ratio, round.completion, round.body, prefix))
		}
		fmt.Fprintf(&report, "        逐轮 %s\n", strings.Join(rounds, " "))
	}

	fmt.Fprintf(&report, "\n## 累计前缀账（探针自己算的结构 + provider 报的账面）\n")
	for _, arm := range []string{abArmA, abArmB} {
		body, lcp, tailBreaks, cacheable, realized, requests := 0, 0, 0, 0, 0, 0
		for _, sample := range samples {
			if sample.arm != arm || sample.failure != "" || sample.requests == 0 {
				continue
			}
			body += sample.promptBodyBytes
			lcp += sample.lcpBodyBytes
			tailBreaks += sample.tailBroken
			cacheable += sample.cacheable
			realized += sample.cacheRealized
			requests += sample.requests - 1 // 首条无前缀可算
		}
		utilization := 0.0
		if cacheable > 0 {
			utilization = float64(realized) / float64(cacheable)
		}
		// 字节口径的"复用率"与 token 口径的"兑现率"并排：前者是我们算的，后者是它报的。
		byteReuse := 0.0
		if body > 0 {
			byteReuse = float64(lcp) / float64(body)
		}
		fmt.Fprintf(&report, "arm=%s 可算前缀的请求=%d 送出字节=%d 公共前缀字节=%d（复用 %.3f）边际字节=%d；"+
			"可命中 token=%d 真实命中=%d（兑现 %.3f）尾部生长破坏=%d\n",
			arm, requests, body, lcp, byteReuse, body-lcp, cacheable, realized, utilization, tailBreaks)
	}

	fmt.Fprintf(&report, "\n## 判据（阈值跑前写死，跑后不改）\n")
	for _, gate := range gates {
		fmt.Fprintf(&report, "[%s]%s %s ｜ %s\n", abVerdict(gate.pass), abHardMark(gate.hard), gate.name, gate.detail)
	}
	if len(notes) > 0 {
		fmt.Fprintf(&report, "\n## 备注\n")
		for _, note := range notes {
			fmt.Fprintf(&report, "- %s\n", note)
		}
	}

	dirCount, totalBytes := abAsyncTempFootprint()
	fmt.Fprintf(&report, "\n## 收尾现场\n")
	fmt.Fprintf(&report, "临时目录内 seelex-async-* 个数=%d 合计字节=%d 存活 sleep.exe=%d\n", dirCount, totalBytes, abSleepProcesses())

	if err := os.WriteFile(path, []byte(report.String()), 0o644); err != nil {
		t.Fatalf("write report %s: %v", path, err)
	}
	return path
}

func abFailureSuffix(sample abSample) string {
	if sample.failure == "" {
		return ""
	}
	return "failure=" + sample.failure
}

func abVerdict(pass bool) string {
	if pass {
		return "成立"
	}
	return "不成立"
}

func abHardMark(hard bool) string {
	if hard {
		return "[硬]"
	}
	return "[软]"
}

// ── 用例 ──────────────────────────────────────────────────────────────

func TestManualSmokeAsyncAB(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the A/B measurement")
	}
	raw, err := os.ReadFile(accountsSource)
	if err != nil {
		t.Fatalf("read accounts file: %v", err)
	}
	upstream := prefixLiveUpstreamBaseURL(t, raw)
	cfg := abConfigFromEnv()
	nonce := strconv.FormatInt(time.Now().UnixNano()%1000000, 36)

	var samples []abSample
	for _, taskID := range cfg.tasks {
		for rep := 1; rep <= cfg.repeats; rep++ {
			// 两臂交替跑：provider 缓存与机器负载的漂移同时作用于 A 与 B。
			for _, arm := range []string{abArmA, abArmB} {
				taskID, rep, arm := taskID, rep, arm
				t.Run(fmt.Sprintf("%s/%s/%d", taskID, arm, rep), func(t *testing.T) {
					sample := abRunTurn(t, accountsSource, upstream, taskID, arm, rep, cfg.seconds, nonce)
					samples = append(samples, sample)
					if sample.failure != "" {
						t.Logf("样本不完整 task=%s arm=%s rep=%d failure=%v", sample.task, sample.arm, sample.rep, sample.failure)
					}
				})
			}
		}
	}

	gates, notes := abJudge(cfg, samples)
	path := abWriteReport(t, cfg, samples, gates, notes)
	t.Logf("report=%s samples=%d", path, len(samples))

	for _, gate := range gates {
		if !gate.pass && gate.hard {
			t.Errorf("硬判据不过：%s ｜ %s（按 §8.3 直接回滚，不调阈值）", gate.name, gate.detail)
		}
	}
	for _, gate := range gates {
		if !gate.pass && !gate.hard {
			t.Logf("软判据不过：%s ｜ %s", gate.name, gate.detail)
		}
	}
}
