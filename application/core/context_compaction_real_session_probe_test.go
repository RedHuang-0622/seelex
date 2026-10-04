//go:build realprobe

package core

// 真实会话记录上实跑「第一次压缩触发点 → 被动压缩链路」。
//
// 为什么要有它：压缩链路此前只在**合成夹具**上跑过（每轮 N 个 ASCII 字符，token
// 量可算可控）。合成夹具答不了三个现场问题——真实会话里第一次压缩在哪一步被触发、
// 触发时内存里的 transcript 是什么、折出的区间与**真实帧**对不对得上——而它们正是
// 判断链路对不对的现场事实。
//
// 本探针用**真实的会话存储**做输入、**真实的装配层压缩链路**做执行：
//
//	① 从真实存储读出该会话的压缩帧链，取第一帧（第一次压缩）的位置锚点
//	   （message id + 事件 seq 区间）。位置事实来自 `TranscriptPrefixRange`，
//	   它的 EventFrom 是**被折区间的首个事件**，因此这一条同时钉住了"那次触发时
//	   内存 transcript 从哪里开始"；
//	② 截取触发点之前的上下文：真实事件行**原样注入**（ReasoningContent / ToolCalls /
//	   ResultRef / TokenCount / 真实 task_id 都照搬），序号基线抬到真实起点，因此
//	   注入后的 seq 与真实存储逐行相等；
//	③ 走**被动**压缩链路（自动路径 `PrepareExecutionContext`：判据命中才折，不是
//	   /compact 那种显式路径），把这一轮的门禁数字、读数闸输入、推帧区间、压缩记录
//	   （成功记录或失败痕）全部打出来，并与真实帧区间逐字对照。
//
// 口径（如实）：读数闸（前缀重放厚摘要）是**替身**——探针不调真模型，回执由夹具给。
// 走真实代码的是：判据与阈值、保留窗口决策、区间推导、推帧请求、记录/失败痕的生成
// 与形状、快照可见面。替身只替"这一次模型回读答了什么"。
//
// 只读铁律（MEMORY.md）：真实会话目录**只读拷贝**到 t.TempDir() 再打开，原目录一个
// 字节都不写。
//
// 运行：
//
//	$env:SEELEX_REAL_COMPACT_PROBE='1'
//	go test -tags realprobe ./application/core/ -run TestRealSessionCompactionChainProbe -v -count=1
//
// 环境变量（都有缺省值）：
//
//	SEELEX_REAL_STORE_ROOT   真实数据根（缺省 dist/seelex-gui-dev/.seelex）
//	SEELEX_REAL_PROJECT      项目 id（缺省 project-2e6431eb439414d0）
//	SEELEX_REAL_SESSION      会话 id（缺省 session-3a5c432dda14d33b）
//	SEELEX_REAL_CONFIG       seelex.yaml（缺省 dist/seelex-gui-dev/config/seelex.yaml）
//	SEELEX_REAL_WINDOW       账号上下文窗口（缺省 200000）
//	SEELEX_REAL_COMPACT_OUT  报告落盘路径（缺省 _tmp/real-compaction-probe-report.md）

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const (
	realProbeGate          = "SEELEX_REAL_COMPACT_PROBE"
	realProbeDefaultRoot   = "dist/seelex-gui-dev/.seelex"
	realProbeDefaultProj   = "project-2e6431eb439414d0"
	realProbeDefaultSess   = "session-3a5c432dda14d33b"
	realProbeDefaultConfig = "dist/seelex-gui-dev/config/seelex.yaml"
	realProbeDefaultWindow = 200_000
	realProbeDefaultOutput = 8_192
)

func realProbeEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func realProbeEnvInt(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

// realProbeRepoRoot 从测试的工作目录（go test 把 CWD 设在包目录）向上找到仓库根。
// 探针的缺省路径都是相对仓库根的（`dist/seelex-gui-dev/.seelex` 等），不解析这一层
// 就会在 application/core 下找 dist/——找不到真实数据，报一个与事实无关的错。
func realProbeRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上找不到仓库根（go.mod）", dir)
		}
		dir = parent
	}
}

// realProbeResolve 把相对路径解析到仓库根下（绝对路径原样保留）。
func realProbeResolve(t *testing.T, path string) string {
	t.Helper()
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(realProbeRepoRoot(t), filepath.FromSlash(path))
}

// realProbeFrameRow 是 compact.jsonl 的一行（帧通道的持久原样）。这里直接读文件
// 而不是只走 Router 读面：Router 的 CompactFramesWorkspace 不填 EventFrom/EventTo
// （帧行的 message_from_seq/message_to_seq 是 head 与帧行共有的水位字段），而本探针
// 恰恰要靠事件序号切上下文。
type realProbeFrameRow struct {
	FrameID        string    `json:"frame_id"`
	MessageFrom    string    `json:"message_from"`
	MessageTo      string    `json:"message_to"`
	MessageFromSeq uint64    `json:"message_from_seq"`
	MessageToSeq   uint64    `json:"message_to_seq"`
	BoundaryStatus string    `json:"boundary_status"`
	Summary        string    `json:"summary"`
	CompressedAt   time.Time `json:"compressed_at"`
}

type realProbeFacts struct {
	root       string
	projectDir string
	sessionDir string
	// projectID/sessionID 是 Router 的键（存储目录名是 hash(键)，反推不出来）：
	// 会话 id 从会话自己的 message head（payload.session_id）读回，项目 id 从
	// workspace_index.json 的 bindings（会话 → 项目）读回。
	projectID      string
	sessionID      string
	sourceDir      string
	events         []model.TranscriptEvent
	frames         []realProbeFrameRow
	channelFrames  []sessionstore.CompactFrame
	records        []json.RawMessage
	recordsHandled bool
}

func realProbeCopyTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("读真实会话目录 %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("建临时目录 %s: %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			realProbeCopyTree(t, srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("只读拷贝 %s: %v", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			t.Fatalf("写入临时副本 %s: %v", dstPath, err)
		}
	}
}

func realProbeTranscriptEvent(row sessionstore.Event) model.TranscriptEvent {
	event := model.TranscriptEvent{
		Seq: row.Seq, TaskID: row.TaskID, MessageID: row.MessageID, Kind: row.Kind,
		Role: row.Role, ReasoningContent: row.ReasoningContent, Content: row.Content,
		ProviderContent: row.ProviderContent, ToolCallID: row.ToolCallID, Name: row.Name,
		ResultRef: row.ResultRef, TokenCount: row.TokenCount, CreatedAt: row.CreatedAt,
		WireMaterial: row.WireMaterial, RoleName: row.RoleName, RoleSessionID: row.RoleSessionID,
		RoundID: row.RoundID, UnitSeq: row.UnitSeq,
	}
	for _, call := range row.ToolCalls {
		event.ToolCalls = append(event.ToolCalls, model.TranscriptToolCall{
			ID: call.ID, Name: call.Name, Arguments: call.Arguments,
		})
	}
	return event
}

// realProbeSessionID 从会话自己的 message head 读回 Router 的会话键：磁盘目录名是
// `session-<hash(会话 id)>`，哈希反推不出来，而 head 的 payload.session_id 就是键。
func realProbeSessionID(t *testing.T, sessionDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sessionDir, "metadata", "message.json"))
	if err != nil {
		t.Fatalf("读 message head %s: %v", sessionDir, err)
	}
	var head struct {
		Payload struct {
			SessionID string `json:"session_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatalf("解 message head: %v", err)
	}
	if strings.TrimSpace(head.Payload.SessionID) == "" {
		t.Fatalf("message head 没有 session_id：%s", sessionDir)
	}
	return head.Payload.SessionID
}

// realProbeProjectID 从 workspace_index.json 的 bindings（会话 → 项目）读回项目键。
// 存储侧的项目目录名同样是哈希（`project-<hash(项目 id)>`），而 bindings 是唯一把
// 两者对上的持久事实。
func realProbeProjectID(t *testing.T, root, sessionID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "workspace_index.json"))
	if err != nil {
		t.Fatalf("读 workspace_index.json（项目绑定）: %v", err)
	}
	var index struct {
		Bindings map[string]string `json:"bindings"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("解 workspace_index.json: %v", err)
	}
	projectID := strings.TrimSpace(index.Bindings[sessionID])
	if projectID == "" {
		t.Fatalf("workspace_index.json 里没有会话 %q 的项目绑定（bindings=%d 项）", sessionID, len(index.Bindings))
	}
	return projectID
}

// realProbeLoadFacts 只读拷贝真实会话目录到临时根，再按生产读面把事实读回来。
func realProbeLoadFacts(t *testing.T) *realProbeFacts {
	t.Helper()
	facts := &realProbeFacts{
		root:       realProbeResolve(t, realProbeEnv("SEELEX_REAL_STORE_ROOT", realProbeDefaultRoot)),
		projectDir: realProbeEnv("SEELEX_REAL_PROJECT", realProbeDefaultProj),
		sessionDir: realProbeEnv("SEELEX_REAL_SESSION", realProbeDefaultSess),
	}
	facts.sourceDir = filepath.Join(facts.root, "sessions-json", facts.projectDir, facts.sessionDir)
	if _, err := os.Stat(facts.sourceDir); err != nil {
		t.Fatalf("真实会话目录不可用 %s: %v", facts.sourceDir, err)
	}
	tmp := t.TempDir()
	copied := filepath.Join(tmp, "sessions-json", facts.projectDir, facts.sessionDir)
	realProbeCopyTree(t, facts.sourceDir, copied)
	facts.sessionID = realProbeSessionID(t, copied)
	facts.projectID = realProbeProjectID(t, facts.root, facts.sessionID)

	// 不拷 session-storage.json：Router 的既定缺省就是 json 后端 + <root>/sessions-json。
	router, err := sessionstore.NewRouter(filepath.Join(tmp, "session-storage.json"), tmp)
	if err != nil {
		t.Fatalf("NewRouter（临时副本）: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	rows, err := router.LoadEventRangeWorkspace(facts.projectID, facts.sessionID, 0, 0)
	if err != nil {
		t.Fatalf("LoadEventRangeWorkspace: %v", err)
	}
	for _, row := range rows {
		facts.events = append(facts.events, realProbeTranscriptEvent(row))
	}
	sort.Slice(facts.events, func(i, j int) bool { return facts.events[i].Seq < facts.events[j].Seq })
	if len(facts.events) == 0 {
		t.Fatalf("真实会话事件流为空 %s", facts.sourceDir)
	}
	channelFrames, handled, err := router.CompactFramesWorkspace(facts.projectID, facts.sessionID)
	if err != nil {
		t.Fatalf("CompactFramesWorkspace: %v", err)
	}
	if !handled {
		t.Fatalf("存储布局不是 v8 JSON：这条探针的读数面不适用")
	}
	facts.channelFrames = channelFrames
	facts.records, facts.recordsHandled, err = router.LoadCompactionRecordsWorkspace(facts.projectID, facts.sessionID)
	if err != nil {
		t.Fatalf("LoadCompactionRecordsWorkspace: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(copied, "compact.jsonl"))
	if err != nil {
		t.Fatalf("读帧通道 compact.jsonl: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame realProbeFrameRow
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("解帧行: %v (%s)", err, line)
		}
		facts.frames = append(facts.frames, frame)
	}
	if len(facts.frames) == 0 {
		t.Fatalf("真实会话没有压缩帧（探针前提：第一次压缩必须已经发生过）")
	}
	return facts
}

// realProbeWindowConfig 读真实 seelex.yaml 的 window 段（缺省档见探针文件头）。
func realProbeWindowConfig(t *testing.T) WindowConfig {
	path := realProbeResolve(t, realProbeEnv("SEELEX_REAL_CONFIG", realProbeDefaultConfig))
	config, err := LoadWindowConfig(path)
	if err != nil {
		t.Logf("真实 window 配置不可读（%s）：%v；退回默认档", path, err)
		return WindowConfig{}
	}
	return config
}

func realProbeWindow(t *testing.T) int {
	return realProbeEnvInt("SEELEX_REAL_WINDOW", realProbeDefaultWindow)
}

// realProbeArm 是一条实跑臂：注入哪一段真实事件、读数闸这次答什么。
type realProbeArm struct {
	name string
	// fromSeq/toSeq 是注入的真实事件区间（含端点；toSeq=0 = 到会话末尾）。
	fromSeq, toSeq uint64
	requestID      string
	// readbackSource 决定这次压缩能不能拿到模型读后感：
	// replay = 拿到（压缩成功）；local = 没拿到（只留失败痕）。
	readbackSource string
	readbackNote   string
}

type realProbeOutcome struct {
	arm             realProbeArm
	injected        int
	firstSeq        uint64
	lastSeq         uint64
	firstMessageID  string
	lastMessageID   string
	storedTokens    int
	sessionID       string
	prepareErr      error
	judgeDetail     string
	replaceDetail   string
	finalState      string
	finalOutcome    string
	pushes          []context_runtime.CompactionIndexRequest
	records         []model.ContextCompaction
	snapshotRecords []model.ContextCompaction
	readbackInput   context_runtime.CompactionIndexRequest
	readbackCalled  bool
}

func realProbeSelectEvents(facts *realProbeFacts, fromSeq, toSeq uint64) []model.TranscriptEvent {
	selected := make([]model.TranscriptEvent, 0, len(facts.events))
	for _, event := range facts.events {
		if event.Seq < fromSeq {
			continue
		}
		if toSeq != 0 && event.Seq > toSeq {
			continue
		}
		selected = append(selected, event)
	}
	return selected
}

// realProbeRunArm 在**被动**路径上跑一轮：自动装配（判据命中才折）。
func realProbeRunArm(t *testing.T, facts *realProbeFacts, arm realProbeArm) realProbeOutcome {
	t.Helper()
	applyWindowConfig(t, realProbeWindowConfig(t))
	events := realProbeSelectEvents(facts, arm.fromSeq, arm.toSeq)
	outcome := realProbeOutcome{arm: arm, injected: len(events)}
	if len(events) == 0 {
		t.Fatalf("%s：注入区间 [%d..%d] 选不出任何真实事件", arm.name, arm.fromSeq, arm.toSeq)
	}
	outcome.firstSeq, outcome.lastSeq = events[0].Seq, events[len(events)-1].Seq
	outcome.firstMessageID, outcome.lastMessageID = events[0].MessageID, events[len(events)-1].MessageID
	for _, event := range events {
		outcome.storedTokens += event.TokenCount
	}

	stubSummary := "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n（探针替身：这一次模型读后感的占位正文）"
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-real-probe-" + arm.requestID,
		Summary:       stubSummary,
		SummarySource: arm.readbackSource,
	}}
	host := &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{
				fakeRuntime: &fakeRuntime{}, window: realProbeWindow(t), output: realProbeDefaultOutput,
			},
			recorder: recorder,
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       stubSummary,
			SummarySource: arm.readbackSource,
			SummaryNote:   arm.readbackNote,
		},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(host))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: arm.requestID}
	service.components.tasks.BeginTask(arm.requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.Snapshot().Session.ID
	outcome.sessionID = sessionID
	// 序号基线抬到真实起点：注入后的 seq 与真实存储逐行相等（区间事实才可比）。
	if first := events[0].Seq; first > 1 {
		service.components.tasks.SeedTranscriptSeqFor(sessionID, first-1)
	}
	subscription, err := service.SubscribeSession(sessionID, 1024)
	if err != nil {
		t.Fatalf("%s：SubscribeSession: %v", arm.name, err)
	}
	defer subscription.Close()

	service.ViewMu.Lock()
	for _, event := range events {
		service.components.tasks.AppendTranscriptEventLocked(event)
	}
	service.ViewMu.Unlock()

	_, err = service.components.context.PrepareExecutionContextFor(sessionID, arm.requestID, "continue")
	outcome.prepareErr = err

	for _, frame := range drainCompactionProgress(t, subscription) {
		switch frame.event.Gate {
		case context_runtime.CompactionGateJudge:
			outcome.judgeDetail = frame.event.Detail
		case context_runtime.CompactionGateReplace:
			outcome.replaceDetail = frame.event.Detail
		}
		if frame.event.State == event.CompactionProgressDone || frame.event.State == event.CompactionProgressFailed {
			outcome.finalState = string(frame.event.State)
			outcome.finalOutcome = frame.event.Outcome
		}
	}
	requests, sessions := recorder.snapshot()
	if len(requests) != 0 {
		if sessions[0] != sessionID {
			t.Fatalf("%s：推帧会话 = %q，want %q", arm.name, sessions[0], sessionID)
		}
		outcome.pushes = requests
	}
	if len(host.lastReadback.Overflow) != 0 || len(host.lastReadback.ReplayHistory) != 0 {
		outcome.readbackCalled = true
		outcome.readbackInput = host.lastReadback
	}
	outcome.records = compactionRecordsOf(t, service)
	if snapshot, snapshotErr := service.SnapshotOf(sessionID); snapshotErr == nil && snapshot.Task != nil {
		outcome.snapshotRecords = append([]ContextCompaction(nil), snapshot.Task.ContextCompactions...)
	}
	return outcome
}

func realProbeDescribeRecords(records []model.ContextCompaction) string {
	if len(records) == 0 {
		return "（无）"
	}
	lines := make([]string, 0, len(records))
	for index, record := range records {
		if record.Failed {
			lines = append(lines, fmt.Sprintf(
				"  [%d] 失败痕: version=%d origin=%s failed=true tokens=%d\n      note=%q",
				index, record.Version, record.Origin, record.EstimatedTokens, record.Note))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"  [%d] 成功记录: version=%d origin=%s 区间 seq[%d..%d] message[%s..%s]\n      帧标识=%s 帧正文 ref=%s tokens=%d",
			index, record.Version, record.Origin, record.EventFrom, record.EventTo,
			record.MessageFrom, record.MessageTo, record.SegmentID, record.FrameRef, record.EstimatedTokens))
	}
	return strings.Join(lines, "\n")
}

func realProbeDumpReport(t *testing.T, text string) {
	t.Helper()
	path := os.Getenv("SEELEX_REAL_COMPACT_OUT")
	if path == "" {
		path = filepath.Join("_tmp", "real-compaction-probe-report.md")
	}
	path = realProbeResolve(t, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("报告落盘失败（建目录）：%v", err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Logf("报告落盘失败：%v", err)
		return
	}
	t.Logf("现场已写入 %s", path)
}

// TestRealSessionCompactionChainProbe 真实会话记录上的「第一次压缩触发点 → 被动压缩
// 链路」实跑。默认跳过（需要 build tag realprobe + SEELEX_REAL_COMPACT_PROBE=1）。
func TestRealSessionCompactionChainProbe(t *testing.T) {
	if os.Getenv(realProbeGate) == "" {
		t.Skip("set SEELEX_REAL_COMPACT_PROBE=1 (with -tags realprobe) to run the real-session compaction probe")
	}
	facts := realProbeLoadFacts(t)
	first := facts.frames[0]
	t.Logf("真实会话 %s/%s：事件 %d 行（seq %d..%d）、压缩帧 %d 帧、压缩记录 %d 条（handled=%v）",
		facts.projectID, facts.sessionID, len(facts.events), facts.events[0].Seq,
		facts.events[len(facts.events)-1].Seq, len(facts.frames), len(facts.records), facts.recordsHandled)
	t.Logf("第一次压缩帧: %s  message[%s..%s] seq[%d..%d] at=%s",
		first.FrameID, first.MessageFrom, first.MessageTo,
		first.MessageFromSeq, first.MessageToSeq, first.CompressedAt.Format(time.RFC3339))

	// ── 上下文剖面（落盘 token_count）+ 闸门位置 ────────────────────────────
	storedTotal := 0
	cumulative := map[uint64]int{}
	positionOf := map[uint64]string{}
	running := 0
	for _, event := range facts.events {
		running += event.TokenCount
		storedTotal += event.TokenCount
		cumulative[event.Seq] = running
		positionOf[event.Seq] = event.MessageID
	}
	beforeFold, foldedTokens := 0, 0
	for _, event := range facts.events {
		switch {
		case event.Seq < first.MessageFromSeq:
			beforeFold = cumulative[event.Seq]
		case event.Seq <= first.MessageToSeq:
			foldedTokens += event.TokenCount
		}
	}
	// 闸门数字取自**真实配置**（window 段 + 账号窗口），不写死：判据量口径见
	// task_context.ContextBudgetFor（budget = 窗口 − 输出预留 − 窗口 ÷ 安全除数）。
	budget := task_context.ContextBudgetFor(runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: realProbeWindow(t), output: realProbeDefaultOutput,
	})
	crossing := uint64(0)
	for _, event := range facts.events {
		if cumulative[event.Seq] >= budget.HardThreshold {
			crossing = event.Seq
			break
		}
	}
	t.Logf("真实闸门: window=%d budget=%d hard=%d target=%d", budget.Window, budget.Budget,
		budget.HardThreshold, budget.TargetAfterCompaction)
	t.Logf("落盘 token_count 剖面: 合计=%d fold 之前(%d..%d)=%d 被折(%d..%d)=%d",
		storedTotal, facts.events[0].Seq, first.MessageFromSeq-1, beforeFold,
		first.MessageFromSeq, first.MessageToSeq, foldedTokens)
	if crossing != 0 {
		t.Logf("累积首次越过硬阈值(%d)的位置: seq=%d message=%s", budget.HardThreshold, crossing, positionOf[crossing])
	}

	// ── 帧里的消息号 vs 落盘行的消息号（自检：两套 id 空间）────────────────
	rowAt := func(seq uint64) model.TranscriptEvent {
		for _, event := range facts.events {
			if event.Seq == seq {
				return event
			}
		}
		return model.TranscriptEvent{}
	}
	fromRow, toRow := rowAt(first.MessageFromSeq), rowAt(first.MessageToSeq)
	lastIDBeforeTo := ""
	for _, event := range facts.events {
		if event.Seq < first.MessageToSeq && event.MessageID != "" {
			lastIDBeforeTo = event.MessageID
		}
	}
	t.Logf("帧消息号自检: seq %d 帧=%q 落盘=%q ｜ seq %d 帧=%q 落盘=%q",
		first.MessageFromSeq, first.MessageFrom, fromRow.MessageID,
		first.MessageToSeq, first.MessageTo, toRow.MessageID)

	arms := []realProbeArm{{
		// 真实触发点：帧的 EventFrom=c… 被折区间的首个事件对应的内存起点
		// 就是这里——读尾重建出来的 transcript（见报告 §①）。
		name: "① 真实触发点：读尾重建（帧 EventFrom..会话末尾）", fromSeq: first.MessageFromSeq,
		requestID: "task-real-trigger", readbackSource: context_runtime.CompactionSummarySourceReplay,
	},
		{
			name: "② 全量上下文（会话开头..末尾）", fromSeq: facts.events[0].Seq,
			requestID: "task-real-full", readbackSource: context_runtime.CompactionSummarySourceReplay,
		},
		{
			name: "③ 只取被折区间（帧区间本身）", fromSeq: first.MessageFromSeq, toSeq: first.MessageToSeq,
			requestID: "task-real-folded", readbackSource: context_runtime.CompactionSummarySourceReplay,
		},
		{
			name: "④ 真实触发点 + 读数闸拿不到读后感", fromSeq: first.MessageFromSeq,
			requestID:      "task-real-trigger-nosummary",
			readbackSource: context_runtime.CompactionSummarySourceLocal,
			readbackNote:   "compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused",
		},
	}

	outcomes := make([]realProbeOutcome, 0, len(arms))
	for _, arm := range arms {
		outcome := realProbeRunArm(t, facts, arm)
		outcomes = append(outcomes, outcome)
	}

	report := &strings.Builder{}
	fmt.Fprintf(report, "# 真实会话上的压缩链路实跑现场\n\n")
	fmt.Fprintf(report, "- 真实数据根：`%s`（**只读拷贝**到临时目录后打开，原目录未写）\n", facts.root)
	fmt.Fprintf(report, "- 会话目录：`%s/%s` → 存储键 project=`%s` session=`%s`\n",
		facts.projectDir, facts.sessionDir, facts.projectID, facts.sessionID)
	fmt.Fprintf(report, "- 事件 %d 行（seq %d..%d）；落盘 token_count 合计 %d\n",
		len(facts.events), facts.events[0].Seq,
		facts.events[len(facts.events)-1].Seq, storedTotal)
	fmt.Fprintf(report, "- 压缩帧 %d 帧；压缩记录 %d 条（通道 handled=%v）\n\n",
		len(facts.frames), len(facts.records), facts.recordsHandled)

	fmt.Fprintf(report, "## ① 第一次压缩帧的位置锚点\n\n")
	fmt.Fprintf(report, "| 字段 | 值 |\n|---|---|\n")
	fmt.Fprintf(report, "| frame_id | `%s` |\n", first.FrameID)
	fmt.Fprintf(report, "| 被折区间（消息号） | `%s` → `%s` |\n", first.MessageFrom, first.MessageTo)
	fmt.Fprintf(report, "| 被折区间（事件 seq） | %d → %d |\n", first.MessageFromSeq, first.MessageToSeq)
	fmt.Fprintf(report, "| boundary_status | %s |\n", first.BoundaryStatus)
	fmt.Fprintf(report, "| compressed_at | %s |\n", first.CompressedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(report, "| 帧摘要长度 | %d 字符 |\n", len(first.Summary))
	for _, channelFrame := range facts.channelFrames {
		fmt.Fprintf(report, "\nRouter 正式读面（CompactFramesWorkspace）对同一份数据的读数：segment_id=`%s` message_from=`%s` message_to=`%s` at=%s\n",
			channelFrame.SegmentID, channelFrame.MessageFrom, channelFrame.MessageTo,
			channelFrame.CompressedAt.Format(time.RFC3339Nano))
	}
	fmt.Fprintf(report, "\n真实闸门（window 段 + 账号窗口）：window=%d budget=%d hard=%d target=%d\n",
		budget.Window, budget.Budget, budget.HardThreshold, budget.TargetAfterCompaction)
	fmt.Fprintf(report, "落盘 token_count 剖面：`[%d..%d]`=%d（帧起点之前）、`[%d..%d]`=%d（被折区间）；累积首次越过硬阈值 %d 的位置 = seq %d（`%s`）\n",
		facts.events[0].Seq, first.MessageFromSeq-1, beforeFold,
		first.MessageFromSeq, first.MessageToSeq, foldedTokens,
		budget.HardThreshold, crossing, positionOf[crossing])

	fmt.Fprintf(report, "\n### 帧的消息号 vs 落盘行的消息号（自检）\n\n")
	fmt.Fprintf(report, "| 位置 | 帧里写的 | 该 seq 的落盘行给的 |\n|---|---|---|\n")
	fmt.Fprintf(report, "| 被折区间起点（seq %d） | `%s` | `%s` |\n", first.MessageFromSeq, first.MessageFrom, fromRow.MessageID)
	fmt.Fprintf(report, "| 被折区间终点（seq %d） | `%s` | `%s` |\n", first.MessageToSeq, first.MessageTo, toRow.MessageID)
	fmt.Fprintf(report, "\n> seq %d 之前最后一个带消息号的行是 `%s`。帧的 `message_to=%s` 正好是它 —— 也就是说，**触发那一刻内存 transcript 的那个前缀里，seq 198..217 这一段事件没有消息号**（`TranscriptPrefixRange` 取\"最后一个非空 MessageID\"，事件序号取最后一个 Seq，两者因此可以不等）。\n",
		first.MessageToSeq, lastIDBeforeTo, first.MessageTo)
	fmt.Fprintf(report, "> 机制线索（Hypothesis，未单独钉）：冷加载走 `session_runtime/archive.go` 的 `enrichTranscriptMessageIDs`——按 CallID / 角色+内容**配对**会话投影，配不上的事件 MessageID 留空；而落盘那一侧的会话投影把 id 派生为 `message-<事件 seq>`（同文件 `RecordConversationTranscript`）。触发时那一轮还在飞的事件尚未进会话投影，因此没有 UI 消息号。要钉死它需要另跑一条\"压缩那一刻内存 id vs 落盘行 id\"的对照探针。\n")

	for _, outcome := range outcomes {
		fmt.Fprintf(report, "\n## ② 实跑：%s\n\n", outcome.arm.name)
		fmt.Fprintf(report, "- 注入真实事件 %d 条（seq %d..%d，`%s` → `%s`），落盘 token_count 合计 %d\n",
			outcome.injected, outcome.firstSeq, outcome.lastSeq,
			outcome.firstMessageID, outcome.lastMessageID, outcome.storedTokens)
		fmt.Fprintf(report, "- 被动路径：`PrepareExecutionContextFor`（自动判据，非 /compact）\n")
		if outcome.judgeDetail == "" {
			fmt.Fprintf(report, "- 判据关：未命中（自动路径只在真的要压时才开进度轮：本轮既无进度、也无记录、也未推帧）\n")
		} else {
			fmt.Fprintf(report, "- 判据关：`%s`\n", outcome.judgeDetail)
		}
		fmt.Fprintf(report, "- 装配关：`%s`\n", outcome.replaceDetail)
		if outcome.prepareErr != nil {
			fmt.Fprintf(report, "- 装配返回：`%v`\n", outcome.prepareErr)
		}
		if outcome.readbackCalled {
			fmt.Fprintf(report, "- 读数闸输入：overflow=%d 条 replay=%d 条（源=%s）\n",
				len(outcome.readbackInput.Overflow), len(outcome.readbackInput.ReplayHistory),
				outcome.arm.readbackSource)
		} else {
			fmt.Fprintf(report, "- 读数闸：本次未被调用（判据没命中或前置关已否）\n")
		}
		if len(outcome.pushes) == 0 {
			fmt.Fprintf(report, "- 推帧：本次没有推帧\n")
		}
		for _, push := range outcome.pushes {
			fmt.Fprintf(report, "- 推帧：区间 seq[%d..%d] message[`%s`..`%s`] overflow=%d 条 replay=%d 条 request=%s\n",
				push.EventFrom, push.EventTo, push.MessageFrom, push.MessageTo,
				len(push.Overflow), len(push.ReplayHistory), push.RequestID)
		}
		fmt.Fprintf(report, "- 压缩记录（内存真值）：\n%s\n", realProbeDescribeRecords(outcome.records))
		fmt.Fprintf(report, "- 快照可见面（右栏唯一来路）：\n%s\n", realProbeDescribeRecords(outcome.snapshotRecords))
		fmt.Fprintf(report, "- 终局：state=%s outcome=%s\n", outcome.finalState, outcome.finalOutcome)

		// 与真实帧对照：这一轮的区间与真实帧逐字相等吗？
		if len(outcome.records) > 0 && !outcome.records[0].Failed {
			record := outcome.records[0]
			match := record.EventFrom == first.MessageFromSeq && record.EventTo == first.MessageToSeq &&
				record.MessageFrom == first.MessageFrom && record.MessageTo == first.MessageTo
			fmt.Fprintf(report, "- 与真实帧对照：%v（实跑 `seq[%d..%d] message[%s..%s]` vs 真实 `seq[%d..%d] message[%s..%s]`）\n",
				match, record.EventFrom, record.EventTo, record.MessageFrom, record.MessageTo,
				first.MessageFromSeq, first.MessageToSeq, first.MessageFrom, first.MessageTo)
		}
	}
	realProbeDumpReport(t, report.String())

	for _, outcome := range outcomes {
		t.Logf("%s → 判据[%s] 记录=%d 快照=%d 推帧=%d err=%v",
			outcome.arm.name, outcome.judgeDetail, len(outcome.records),
			len(outcome.snapshotRecords), len(outcome.pushes), outcome.prepareErr)
		t.Logf("    %s", realProbeDescribeRecords(outcome.records))
	}
}
