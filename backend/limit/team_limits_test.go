package limit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLimitConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seelex.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestTeamLimitsSeparateRolesAndJobs(t *testing.T) {
	defaults := DefaultLimits()
	if defaults.Team.MaxRoles != 6 || defaults.Team.MaxTeammates != 6 || defaults.Team.MaxJobs != 6 {
		t.Fatalf("role/job defaults must both be 6 and remain independent: %+v", defaults.Team)
	}
	if defaults.Team.WorkItemToolFailure.Enabled {
		t.Fatal("workitem tool failure return must default off")
	}
	if defaults.Team.WorkItemToolFailure.Threshold != 3 {
		t.Fatalf("workitem tool failure threshold default = %d, want 3", defaults.Team.WorkItemToolFailure.Threshold)
	}
	if defaults.Concurrency.Teammate.Enabled || defaults.Concurrency.Subagent.Enabled {
		t.Fatalf("route concurrency limits must default off: %+v", defaults.Concurrency)
	}
}

func TestTeamLimitsLegacyMaxTeammatesStillMeansRoles(t *testing.T) {
	path := writeLimitConfig(t, `
limits:
  team:
    max_teammates: 4
    max_jobs: 2
`)
	got, err := LoadLimits(path)
	if err != nil {
		t.Fatalf("LoadLimits: %v", err)
	}
	got = got.WithDefaults()
	if got.Team.MaxRoles != 4 || got.Team.MaxTeammates != 4 {
		t.Fatalf("legacy max_teammates must map to role count: %+v", got.Team)
	}
	if got.Team.MaxJobs != 2 {
		t.Fatalf("max_jobs must stay independent: %+v", got.Team)
	}
}

func TestTeamLimitsMaxRolesAndLegacyAliasMustAgree(t *testing.T) {
	path := writeLimitConfig(t, `
limits:
  team:
    max_roles: 5
    max_teammates: 4
`)
	if _, err := LoadLimits(path); err == nil || !strings.Contains(err.Error(), "max_roles") {
		t.Fatalf("different role aliases must fail explicitly, got %v", err)
	}
}

func TestTeamLimitsConcurrencyEnableRequiresPositiveMax(t *testing.T) {
	path := writeLimitConfig(t, `
limits:
  concurrency:
    teammate:
      enabled: true
      max: 0
`)
	if _, err := LoadLimits(path); err == nil || !strings.Contains(err.Error(), "concurrency.teammate.max") {
		t.Fatalf("enabled concurrency with max<=0 must fail, got %v", err)
	}

	path = writeLimitConfig(t, `
limits:
  concurrency:
    subagent:
      enabled: false
      max: 0
`)
	if _, err := LoadLimits(path); err != nil {
		t.Fatalf("disabled concurrency may leave max at zero: %v", err)
	}
}

func TestTeamLimitsWorkItemFailurePolicyParses(t *testing.T) {
	path := writeLimitConfig(t, `
limits:
  team:
    workitem_tool_failure:
      enabled: true
      threshold: 5
`)
	got, err := LoadLimits(path)
	if err != nil {
		t.Fatalf("LoadLimits: %v", err)
	}
	if !got.Team.WorkItemToolFailure.Enabled || got.Team.WorkItemToolFailure.Threshold != 5 {
		t.Fatalf("workitem failure policy not parsed: %+v", got.Team.WorkItemToolFailure)
	}
}

func TestTeamLimitsRejectNegativeValues(t *testing.T) {
	for name, body := range map[string]string{
		"max_roles": `
limits:
  team:
    max_roles: -1
`,
		"max_jobs": `
limits:
  team:
    max_jobs: -1
`,
		"failure_threshold": `
limits:
  team:
    workitem_tool_failure:
      threshold: -1
`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadLimits(writeLimitConfig(t, body)); err == nil {
				t.Fatal("negative value must be rejected")
			}
		})
	}
}
