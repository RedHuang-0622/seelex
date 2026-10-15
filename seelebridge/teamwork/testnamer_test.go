package teamwork

import (
	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// testNamer 是本包用例的团队作业命名策略：与生产侧**同一个 token**（`bteam-`），序号用
// Manager 自己的登记序号。
//
// Seele 的 jobs 不再自命名（`jobs.New` 缺注入即 `ErrNoNamer`），因此每个构造 jobs.Manager
// 的用例都要自带一个命名器。这里刻意复用生产口径的 token，好让"用例里的句柄形状"与
// "生产里的句柄形状"是同一个事实，而不是各写一份字面量。
type testNamer struct{}

func (testNamer) Name(spec jobs.Spec, seq int) jobs.Handle {
	return jobs.Handle(dto.JobHandle(dto.JobNamespaceTeamwork, seq))
}

var _ jobs.Namer = testNamer{}
