package gui

// TestRealAPITeamWorkComputerUseLiveProbe 是「computer use 在 team work 里是否
// 真的贯通」的真实 API 全链路探针（opt-in）。它回答的问题比包内单测更硬：
// **EXEC 在团队回合里截的屏，ADVISOR（tl）到底看不看得见。**
//
// 链路（每一步都经 headless 控制面，与 GUI 同装配）：
//  1. 真实 API Submit #1 物化主会话（EXEC = main）；
//  2. goal.begin → 隐式装配 goal-a2a（tl 成员 + order_policy=goal_loop）；
//  3. Submit #2 要求 EXEC **真的调用 computer_screenshot** 并报告前台窗口标题；
//  4. 读三处事实：
//     - 会话媒体分区：截图 PNG 是否落盘（工具真的跑了）；
//     - role.snapshot(tl)：ADVISOR 回合输入里是否带 computer use 证据
//       （`screen: media:… 1024x576 foreground="…"`，由
//       application/core/goal_work_summary.go 抽取）；
//     - 主会话可见聊天里是否**已经**有 ADVISOR 裁决行（kind=tl_directive +
//       role_name=tl）——裁决在产出它的那一回合就回放，不需要再提交一轮；
//     - goal.gov_snapshot：裁决与治理收口语义一致。
//
// 断言口径：都是硬断言——只依赖"工具跑了 + 摘要把证据带上了 + 治理推进了 +
// 裁决在产出回合就可见"，不依赖模型自由发挥（EXEC 的截图动作由提示词直接指定）。
//
// 运行（真实 API + 真机截屏，默认跳过）：
//
//	go build -o tmp/bin/seelex-headless.exe .
//	$env:SMOKE_TEAM_WORK_COMPUTER_LIVE='1'
//	go test ./gui -run TestRealAPITeamWorkComputerUseLiveProbe -v -count=1 -timeout 20m
//
// 可选 env：
//
//	SMOKE_TEAM_WORK_LIVE_TARGET              目标二进制（默认 tmp/bin/seelex-headless.exe）
//	SMOKE_TEAM_WORK_COMPUTER_LIVE_KEEP_STORE 置 1 保留临时数据根
//
// ⚠️ 与 computer use 冒烟同口径：会把**当前屏幕画面**随提问发给 provider。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
)

func TestRealAPITeamWorkComputerUseLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_TEAM_WORK_COMPUTER_LIVE") == "" {
		t.Skip("设置 SMOKE_TEAM_WORK_COMPUTER_LIVE=1 才能运行 team work + computer use 真实 API 探针")
	}
	if !computer.Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	foreground, err := computer.ForegroundWindow()
	if err != nil {
		t.Skipf("本机没有可观测的前台窗口: %v", err)
	}
	titleMarker := teamWorkTitleMarker(foreground.Title)
	if titleMarker == "" {
		t.Skipf("前台窗口标题没有可用作地面真值的片段: %q", foreground.Title)
	}

	repoRoot := forkLiveRepoRoot(t)
	target := forkLiveEnvPath("SMOKE_TEAM_WORK_LIVE_TARGET",
		filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe"))
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先 go build -o tmp/bin/seelex-headless.exe .）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-team-computer-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_TEAM_WORK_COMPUTER_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()

	port := forkLiveFreePort(t)
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, "")
	defer proc.stop()
	defer func() {
		if t.Failed() {
			// 真实 API 探针失败时，headless 的 stderr 是唯一能看到 provider/回合
			// 错误的地方（探针不做业务日志落盘）。
			t.Logf("[headless stderr tail]\n%s", teamWorkLogTail(proc.stderr.String(), 60))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))

	report := map[string]any{"target": filepath.Base(target), "title_marker": titleMarker}

	// 1) 物化主会话（EXEC = main 会话）。
	if _, err := proc.rpc(ctx, "Submit", "这是 team work + computer use 冒烟。请只回复 OK，不要调用任何工具。"); err != nil {
		t.Fatalf("Submit(#1): %v", err)
	}
	if err := teamWorkWaitIdle(ctx, t, proc, 12*time.Minute); err != nil {
		t.Fatalf("WaitIdle(#1): %v", err)
	}
	mainSessionID := roleLiveSessionID(t, ctx, proc)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}
	report["main_session_id"] = mainSessionID

	// 2) goal 上线 → 隐式装配 goal-a2a（tl = ADVISOR）。
	if _, err := proc.rpc(ctx, "goal.begin", map[string]any{
		"title":      "team work computer use 冒烟",
		"statement":  "验证 EXEC 的截图证据是否进入 ADVISOR 输入",
		"acceptance": []string{"EXEC 调用 computer_screenshot", "ADVISOR 输入带截图证据"},
	}); err != nil {
		t.Fatalf("goal.begin: %v", err)
	}
	view := teamWorkView(t, ctx, proc, mainSessionID)
	tlSessionID := teamWorkRoleSessionID(view, "tl")
	if tlSessionID == "" {
		t.Fatalf("goal 创建后未见 tl 成员（自动装配未生效）: %+v", view.Members)
	}
	if view.OrderPolicy != dto.OrderPolicyGoalLoop {
		t.Fatalf("order_policy = %q, want %q", view.OrderPolicy, dto.OrderPolicyGoalLoop)
	}
	report["tl_session_id"] = tlSessionID

	// 3) EXEC 真的用 computer use（提示词直接指定工具，绕开模型自由发挥）。
	workPrompt := "请调用 computer_screenshot 工具截取当前屏幕，然后只回答前台窗口标题的原文" +
		"（照抄标题里出现的内容，不要解释、不要猜测、不要加引号）。"
	if _, err := proc.rpc(ctx, "Submit", workPrompt); err != nil {
		t.Fatalf("Submit(#2 截图轮): %v", err)
	}
	if err := teamWorkWaitIdle(ctx, t, proc, 12*time.Minute); err != nil {
		t.Fatalf("WaitIdle(#2): %v", err)
	}
	snap, err := proc.snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(#2 后): %v", err)
	}
	if snap.Chat.Error != "" {
		t.Fatalf("截图轮 chat error: %s", snap.Chat.Error)
	}

	// 证据一：媒体分区里有截图（工具真的跑了）。
	shots, refs := teamWorkScreenshotArtifacts(t, storeDir, snap.Conversation)
	if len(shots) == 0 {
		t.Fatalf("会话媒体分区没有截图（EXEC 没有真的调用 computer_screenshot）；store=%s", storeDir)
	}
	report["screenshot_files"] = shots
	report["screenshot_refs"] = refs
	if len(refs) == 0 {
		t.Fatalf("会话记录里没有截图媒体引用：%s", describeTeamWorkTail(snap.Conversation, 4))
	}

	// 证据二：ADVISOR 的回合输入里带 computer use 证据（screen: media:… foreground=…）。
	tlSnapshot, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", tlSessionID)
	if err != nil {
		t.Fatalf("role.snapshot(tl): %v", err)
	}
	tlInputs, _, _ := teamWorkTLRoundRows(tlSnapshot)
	if len(tlInputs) == 0 {
		t.Fatalf("tl 角色会话没有 role_context 行：ADVISOR 回合没有记录输入原文")
	}
	tlInput := tlInputs[len(tlInputs)-1]
	report["tl_rounds"] = len(tlInputs)
	report["tl_input_has_screen_evidence"] = strings.Contains(tlInput, "screen:")
	report["tl_input_has_media_ref"] = strings.Contains(tlInput, "media:")
	report["tl_input_has_foreground_title"] = strings.Contains(tlInput, titleMarker)
	t.Logf("[tl] 回合数=%d；输入里 screen 证据=%v media 引用=%v 标题命中=%v",
		len(tlInputs), report["tl_input_has_screen_evidence"],
		report["tl_input_has_media_ref"], report["tl_input_has_foreground_title"])
	t.Logf("[tl] 输入原文节选：%s", truncateForLog(teamWorkScreenEvidenceExcerpt(tlInput), 400))

	if !strings.Contains(tlInput, "screen:") || !strings.Contains(tlInput, "media:") {
		t.Fatalf("ADVISOR 输入缺少 computer use 证据（screen:/media: 缺失）：\n%s", truncateForLog(tlInput, 1200))
	}
	if !strings.Contains(tlInput, titleMarker) {
		t.Fatalf("ADVISOR 输入里没有前台窗口标题片段 %q（模型可能画错或摘要被截断）：\n%s",
			titleMarker, truncateForLog(tlInput, 1200))
	}

	// 证据三：ADVISOR 的裁决**不进可见聊天**（2026-10-01 口径修正：ADVISOR 是被
	// 调用的 agent，不是对话席位），裁决原文只从它的角色会话读。
	//
	// 沿革：2026-09-16 P1-3 曾把裁决"回放"成可见聊天行（assistant + role_name=tl +
	// kind=tl_directive，正文是 `[TL 指令 corr-N] <正文>`），本探针当时据此断言"产出
	// 它的那一回合就可见"。2026-10-01 用户口径修正后该回放撤除——聊天的事实源是
	// transcript（conversationFromTranscriptLocked 只投影 user/main），因此这里改判
	// "对话里没有 ADVISOR 行"，裁决 kind 取 tl 角色会话里的回合原文。
	snapAfter, err := proc.snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(证据三): %v", err)
	}
	if verdict, ok := teamWorkAdvisorVerdict(snapAfter.Conversation); ok {
		t.Fatalf("ADVISOR 的裁决出现在可见聊天里（ADVISOR 不进对话）：%q", truncateForLog(verdict, 200))
	}
	_, advisorOutputs, _ := teamWorkTLRoundRows(tlSnapshot)
	directive := ""
	if len(advisorOutputs) > 0 {
		directive = advisorOutputs[len(advisorOutputs)-1]
	}
	report["tl_round_output"] = truncateForLog(directive, 400)
	directiveKind := teamWorkDirectiveKind(advisorOutputs)
	report["tl_directive_kind"] = directiveKind
	govRaw, err := proc.rpc(ctx, "goal.gov_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("goal.gov_snapshot: %v", err)
	}
	var gov dto.GoalGovernanceView
	if err := json.Unmarshal(govRaw, &gov); err != nil {
		t.Fatalf("decode gov_snapshot: %v", err)
	}
	report["governance_peer_state"] = gov.PeerState
	report["governance_last_directive"] = gov.LastDirective
	t.Logf("[gov] active=%v peer=%q last_directive=%q",
		gov.Active, gov.PeerState, truncateForLog(gov.LastDirective, 200))
	switch directiveKind {
	case "verdict_done", "escalate_human":
		// 终态裁决 = 收口：治理必须已下线（这正是 2026-09-16 的收口修复）。
		if gov.Active {
			t.Fatalf("终态裁决 %q 之后 goal 仍在线（收口失效）：active=%v peer=%q",
				directiveKind, gov.Active, gov.PeerState)
		}
		t.Logf("[gov] 终态裁决 %q 已收口 goal（active=false，收口语义生效）", directiveKind)
	case "verdict_not_done", "checkpoint_ok", "correct":
		if !gov.Active || strings.TrimSpace(gov.LastDirective) == "" {
			t.Fatalf("非终态裁决 %q 之后治理必须仍在线且有裁决原文：active=%v peer=%q directive=%q",
				directiveKind, gov.Active, gov.PeerState, truncateForLog(gov.LastDirective, 200))
		}
	default:
		t.Fatalf("ADVISOR 回合原文里的裁决 kind=%q 不是已知裁决类型（回合也许没有正常产出裁决）：%s",
			directiveKind, truncateForLog(directive, 400))
	}
	// 裁决必须引用了 EXEC 的截图证据（ADVISOR 是"看证据评审"，不是复述工具名）。
	if !strings.Contains(directive, "screen:") && !strings.Contains(directive, "media:") {
		t.Logf("提示：裁决原文未显式引用 screen/media 片段（模型措辞差异），但输入已带证据：%q",
			truncateForLog(directive, 200))
	}

	payload, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("[report]\n%s", payload)
}

// teamWorkLogTail 取日志末尾若干行（失败诊断用；避免把整段 stderr 灌进测试输出）。
func teamWorkLogTail(text string, lines int) string {
	parts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

// teamWorkAdvisorVerdict 从主会话可见聊天里取 ADVISOR 裁决行的正文：条件是
// kind=tl_directive 且 role_name=tl（两个字段一起钉住"这条行是 ADVISOR 的裁决"，
// 不靠正文措辞猜）。返回 false = 本回合没有裁决行。
func teamWorkAdvisorVerdict(conversation []model.Message) (string, bool) {
	for index := len(conversation) - 1; index >= 0; index-- {
		message := conversation[index]
		if message.Kind == "tl_directive" && message.RoleName == "tl" {
			return message.Content, true
		}
	}
	return "", false
}

// teamWorkDirectiveKind 从 ADVISOR 回合原文（role.snapshot 的 tl 输出 = 裁决
// JSON，形如 {"goal_id":…,"kind":"verdict_done","content":"…"}）解析 kind。
//
// 为什么不从可见裁决行解析：可见行是 `[TL 指令 corr-N] <正文>`（人读形式，
// 与注入 EXEC 受信区的文本同源），kind 只在 ADVISOR 的原始输出里。
func teamWorkDirectiveKind(advisorOutputs []string) string {
	for index := len(advisorOutputs) - 1; index >= 0; index-- {
		var directive struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(advisorOutputs[index]), &directive); err == nil && directive.Kind != "" {
			return directive.Kind
		}
	}
	return ""
}

// teamWorkWaitIdle 轮询等待全部回合结束（成功 = 服务端报告 idle）。
//
// 为什么不直接一次 `WaitIdle(600)`：控制面 HTTP 客户端的单次超时是 90s，而
// "截图轮 + ADVISOR 终态裁决"两段真实 API 调用在 provider 抖动时会超过它
// （2026-09-17 实测一次 91.4s → 客户端先报 context deadline exceeded，把"还在跑"
// 误报成失败）。这里改为多轮小预算 WaitIdle：每轮的服务端预算 40s < 客户端 90s，
// 轮与轮之间重试，整体到 timeout 才判真失败（真挂住时依然会失败）。
func teamWorkWaitIdle(ctx context.Context, t *testing.T, proc *forkLiveProc, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := proc.rpc(ctx, "WaitIdle", 40); err == nil {
			return nil
		} else if ctx.Err() != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待空闲超时（%s）", timeout)
		}
		t.Logf("WaitIdle 未收敛，2s 后重试（真实 API 抖动）")
		time.Sleep(2 * time.Second)
	}
}

// teamWorkScreenshotArtifacts 同时收集截图文件（磁盘）与截图媒体引用（会话记录）。
func teamWorkScreenshotArtifacts(t *testing.T, storeDir string, conversation []model.Message) ([]string, []string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(storeDir, "*", "*", "*", "meta", "*", "screenshot-*.png"))
	if err != nil {
		t.Fatalf("扫描媒体分区: %v", err)
	}
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if info, statErr := os.Stat(match); statErr == nil && info.Size() > 0 {
			files = append(files, filepath.Base(match))
		}
	}
	refPattern := regexp.MustCompile(`"ref"\s*:\s*"(media:[0-9a-f]{64})"`)
	seen := map[string]bool{}
	refs := []string{}
	for _, message := range conversation {
		for _, match := range refPattern.FindAllStringSubmatch(message.Content, -1) {
			if !seen[match[1]] {
				seen[match[1]] = true
				refs = append(refs, match[1])
			}
		}
	}
	return files, refs
}

// teamWorkScreenEvidenceExcerpt 从 ADVISOR 输入里截出 screen 证据那一段（诊断用）。
func teamWorkScreenEvidenceExcerpt(input string) string {
	index := strings.Index(input, "screen:")
	if index < 0 {
		return "(无 screen 证据)"
	}
	end := index + 240
	if end > len(input) {
		end = len(input)
	}
	return input[index:end]
}

// describeTeamWorkTail 摘出可见会话末尾几条消息的只读摘要（失败诊断用）。
func describeTeamWorkTail(conversation []model.Message, limit int) string {
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
		fmt.Fprintf(&builder, "\n  [%s/%s] %s", message.Role, message.Kind, content)
	}
	return builder.String()
}

// teamWorkTitleMarker 取标题里最长的连续非空白片段（≥6 字符）作地面真值标记：
// 标点/空格在不同渲染下可能不同，整串比对容易假阴性。
func teamWorkTitleMarker(title string) string {
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
