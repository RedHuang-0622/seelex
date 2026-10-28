package teamwork

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	workstore "github.com/RedHuang-0622/seelex/store/teamwork"
)

// output_store_test.go — （P21-23 随实现从 backend/engine/assemble/runtime_teamwork_output_test.go
// 搬进本域，断言逐字）产品自有作业输出面的行为（S5 / §4.7 输出归属）。
//
// 钉三件事：① 每次派发一个**互不覆盖**的路径，且都落在会话自己的目录里；② 角色名进
// 路径前被折成安全片段（`..` / 分隔符不能走到文件系统上）；③ 收口清理清的是**整目录**、
// **作废**这一批落点（迟到的写被丢弃、目录不会被重新造出来），且进程内**不复用**落点。

// fakeTeamworkStore 只实现输出目录面；计划 / 审计面在本用例里不被触达（返回零值即可，
// 但它们必须存在——接口是完整的，不做"只实现一半"的替身）。
type fakeTeamworkStore struct {
	dir string
}

func (s *fakeTeamworkStore) WritePlan(dto.SessionKey, workstore.Plan, int) error {
	return nil
}

func (s *fakeTeamworkStore) ReadPlan(dto.SessionKey) (workstore.Plan, error) {
	return workstore.Plan{}, nil
}

func (s *fakeTeamworkStore) AppendEvent(dto.SessionKey, workstore.Event) error {
	return nil
}

func (s *fakeTeamworkStore) ReadEvents(dto.SessionKey) ([]workstore.Event, error) {
	return nil, nil
}

func (s *fakeTeamworkStore) JobOutputDir(dto.SessionKey) (string, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	return s.dir, nil
}

func (s *fakeTeamworkStore) AppendBinding(dto.SessionKey, workstore.Binding) error {
	return nil
}

func (s *fakeTeamworkStore) ReadBindings(dto.SessionKey) ([]workstore.Binding, error) {
	return nil, nil
}

func newOutputStoreFixture(t *testing.T) (*OutputStore, string, context.Context) {
	t.Helper()
	dir := t.TempDir()
	st := &fakeTeamworkStore{dir: dir}
	outputs := NewOutputStore(st, func(sessionID string) (dto.SessionKey, bool) {
		return dto.SessionKey{ProjectID: "project", SessionID: sessionID}, true
	})
	if outputs == nil {
		t.Fatal("NewOutputStore 在给了 store 与 keyFor 时不该返回 nil")
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "session-1")
	return outputs, dir, ctx
}

func TestOutputStoreAllocatesDistinctPathsPerDispatch(t *testing.T) {
	t.Parallel()
	outputs, dir, ctx := newOutputStoreFixture(t)

	first, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	second, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	if first == second {
		t.Fatalf("同一角色的连续两轮不能分到同一个文件（第 n+1 轮会覆盖第 n 轮的正文）：%q", first)
	}
	for _, path := range []string{first, second} {
		if filepath.Dir(path) != dir {
			t.Fatalf("输出路径必须落在会话自己的目录里：got %q want dir %q", path, dir)
		}
	}
	if filepath.Base(first) != "exec-1.log" || filepath.Base(second) != "exec-2.log" {
		t.Fatalf("文件名应是 <role>-<n>.log 且序号递增：%q / %q", filepath.Base(first), filepath.Base(second))
	}
}

func TestOutputStoreSanitizesRoleName(t *testing.T) {
	t.Parallel()
	outputs, dir, ctx := newOutputStoreFixture(t)

	path, err := outputs.JobOutputPath(ctx, "../evil/../x")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	// 安全判据是"名字里不再有分隔符"（因此它不可能把路径带出目录），而不是"看起来像什么"：
	// `..` 片段留在名字里无害（它只是普通字符），能走出目录才有害。
	if name := filepath.Base(path); strings.ContainsAny(name, `/\`) {
		t.Fatalf("角色名进路径前必须折成安全片段：%q", name)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("恶意角色名不能把路径带出会话目录：%q", path)
	}
	// 空角色名也不能把路径退化成目录本身。
	if path, err := outputs.JobOutputPath(ctx, "  "); err != nil {
		t.Fatalf("JobOutputPath(空角色): %v", err)
	} else if filepath.Dir(path) != dir {
		t.Fatalf("空角色名必须退回一个安全的文件名：%q", path)
	}
}

func TestOutputStoreClearRemovesDirAndRetiresPaths(t *testing.T) {
	t.Parallel()
	outputs, dir, ctx := newOutputStoreFixture(t)

	path, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	if err := outputs.WriteJobOutput(path, "正文"); err != nil {
		t.Fatalf("WriteJobOutput: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "正文" {
		t.Fatalf("在册落点的写必须落盘：data=%q err=%v", data, err)
	}
	if err := outputs.ClearJobOutputs(ctx); err != nil {
		t.Fatalf("ClearJobOutputs: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("收口清理必须清掉整个输出目录：stat err=%v", err)
	}
	// 作废落点上迟到的写被**丢弃**：不把文件/目录重新造出来（S5 残边，devlog §4.3）。
	if err := outputs.WriteJobOutput(path, "迟到的写"); err != nil {
		t.Fatalf("作废落点的写应是静默丢弃（不是错误）：%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("作废落点上的迟到写不得把目录造回来：stat err=%v", err)
	}
	// 进程内**不复用**落点：清过目录之后下一次派发换新序号（否则旧回合的迟到写会打到
	// 新团队的同名文件上）。
	again, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath（清理后）: %v", err)
	}
	if again == path || filepath.Base(again) != "exec-2.log" {
		t.Fatalf("清理后不得复用同一个落点，应换新序号：%q", filepath.Base(again))
	}
	// 幂等：目录已经不在，再清一次不是失败。
	if err := outputs.ClearJobOutputs(ctx); err != nil {
		t.Fatalf("重复清理不该失败（目录不存在 = 无事可做）：%v", err)
	}
}

func TestOutputStoreWriteRefusesForeignPath(t *testing.T) {
	t.Parallel()
	outputs, _, _ := newOutputStoreFixture(t)
	stray := filepath.Join(t.TempDir(), "stray.log")
	// 从没经本面分配的落点：写被丢弃，不落到别人的路径上。
	if err := outputs.WriteJobOutput(stray, "x"); err != nil {
		t.Fatalf("未登记落点的写应是静默丢弃：%v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("未登记落点不得被写出来：stat err=%v", err)
	}
	// 空落点是**调用错误**，不是静默丢弃。
	if err := outputs.WriteJobOutput("  ", "x"); err == nil {
		t.Fatal("空落点必须显式报错")
	}
}

func TestOutputStoreLatestJobOutputPathPicksNewest(t *testing.T) {
	t.Parallel()
	outputs, _, ctx := newOutputStoreFixture(t)

	if _, ok, err := outputs.LatestJobOutputPath(ctx, "exec"); err != nil || ok {
		t.Fatalf("没派过活时不该定位到正文：ok=%v err=%v", ok, err)
	}
	first, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	second, err := outputs.JobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("JobOutputPath: %v", err)
	}
	if err := outputs.WriteJobOutput(first, "第一轮"); err != nil {
		t.Fatalf("WriteJobOutput(first): %v", err)
	}
	if err := outputs.WriteJobOutput(second, "第二轮"); err != nil {
		t.Fatalf("WriteJobOutput(second): %v", err)
	}
	path, ok, err := outputs.LatestJobOutputPath(ctx, "exec")
	if err != nil {
		t.Fatalf("LatestJobOutputPath: %v", err)
	}
	if !ok || path != second {
		t.Fatalf("必须定位到序号最大的那份：got %q ok=%v want %q", path, ok, second)
	}
	// 收口清目录之后无可回读。
	if err := outputs.ClearJobOutputs(ctx); err != nil {
		t.Fatalf("ClearJobOutputs: %v", err)
	}
	if _, ok, err := outputs.LatestJobOutputPath(ctx, "exec"); err != nil || ok {
		t.Fatalf("清目录后不该再定位到正文：ok=%v err=%v", ok, err)
	}
}

func TestOutputStoreRefusesWithoutSession(t *testing.T) {
	t.Parallel()
	outputs, _, _ := newOutputStoreFixture(t)
	if _, err := outputs.JobOutputPath(context.Background(), "exec"); err == nil {
		t.Fatal("没有会话归属的调用必须显式报错（否则路径会落到别人的会话目录上）")
	}
	if outputs := NewOutputStore(nil, nil); outputs != nil {
		t.Fatal("缺 store / keyFor 时构造函数必须返回 nil（缺失 = 不接管，不是半残实现）")
	}
}
