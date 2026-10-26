package core

import (
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type editForkOperationRegistry = applicationedit.OperationRegistry

func newEditForkOperationRegistry(ttl time.Duration, now func() time.Time) *editForkOperationRegistry {
	return applicationedit.NewOperationRegistry(ttl, now)
}

type editForkValidator = applicationedit.Validator
type editForkValidatorDeps = applicationedit.ValidatorDeps
type editForkParentState = applicationedit.ParentState

func newEditForkValidator(deps editForkValidatorDeps) *editForkValidator {
	if deps.ChatRunningError == nil {
		deps.ChatRunningError = ErrChatRunning
	}
	return applicationedit.NewValidator(deps)
}

type editForkTargetResolver = applicationedit.TargetResolver
type editForkTargetResolverDeps = applicationedit.TargetResolverDeps

func newEditForkTargetResolver(deps editForkTargetResolverDeps) *editForkTargetResolver {
	if deps.IsUserText == nil {
		deps.IsUserText = func(message model.Message) bool {
			return inputIndexUserText(message) != ""
		}
	}
	return applicationedit.NewTargetResolver(deps)
}

type editForkProcess = applicationedit.ProcessApplication
type editForkProcessDeps = applicationedit.ProcessDeps

func newEditForkProcess(deps editForkProcessDeps) *editForkProcess {
	return applicationedit.NewProcess(deps)
}

type editForkRollback = applicationedit.RollbackApplication
type editForkRollbackDeps = applicationedit.RollbackDeps

func newEditForkRollback(deps editForkRollbackDeps) *editForkRollback {
	return applicationedit.NewRollback(deps)
}

type editForkAnnotation = applicationedit.AnnotationApplication
type editForkAnnotationDeps = applicationedit.AnnotationDeps
type editForkAnnotationInput = applicationedit.AnnotationInput

func newEditForkAnnotation(deps editForkAnnotationDeps) *editForkAnnotation {
	if deps.IsUserText == nil {
		deps.IsUserText = func(message model.Message) bool {
			return inputIndexUserText(message) != ""
		}
	}
	return applicationedit.NewAnnotation(deps)
}
