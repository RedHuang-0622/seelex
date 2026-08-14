package session

import (
	"context"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

type stubStageCompleter struct{}

func (stubStageCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{}, nil
}

func (stubStageCompleter) CompleteStream(context.Context, []types.Message, []types.Tool, func(string)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

func (stubStageCompleter) CompleteStreamEvents(context.Context, []types.Message, []types.Tool, func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

type stubStageAgent struct{}

func (stubStageAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (stubStageAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (stubStageAgent) LLM() types.ChatCompleter { return stubStageCompleter{} }

// TestSubagentStageLogsAndSemanticResult 验证 node 第一视角数据面：
//   - 同一 node 的多阶段日志共享 SessionID（同一 subagent 认证）；
//   - turn 阶段按序编号；
//   - 语义结果登记后自动附带阶段日志，并经语义结果队列 drain。
func TestSubagentStageLogsAndSemanticResult(t *testing.T) {
	rec := NewSubagentSessions(nil)
	defer rec.Close()

	sess, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: stubStageAgent{}, SessionID: "stage-sess-1",
	})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	rec.Register("n1", sess, "goal-1")

	rec.RecordStage("n1", model.NodeStageLog{Stage: model.NodeStageSpawn, Preview: "spawn"})
	rec.RecordStage("n1", model.NodeStageLog{Stage: model.NodeStageTurn, Preview: "llm-1"})
	rec.RecordStage("n1", model.NodeStageLog{Stage: model.NodeStageTurn, Preview: "llm-2"})
	rec.RecordStage("n1", model.NodeStageLog{Stage: model.NodeStageTool, Preview: "bash"})

	logs := rec.StageLogs("n1")
	if len(logs) != 4 {
		t.Fatalf("stage logs = %d, want 4", len(logs))
	}
	// 实时推送通道：4 条阶段日志逐一即时投递（同序）。
	var pushed []model.NodeStageLog
	for index := 0; index < 4; index++ {
		select {
		case event := <-rec.StageEvents():
			pushed = append(pushed, event)
		case <-time.After(2 * time.Second):
			t.Fatalf("stage event %d not pushed within 2s", index)
		}
	}
	if len(pushed) != 4 || pushed[0].Stage != model.NodeStageSpawn || pushed[3].Stage != model.NodeStageTool {
		t.Fatalf("pushed stage events out of order: %+v", pushed)
	}
	for index, log := range logs {
		if log.NodeID != "n1" {
			t.Fatalf("log %d node id = %q, want n1", index, log.NodeID)
		}
		if log.SessionID != "stage-sess-1" {
			t.Fatalf("log %d session id = %q, want stage-sess-1 (same-subagent certification)", index, log.SessionID)
		}
	}
	if logs[1].Turn != 1 || logs[2].Turn != 2 {
		t.Fatalf("turn numbering = %d/%d, want 1/2", logs[1].Turn, logs[2].Turn)
	}

	rec.RecordResult("n1", &model.NodeSemanticResult{
		SchemaVersion: model.NodeSemanticSchemaVersion,
		NodeID:        "n1",
		Status:        "completed",
		Output:        "out",
	})
	res := rec.Result("n1")
	if res == nil {
		t.Fatal("semantic result missing")
	}
	if res.SessionID != "stage-sess-1" {
		t.Fatalf("result session id = %q, want stage-sess-1", res.SessionID)
	}
	if len(res.Stages) != 4 {
		t.Fatalf("result stages = %d, want 4", len(res.Stages))
	}

	drained := rec.DrainResults()
	if len(drained) != 1 || drained[0].NodeID != "n1" {
		t.Fatalf("drained results = %+v, want [n1]", drained)
	}
	if again := rec.DrainResults(); len(again) != 0 {
		t.Fatalf("second drain = %d, want 0", len(again))
	}
	if rec.Result("n1") == nil {
		t.Fatal("result must be retained for read after queue drain")
	}
}
