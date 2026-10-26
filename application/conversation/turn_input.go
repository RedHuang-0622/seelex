package conversation

// TurnInputDeps supplies prompt/context preparation and normalization.
type TurnInputDeps struct {
	ApplyPrompt    func(sessionID, requestID string)
	PrepareContext func(sessionID, requestID, modelInput string) (string, error)
	Normalize      func(input string) string
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
	return application.deps.Normalize(prepared), nil
}
