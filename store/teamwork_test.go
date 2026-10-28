package store

import (
	"context"
	"encoding/json"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func teamworkFixture(t *testing.T) (*jsonRepository, Key) {
	t.Helper()
	repository, err := newJSONRepository(t.TempDir(), storeroot.Settings{})
	if err != nil {
		t.Fatalf("newJSONRepository: %v", err)
	}
	key := Key{ProjectID: "project-teamwork", SessionID: "session-teamwork"}
	return repository, key
}

func samplePlan() storeteamwork.Plan {
	return storeteamwork.Plan{
		TeamID:  "v-model",
		Version: 1,
		Members: []storeteamwork.Member{
			{Role: "pm", RoleSessionID: "v-model-pm"},
			{Role: "exec", RoleSessionID: "v-model-exec", Worktree: "seelex/exec"},
			{Role: "test_case", RoleSessionID: "v-model-test_case"},
			{Role: "tl", RoleSessionID: "v-model-tl"},
		},
		Milestones: []storeteamwork.Milestone{
			{ID: "m-req", Name: "需求", Required: []string{"pm"}, Status: "pending"},
			{ID: "m-impl", Name: "实现", Required: []string{"exec"}, DependsOn: []string{"m-req"}, Status: "pending"},
		},
		State: storeteamwork.State{Jobs: map[string]string{"exec": "a12"}},
	}
}

func TestTeamworkJobOutputDirIsSessionScopedAndIdempotent(t *testing.T) {
	repository, key := teamworkFixture(t)
	dir, err := repository.TeamworkJobOutputDir(context.Background(), key)
	if err != nil {
		t.Fatalf("TeamworkJobOutputDir: %v", err)
	}
	want := filepath.Join(repository.layout.sessionRoot(key), "teamwork", "jobs")
	if dir != want {
		t.Fatalf("输出目录必须落在会话目录内的 teamwork/jobs：got %q want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("目录必须真的被建出来（调用方拿到路径就要能写）：err=%v", err)
	}
	again, err := repository.TeamworkJobOutputDir(context.Background(), key)
	if err != nil || again != dir {
		t.Fatalf("同一会话重复取目录必须幂等：got %q err=%v", again, err)
	}
	if _, err := repository.TeamworkJobOutputDir(context.Background(), Key{}); err == nil {
		t.Fatal("空作用域键必须被拒绝（否则输出会落到一个不属于任何会话的目录上）")
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
	if len(loaded.Members) != 4 || len(loaded.Milestones) != 2 {
		t.Fatalf("plan shape mismatch: %+v", loaded)
	}
	if loaded.Milestones[1].DependsOn[0] != "m-req" {
		t.Fatalf("depends_on lost: %+v", loaded.Milestones[1])
	}
	if loaded.State.Jobs["exec"] != "a12" {
		t.Fatalf("state lost: %+v", loaded.State)
	}
}

// 计划**缺失** = "还没有 team_plan 过"，是一个状态，不是错误（2026-10-15 契约变更）。
//
// 现场：模型在没有团队计划的会话里调 team_items / team_context，收到
// `open …\metadata\teamwork.json: The system cannot find the path specified.`——一条内部
// 路径噪声，读面因此把"还没有团队"报成工具故障。同文件的 ReadTeamworkEvents 早已是
// "缺失即空集"，本次把同一口径补到计划这一路：读面宽容（空计划），写面自己判空并给领域
// 文案（见 backend/multiagent/teamwork 的 requireTeam）。
func TestReadTeamworkPlanMissingIsEmptyPlan(t *testing.T) {
	repository, key := teamworkFixture(t)
	plan, err := repository.ReadTeamworkPlan(context.Background(), key)
	if err != nil {
		t.Fatalf("缺少计划不是错误：%v", err)
	}
	if !storeteamwork.PlanIsEmpty(plan) {
		t.Fatalf("缺失必须读成空计划（IsEmpty 为真）：%+v", plan)
	}
}

func TestTeamworkPlanRejectsDynamicViolations(t *testing.T) {
	repository, key := teamworkFixture(t)
	cases := []struct {
		name         string
		mutate       func(*storeteamwork.Plan)
		maxTeammates int
		wantContains string
	}{
		{"over limit", func(plan *storeteamwork.Plan) {}, 3, "max_teammates"},
		{"duplicate role", func(plan *storeteamwork.Plan) {
			plan.Members[1].Role = "pm"
		}, 6, "重复"},
		{"builtin role", func(plan *storeteamwork.Plan) {
			plan.Members[0].Role = "main"
		}, 6, "内置角色"},
		{"duplicate session", func(plan *storeteamwork.Plan) {
			plan.Members[1].RoleSessionID = plan.Members[0].RoleSessionID
		}, 6, "role_session_id"},
		{"missing session", func(plan *storeteamwork.Plan) {
			plan.Members[0].RoleSessionID = ""
		}, 6, "role_session_id"},
		{"cyclic milestones", func(plan *storeteamwork.Plan) {
			plan.Milestones[0].DependsOn = []string{"m-impl"}
		}, 6, "环"},
		{"unknown milestone dependency", func(plan *storeteamwork.Plan) {
			plan.Milestones[1].DependsOn = []string{"nope"}
		}, 6, "不存在的里程碑"},
		{"self dependency", func(plan *storeteamwork.Plan) {
			plan.Milestones[1].DependsOn = []string{"m-impl"}
		}, 6, "依赖自己"},
		{"no milestones", func(plan *storeteamwork.Plan) {
			plan.Milestones = nil
		}, 6, "milestones"},
		{"milestone required unknown role", func(plan *storeteamwork.Plan) {
			plan.Milestones[0].Required = []string{"ghost"}
		}, 6, "required"},
		{"zero version", func(plan *storeteamwork.Plan) {
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
		plan.Members = append(plan.Members, storeteamwork.Member{Role: role, RoleSessionID: "v-model-" + role})
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
	rows := []storeteamwork.Event{
		{Kind: storeteamwork.EventPlan, TeamID: "v-model", Detail: "members=4 milestones=2"},
		{Kind: storeteamwork.EventDispatch, TeamID: "v-model", Role: "exec", Handle: "a12", Node: "wi-impl"},
		{Kind: storeteamwork.EventMilestone, TeamID: "v-model", Milestone: "m-impl", Detail: "impl 完成"},
		{Kind: storeteamwork.EventRetire, TeamID: "v-model", Role: "exec"},
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
	if err := repository.AppendTeamworkEvent(ctx, key, storeteamwork.Event{Kind: storeteamwork.EventJoin, Handle: "a12"}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	loaded, err = repository.ReadTeamworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkEvents: %v", err)
	}
	if len(loaded) != len(rows)+1 || loaded[0].Kind != rows[0].Kind || loaded[len(loaded)-1].Kind != storeteamwork.EventJoin {
		t.Fatalf("append did not preserve order: %+v", loaded)
	}
	if err := repository.AppendTeamworkEvent(ctx, key, storeteamwork.Event{}); err == nil {
		t.Fatal("an audit row without a kind must be refused")
	}
}

func TestTeamworkAuditSkipsCrashTail(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	if err := repository.AppendTeamworkEvent(ctx, key, storeteamwork.Event{Kind: storeteamwork.EventPlan}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	path := repository.layout.teamwork.EventPath(key)
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
	if len(loaded) != 1 || loaded[0].Kind != storeteamwork.EventPlan {
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
	if err := repository.AppendTeamworkEvent(ctx, key, storeteamwork.Event{Kind: storeteamwork.EventPlan}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	if _, err := os.Stat(repository.layout.teamwork.EventPath(key)); err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
}

// TestTeamworkPlanClosedRoundTrip：闭板三字段（state / closed_at / closed_reason）
// 必须整份落盘再读回——U3 裁决把闭板事实的域内权威放在 plan.State；未收口时三字段
// 缺省（omitempty 的语义是"没有这条事实"，不是"合成一个默认值"）。
func TestTeamworkPlanClosedRoundTrip(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	closed := samplePlan()
	closed.State.State = storeteamwork.StateClosed
	closed.State.ClosedAt = 1760000000
	closed.State.ClosedReason = dto.BoardCloseTeamClose
	if err := repository.WriteTeamworkPlan(ctx, key, closed, 6); err != nil {
		t.Fatalf("WriteTeamworkPlan(closed): %v", err)
	}
	loaded, err := repository.ReadTeamworkPlan(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkPlan: %v", err)
	}
	if loaded.State.State != storeteamwork.StateClosed {
		t.Fatalf("收口态未落盘：state=%q，want %q", loaded.State.State, storeteamwork.StateClosed)
	}
	if loaded.State.ClosedAt != closed.State.ClosedAt || loaded.State.ClosedReason != dto.BoardCloseTeamClose {
		t.Fatalf("收口事实未落盘：%+v", loaded.State)
	}
	// 未收口的计划读出来仍是"没有闭板事实"。
	open := samplePlan()
	if err := repository.WriteTeamworkPlan(ctx, key, open, 6); err != nil {
		t.Fatalf("WriteTeamworkPlan(open): %v", err)
	}
	loaded, err = repository.ReadTeamworkPlan(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkPlan(open): %v", err)
	}
	if loaded.State.State != "" || loaded.State.ClosedAt != 0 || loaded.State.ClosedReason != "" {
		t.Fatalf("未收口的计划不得带闭板事实：%+v", loaded.State)
	}
}

// TestTeamworkPlanColdReadsLegacyStagesPayload：阶段制时代留在计划里的 `stages`
// / `order_roles` 未知字段必须**照读不误**（Go JSON 忽略未知字段），且冷读**不改盘**
// ——旧 head 里的 `stages` 逐字留在盘上，读侧不做任何 destructive rewrite（A10）。
func TestTeamworkPlanColdReadsLegacyStagesPayload(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	// 手写一份"旧版本写过"的计划 head payload：既有里程碑口径（现行事实），
	// 也带阶段制时代已退场的 stages / order_roles / after。
	legacy := []byte(`{
	  "team_id": "v-model",
	  "version": 1,
	  "members": [
	    {"role": "pm", "role_session_id": "v-model-pm"},
	    {"role": "exec", "role_session_id": "v-model-exec", "worktree": "seelex/exec"}
	  ],
	  "milestones": [
	    {"id": "m-req", "name": "需求", "required": ["pm"], "status": "pending"},
	    {"id": "m-impl", "name": "实现", "required": ["exec"], "depends_on": ["m-req"], "status": "pending"}
	  ],
	  "stages": [
	    {"id": "req", "name": "需求阶段"},
	    {"id": "impl", "name": "实现阶段", "after": "req"}
	  ],
	  "order_roles": ["pm", "exec"]
	}`)
	if err := repository.layout.commitModuleHead(key, moduleTeamwork, "", json.RawMessage(legacy)); err != nil {
		t.Fatalf("写入旧 shape 计划 head: %v", err)
	}

	plan, err := repository.ReadTeamworkPlan(ctx, key)
	if err != nil {
		t.Fatalf("旧计划必须能冷读（未知字段忽略，不是错误）：%v", err)
	}
	if plan.TeamID != "v-model" || plan.Version != 1 || len(plan.Members) != 2 || len(plan.Milestones) != 2 {
		t.Fatalf("旧计划的成员/里程碑必须照旧解出：%+v", plan)
	}
	if len(plan.Milestones[1].DependsOn) != 1 || plan.Milestones[1].DependsOn[0] != "m-req" {
		t.Fatalf("里程碑屏障（现行顺序事实）必须解出：%+v", plan.Milestones[1])
	}

	// 只读冷读不得改写旧 head：stages 字段仍在盘上。
	raw, err := storeroot.ReadSharedFile(repository.layout.modulePath(key, moduleTeamwork))
	if err != nil {
		t.Fatalf("读旧 head 文件: %v", err)
	}
	if !strings.Contains(string(raw), `"stages"`) {
		t.Fatal("只读冷读不得改写旧 head：旧 stages 载荷必须原样留在盘上")
	}

	// 产品自己的整份替换写路径按当前 schema 接受这份计划：stages 不会复活成顺序事实。
	if err := repository.WriteTeamworkPlan(ctx, key, plan, 6); err != nil {
		t.Fatalf("按当前 schema 重新落盘旧计划: %v", err)
	}
	reloaded, err := repository.ReadTeamworkPlan(ctx, key)
	if err != nil {
		t.Fatalf("重新落盘后读回: %v", err)
	}
	if reloaded.TeamID != plan.TeamID || len(reloaded.Members) != len(plan.Members) ||
		len(reloaded.Milestones) != len(plan.Milestones) {
		t.Fatalf("重新落盘必须保形：%+v", reloaded)
	}
}

// TestTeamworkAuditColdReadsLegacyRetireRow：旧口径 `team_retire` 留在审计流里的
// 行必须还能被读回（`storeteamwork.EventRetire` 常量与解码保留的唯一理由），且审计是
// **只追加不重写**——新写入不覆盖旧行（A10）。
func TestTeamworkAuditColdReadsLegacyRetireRow(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	path := repository.layout.teamwork.EventPath(key)
	if err := os.MkdirAll(repository.layout.teamwork.Dir(key), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// 手写一行旧版本追加的审计（schema 与今天同形：多出的未知字段也会被忽略）。
	legacyRow := `{"at":"2026-09-20T03:04:05Z","kind":"retire","team_id":"v-model","role":"exec","detail":"旧口径退场行"}`
	if err := os.WriteFile(path, []byte(legacyRow+"\n"), 0o600); err != nil {
		t.Fatalf("写旧审计行: %v", err)
	}

	events, err := repository.ReadTeamworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != storeteamwork.EventRetire || events[0].Role != "exec" {
		t.Fatalf("旧 retire 审计行必须解出：%+v", events)
	}

	if err := repository.AppendTeamworkEvent(ctx, key, storeteamwork.Event{Kind: storeteamwork.EventClose, TeamID: "v-model"}); err != nil {
		t.Fatalf("AppendTeamworkEvent: %v", err)
	}
	raw, err := storeroot.ReadSharedFile(path)
	if err != nil {
		t.Fatalf("读审计文件: %v", err)
	}
	if !strings.Contains(string(raw), legacyRow) {
		t.Fatal("审计追加不得重写旧 retire 行")
	}
}
