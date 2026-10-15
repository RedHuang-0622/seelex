package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// 作业身份层的判据（2026-10-15 审计 D1）：**句柄自带来源** + **拿错工具显式报错**。
//
// 缺陷本体（真实会话证据见 docs/2026-10-15-teamwork-defect-audit/README.md）：两张作业表
// 各自维护派发序号却共用同一套句柄形状（都从 `a1` 起），于是同一个会话里能同时存在两条
// `a5`；把其中一条交给另一张表的管理工具会**静默命中无关作业**（leader 想停 teammate 的
// `a5`，`op=done` 销掉的是一条无关后台作业，还回了"成功"）。这两条用例钉住修复：
// 句柄带命名空间 token，且跨表调用必须**响亮失败**。

// TestJobHandlesCarryTheirTableToken：三类后台作业各自铸造的句柄必须带自己的 token。
func TestJobHandlesCarryTheirTableToken(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-ns")

	t.Run("process", func(t *testing.T) {
		payload := dispatchForTest(t, router, ctx, "echo ns")
		assertNamespace(t, payload.Handle, dto.JobNamespaceToolJob)
	})

	t.Run("inline", func(t *testing.T) {
		args, err := json.Marshal(map[string]interface{}{"paths": []string{"a.go", "b.go"}})
		if err != nil {
			t.Fatal(err)
		}
		output, err := router.scopedReadBatch(ctx, string(args))
		if err != nil {
			t.Fatalf("read_batch: %v", err)
		}
		var receipt struct {
			Jobs []struct {
				Handle string `json:"handle"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal([]byte(output), &receipt); err != nil {
			t.Fatalf("解析回执: %v (%q)", err, output)
		}
		if len(receipt.Jobs) != 2 {
			t.Fatalf("回执条数 = %d，want 2", len(receipt.Jobs))
		}
		for _, job := range receipt.Jobs {
			assertNamespace(t, job.Handle, dto.JobNamespaceToolJob)
		}
	})

	t.Run("subagent", func(t *testing.T) {
		handle, err := router.AddSubagentJob(JobSpec{
			SessionID: "sess-ns", Title: "子代理作业", Command: "probe",
		}, nil)
		if err != nil {
			t.Fatalf("AddSubagentJob: %v", err)
		}
		assertNamespace(t, handle, dto.JobNamespaceSubagent)
	})
}

// TestBgManageRefusesHandlesFromOtherTables：跨表调用必须在工具边界**显式报错并点名对偶
// 工具**——静默命中不可能再发生（命名空间不相交），但"不可能命中"必须表现为一句能照做的
// 错误，否则模型只会看到"未知句柄"这类无从下手的回执。
func TestBgManageRefusesHandlesFromOtherTables(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-ns")
	own := dispatchForTest(t, router, ctx, "echo ns").Handle

	cases := []struct {
		name   string
		handle string
	}{
		{"团队作业的句柄", dto.JobHandle(dto.JobNamespaceTeamwork, 7)},
		{"旧形态（无 token）", "a5"},
		{"外来字符串", "handle-from-nowhere"},
	}
	for _, testCase := range cases {
		for _, op := range []string{"observe", "fetch", "kill", "done"} {
			args, err := json.Marshal(map[string]interface{}{"op": op, "handle": testCase.handle})
			if err != nil {
				t.Fatal(err)
			}
			_, err = router.scopedJobManage(ctx, string(args))
			if err == nil {
				t.Fatalf("op=%s handle=%q（%s）必须被拒：跨表句柄不许在本表执行",
					op, testCase.handle, testCase.name)
			}
			if !strings.Contains(err.Error(), "jobs_manage") {
				t.Fatalf("op=%s handle=%q 的拒绝文案必须点名对偶工具 jobs_manage：%v",
					op, testCase.handle, err)
			}
		}
	}

	// 对照组：本表自己的句柄照常可用（拒绝不能连自己人都挡掉）。
	args, err := json.Marshal(map[string]interface{}{"op": "observe", "handle": own})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.scopedJobManage(ctx, string(args)); err != nil {
		t.Fatalf("本表句柄 %q 不该被拒：%v", own, err)
	}
}

// TestJobNamespaceGuardIsPositiveWhitelist：守卫必须按**正向白名单**（只认本表的 token），
// 而不是"不认得就放行"——后者在第三张作业表出现时会静默漏掉一类跨表调用。
func TestJobNamespaceGuardIsPositiveWhitelist(t *testing.T) {
	if err := requireOwnedJobHandles([]string{
		dto.JobHandle(dto.JobNamespaceToolJob, 1),
		dto.JobHandle(dto.JobNamespaceSubagent, 2),
	}); err != nil {
		t.Fatalf("本表句柄被拒：%v", err)
	}
	for _, handle := range []string{
		dto.JobHandle(dto.JobNamespaceTeamwork, 1),
		"a1",
		"",
	} {
		if err := requireOwnedJobHandles([]string{handle}); err == nil {
			t.Fatalf("句柄 %q 必须被拒", handle)
		}
	}
}

// assertNamespace 断言句柄属于期望的命名空间（失败时直接说出句柄与期望，便于定位）。
func assertNamespace(t *testing.T, handle string, want dto.JobNamespace) {
	t.Helper()
	namespace, known := dto.JobNamespaceOf(handle)
	if !known || namespace != want {
		t.Fatalf("句柄 %q 的命名空间 = (%q, %v)，want (%q, true)", handle, namespace, known, want)
	}
}
