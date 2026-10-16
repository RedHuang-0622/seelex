package resume

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakePort 记录调用轨迹，便于断言步骤顺序与幂等性。
type fakePort struct {
	mu sync.Mutex

	units       []Unit
	locateErr   error
	repairErr   error
	sceneErr    error
	injectErr   error
	convergeErr error
	outcome     Outcome
	reexecErr   error

	repairs   []string
	restores  []string
	injects   []string
	reexecs   []string
	converges []string

	reexecEntered chan struct{}
	reexecRelease chan struct{}
}

func (p *fakePort) Locate(context.Context) ([]Unit, error) {
	if p.locateErr != nil {
		return nil, p.locateErr
	}
	return append([]Unit(nil), p.units...), nil
}

func (p *fakePort) RepairParent(_ context.Context, unit Unit) error {
	p.mu.Lock()
	p.repairs = append(p.repairs, unit.Key)
	p.mu.Unlock()
	return p.repairErr
}

func (p *fakePort) RestoreScene(_ context.Context, unit Unit) (Scene, error) {
	p.mu.Lock()
	p.restores = append(p.restores, unit.Key)
	p.mu.Unlock()
	if p.sceneErr != nil {
		return Scene{}, p.sceneErr
	}
	return Scene{Payload: []byte(`{"goal":"` + unit.Goal + `"}`), Summary: "scene"}, nil
}

func (p *fakePort) InjectNote(_ context.Context, unit Unit, _ Scene) error {
	p.mu.Lock()
	p.injects = append(p.injects, unit.Key)
	p.mu.Unlock()
	return p.injectErr
}

func (p *fakePort) Reexecute(_ context.Context, unit Unit, _ Scene) (Outcome, error) {
	p.mu.Lock()
	p.reexecs = append(p.reexecs, unit.Key)
	entered := p.reexecEntered
	release := p.reexecRelease
	p.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if p.reexecErr != nil {
		return Outcome{}, p.reexecErr
	}
	return p.outcome, nil
}

func (p *fakePort) Converge(_ context.Context, unit Unit, _ Outcome) error {
	p.mu.Lock()
	p.converges = append(p.converges, unit.Key)
	p.mu.Unlock()
	return p.convergeErr
}

func (p *fakePort) calls() (repairs, restores, injects, reexecs, converges []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.repairs...), append([]string(nil), p.restores...),
		append([]string(nil), p.injects...), append([]string(nil), p.reexecs...),
		append([]string(nil), p.converges...)
}

func TestOrderIsStable(t *testing.T) {
	want := "locate,decide,repair_parent,restore_scene,inject_note,reexecute,converge"
	got := make([]string, 0, 7)
	for _, step := range Order() {
		got = append(got, string(step))
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("模板步骤顺序 = %v, want %s", got, want)
	}
}

func TestExecuteActiveUnitRunsAllSteps(t *testing.T) {
	port := &fakePort{
		units:   []Unit{{Key: "node-1", Kind: "subagent", Status: UnitActive, Goal: "g"}},
		outcome: Outcome{Status: UnitDone, Summary: "ok"},
	}
	report, err := NewRunner(port).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Located != 1 || len(report.Units) != 1 {
		t.Fatalf("report = %+v", report)
	}
	unit := report.Units[0]
	want := "decide,repair_parent,restore_scene,inject_note,reexecute,converge"
	if joined := joinSteps(unit.Steps); joined != want {
		t.Fatalf("steps = %s, want %s", joined, want)
	}
	if !unit.Resumed || unit.Skipped || unit.Failed() {
		t.Fatalf("unit = %+v", unit)
	}
	if keys := report.Resumed(); len(keys) != 1 || keys[0] != "node-1" {
		t.Fatalf("resumed = %v", keys)
	}
	_, restores, injects, reexecs, converges := port.calls()
	if len(restores) != 1 || len(injects) != 1 || len(reexecs) != 1 || len(converges) != 1 {
		t.Fatalf("port calls: restores=%v injects=%v reexecs=%v converges=%v",
			restores, injects, reexecs, converges)
	}
}

func TestExecuteTerminalUnitRepairsHistoryOnly(t *testing.T) {
	port := &fakePort{units: []Unit{
		{Key: "node-done", Status: UnitDone},
		{Key: "node-failed", Status: UnitFailed},
	}}
	report, err := NewRunner(port).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(report.Units) != 2 {
		t.Fatalf("units = %+v", report.Units)
	}
	repairs, restores, injects, reexecs, converges := port.calls()
	if len(repairs) != 2 {
		t.Fatalf("terminal units must still repair parent history: %v", repairs)
	}
	if len(converges) != 2 {
		t.Fatalf("terminal units must still converge residual scene: %v", converges)
	}
	if len(restores)+len(injects)+len(reexecs) != 0 {
		t.Fatalf("terminal units must not restart: restores=%v injects=%v reexecs=%v",
			restores, injects, reexecs)
	}
	for _, unit := range report.Units {
		if !unit.Skipped || unit.Resumed {
			t.Fatalf("unit = %+v", unit)
		}
	}
	if skipped := report.Skipped(); len(skipped) != 2 {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestExecuteDeduplicatesByKeyAndIgnoresEmptyKeys(t *testing.T) {
	port := &fakePort{
		units: []Unit{
			{Key: "node-1", Status: UnitActive},
			{Key: "node-1", Status: UnitActive},
			{Key: "", Status: UnitActive},
		},
		outcome: Outcome{Status: UnitDone},
	}
	report, err := NewRunner(port).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Located != 1 || len(report.Units) != 1 {
		t.Fatalf("report = %+v", report)
	}
	_, _, _, reexecs, _ := port.calls()
	if len(reexecs) != 1 {
		t.Fatalf("duplicate keys must re-run once: %v", reexecs)
	}
}

func TestExecuteFailureIsReportedAndRetryable(t *testing.T) {
	port := &fakePort{
		units:     []Unit{{Key: "node-1", Status: UnitActive}},
		reexecErr: errors.New("provider boom"),
	}
	runner := NewRunner(port)
	first, err := runner.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if failures := first.Failed(); len(failures) != 1 {
		t.Fatalf("failures = %+v", failures)
	}
	if !strings.Contains(first.Units[0].Err, "provider boom") {
		t.Fatalf("err = %q", first.Units[0].Err)
	}
	// 失败必须走收敛（留下可重试的未完成事实），且不得阻塞下一次重试。
	_, _, _, _, converges := port.calls()
	if len(converges) != 1 {
		t.Fatalf("failure must still converge: %v", converges)
	}
	second, err := runner.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute(retry): %v", err)
	}
	if second.Units[0].Skipped {
		t.Fatalf("failed unit must be retryable, got %+v", second.Units[0])
	}
}

func TestExecuteSkipsKeyAlreadyInFlight(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	port := &fakePort{
		units:         []Unit{{Key: "node-1", Status: UnitActive}},
		outcome:       Outcome{Status: UnitDone},
		reexecEntered: entered,
		reexecRelease: release,
	}
	runner := NewRunner(port)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Execute(context.Background())
	}()
	<-entered
	result, err := runner.ExecuteKey(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("ExecuteKey: %v", err)
	}
	if !result.Skipped || result.Resumed {
		t.Fatalf("concurrent same-key resume must skip, got %+v", result)
	}
	if !strings.Contains(result.Summary, "in flight") {
		t.Fatalf("summary = %q", result.Summary)
	}
	close(release)
	<-done
}

func TestExecuteKeyReportsUnknownUnit(t *testing.T) {
	port := &fakePort{units: []Unit{{Key: "node-1", Status: UnitActive}}}
	result, err := NewRunner(port).ExecuteKey(context.Background(), "missing")
	if err != nil {
		t.Fatalf("ExecuteKey: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("result = %+v", result)
	}
	_, _, _, reexecs, _ := port.calls()
	if len(reexecs) != 0 {
		t.Fatalf("unknown key must not re-execute: %v", reexecs)
	}
}

func TestExecuteRequiresPort(t *testing.T) {
	if _, err := NewRunner(nil).Execute(context.Background()); !errors.Is(err, ErrPortNotAssembled) {
		t.Fatalf("err = %v, want ErrPortNotAssembled", err)
	}
}

func TestExecuteLocateErrorIsBlocking(t *testing.T) {
	port := &fakePort{locateErr: errors.New("store down")}
	_, err := NewRunner(port).Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "store down") {
		t.Fatalf("err = %v", err)
	}
}

func joinSteps(steps []Step) string {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, string(step))
	}
	return strings.Join(names, ",")
}
