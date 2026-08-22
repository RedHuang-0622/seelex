package core

import (
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/prompt_layer"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/subagent_view"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

// serviceComponents is the application composition graph. Service coordinates
// these parts but does not own their implementation details.
type serviceComponents struct {
	prompts  *prompt_layer.Coordinator
	context  *context_runtime.Coordinator
	history  *context_runtime.HistoryCoordinator
	sessions *session_runtime.Coordinator
	tasks    *task_context.Coordinator
	view     *view_state.Coordinator
	subagent *subagent_view.Coordinator
	input    inputDispatcher
}
