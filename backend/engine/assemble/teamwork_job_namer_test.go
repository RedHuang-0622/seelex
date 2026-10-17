package assemble

import (
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 团队作业表的命名器判据（2026-10-15 审计 D1）：Seele 不再自命名，命名由 Seelex 注入。

// TestTeamworkJobNamerMintsNamespaceHandle：命名器给出的句柄必须带团队表的 token，
// 且与契约层的铸造函数**逐字一致**（形状只有一处定义，用例不另写一份）。
func TestTeamworkJobNamerMintsNamespaceHandle(t *testing.T) {
	namer := teamworkJobNamer{}
	for _, seq := range []int{1, 9, 10, 128} {
		got := namer.Name(jobs.Spec{Kind: "worker"}, seq)
		want := jobs.Handle(dto.JobHandle(dto.JobNamespaceTeamwork, seq))
		if got != want {
			t.Fatalf("Name(seq=%d) = %q，want %q", seq, got, want)
		}
		namespace, known := dto.JobNamespaceOf(string(got))
		if !known || namespace != dto.JobNamespaceTeamwork {
			t.Fatalf("铸造出的句柄 %q 的命名空间 = (%q, %v)，want (%q, true)",
				got, namespace, known, dto.JobNamespaceTeamwork)
		}
	}
}

// TestTeamworkJobNamerIsNotSharedWithTheToolTable：团队句柄与后台工具作业的句柄必须落在
// **不相交**的命名空间里——这是"跨表静默命中"不可能再发生的前提。
func TestTeamworkJobNamerIsNotSharedWithTheToolTable(t *testing.T) {
	namer := teamworkJobNamer{}
	for _, seq := range []int{1, 2, 7, 10, 12} {
		teamwork := string(namer.Name(jobs.Spec{}, seq))
		for _, toolNamespace := range []dto.JobNamespace{
			dto.JobNamespaceToolJob,
			dto.JobNamespaceSubagent,
		} {
			if collide := dto.JobHandle(toolNamespace, seq); collide == teamwork {
				t.Fatalf("团队句柄 %q 与工具表句柄 %q 撞名", teamwork, collide)
			}
		}
	}
}
