package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

type extractedOwnerCase struct {
	name         string
	legacy       []string
	owner        []string
	ownerPackage string
}

func TestExtractedOwnersUseCanonicalPackages(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	cases := []extractedOwnerCase{
		{name: "builtin commands", owner: []string{"input/command/command.go", "input/command/README.md"}},
		{name: "input completion", legacy: []string{"assembly/completion.go"}},
		{name: "session input index", legacy: []string{"assembly/session_input_index.go"}},
		{name: "session history", legacy: []string{"assembly/session_history.go"}, owner: []string{"session/history/history_runtime.go"}},
		{name: "session fork", legacy: []string{"assembly/session_fork.go"}, owner: []string{"session/fork/fork_runtime.go"}},
		{name: "context retention", legacy: []string{"assembly/window.go"}, owner: []string{"window/retention.go"}},
		{name: "provider recovery", legacy: []string{"assembly/history_safety.go"}, owner: []string{"conversation/context/recovery.go"}},
		{name: "environment context", legacy: []string{"assembly/environment_context.go"}, owner: []string{"prompt/environment.go"}},
		{name: "compaction gate", legacy: []string{"assembly/context_compact_gate.go"}, owner: []string{"conversation/context/compaction_gate.go"}},
		{name: "tool record host", legacy: []string{"assembly/tool_record.go", "assembly/tool_record_stage.go"}, owner: []string{"execution/toolrecord/host.go"}},
		{name: "turn state", owner: []string{"conversation/turn_state.go"}, ownerPackage: "conversation"},
		{name: "session view runtime", legacy: []string{"assembly/service_snapshot.go"}, owner: []string{"session/viewstate/session_runtime.go"}},
		{
			name:   "session residency runtime",
			legacy: []string{"assembly/resident_lru.go", "assembly/content_lru.go", "assembly/session_release.go"},
			owner:  []string{"session/residency/runtime.go"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, relative := range test.legacy {
				path := filepath.Join(applicationDir, filepath.FromSlash(relative))
				if _, err := os.Stat(path); err == nil {
					t.Errorf("legacy file still exists: application/%s", relative)
				} else if !os.IsNotExist(err) {
					t.Fatalf("stat application/%s: %v", relative, err)
				}
			}
			for _, relative := range test.owner {
				path := filepath.Join(applicationDir, filepath.FromSlash(relative))
				if _, err := os.Stat(path); err != nil {
					t.Errorf("owner file missing: application/%s: %v", relative, err)
					continue
				}
				if test.ownerPackage != "" {
					file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
					if err != nil {
						t.Errorf("parse application/%s: %v", relative, err)
					} else if file.Name.Name != test.ownerPackage {
						t.Errorf("application/%s package = %q, want %q", relative, file.Name.Name, test.ownerPackage)
					}
				}
			}
		})
	}
}
