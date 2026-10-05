package workunit

import (
	"errors"
	"fmt"
	"strings"
)

// ── 收尾合并的两个哨兵错误（契约自己的词表）──────────────────────────────
//
// 它们是「这一轮跑完了，但产出没合进去」的**两族**事实——契约早就用 `OutcomeUncommitted` /
// `OutcomeMergeBlocked` 给这两族起了名（而且这两族**不判死**：产出有效，只是没合进去）。
// 既然分类的判据在这里，判据认的那两个哨兵也只能在这里——否则契约包就要 import 现场实现包
// （`worktree`）去认它们，依赖方向就反了：**契约不 import 实现包，实现包 import 契约**。
//
// 这一次搬迁（步骤③E）把依赖方向正了过来：
//
//	搬之前：workunit/classify.go → worktree（契约依赖实现；门禁里登记为"唯一一处已记录例外"）
//	搬之后：worktree → workunit（实现依赖契约；哨兵由**产生它的那一层**用契约的词表构造）
//
// 语义零变化：哨兵是**同一批错误值**，`errors.Is` 链上一路照旧（`worktree` 那边只是把
// 定义处换成引用处）；`ClassifyFinish` 的签名与判定顺序一个字没动。
var (
	// ErrUncommittedChanges 是"现场收尾协议未执行"这一族的哨兵：子代理/teammate 在自己
	// 的现场里留下了未提交改动。它只说明**改动没有合进去**，不说明产出无效——因此分类器
	// 判它**不判死**（现场保留、结论照常交付；2026-09-11 事故：收尾失败 fail-fast 连坐同批
	// 兄弟节点，两份已完成产出全丢）。
	ErrUncommittedChanges = errors.New("workunit: scene finish protocol not executed")

	// ErrMergeBlockedByMain 是"合并被主工作区的在途改动挡住"这一族的哨兵：与上面同族——
	// 处置动作是"先让主工作区干净，再重试合并"，不是判死。产生点上，现场的合并错误必须
	// 包着它（`%w` / `Unwrap`），否则分类器认不出来。
	ErrMergeBlockedByMain = errors.New("workunit: merge blocked by in-flight changes in the main workspace")
)

// IsUncommittedChanges 报告 err 是不是"现场收尾协议未执行"那一族（判据**唯一一份**）。
func IsUncommittedChanges(err error) bool { return errors.Is(err, ErrUncommittedChanges) }

// IsMergeBlockedByMain 报告 err 是不是"合并被主工作区挡路"那一族（判据**唯一一份**）。
func IsMergeBlockedByMain(err error) bool { return errors.Is(err, ErrMergeBlockedByMain) }

// ClassifyFinish 是三层**唯一一份**收尾分类：把"这一轮怎么结束的"归到四类之一。
//
// 判据顺序即语义：
//
//  1. runErr 非空 —— 这一轮本身失败：判死。合并状态此时不改变结论（node 侧的 switch
//     整段在 `if err == nil` 之内，同一口径）。
//  2. 未提交改动（workunit.ErrUncommittedChanges）—— 只说明"改动没合进去"，**不代表
//     产出无效**：现场保留、结论照常交付（2026-09-11 事故：收尾失败 fail-fast 连坐
//     同批兄弟节点，两份已完成产出全丢）。
//  3. 主工作区挡路（workunit.ErrMergeBlockedByMain）—— 同族：处置动作是"先让主工作区
//     干净，再重试合并"，不是判死。
//  4. 其余合并错误 —— 判死。
//  5. 都没有 —— 落定，待验收。
//
// 说明（Outcome.Notice）对**非落定**的每一类都带上给人看的处置办法（见 remedyFor）：
// 合同要求它非空，而"原因 + 下一步"就是这一行必须承载的全部信息。
func ClassifyFinish(result Result, mergeErr error) Outcome {
	// 说明一律"先拼齐、再整体裁"：裁的是进回执与看板的**那一行**，不是它的某一段。
	compose := func(kind OutcomeKind, prefix string, err error) Outcome {
		return Outcome{Kind: kind, Notice: bounded(prefix + err.Error() + "（" + remedyFor(kind) + "）")}
	}
	switch {
	case result.Err != nil:
		return compose(OutcomeFailed, "跑失败：", result.Err)
	case IsUncommittedChanges(mergeErr):
		return compose(OutcomeUncommitted, "现场有未提交改动，本次未合并：", mergeErr)
	case IsMergeBlockedByMain(mergeErr):
		return compose(OutcomeMergeBlocked, "合并被主工作区的在途改动挡住，本次未合并：", mergeErr)
	case mergeErr != nil:
		return compose(OutcomeFailed, "收尾失败：", mergeErr)
	default:
		return Outcome{Kind: OutcomeSettled, Notice: "跑完待验收"}
	}
}

// remedyFor 是每一类收尾结论的**处置办法**（非落定必带，合同见 Outcome）。
//
// 它与分类本身绑在一处：写在别处就会漂成第二份口径（"未提交"到底是补提交还是重派，
// 只有这张表说了算）。落定不需要处置办法——它要的是验收。
const (
	remedyUncommitted  = "处置：现场已原样保留，请补提交（或人工检查）后重试合并"
	remedyMergeBlocked = "处置：先让主工作区干净（提交或暂存），再重试合并"
	remedyFailed       = "处置：可重派；现场与记忆都保留，供人工检查"
)

// remedyFor 返回该结论的处置办法（落定为空 —— 它走验收，不走处置）。
func remedyFor(kind OutcomeKind) string {
	switch kind {
	case OutcomeUncommitted:
		return remedyUncommitted
	case OutcomeMergeBlocked:
		return remedyMergeBlocked
	case OutcomeFailed:
		return remedyFailed
	default:
		return ""
	}
}

// outcomeNoticeLimit 是收尾说明的字符上限：它要进回执与看板行，是"有界一行"。
const outcomeNoticeLimit = 400

// boundedTo 把说明裁到有界长度（按 rune 裁，避免切断多字节字符）。
func boundedTo(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) <= limit {
		return trimmed
	}
	return string([]rune(trimmed)[:limit]) + "…"
}

// bounded 把收尾说明裁到 Outcome 的长度上限。
func bounded(text string) string { return boundedTo(text, outcomeNoticeLimit) }

// Settled 报告一次收尾是否算"落定"（可以进验收）。
//
// 未提交与挡路都是"没合进去"：它们不改判死，但也不等于可以验收——调用方据此决定
// 状态写 review 还是 failed（口径：只有 failed 是"可重派/待人工处置"）。
func (o Outcome) Settled() bool { return o.Kind == OutcomeSettled }

// String 便于日志与看板投影（`kind: notice`）。
func (o Outcome) String() string {
	if strings.TrimSpace(o.Notice) == "" {
		return string(o.Kind)
	}
	return fmt.Sprintf("%s: %s", o.Kind, o.Notice)
}
