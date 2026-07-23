package main

import (
	"flag"
	"testing"

	"github.com/RedHuang-0622/Seele/agent/core/tool/permission"
)

func TestParseFrontendMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "tui", want: "tui"},
		{input: " GUI ", want: "gui"},
	} {
		got, err := parseFrontendMode(test.input)
		if err != nil {
			t.Fatalf("parseFrontendMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseFrontendMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := parseFrontendMode("browser"); err == nil {
		t.Fatal("parseFrontendMode accepted an unsupported frontend")
	}
}

func TestParsePermissionMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  permission.Mode
	}{
		{input: "manual", want: permission.ModeManual},
		{input: " FULL_ACCESS ", want: permission.ModeFullAccess},
	} {
		got, err := parsePermissionMode(test.input)
		if err != nil {
			t.Fatalf("parsePermissionMode(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parsePermissionMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := parsePermissionMode("unsafe"); err == nil {
		t.Fatal("parsePermissionMode accepted an unsupported permission mode")
	}
}

func TestReleaseVersionIsNotStale(t *testing.T) {
	t.Parallel()
	if Version == "" || Version == "v0.0.2" {
		t.Fatalf("unexpected release version %q", Version)
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
