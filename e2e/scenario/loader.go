package scenario

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func Load(reader io.Reader) (Scenario, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var value Scenario
	if err := decoder.Decode(&value); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Scenario{}, fmt.Errorf("decode scenario: multiple JSON values")
		}
		return Scenario{}, fmt.Errorf("decode scenario trailer: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Scenario{}, fmt.Errorf("validate scenario: %w", err)
	}
	return value, nil
}

func LoadFile(path string) (Scenario, error) {
	file, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("open scenario %q: %w", path, err)
	}
	defer file.Close()
	return Load(file)
}
