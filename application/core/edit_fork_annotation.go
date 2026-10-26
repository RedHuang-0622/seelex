package core

import (
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func newServiceEditForkAnnotation(service *Service) *applicationedit.AnnotationApplication {
	return applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
		Input: func(sessionID string) applicationedit.AnnotationInput {
			floor, hasWindow := service.components.tasks.RetainedWindowFirstEventSeqFor(sessionID)
			return applicationedit.AnnotationInput{
				Transcript: service.components.tasks.TranscriptFor(sessionID),
				Floor:      floor,
				HasWindow:  hasWindow,
			}
		},
		IsUserText: func(message model.Message) bool {
			return inputIndexUserText(message) != ""
		},
	})
}
