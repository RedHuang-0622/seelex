//go:build compactlive

// 上下文主动压缩（compact）真实 API 冒烟（opt-in，走**完整应用链路**）：
//
//	app.Submit（真实 provider）→ 累积上下文越过软阈值 → 装配层折叠为有界
//	  checkpoint 帧（ContextCompaction 记录）→ 模型仍给出回答
//	→ /compact 命令（同一落点）→ 压缩后链路继续可用
//
// 为什么需要真实 API：压缩是"装配 provider 上下文"这一步的行为，只有真跑一次
// 请求才能证明折叠后的上下文**确实被 provider 接受**、且后续回合与手动入口在同
// 一条链路上工作。离线用例（application/core/context_compact_test.go）用假
// provider 证明落点与记录，这里补"真链路"。
//
// 账号副本会被**临时改写上下文预算**（`defaults.context_window` / `max_tokens`
// 调小）——否则要攒够 12.5 万 token 才能触发，冒烟又慢又贵。改写只发生在
// t.TempDir() 的副本上，源文件只读不写、不解析、不打印内容。
//
// 运行：
//
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags compactlive . -run TestCompactLiveSmoke -count=1 -v -timeout=15m
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/application/model"
)

// compactSmokeContextWindow / MaxTokens：把窗口调小到几十 k，让两三轮真实回合
// 就能越过软阈值（默认 200k 窗口要攒 12.5 万 token，冒烟过慢）。校验规则要求
// max_tokens < window − window/8，2048 对 40000 安全。
const (
	compactSmokeContextWindow = 40000
	compactSmokeMaxTokens     = 2048
)

// TestCompactLivePreflight 是 compact 冒烟的前置探针：只做一次极小的真实回合，
// 用来把"provider 本身通不通/快不快"和"压缩链路对不对"分开。缺账号即跳过。
func TestCompactLivePreflight(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API compact 冒烟")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyAccountsOpaqueForCompact(t, accountsSource, accountsPath)
	rewriteAccountsContextLimits(t, accountsPath, compactSmokeContextWindow, compactSmokeMaxTokens)

	harness := newFullChainHarness(t, accountsPath, projectRoot, 60*time.Second)
	defer harness.app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := harness.app.Submit(ctx, "只回复两个字：在的。不要调用任何工具。"); err != nil {
		t.Fatalf("预检提交失败: %v", err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		snapshot := harness.app.Snapshot()
		t.Fatalf("预检回合未回到 idle（%v）：running=%v error=%q 对话=%s",
			err, snapshot.Chat.Running, snapshot.Chat.Error, describeConversationTail(snapshot.Conversation, 4))
	}
	snapshot := harness.app.Snapshot()
	t.Logf("预检通过：running=%v error=%q 回答=%s 压缩记录=%+v 窗口=%d max_tokens=%d",
		snapshot.Chat.Running, snapshot.Chat.Error, truncateForLog(lastAssistantContent(snapshot)),
		compactRecords(snapshot), compactSmokeContextWindow, compactSmokeMaxTokens)
}

func TestCompactLiveSmoke(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API compact 冒烟")
	}

	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyAccountsOpaqueForCompact(t, accountsSource, accountsPath)
	rewriteAccountsContextLimits(t, accountsPath, compactSmokeContextWindow, compactSmokeMaxTokens)

	harness := newFullChainHarness(t, accountsPath, projectRoot, 90*time.Second)
	defer harness.app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// 事件日志：真实回合卡住时，靠它把"provider 慢"、"工具循环"、"装配失败"
	// 区分开（只打前若干条 + 每 25 条抽样，避免刷屏）。
	if harness.events != nil {
		subscription := harness.events.Subscribe(512)
		defer subscription.Close()
		stopLog := make(chan struct{})
		defer close(stopLog)
		go func() {
			count := 0
			for {
				select {
				case event, ok := <-subscription.Events:
					if !ok {
						return
					}
					count++
					if count <= 30 || count%25 == 0 {
						t.Logf("event#%d %s", count, event.Kind)
					}
				case <-stopLog:
					return
				}
			}
		}()
	}

	submit := func(stage, prompt string) model.Snapshot {
		t.Helper()
		turnCtx, turnCancel := context.WithTimeout(ctx, 4*time.Minute)
		defer turnCancel()
		if err := harness.app.Submit(turnCtx, prompt); err != nil {
			t.Fatalf("%s：提交失败: %v", stage, err)
		}
		if err := harness.app.WaitForIdle(turnCtx); err != nil {
			snapshot := harness.app.Snapshot()
			t.Fatalf("%s：真实回合未回到 idle（%v）：running=%v error=%q 对话=%s",
				stage, err, snapshot.Chat.Running, snapshot.Chat.Error, describeConversationTail(snapshot.Conversation, 4))
		}
		snapshot := harness.app.Snapshot()
		if snapshot.Chat.Error != "" {
			t.Fatalf("%s：真实回合失败: %s", stage, snapshot.Chat.Error)
		}
		return snapshot
	}

	// 累积**小片**材料直到压缩触发：单条大料一进门就被"单条超预算外置"归档成
	// result_ref（模型只收到引用告示、provider 历史里没有正文），既挤爆窗口又测不到
	// 折叠链路。所以每轮喂若干小片（每片 < 单条外置阈值），有界循环"攒到压缩发生"。
	// 每片都对压缩记录取证：reason 前缀 context_budget（含 _autonomous）、区间、帧。
	var compacted model.ContextCompaction
	round := 0
	for round < 3 && compacted.Version == 0 {
		round++
		for part := 1; part <= compactSmokePiecesPerRound; part++ {
			stage := fmt.Sprintf("第 %d 轮第 %d 片", round, part)
			snapshot := submit(stage, fmt.Sprintf(
				"这是上下文压力测试，本轮禁止调用任何工具，只回复『已记录 %d-%d』。下面是本片材料：\n%s",
				round, part, compactSmokePiece(round, part)))
			if answer := lastAssistantContent(snapshot); answer == "" {
				t.Fatalf("%s 没有助手回答；对话末尾：%s", stage, describeConversationTail(snapshot.Conversation, 4))
			}
			records := compactRecords(snapshot)
			if len(records) > 0 {
				// 每片材料各自成帧：记录里有区间（消息号 + 事件序号）与 frame_ref，
				// 帧正文可按 ref 回读——这三件事齐备才算"看得见压了什么"。
				for _, record := range records {
					t.Logf("%s 结束：v%d origin=%s 区间=%s..%s（事件 %d..%d）frame_ref=%s bytes=%d",
						stage, record.Version, record.Origin, record.MessageFrom, record.MessageTo,
						record.EventFrom, record.EventTo, record.FrameRef, record.FrameBytes)
					if strings.HasPrefix(record.Reason, "context_budget") {
						compacted = record
					}
				}
			}
		}
	}
	if compacted.Version == 0 {
		t.Fatalf("累积 %d 轮 × %d 片真实回合仍未触发压缩（窗口=%d max_tokens=%d）——压缩链路没有触发",
			round, compactSmokePiecesPerRound, compactSmokeContextWindow, compactSmokeMaxTokens)
	}
	if compacted.EstimatedTokens <= 0 {
		t.Fatalf("压缩记录字段不完整: %+v", compacted)
	}
	t.Logf("压缩记录：version=%d reason=%s messages_before=%d estimated_tokens=%d at=%s",
		compacted.Version, compacted.Reason, compacted.MessagesBefore, compacted.EstimatedTokens, compacted.CompactedAt.Format(time.RFC3339))

	// 帧纪律：一次折叠一个帧、帧是终态（不再被聚合进下一个帧）。判据取事实而非感觉：
	//   ① 每条记录的 frame_ref 唯一（同一次折叠不会有第二个 ref）；
	//   ② 回读帧正文，表头 "checkpoint frame v" 恰好出现一次——出现两次就意味着
	//      上一帧的正文被当成材料又折了一遍（信息会随每次重摘要衰减）。
	seenFrames := map[string]bool{}
	for _, record := range compactRecords(harness.app.Snapshot()) {
		if record.FrameRef == "" {
			t.Fatalf("压缩记录没有 frame_ref，帧正文不可回读：%+v", record)
		}
		if seenFrames[record.FrameRef] {
			t.Fatalf("同一个 frame_ref 出现在多条记录里：%s", record.FrameRef)
		}
		seenFrames[record.FrameRef] = true
		page, err := harness.app.ReadToolResultHandler(ctx, fmt.Sprintf(`{"result_ref":%q,"offset":0,"limit":120}`, record.FrameRef))
		if err != nil {
			t.Fatalf("回读帧正文失败 %s: %v", record.FrameRef, err)
		}
		if got := strings.Count(page, "checkpoint frame v"); got != 1 {
			t.Fatalf("帧 %s 的表头出现 %d 次（>1 说明帧被二次聚合）：%s",
				record.FrameRef, got, truncateForLog(page))
		}
		t.Logf("帧 %s：v%d 区间=%s..%s（事件 %d..%d）%d 字节；正文=%s",
			record.FrameRef, record.Version, record.MessageFrom, record.MessageTo,
			record.EventFrom, record.EventTo, record.FrameBytes, truncateForLog(page))
	}

	// 压缩之后普通对话链路仍然可用（回答不为空）。
	postCompact := submit("压缩后回合", "不要调用任何工具，只回复『继续』。")
	if answer := lastAssistantContent(postCompact); answer == "" {
		t.Fatalf("压缩后回合没有助手回答；task=%+v", postCompact.Task)
	}
	t.Logf("压缩后回答：%s", truncateForLog(lastAssistantContent(postCompact)))

	// 手动入口 /compact（与 compact_context 工具同一落点）必须给出明确结论
	// （已压缩或如实"未达阈值"），不能报错。
	manual := submit("手动 /compact", "/compact")
	notice := lastSystemNotice(manual.Conversation)
	if notice == "" || !strings.Contains(notice, "压缩") {
		t.Fatalf("/compact 没有给出压缩相关提示：%q", notice)
	}
	t.Logf("/compact 提示：%s", notice)

	// 手动入口之后，普通对话链路仍然可用（回答不为空）。
	afterManual := submit("手动压缩后回合", "不要调用任何工具，只回复『链路正常』。")
	answer := lastAssistantContent(afterManual)
	if answer == "" {
		t.Fatalf("压缩后普通对话没有回答；task=%+v", afterManual.Task)
	}
	t.Logf("压缩后回答：%s", truncateForLog(answer))
	t.Logf("session=%s 压缩记录数=%d", afterManual.Session.ID, len(compactRecords(afterManual)))
}

// compactSmokePiecesPerRound 是每轮喂的小片数：单片 ≈5k tokens，几片即可越过
// 调小窗口后的软阈值——用小片累积，而不是单条大料（大料必被"单条超预算外置"
// 拦截成 result_ref，测到的是外置链路而非折叠链路）。
const compactSmokePiecesPerRound = 4

// compactSmokePiece 生成**一小片**材料（第 round 轮第 part 片，带可检索的片号）。
// 单片必须小于单条外置阈值 = 预算 × context_single_item_percent（默认 50%，40000
// 窗口下约 16.5k tokens）：按 4 字符/token 估算，140 行 ≈ 21k 字符 ≈ 5k tokens，
// 留足余量。片号进正文，便于事后在会话存储里按片核对原文。
func compactSmokePiece(round, part int) string {
	line := fmt.Sprintf("seelex compact smoke round %02d part %02d material: the assembly layer folds settled rounds into a bounded checkpoint frame so the provider context stays inside budget. ", round, part)
	return strings.Repeat(line, 140)
}

func compactRecords(snapshot model.Snapshot) []model.ContextCompaction {
	if snapshot.Task == nil {
		return nil
	}
	return snapshot.Task.ContextCompactions
}

func lastAssistantContent(snapshot model.Snapshot) string {
	for index := len(snapshot.Conversation) - 1; index >= 0; index-- {
		message := snapshot.Conversation[index]
		if message.Role == "assistant" && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}

// lastSystemNotice 取最后一条 system 角色提示（命令结果的可见回执经
// addNotice 以 system 消息进入会话）。
func lastSystemNotice(conversation []model.Message) string {
	for index := len(conversation) - 1; index >= 0; index-- {
		message := conversation[index]
		if message.Role == "system" && strings.TrimSpace(message.Content) != "" {
			return strings.TrimSpace(message.Content)
		}
	}
	return ""
}

// describeConversationTail 汇总对话末尾若干条（role: 摘要），失败信息里用。
func describeConversationTail(conversation []model.Message, limit int) string {
	if limit <= 0 || limit > len(conversation) {
		limit = len(conversation)
	}
	tail := conversation[len(conversation)-limit:]
	parts := make([]string, 0, len(tail))
	for _, message := range tail {
		parts = append(parts, message.Role+": "+truncateForLog(message.Content))
	}
	return strings.Join(parts, " | ")
}

func truncateForLog(value string) string {
	text := strings.Join(strings.Fields(value), " ")
	if len([]rune(text)) <= 120 {
		return text
	}
	return string([]rune(text)[:120]) + "…"
}

func copyAccountsOpaqueForCompact(t *testing.T, source, destination string) {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("读取账号配置失败: %v", err)
	}
	if err := os.WriteFile(destination, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rewriteAccountsContextLimits 把账号副本的上下文预算调小（只动 t.TempDir() 的
// 副本）：defaults 设小窗口，并删掉各账号自己的覆盖项，避免个别角色仍按大窗口
// 生效。密钥等字段原样保留，不解析、不打印。
func rewriteAccountsContextLimits(t *testing.T, path string, window, maxTokens int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("账号配置不是合法 YAML: %v", err)
	}
	defaults, _ := doc["defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	defaults["context_window"] = window
	defaults["max_tokens"] = maxTokens
	doc["defaults"] = defaults
	if roles, ok := doc["roles"].(map[string]any); ok {
		for _, entries := range roles {
			list, ok := entries.([]any)
			if !ok {
				continue
			}
			for _, entry := range list {
				account, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				delete(account, "context_window")
				delete(account, "max_tokens")
			}
		}
	}
	encoded, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
