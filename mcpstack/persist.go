package mcpstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultDir  = ".seelex/mcp-traces"
	FileSuffix  = ".mcpstack.json"
)

// StackPath returns the conventional file path for a session trace.
func StackPath(sessionID, dir string) string {
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, sessionID+FileSuffix)
}

// Marshal serializes the stack to JSON (thread-safe).
func (s *MCPStack) Marshal() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.MarshalIndent(s, "", "  ")
}

// Unmarshal deserializes JSON data into the stack (thread-safe).
func (s *MCPStack) Unmarshal(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, s)
}

// Save writes the stack to a file atomically (write .tmp then rename).
func (s *MCPStack) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.save(path)
}

func (s *MCPStack) save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("mcpstack: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mcpstack: mkdir: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("mcpstack: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("mcpstack: rename: %w", err)
	}
	return nil
}

// Load reads a stack from a file, replacing current state.
func (s *MCPStack) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("mcpstack: file not found: %s: %w", path, err)
		}
		return fmt.Errorf("mcpstack: read file: %w", err)
	}
	return s.Unmarshal(data)
}

// LoadStack loads a complete MCPStack from a file.
func LoadStack(path string) (*MCPStack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcpstack: load: %w", err)
	}
	s := New()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("mcpstack: unmarshal: %w", err)
	}
	return s, nil
}
