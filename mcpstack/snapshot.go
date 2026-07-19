package mcpstack

import (
	"encoding/json"
)

// Snapshot returns a deep copy of the current stack state (thread-safe).
// The returned stack is fully independent of the original.
func (s *MCPStack) Snapshot() (*MCPStack, error) {
	// optimistic read — no lock

	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	copy := New()
	if err := json.Unmarshal(data, copy); err != nil {
		return nil, err
	}
	copy.autoSavePath = s.autoSavePath
	return copy, nil
}
