package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestChatTurnPipelineComponentsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"turn_input.go": {
			"conversationTurnInput":     true,
			"conversationTurnInputDeps": true,
		},
		"turn_response.go": {
			"conversationTurnResponse":     true,
			"conversationTurnResponseDeps": true,
		},
		"turn_recovery.go": {
			"conversationTurnRecovery":     true,
			"conversationTurnRecoveryDeps": true,
		},
		"turn_cleanup.go": {
			"conversationTurnCleanup":     true,
			"conversationTurnCleanupDeps": true,
		},
		"turn_process.go": {
			"conversationTurnProcess":     true,
			"conversationTurnProcessDeps": true,
			"chatTurnPromotion":           true,
			"chatTurnStartOptions":        true,
		},
		"turn_outcome.go": {
			"conversationTurnOutcome":     true,
			"conversationTurnOutcomeDeps": true,
		},
		"turn_completion.go": {
			"conversationTurnCompletion":     true,
			"conversationTurnCompletionDeps": true,
		},
		"turn_commit.go": {
			"conversationTurnCommit":     true,
			"conversationTurnCommitDeps": true,
			"chatTurnCommitResult":       true,
		},
		"turn_finalize.go": {
			"conversationTurnFinalize":     true,
			"conversationTurnFinalizeDeps": true,
		},
		"turn_publication.go": {
			"conversationTurnPublisher":     true,
			"conversationTurnPublisherDeps": true,
			"chatTurnPublication":           true,
		},
		"turn_tail.go": {
			"conversationTurnTail":     true,
			"conversationTurnTailDeps": true,
		},
		"turn_execution.go": {
			"conversationTurnExecution":     true,
			"conversationTurnExecutionDeps": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "conversation", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/conversation/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/conversation/%s: %v", name, err)
		}
		if parsed.Name.Name != "conversation" {
			t.Fatalf("application/conversation/%s package = %q", name, parsed.Name.Name)
		}
	}
	coreFiles := []string{
		"chat_turn_input.go",
		"chat_turn_response.go",
		"chat_turn_recovery.go",
		"chat_turn_cleanup.go",
		"chat_turn_process.go",
		"chat_turn_outcome.go",
		"chat_turn_completion.go",
		"chat_turn_commit.go",
		"chat_turn_finalize.go",
		"chat_turn_publication.go",
		"chat_turn_tail.go",
		"chat_turn_execution.go",
	}
	for _, name := range coreFiles {
		path := filepath.Join(applicationDir, "core", name)
		core, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("parse core/%s: %v", name, err)
		}
		for _, declaration := range core.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				for _, forbidden := range files {
					if forbidden[typeSpec.Name.Name] {
						t.Errorf("core/%s still owns %s", name, typeSpec.Name.Name)
					}
				}
			}
		}
	}
}
