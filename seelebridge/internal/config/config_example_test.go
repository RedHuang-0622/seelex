package config

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// TestShippedAccountsExampleLoads 钉住"发行示例永远可装载"。
//
// accounts.example.yaml 是**唯一对外承诺的配置形状**（README、发行包、用户照抄
// 的模板都用它）。它一旦写坏，用户是照着坏模板抄的——所以它必须由真正的 loader
// 亲自过一遍，而不是靠人眼看注释。新增字段（如 reasoning_effort）时，这条用例
// 同时保证"文档里写了的键，loader 真的认识"。
func TestShippedAccountsExampleLoads(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "accounts.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("shipped example must load: %v", err)
	}
	if len(cfg.Specs) == 0 {
		t.Fatal("shipped example produced no account specs")
	}
	for _, spec := range cfg.Specs {
		if !model.ValidReasoningEffort(spec.ReasoningEffort) {
			t.Errorf("account %s: example yielded an invalid reasoning_effort %q", spec.Name, spec.ReasoningEffort)
		}
	}
}
