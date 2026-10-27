package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestToolRecordHostAdaptersLiveInToolRecord(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"tool_record.go", "tool_record_stage.go"} {
		path := filepath.Join(applicationDir, "assembly", name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("application/assembly/%s 仍存在；ToolRecord host adapter 必须由 toolrecord owner 承接", name)
		} else if !os.IsNotExist(err) {
			t.Fatalf("检查 %s: %v", path, err)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, "execution", "toolrecord", "host.go"), nil, 0); err != nil {
		t.Fatalf("ToolRecord host owner 缺失: %v", err)
	}
}
