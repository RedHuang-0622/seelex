package edit

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// AnnotationInput is the transcript/window fact for one session.
type AnnotationInput struct {
	Transcript []model.TranscriptEvent
	Floor      uint64
	HasWindow  bool
}

// AnnotationDeps supplies session input and user-text recognition.
type AnnotationDeps struct {
	Input      func(sessionID string) AnnotationInput
	IsUserText func(model.Message) bool
}

// AnnotationApplication owns visible-message editability projection.
type AnnotationApplication struct {
	deps AnnotationDeps
}

// NewAnnotation constructs the annotation application.
func NewAnnotation(deps AnnotationDeps) *AnnotationApplication {
	return &AnnotationApplication{deps: deps}
}

// Annotate backfills EventSeq and the single disabled reason.
func (application *AnnotationApplication) Annotate(sessionID string, messages []model.Message, running bool) []model.Message {
	annotated := append([]model.Message(nil), messages...)
	input := application.deps.Input(sessionID)
	seqByMessageID := make(map[string]uint64, len(input.Transcript))
	for _, event := range input.Transcript {
		if event.Role == "user" && event.MessageID != "" && event.Seq > 0 {
			seqByMessageID[event.MessageID] = event.Seq
		}
	}
	for index := range annotated {
		message := &annotated[index]
		message.Editable = false
		message.EditDisabledReason = ""
		if message.Role != "user" || !application.deps.IsUserText(*message) {
			continue
		}
		if message.EventSeq == 0 && message.ID != "" {
			message.EventSeq = seqByMessageID[message.ID]
		}
		switch {
		case running:
			message.EditDisabledReason = RunningReason
		case message.EventSeq == 0:
			message.EditDisabledReason = MissingSeqReason
		case !input.HasWindow:
			message.EditDisabledReason = NoWindowReason
		case input.Floor > 0 && message.EventSeq < input.Floor:
			message.EditDisabledReason = CompressedReason
		default:
			message.Editable = true
		}
	}
	return annotated
}
