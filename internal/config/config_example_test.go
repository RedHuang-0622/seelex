package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/internal/model"
)

// TestShippedAccountsExampleLoads 钉住"发行示例永远可装载"。
//
// accounts.example.yaml 是**唯一对外承诺的配置形状**（README、发行包、用户照抄
// 的模板都用它）。它一旦写坏，用户是照着坏模板抄的——所以它必须由真正的 loader
// 亲自过一遍，而不是靠人眼看注释。新增字段（如 reasoning_effort）时，这条用例
// 同时保证"文档里写了的键，loader 真的认识"。
func TestShippedAccountsExampleLoads(t *testing.T) {
	path := filepath.Join("..", "..", "config", "accounts.example.yaml")
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

// TestShippedAccountsExampleCarriesNoRuntimeConfig 钉住 2026-10-15 的口径：**账号档只放
// 凭据与逐账号的模型参数**，运行配置不许出现在模板里——窗口的家是 config/seelex.yaml 的
// `limits.context_window`（见 limit.Limits.ContextWindow 与 Runtime.ContextWindow）。
//
// 为什么要一条牙：模板是用户照抄的形状（README、发行包都用它），`context_window` 一旦被
// 加回模板，等于又把运行配置教给用户放进凭据文件。判"写没写"用 loader 自己的 schema
// （simplifiedConfig），与 Load 走同一份结构，不另立一份键名清单——两份清单必然漂移。
func TestShippedAccountsExampleCarriesNoRuntimeConfig(t *testing.T) {
	path := filepath.Join("..", "..", "config", "accounts.example.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg simplifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.ContextWindow != nil {
		t.Errorf("defaults.context_window = %d：运行配置不许进账号模板（家在 seelex.yaml 的 limits.context_window）",
			*cfg.Defaults.ContextWindow)
	}
	for role, accounts := range map[string][]simplifiedAccount{
		"agent":    cfg.Roles.Agent,
		"subagent": cfg.Roles.SubAgent,
		"goalplan": cfg.Roles.GoalPlan,
	} {
		for _, account := range accounts {
			if account.ContextWindow != nil {
				t.Errorf("%s/%s: context_window = %d：同上（账号档只放凭据与模型参数）",
					role, account.Model, *account.ContextWindow)
			}
		}
	}
}
