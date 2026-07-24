package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const guiDocsRoot = "docs/gui"

type moduleRegistry struct {
	Modules []struct {
		ID        string   `json:"id"`
		Document  string   `json:"document"`
		DependsOn []string `json:"depends_on"`
	} `json:"modules"`
}

func TestGUIDocumentContracts(t *testing.T) {
	compiler := loadGUISchemas(t)
	cases := map[string]string{
		"https://seelex.dev/schemas/module-dotting.schema.json":         "module_dotting.json",
		"https://seelex.dev/schemas/error.schema.json":                  "examples/error.json",
		"https://seelex.dev/schemas/event.schema.json":                  "examples/event.json",
		"https://seelex.dev/schemas/snapshot.schema.json":               "examples/snapshot.json",
		"https://seelex.dev/schemas/page.schema.json":                   "examples/workspace-page.json",
		"https://seelex.dev/schemas/generation-manifest.schema.json":    "examples/generation-manifest.json",
		"https://seelex.dev/schemas/card.schema.json":                   "examples/card.json",
		"https://seelex.dev/schemas/session-snapshot.schema.json":       "examples/session-snapshot.json",
		"https://seelex.dev/schemas/turn-request.schema.json":           "examples/turn-request.json",
		"https://seelex.dev/schemas/turn-accepted.schema.json":          "examples/turn-accepted.json",
		"https://seelex.dev/schemas/interaction-resolution.schema.json": "examples/interaction-resolution.json",
		"https://seelex.dev/schemas/generation-operation.schema.json":   "examples/generation-operation.json",
		"https://seelex.dev/schemas/evidence-assessment.schema.json":    "examples/evidence-assessment.json",
		"https://seelex.dev/schemas/dev-iteration.schema.json":          "examples/dev-iteration.json",
	}
	validated := make(map[string]bool, len(cases))
	for schemaID, example := range cases {
		schema, err := compiler.Compile(schemaID)
		if err != nil {
			t.Fatalf("compile %s: %v", schemaID, err)
		}
		if err := schema.Validate(readJSON(t, filepath.Join(guiDocsRoot, example))); err != nil {
			t.Errorf("validate %s with %s: %v", example, schemaID, err)
		}
		validated[filepath.Base(example)] = true
	}
	examples, err := filepath.Glob(filepath.Join(guiDocsRoot, "examples", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		if !validated[filepath.Base(example)] {
			t.Errorf("example %s has no corresponding schema test", example)
		}
	}
}

func TestGUIModuleRegistry(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(guiDocsRoot, "module_dotting.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry moduleRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	modules := make(map[string][]string, len(registry.Modules))
	for _, module := range registry.Modules {
		if _, exists := modules[module.ID]; exists {
			t.Errorf("duplicate module id %q", module.ID)
		}
		modules[module.ID] = module.DependsOn
		if _, err := os.Stat(filepath.Join(guiDocsRoot, filepath.FromSlash(module.Document))); err != nil {
			t.Errorf("module %q document: %v", module.ID, err)
		}
	}
	checkModuleGraph(t, modules)
}

func TestGUIDocumentLinks(t *testing.T) {
	pattern := regexp.MustCompile(`\]\(([^)]+)\)`)
	err := filepath.WalkDir(guiDocsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			target := strings.Trim(match[1], "<>")
			target, _, _ = strings.Cut(target, "#")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %q: %v", path, match[1], err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadGUISchemas(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	paths, err := filepath.Glob(filepath.Join(guiDocsRoot, "schemas", "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		doc := readJSON(t, path)
		object, ok := doc.(map[string]any)
		if !ok {
			t.Fatalf("schema %s is not an object", path)
		}
		id, ok := object["$id"].(string)
		if !ok || id == "" {
			t.Fatalf("schema %s has no $id", path)
		}
		if err := compiler.AddResource(id, doc); err != nil {
			t.Fatalf("register %s: %v", path, err)
		}
	}
	return compiler
}

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func checkModuleGraph(t *testing.T, modules map[string][]string) {
	t.Helper()
	state := make(map[string]uint8, len(modules))
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("dependency cycle at %q", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, dependency := range modules[id] {
			if _, exists := modules[dependency]; !exists {
				return fmt.Errorf("module %q depends on unknown module %q", id, dependency)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range modules {
		if err := visit(id); err != nil {
			t.Error(err)
		}
	}
}
