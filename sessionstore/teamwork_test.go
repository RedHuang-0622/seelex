package sessionstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func teamworkFixture(t *testing.T) (*jsonRepository, Key) {
	t.Helper()
	repository, err := newJSONRepository(t.TempDir(), storageSettings{})
	if err != nil {
		t.Fatalf("newJSONRepository: %v", err)
	}
	key := Key{ProjectID: "project-teamwork", SessionID: "session-teamwork"}
	return repository, key
}

func samplePlan() TeamworkPlan {
	return TeamworkPlan{
		TeamID:  "v-model",
		Version: 1,
		Stages: []TeamworkStage{
			{ID: "req", Roles: []string{"pm"}},
			{ID: "impl", Roles: []string{"exec"}, DependsOn: []string{"req"}},
			{ID: "test", Roles: []string{"test_case"}, DependsOn: []string{"impl"}},
			{ID: "review", Roles: []string{"tl"}, DependsOn: []string{"test"}},
		},
		Members: []TeamworkMember{
			{Role: "pm", RoleSessionID: "v-model-pm"},
			{Role: "exec", RoleSessionID: "v-model-exec", Worktree: "seelex/exec"},
			{Role: "test_case", RoleSessionID: "v-model-test_case"},
			{Role: "tl", RoleSessionID: "v-model-tl"},
		},
		Milestones: []TeamworkMilestone{
			{ID: "m-impl", After: []string{"impl"}, Required: []string{"exec"}, Status: "pending"},
		},
		State: TeamworkState{Stage: "impl", Jobs: map[string]string{"exec": "a12"}},
	}
}

func TestTeamworkPlanRoundTrip(t *testing.T) {
	repository, key := teamworkFixture(t)
	plan := samplePlan()
	if err := repository.WriteTeamworkPlan(context.Background(), key, plan, 6); err != nil {
		t.Fatalf("WriteTeamworkPlan: %v", err)
	}
	loaded, err := repository.ReadTeamworkPlan(context.Background(), key)
	if err != nil {
		t.Fatalf("ReadTeamworkPlan: %v", err)
	}
	if loaded.TeamID != plan.TeamID || loaded.Version != plan.Version {
		t.Fatalf("plan header mismatch: %+v", loaded)
	}
	if len(loaded.Stages) != 4 || len(loaded.Members) != 4 || len(loaded.Milestones) != 1 {
		t.Fatalf("plan shape mismatch: %+v", loaded)
	}
	if loaded.Stages[1].DependsOn[0] != "req" {
		t.Fatalf("depends_on lost: %+v", loaded.Stages[1])
	}
	if loaded.State.Jobs["exec"] != "a12" {
		t.Fatalf("state lost: %+v", loaded.State)
	}
}

func TestReadTeamworkPlanMissingIsNotExist(t *testing.T) {
	repository, key := teamworkFixture(t)
	if _, err := repository.ReadTeamworkPlan(context.Background(), key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read of a missing plan err = %v, want fs.ErrNotExist", err)
	}
}

func TestTeamworkPlanRejectsDynamicViolations(t *testing.T) {
	repository, key := teamworkFixture(t)
	cases := []struct {
		name         string
		mutate       func(*TeamworkPlan)
		maxTeammates int
		wantContains string
	}{
		{"over limit", func(plan *TeamworkPlan) {}, 3, "max_teammates"},
		{"duplicate role", func(plan *TeamworkPlan) {
			plan.Members[1].Role = "pm"
		}, 6, "重复"},
		{"builtin role", func(plan *TeamworkPlan) {
			plan.Members[0].Role = "main"
		}, 6, "内置角色"},
		{"duplicate session", func(plan *TeamworkPlan) {
			plan.Members[1].RoleSessionID = plan.Members[0].RoleSessionID
		}, 6, "role_session_id"},
		{"missing session", func(plan *TeamworkPlan) {
			plan.Members[0].RoleSessionID = ""
		}, 6, "role_session_id"},
		{"cyclic stages", func(plan *TeamworkPlan) {
			plan.Stages[0].DependsOn = []string{"review"}
		}, 6, "环"},
		{"unknown dependency", func(plan *TeamworkPlan) {
			plan.Stages[1].DependsOn = []string{"nope"}
		}, 6, "不存在的阶段"},
		{"self dependency", func(plan *TeamworkPlan) {
			plan.Stages[1].DependsOn = []string{"impl"}
		}, 6, "依赖自己"},
		{"milestone after unknown stage", func(plan *TeamworkPlan) {
			plan.Milestones[0].After = []string{"nope"}
		}, 6, "after"},
		{"milestone required unknown role", func(plan *TeamworkPlan) {
			plan.Milestones[0].Required = []string{"ghost"}
		}, 6, "required"},
		{"stage without roles", func(plan *TeamworkPlan) {
			plan.Stages[0].Roles = nil
		}, 6, "至少要指定一个角色"},
		{"zero version", func(plan *TeamworkPlan) {
			plan.Version = 0
		}, 6, "version"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := samplePlan()
			testCase.mutate(&plan)
			err := repository.WriteTeamworkPlan(context.Background(), key, plan, testCase.maxTeammates)
			if err == nil {
				t.Fatalf("plan was accepted, want rejection containing %q", testCase.wantContains)
			}
			if !strings.Contains(err.Error(), testCase.wantContains) {
				t.Fatalf("error %q does not mention %q", err.Error(), testCase.wantContains)
			}
		})
	}
}

func TestTeamworkMaxTeammatesDefaultsToSix(t *testing.T) {
	repository, key := teamworkFixture(t)
	plan := samplePlan()
	for index := 0; index < 3; index++ {
		role := []string{"a", "b", "c"}[index]
		plan.Members = append(plan.Members, TeamworkMember{Role: role, RoleSessionID: "v-model-" + role})
	}
	// 7 members vs the default ceiling of 6.
	if err := repository.WriteTeamworkPlan(context.Background(), key, plan, 6); err == nil {
		t.Fatal("7 members must be refused at max_teammates=6")
	}
	plan.Members = plan.Members[:6]
	if err := repository.WriteTeamworkPlan(context.Background(), key, plan, 6); err != nil {
		t.Fatalf("6 members must be accepted at max_teammates=6: %v", err)
	}
}

func TestTeamworkAuditIsAppendOnly(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	rows := []TeamworkEvent{
		{Kind: TeamworkEventPlan, TeamID: "v-model", Detail: "stages=4"},
		{Kind: TeamworkEventDispatch, TeamID: "v-model", Stage: "impl", Role: "exec", Handle: "a12", Node: "impl"},
		{Kind: TeamworkEventMilestone, TeamID: "v-model", Milestone: "m-impl", Detail: "impl 完成"},
		{Kind: TeamworkEventRetire, TeamID: "v-model", Role: "exec"},
	}
	for _, row := range rows {
		if err := repository.AppendTeamworkEvent(ctx, key, row); err != nil {
			t.Fatalf("AppendTeamworkEvent: %v", err)
		}
	}
	loaded, err := repository.ReadTeamworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkEvents: %v", err)
	}
	if len(loaded) != len(rows) {
		t.Fatalf("audit rows = %d, want %d", len(loaded), len(rows))
	}
	for index := range rows {
		if loaded[index].Kind != rows[index].Kind || loaded[index].Role != rows[index].Role {
			t.Fatalf("row %d mismatch: %+v", index, loaded[index])
		}
		if loaded[index].At.IsZero() {
			t.Fatalf("row %d has no timestamp", index)
		}
	}
	// 追加不重写：再读一次仍是同样的前缀，且新行append在尾部。
	if err := repository.AppendTeamworkEvent(ctx, key, TeamworkEvent{Kind: TeamworkEventJoin, Handle: "a12"}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	loaded, err = repository.ReadTeamworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkEvents: %v", err)
	}
	if len(loaded) != len(rows)+1 || loaded[0].Kind != rows[0].Kind || loaded[len(loaded)-1].Kind != TeamworkEventJoin {
		t.Fatalf("append did not preserve order: %+v", loaded)
	}
	if err := repository.AppendTeamworkEvent(ctx, key, TeamworkEvent{}); err == nil {
		t.Fatal("an audit row without a kind must be refused")
	}
}

func TestTeamworkAuditSkipsCrashTail(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	if err := repository.AppendTeamworkEvent(ctx, key, TeamworkEvent{Kind: TeamworkEventPlan}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	path := repository.layout.teamworkEventsPath(key)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	// 半行残尾：崩溃时最常见的形态。
	if _, err := file.WriteString(`{"kind":"dispatch","role":"ex`); err != nil {
		t.Fatalf("write crash tail: %v", err)
	}
	_ = file.Close()
	loaded, err := repository.ReadTeamworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkEvents must tolerate a crash tail: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Kind != TeamworkEventPlan {
		t.Fatalf("crash tail changed the readable audit: %+v", loaded)
	}
}

func TestTeamworkStorageKeepsPlanAndAuditSeparate(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	if err := repository.WriteTeamworkPlan(ctx, key, samplePlan(), 6); err != nil {
		t.Fatalf("WriteTeamworkPlan: %v", err)
	}
	// 计划是 head（整份替换），审计是数据文件（只追加）：两者物理不同文件。
	if _, err := os.Stat(repository.layout.modulePath(key, moduleTeamwork)); err != nil {
		t.Fatalf("plan head missing: %v", err)
	}
	if err := repository.AppendTeamworkEvent(ctx, key, TeamworkEvent{Kind: TeamworkEventPlan}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository.layout.teamworkDir(key), teamworkEventFile)); err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
}
