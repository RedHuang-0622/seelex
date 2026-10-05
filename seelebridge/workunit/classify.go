package workunit

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// ClassifyFinish 是三层**唯一一份**收尾分类：把"这一轮怎么结束的"归到四类之一。
//
// 此前这份判断写了两处，且第二处是漂移的：node 侧（agent_node.go 的 Run 尾部）
// 分 uncommitted / merge_blocked / 其他三支，teammate 侧（SettleWorkItem）只有一句
// "有错就 failed"——同一份"跑完了但没合进去"在一条路上是警告、在另一条路上是判死。
//
// 判据顺序即语义：
//
//  1. runErr 非空 —— 这一轮本身失败：判死。合并状态此时不改变结论（node 侧的 switch
//     整段在 `if err == nil` 之内，同一口径）。
//  2. 未提交改动（worktree.ErrUncommittedChanges）—— 只说明"改动没合进去"，**不代表
//     产出无效**：现场保留、结论照常交付（2026-09-11 事故：收尾失败 fail-fast 连坐
//     同批兄弟节点，两份已完成产出全丢）。
//  3. 主工作区挡路（worktree.ErrMergeBlockedByMain）—— 同族：处置动作是"先让主工作区
//     干净，再重试合并"，不是判死。
//  4. 其余合并错误 —— 判死。
//  5. 都没有 —— 落定，待验收。
func ClassifyFinish(result Result, mergeErr error) Outcome {
	// 说明一律"先拼齐、再整体裁"：裁的是进回执与看板的**那一行**，不是它的某一段。
	compose := func(kind OutcomeKind, prefix string, err error) Outcome {
		return Outcome{Kind: kind, Notice: bounded(prefix + err.Error())}
	}
	switch {
	case result.Err != nil:
		return compose(OutcomeFailed, "跑失败：", result.Err)
	case worktree.IsUncommittedChanges(mergeErr):
		return compose(OutcomeUncommitted, "现场有未提交改动，本次未合并：", mergeErr)
	case worktree.IsMergeBlockedByMain(mergeErr):
		return compose(OutcomeMergeBlocked, "合并被主工作区的在途改动挡住，本次未合并：", mergeErr)
	case mergeErr != nil:
		return compose(OutcomeFailed, "收尾失败：", mergeErr)
	default:
		return Outcome{Kind: OutcomeSettled, Notice: "跑完待验收"}
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
