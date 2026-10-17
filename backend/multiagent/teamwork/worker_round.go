package teamwork

// worker_round.go 收纳 teammate **回合面**的三条纯判据（P21-29「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork.go` 搬进本域；plan §2.7 把 `runtime_teamwork*.go`
// 列为 `backend/multiagent/teamwork/` 的目标）。

import (
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"
)

// RoundsPerMember 是"一名 teammate 在整队收口前最多派多少轮"的量级取用值。
//
// 它是**量级**而不是契约：一条 V 模型流水线里，一个角色通常只被派 1–3 轮（三层验证共用
// 一个 test_case 时是 3 轮）。取 64 是为了让"记录槽上限"落在数量级正确的一侧，而不是
// 精确预估——精确值要靠经验数据，而这不是一条需要精确的约束（`JobRecordCeiling` 那条只抬
// 不降的钳制保证它永远不会把上限压到框架缺省之下）。
const RoundsPerMember = 64

// JobRecordCeiling 是作业表的记录槽上限（`jobs.Limits.Records`，"防 prune"）。
//
// 为什么产品要自己给这个数：框架缺省（`jobs.DefaultLimits().Records` = 256）是**进程级兜底**，
// 与"一支团队从开工到收口能派多少次活"没有关系。prune 的判据是"表长 > 上限"、且只逐最老的
// **终态**行（活着的行永不驱逐），所以撞上它时丢掉的恰好是"团队还开着、但某一轮已经跑完"
// 的那些行——正是看板与 `team_context` 在收口之前要读的证据。
//
// 只抬不降：上限永不低于框架缺省。记录槽只保证"行**还在册**"，真正让正文不丢的是**输出归属**
// （`Spec.OutputPath`：文件归产品，销项 / 驱逐 / Close 都不删）——两条独立的路，别把后者
// 当成前者的替代（行被驱逐时读面就找不到那个句柄了）。
func JobRecordCeiling(maxTeammates int) int {
	if maxTeammates <= 0 {
		// 计划侧"不限制"（<=0）：按一名成员算，最终仍被框架缺省下限托住。
		maxTeammates = 1
	}
	ceiling := maxTeammates * RoundsPerMember
	if floor := jobs.DefaultLimits().Records; ceiling < floor {
		return floor
	}
	return ceiling
}

// TurnLimit 解析这一轮的回数上限：调用方给了正数（`team_plan` 的成员级 `max_turns`）就用它，
// 否则回退**装配层的角色回合默认**（由宿主以参数传入——那个常量属于角色回合面）。
func TurnLimit(requested, fallback int) int {
	if requested > 0 {
		return requested
	}
	return fallback
}

// RoundInput 组装 worker 回合的**输入正文**（`<milestone>` 标签 + `<round_input>` + `<task>`）。
//
// 归属标签只认里程碑（阶段口径已退场，2026-10-04）：worker 因此永远看得到"我这一轮是哪一步的活"，
// 而不是一个已经不存在的阶段 id。没有正文时如实说明"本轮没有新的工作正文"，不假装有输入。
func RoundInput(request WorkerRequest) string {
	goal := strings.TrimSpace(request.Goal)
	if goal == "" {
		goal = "（本轮没有新的工作正文）"
	}
	header := ""
	if milestone := strings.TrimSpace(request.Milestone); milestone != "" {
		header = "<milestone>" + milestone + "</milestone>\n"
	}
	return header + "<round_input>\n" + goal + "\n</round_input>\n\n" +
		"<task>\n以 " + request.Role + " 的身份完成这一轮：给出本轮的结论与下一步（≤800 字）。\n</task>"
}
