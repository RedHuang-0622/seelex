package gui

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/headless"
)

// fakeSubagents 满足 headless 的 SubagentRecovery 端口（owner：application/execution
// 的子代理用例）。方法名逐字等于 owner 的方法名。A 流的语义（七步模板、幂等收敛）
// 由 application/execution/resume 与 seelebridge 用例覆盖，这里只钉 headless 的
// 参数解码与分派落点。
type fakeSubagents struct {
	views    []dto.SubagentRecoveryView
	report   dto.SubagentResumeReport
	result   dto.SubagentResumeResult
	session  string
	node     string
	recoverC int
	forked   []dto.SubagentForkSpec
}

func (app *fakeSubagents) ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	app.session = sessionID
	return app.views, nil
}

func (app *fakeSubagents) ResumeInterrupted(_ context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	app.session = sessionID
	app.recoverC++
	return app.report, nil
}

func (app *fakeSubagents) Fork(_ context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	app.session = sessionID
	app.forked = append(app.forked, specs...)
	return "forked", nil
}

func (app *fakeSubagents) Resume(_ context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	app.session = sessionID
	app.node = nodeID
	return app.result, nil
}

func TestHeadlessSubagentRPC(t *testing.T) {
	app := &fakeSubagents{
		views: []dto.SubagentRecoveryView{{
			NodeID: "sub-1", SessionID: "node-hash-1", Goal: "inspect",
			Status: dto.SubAgentRunning, Active: true, Resumable: true,
		}},
		report: dto.SubagentResumeReport{
			SessionID: "main-1", Located: 1, Resumed: []string{"sub-1"},
			RecoveryNoteRole: "system",
		},
		result: dto.SubagentResumeResult{NodeID: "sub-1", Resumed: true, Steps: []string{"decide", "reexecute"}},
	}
	base := newHeadlessTestServer(t, headless.Dependencies{Subagents: app})

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
