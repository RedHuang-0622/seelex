package seelebridge

// teamwork_job_namer.go — 团队作业表的**命名策略**（Seelex 侧注入 Seele jobs.Manager）。
//
// 为什么命名在 Seelex 而不在 Seele：`jobs` 是产品中立的根能力，而句柄是**产品的词汇**——
// 同一进程里并存两张作业表（后台工具 / 子代理作业 `bt-` / `bs-`，团队作业 `bteam-`），
// 只有产品知道哪些命名空间绝不能互相混。Seele 因此不再自命名，改由构造期注入的
// `jobs.Namer` 提供（见 Seele `jobs/namer.go` 的契约；`jobs.New` 缺注入即 `ErrNoNamer`）。
//
// 历史：此前两张表都铸 `a<seq>`，于是同一个会话里可以同时存在两条 `a5`——把其中一条交给
// 另一张表的管理工具会静默命中无关作业（2026-10-15 审计 D1）。

import (
	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// teamworkJobNamer 是团队作业表的命名器：`bteam-<seq>`。
//
// token 从 `dto` 的契约表读（唯一一份），序号用 Manager 自己的登记序号——它在 Manager 的
// 锁内单调递增，因此同一张表内句柄不会重复；跨表的唯一性由 token 保证（见 dto.JobNamespace）。
type teamworkJobNamer struct{}

// Name 实现 jobs.Namer。
//
// 它在 Manager 的锁内被调用，因此这里只做一次字符串拼接：不做 IO、不回调 Manager、
// 不依赖时钟或随机源（同一 (spec, seq) 必须给出同一个名字）。
func (teamworkJobNamer) Name(spec jobs.Spec, seq int) jobs.Handle {
	return jobs.Handle(dto.JobHandle(dto.JobNamespaceTeamwork, seq))
}

var _ jobs.Namer = teamworkJobNamer{}
