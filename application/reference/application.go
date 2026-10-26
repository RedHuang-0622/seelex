// Package reference owns the read-only application adapters for oversized
// tool results and historical Plan projection.
package reference

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	toolreference "github.com/RedHuang-0622/seelex/backend/tool/reference"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// Deps supplies the reference use cases with bounded storage and archive
// capabilities. The callbacks must return the authoritative archived values;
// this package only validates requests, resolves aliases and shapes pages.
type Deps struct {
	ReferencePageSize    func() int
	MaxReferencePageSize func() int
	NodeResult           func(nodeID, ref string) (string, bool)
	ResolveAlias         func(ref string) string
	ResolveResult        func(ref string) (model.StoredToolResult, bool, error)
	ActivePlanID         func() string
	PlanFrame            func(planRef string) (string, model.RuntimeState, bool)
	CompressedTurnView   func() (sessionID, workspaceID string)
	LoadCompressedTurn   func(workspaceID, sessionID, ref string) (string, error)
	CompressedTurnPrefix string
}

// Application coordinates read_tool_result and read_plan without owning the
// Engine, Task or Session stores that provide their durable facts.
type Application struct {
	deps Deps
}

// New constructs the reference application from its consumer-defined ports.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// ReadToolResultHandler handles the read_tool_result model tool request.
func (application *Application) ReadToolResultHandler(argsJSON string) (string, error) {
	var input struct {
		ResultRef string `json:"result_ref"`
		Offset    int    `json:"offset,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Contains  string `json:"contains,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_tool_result: invalid JSON: %w", err)
	}
	input.ResultRef = strings.TrimSpace(input.ResultRef)
	if input.ResultRef == "" || input.Offset < 0 {
		return "", errors.New("read_tool_result: result_ref is required and offset must be non-negative")
	}
	if input.Limit <= 0 {
		input.Limit = application.deps.ReferencePageSize()
	}
	if max := application.deps.MaxReferencePageSize(); max > 0 && input.Limit > max {
		input.Limit = max
	}
	page, err := application.toolResultContent(input.ResultRef, input.Offset, input.Limit, input.Contains)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(page)
	return string(encoded), err
}

// ToolResultContent reads a bounded page for the GUI full-output viewer.
func (application *Application) ToolResultContent(resultRef string, offset, limit int) (model.ToolResultPage, error) {
	resultRef = strings.TrimSpace(resultRef)
	if resultRef == "" || offset < 0 {
		return model.ToolResultPage{}, errors.New("tool_result_content: result_ref is required and offset must be non-negative")
	}
	if limit <= 0 {
		limit = application.deps.ReferencePageSize()
	}
	if max := application.deps.MaxReferencePageSize(); max > 0 && limit > max {
		limit = max
	}
	return application.toolResultContent(resultRef, offset, limit, "")
}

func (application *Application) toolResultContent(resultRef string, offset, limit int, contains string) (model.ToolResultPage, error) {
	if nodeID, ok := toolreference.NodeResultRef(resultRef); ok {
		raw, found := application.deps.NodeResult(nodeID, resultRef)
		if !found {
			return model.ToolResultPage{}, errors.New("read_tool_result: node result_ref is not available (node finished or ref unknown)")
		}
		result := model.StoredToolResult{ToolResultRef: model.ToolResultRef{Ref: resultRef}, Content: raw}
		return toolreference.BuildPage(result, offset, limit, contains), nil
	}

	resultRef = application.deps.ResolveAlias(resultRef)
	result, found, err := application.deps.ResolveResult(resultRef)
	if err != nil {
		return model.ToolResultPage{}, fmt.Errorf("read_tool_result: %w", err)
	}
	if !found {
		return model.ToolResultPage{}, errors.New("read_tool_result: result_ref is not available in the current session")
	}
	return toolreference.BuildPage(result, offset, limit, contains), nil
}

// ReadPlanHandler returns the selected canonical Plan nodes and state.
func (application *Application) ReadPlanHandler(argsJSON string) (string, error) {
	var input struct {
		PlanRef string   `json:"plan_ref"`
		NodeIDs []string `json:"node_ids,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_plan: invalid JSON: %w", err)
	}
	planRef := strings.TrimSpace(input.PlanRef)
	if planRef == "" {
		planRef = application.deps.ActivePlanID()
	}
	arguments, planState, ok := application.deps.PlanFrame(planRef)
	if !ok {
		return "", errors.New("read_plan: plan_ref is not available in the current session")
	}

	var canonical map[string]any
	if err := json.Unmarshal([]byte(arguments), &canonical); err != nil {
		return "", fmt.Errorf("read_plan: stored Plan is invalid: %w", err)
	}
	if len(input.NodeIDs) > 0 {
		nodes, _ := canonical["nodes"].(map[string]any)
		selected := make(map[string]any, len(input.NodeIDs))
		for _, nodeID := range input.NodeIDs {
			if node, ok := nodes[nodeID]; ok {
				selected[nodeID] = node
			}
		}
		canonical["nodes"] = selected
	}
	payload := map[string]any{"plan_ref": planRef, "canonical": canonical, "state": planState}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}
