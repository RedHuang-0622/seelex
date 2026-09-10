package gui

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// fakeSubagentApplication 在 headless RPC 单测里复刻 A 流的 subagent.* 扩展面；
// 语义（七步模板、幂等收敛）由 application/core/resume 与 seelebridge 测试
// 覆盖，这里只验证 headless 的参数解码与窄转发。
type fakeSubagentApplication struct {
	*fakeApplication
	views    []dto.SubagentRecoveryView
	report   dto.SubagentResumeReport
	result   dto.SubagentResumeResult
	session  string
	node     string
	recoverC int
	forked   []dto.SubagentForkSpec
}

func (app *fakeSubagentApplication) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	app.session = sessionID
	return app.views, nil
}

func (app *fakeSubagentApplication) ResumeInterruptedSubagents(_ context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	app.session = sessionID
	app.recoverC++
	return app.report, nil
}

func (app *fakeSubagentApplication) ForkSubagents(_ context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	app.session = sessionID
	app.forked = append(app.forked, specs...)
	return "forked", nil
}

func (app *fakeSubagentApplication) ResumeSubagent(_ context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	app.session = sessionID
	app.node = nodeID
	return app.result, nil
}

func TestHeadlessSubagentRPC(t *testing.T) {
	app := &fakeSubagentApplication{
		fakeApplication: newFakeApplication(),
		views: []dto.SubagentRecoveryView{{
			NodeID: "sub-1", SessionID: "node-hash-1", Goal: "inspect",
			Status: "running", Active: true, Resumable: true,
		}},
		report: dto.SubagentResumeReport{
			SessionID: "main-1", Located: 1, Resumed: []string{"sub-1"},
			RecoveryNoteRole: "system",
		},
		result: dto.SubagentResumeResult{NodeID: "sub-1", Resumed: true, Steps: []string{"decide", "reexecute"}},
	}
	base := newHeadlessTestServer(t, app)

	result := headlessRPC(t, base, "subagent.list", "main-1")
	if !result.OK || app.session != "main-1" {
		t.Fatalf("subagent.list = ok=%v err=%q session=%q", result.OK, result.Error, app.session)
	}
	result = headlessRPC(t, base, "subagent.recover", "main-1")
	if !result.OK || app.recoverC != 1 || app.session != "main-1" {
		t.Fatalf("subagent.recover = ok=%v err=%q calls=%d", result.OK, result.Error, app.recoverC)
	}
	result = headlessRPC(t, base, "subagent.fork", "main-1", []map[string]any{
		{"id": "sub-9", "goal": "inspect"},
	})
	if !result.OK || len(app.forked) != 1 || app.forked[0].ID != "sub-9" {
		t.Fatalf("subagent.fork = ok=%v err=%q forked=%+v", result.OK, result.Error, app.forked)
	}
	result = headlessRPC(t, base, "subagent.resume", "main-1", "sub-1")
	if !result.OK || app.node != "sub-1" {
		t.Fatalf("subagent.resume = ok=%v err=%q node=%q", result.OK, result.Error, app.node)
	}
	result = headlessRPC(t, base, "subagent.unknown", "main-1")
	if result.OK {
		t.Fatalf("未知 subagent 方法必须报错，得到 %+v", result)
	}
}
