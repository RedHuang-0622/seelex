package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGitHubConfigurationIsValidYAML(t *testing.T) {
	files := []string{
		".github/workflows/release.yml",
		".github/dependabot.yml",
		".github/ISSUE_TEMPLATE/bug.yml",
		".github/ISSUE_TEMPLATE/feature.yml",
		".github/ISSUE_TEMPLATE/reproduction.yml",
		".github/ISSUE_TEMPLATE/config.yml",
	}
	workflowFiles, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, workflowFiles...)
	seen := make(map[string]struct{})
	for _, path := range files {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s is invalid YAML: %v", path, err)
		}
	}
}
