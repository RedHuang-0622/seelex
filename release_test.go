package main

import (
	"flag"
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/buildinfo"
	"github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/bootstrap"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func TestParseFrontendMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "tui", want: "tui"},
		{input: " GUI ", want: "gui"},
		{input: "HEADLESS", want: "headless"},
		{input: "BACKEND", want: "backend"},
	} {
		got, err := config.ParseFrontendMode(test.input)
		if err != nil {
			t.Fatalf("config.ParseFrontendMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("config.ParseFrontendMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := config.ParseFrontendMode("browser"); err == nil {
		t.Fatal("ParseFrontendMode accepted an unsupported frontend")
	}
}

func TestParsePermissionMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "manual", want: dto.PermissionTierManual},
		{input: "edit", want: dto.PermissionTierEdit},
		{input: " AUTO ", want: dto.PermissionTierAuto},
		{input: "full", want: dto.PermissionTierFull},
		// 旧别名：full_access → full 档。
		{input: " FULL_ACCESS ", want: dto.PermissionTierFull},
	} {
		got, err := config.ParsePermissionMode(test.input)
		if err != nil {
			t.Fatalf("config.ParsePermissionMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("config.ParsePermissionMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := config.ParsePermissionMode("unsafe"); err == nil {
		t.Fatal("ParsePermissionMode accepted an unsupported permission mode")
	}
}

func TestReleaseVersionIsNotStale(t *testing.T) {
	t.Parallel()
	// 版本号的权威值是**构建期注入点**（internal/buildinfo）：发布构建用
	// `-X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=<tag>` 覆盖它，
	// 源码里的 dev 必须留得住——否则 ldflags 没接上也能"看起来发布过了"。
	if buildinfo.Version != "dev" {
		t.Fatalf("source version = %q, want dev; releases must inject their tag with ldflags", buildinfo.Version)
	}
}

func TestSafeFlagDefaults(t *testing.T) {
	t.Parallel()
	if got := flag.Lookup("permission").DefValue; got != "manual" {
		t.Fatalf("permission default = %q, want manual", got)
	}
	if got := flag.Lookup("frontend").DefValue; got != "tui" {
		t.Fatalf("frontend default = %q, want tui", got)
	}
}

func TestDefaultManualRulesAllowSafeControlPlaneTools(t *testing.T) {
	checker := toolspermission.NewPermissionChecker(toolspermission.PermissionConfig{
		Mode: toolspermission.ModeManual, Rules: bootstrap.DefaultManualRules(),
	})
	for _, name := range []string{
		"todolist_init", "todolist_add", "todolist_done", "todolist_status",
		"task_complete", "task_failed", "task_needs_user_decision",
	} {
		if result := checker.Check(name, `{}`); result != toolspermission.ResultAllow {
			t.Fatalf("safe control-plane tool %q permission = %v, want allow", name, result)
		}
	}
	if result := checker.Check("bash", `{"command":"pwd"}`); result != toolspermission.ResultAsk {
		t.Fatalf("bash permission = %v, want ask in manual mode", result)
	}
}

type permissionRuntimeRecorder struct {
	config     toolspermission.PermissionConfig
	handler    toolspermission.ApprovalHandler
	fullAccess []bool
	tiers      []string
}

func (runtime *permissionRuntimeRecorder) SetPermissionConfig(
	config toolspermission.PermissionConfig,
	handler toolspermission.ApprovalHandler,
) {
	runtime.config = config
	runtime.handler = handler
}

func (runtime *permissionRuntimeRecorder) SetFullAccess(on bool) {
	runtime.fullAccess = append(runtime.fullAccess, on)
}

func (runtime *permissionRuntimeRecorder) SetPermissionTierFor(_ string, tier string) error {
	runtime.tiers = append(runtime.tiers, tier)
	return nil
}

func TestFullAccessStartupKeepsManualPermissionBaseline(t *testing.T) {
	runtime := &permissionRuntimeRecorder{}
	// 档位由调用方显式给出（装配段不再读包级 flag）：M5-S3 之前这里是
	// `launchFlags.PermissionMode = full_access` 再调 setupPermissionGate。
	if err := bootstrap.SetupPermissionGate(
		runtime, wiring.NewApprovalBroker(nil), string(toolspermission.ModeFullAccess),
	); err != nil {
		t.Fatal(err)
	}
	if runtime.config.Mode != toolspermission.ModeManual {
		t.Fatalf("baseline permission mode = %q, want manual", runtime.config.Mode)
	}
	if runtime.handler == nil {
		t.Fatal("full-access startup discarded the manual approval bridge")
	}
	// 旧的 -permission full_access 现在落到 full 档（进程级默认）。
	if len(runtime.tiers) != 1 || runtime.tiers[0] != dto.PermissionTierFull {
		t.Fatalf("full-access startup tiers = %#v, want [full]", runtime.tiers)
	}
}
