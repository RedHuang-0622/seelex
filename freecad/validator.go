package freecad

import (
	"encoding/json"
	"fmt"
)

// PreValidate checks CAD operation parameters BEFORE they reach the MCP server.
// Returns nil if valid, or an error describing the violation.
//
// This is the CAD-specific "more rigorous validation" — WebSearch doesn't need this
// because there's no dimensionally wrong search query. But a 0-length rectangle
// or a negative-radius fillet would crash the CAD kernel.
func PreValidate(toolName string, args json.RawMessage) error {
	_, err := Validate(toolName, args)
	return err
}

// PostValidate checks the result from a CAD MCP call for well-formedness.
// This catches cases where the MCP server returns unexpected data.
func PostValidate(toolName string, result json.RawMessage) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		return fmt.Errorf("CAD %s: result not valid JSON: %w", toolName, err)
	}
	status, ok := raw["status"]
	if !ok {
		return fmt.Errorf("CAD %s: result missing 'status' field", toolName)
	}
	if status != "ok" && status != "success" {
		msg, _ := raw["message"].(string)
		if msg != "" {
			return fmt.Errorf("CAD %s: %v: %s", toolName, status, msg)
		}
		return fmt.Errorf("CAD %s: %v", toolName, status)
	}
	return nil
}

// Usage example for integrating with seelebridge:
//
//	// In seelebridge, when attaching an MCP server:
//	serverName := "freecad"
//	traceStack := mcpstack.New(mcpstack.WithAutoSave(".seelex/mcp-traces/freecad.json"))
//
//	// Before dispatching each tool call:
//	rec := mcpstack.BeforeCall(traceStack, serverName, toolName, argsJSON, aiMsgID)
//
//	// Pre-flight CAD validation
//	if err := freecad.PreValidate(toolName, argsJSON); err != nil {
//	    rec.AfterCall(nil, err)
//	    return "", err
//	}
//
//	// Actual MCP call (handled by the framework)
//	result, mcpErr := actualMCPServer(ctx, toolName, args)
//	rec.AfterCall(resultJSON, mcpErr)
//
//	// Post-flight check
//	if mcpErr == nil {
//	    _ = freecad.PostValidate(toolName, resultJSON) // warning only
//	}
