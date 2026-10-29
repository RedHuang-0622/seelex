package conversation

// TurnInputDeps supplies prompt/context preparation and normalization.
type TurnInputDeps struct {
	ApplyPrompt    func(sessionID, requestID string)
	PrepareContext func(sessionID, requestID, modelInput string) (string, error)
	Normalize      func(input string) string
	// RecordWire 把归一化后真正发给 provider 的 user 正文写回 transcript
	// （已发出字节，不变量 I1）。缺省为 nil 时跳过——仅为测试夹具便利。
	//
	// 返回 bool 而非空：调用方通常忽略，但 kernel 侧需要它表达「是否命中该轮的
	// user 行」。签名为 bool 的另一个原因是装配层规模有只降不升的预算
	// （application/architecture_budget_test.go），bool 签名让装配处可以直接用
	// 方法值 `tasks.RecordUserWireContentLocked` 赋值，而不必写一层闭包。
	RecordWire func(sessionID, requestID, wire string) bool
}

// TurnInputApplication owns pre-engine prompt/context preparation.
type TurnInputApplication struct {
	deps TurnInputDeps
}

// NewTurnInput constructs the turn-input application.
func NewTurnInput(deps TurnInputDeps) *TurnInputApplication {
	return &TurnInputApplication{deps: deps}
}

// Prepare applies the prompt, prepares provider context and normalizes input.
func (application *TurnInputApplication) Prepare(sessionID, requestID, modelInput string) (string, error) {
	application.deps.ApplyPrompt(sessionID, requestID)
	prepared, err := application.deps.PrepareContext(sessionID, requestID, modelInput)
	if err != nil {
		return "", err
	}
	normalized := application.deps.Normalize(prepared)
	// 冻结本轮 user 消息的已发出字节：装配会把动态尾块拼进 prepared，若只记
	// 原始输入，下一轮重投影会自该消息起分叉（I1）。这里记的是**最终送进引擎
	// 的那一份**，因此与 wire 逐字节一致。
	if application.deps.RecordWire != nil {
		application.deps.RecordWire(sessionID, requestID, normalized)
	}
	return normalized, nil
}
