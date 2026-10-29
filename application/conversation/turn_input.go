package conversation

// TurnInputDeps supplies prompt/context preparation and normalization.
type TurnInputDeps struct {
	ApplyPrompt    func(sessionID, requestID string)
	PrepareContext func(sessionID, requestID, modelInput string) (string, error)
	Normalize      func(input string) string
	// RecordWire 把归一化后真正发给 provider 的 user 正文写回 transcript
	// （已发出字节，不变量 I1）。缺省为 nil 时跳过——仅为测试夹具便利。
	RecordWire func(sessionID, requestID, wire string)
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
