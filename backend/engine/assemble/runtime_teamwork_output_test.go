package assemble

// runtime_teamwork_output_test.go — **宿主级**接线用例：产品自有作业输出面（实现已在团队域
// `backend/multiagent/teamwork/output_store.go`，P21-23 归位）与 Runtime 的接线。

import (
	"context"
	"os"
	"testing"

	seeteamwork "github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	workstore "github.com/RedHuang-0622/seelex/store/teamwork"
)

// TestWriteWorkerOutputRoutesThroughProductFaceAndDropsAfterClear 钉住 Runtime 侧的接线：
// 执行体的写**经产品面**（而不是各自 os.WriteFile），于是它与收口清目录串行；收口之后
// 迟到的写被丢弃，不留下残文件（devlog §4.3）。
func TestWriteWorkerOutputRoutesThroughProductFaceAndDropsAfterClear(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	outputs := seeteamwork.NewOutputStore(&outputStoreDirStub{dir: t.TempDir()}, func(id string) (dto.SessionKey, bool) {
		return dto.SessionKey{ProjectID: "project", SessionID: id}, true
	})
	backend := teamworkTestBackend(&memPlanStore{}, "s-team")
	backend.JobOutputs = outputs
	if err := r.SetTeamworkBackend(backend); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	path, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	if err := r.writeWorkerOutput(path, "本轮正文"); err != nil {
		t.Fatalf("writeWorkerOutput: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "本轮正文" {
		t.Fatalf("执行体的写应经产品面落盘：data=%q err=%v", data, err)
	}
	if err := outputs.ClearJobOutputs(ctx); err != nil {
		t.Fatalf("ClearJobOutputs: %v", err)
	}
	if err := r.writeWorkerOutput(path, "迟到的最后一次写"); err != nil {
		t.Fatalf("迟到的写应是静默丢弃：%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("收口之后不得留下残文件：stat err=%v", err)
	}
}

// outputStoreDirStub 只实现输出目录面（`store.TeamworkRepository` 的其余方法在本用例里不被触达：
// 接口是完整的，替身不做"只实现一半"）。
type outputStoreDirStub struct{ dir string }

func (s *outputStoreDirStub) WritePlan(dto.SessionKey, workstore.Plan, int) error {
	return nil
}

func (s *outputStoreDirStub) ReadPlan(dto.SessionKey) (workstore.Plan, error) {
	return workstore.Plan{}, nil
}

func (s *outputStoreDirStub) AppendEvent(dto.SessionKey, workstore.Event) error {
	return nil
}

func (s *outputStoreDirStub) ReadEvents(dto.SessionKey) ([]workstore.Event, error) {
	return nil, nil
}

func (s *outputStoreDirStub) JobOutputDir(dto.SessionKey) (string, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	return s.dir, nil
}

func (s *outputStoreDirStub) AppendBinding(dto.SessionKey, workstore.Binding) error {
	return nil
}

func (s *outputStoreDirStub) ReadBindings(dto.SessionKey) ([]workstore.Binding, error) {
	return nil, nil
}
