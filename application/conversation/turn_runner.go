package conversation

import "context"

// StreamBatcher is the flush capability required from the streaming sink.
type StreamBatcher interface {
	Flush() error
}

// TurnRunnerDeps supplies the existing turn components and host lifecycle
// callbacks. Engine execution remains behind Stream.
type TurnRunnerDeps struct {
	WithSessionID       func(context.Context, string) context.Context
	StartDraftSnapshot  func(context.Context, string, string) func()
	PrepareExecution    func(string)
	ClearReActBudget    func(string)
	Debug               func(string, ...any)
	NewBatchedDeltaSink func(string) (StreamBatcher, func(string))
	Stream              func(context.Context, string, string, func(string)) (string, error)
	Input               *TurnInputApplication
	Response            *TurnResponseApplication
	Recovery            *TurnRecoveryApplication
	Cleanup             *TurnCleanupApplication
	Commit              *TurnCommitApplication
	Finalize            *TurnFinalizeApplication
	// SignalContinuation 在**回合休止点**（Commit + Finalize 之后）发一次自动续跑
	// 唤醒（payload = sessionID）。绝不能挂在 loop / 工具轮上：一个含 N 个工具轮的
	// 回合仍然只发 1 次，判定因此也只发生 1 次（docs/arch/goal-auto-continue.md §2.8.2）。
	// 缺省 = 这条通道没装配（不报错，也不假装发过）。
	SignalContinuation func(sessionID string)
}

// TurnRunnerApplication owns the per-turn application orchestration.
type TurnRunnerApplication struct {
	deps TurnRunnerDeps
}

// NewTurnRunner constructs the turn runner.
func NewTurnRunner(deps TurnRunnerDeps) *TurnRunnerApplication {
	return &TurnRunnerApplication{deps: deps}
}

// Run executes one accepted ChatTurn through the existing component chain.
func (application *TurnRunnerApplication) Run(
	ctx context.Context,
	sessionID, requestID string,
	request Request,
) {
	ctx = application.deps.WithSessionID(ctx, sessionID)
	stopDraftSnapshot := application.deps.StartDraftSnapshot(ctx, sessionID, requestID)
	defer stopDraftSnapshot()
	application.deps.PrepareExecution(sessionID)
	defer application.deps.ClearReActBudget(requestID)

	var err error
	application.deps.Debug("runChat start session=%s request=%s input=%q", sessionID, requestID, request.DisplayInput)
	defer func() {
		application.deps.Debug("runChat end session=%s request=%s err=%v", sessionID, requestID, err)
	}()
	modelInput := request.ModelInput
	batcher, onChunk := application.deps.NewBatchedDeltaSink(requestID)
	modelInput, err = application.deps.Input.Prepare(sessionID, requestID, modelInput)
	if err == nil {
		application.deps.Debug("runChat entering chatStream session=%s request=%s", sessionID, requestID)
		var reply string
		reply, err = application.deps.Stream(ctx, sessionID, modelInput, onChunk)
		application.deps.Debug("runChat chatStream returned session=%s request=%s replyLen=%d err=%v", sessionID, requestID, len(reply), err)
		application.deps.Response.Finalize(ctx, sessionID, requestID, reply, err)
		err = application.deps.Recovery.Finalize(
			ctx, sessionID, requestID, request.DisplayInput, reply, err, onChunk,
		)
	}
	runtimeProjection, err := application.deps.Cleanup.Cleanup(
		sessionID, requestID, err, batcher.Flush, stopDraftSnapshot,
	)
	committed := application.deps.Commit.Commit(sessionID, requestID, err, runtimeProjection)
	application.deps.Finalize.Finalize(committed)
	// 回合休止点：到这里这一轮所有 loop / 工具轮都结束了，续跑门只该在这里被问一次。
	if application.deps.SignalContinuation != nil {
		application.deps.SignalContinuation(sessionID)
	}
}
