// interaction.go —— 交互（interaction）的**词表与选项装配**归位（plan §2.4 的
// `service_interaction.go` → `middle/switch/`）。
//
// 落点沿革：2026-10-17 随 **M5-R6-interaction** 接收者转移从 `application/core/service_interaction.go`
// 归位。本文件只装"交互长什么样、用户选项叫什么名、点了之后该说哪句话"这三类判据；
// **怎么处置**（审批结案、恢复会话、切账号、重跑工作流）仍是宿主的编排，留在宿主。
package switching

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// 交互 kind 词表：宿主 `ResolveInteraction` 按它分派处置，前端按它决定渲染面。
// 未列出的 kind 一律走 UnsupportedInteractionKind 报错（不许静默吞）。
const (
	InteractionKindApproval  = "approval"
	InteractionKindSession   = "session"
	InteractionKindAccount   = "account"
	InteractionKindPlanRetry = "plan_retry"
)

// 选项 id 词表（前端按钮的机器名）：
//   - `InteractionCancelOptionID` 是"取消"哨兵：approval 走结案，其余 kind 直接关格；
//   - `PlanRetryOption*` 是 plan_retry 的四个分支，宿主按它们分派，前端按它们出按钮。
const (
	InteractionCancelOptionID = "__CANCEL__"

	PlanRetryOptionReplan = "replan"
	PlanRetryOptionRetry  = "retry"
	PlanRetryOptionSkip   = "skip"
	PlanRetryOptionAbort  = "abort"
)

// 交互标题与提示文案（用户可见文字只此一份）。
const (
	InteractionTitleSession = "选择会话"
	InteractionTitleAccount = "切换账号"

	// PlanRetryNoticeRetry / PlanRetryNoticeSkip 是 plan_retry 选项落到对话里的
	// system 行（用户点"重试/跳过"后，模型下一轮要读到这句指令才知道怎么做）。
	PlanRetryNoticeRetry = "节点失败，用户选择重试。请调用 plan_run 重新执行工作流。"
	PlanRetryNoticeSkip  = "节点失败，用户选择跳过。请修改工作流（plan_clear + plan_load）排除失败节点后重新 plan_run。"
	// PlanAbortNotice 是"终止"分支的对话回执。
	PlanAbortNotice = "工作流已终止。"
)

// UnsupportedInteractionKind 是未知 kind 的报错（文案逐字保留 `%q` 的引号形式，
// 前端与日志都按它认"这一笔交互本进程不支持"）。
func UnsupportedInteractionKind(kind string) error {
	return fmt.Errorf("unsupported interaction kind %q", kind)
}

// SessionInteractionID / AccountInteractionID 是视图单格交互的 ID 口径：
// `<kind>-<UnixNano>`——两类交互互不覆盖，且同一进程内多次打开不撞。
func SessionInteractionID(now time.Time) string {
	return fmt.Sprintf("session-%d", now.UnixNano())
}

func AccountInteractionID(now time.Time) string {
	return fmt.Sprintf("account-%d", now.UnixNano())
}

// SessionChoice 是"选择会话"的候选项：宿主给出会话目录读数，标签回退由本包决定。
//
// ShortID 是**惰性端口**：会话名缺失时才取短 ID（少读一次限制配置，也与归位前的调用点一致）。
type SessionChoice struct {
	ID         string
	Name       string
	ShortID    func() string
	TokenCount int
	UpdatedAt  time.Time
}

// AccountChoice 是"切换账号"的候选项。
type AccountChoice struct {
	Name     string
	Disabled bool
	Provider string
	Model    string
}

// BuildSessionInteraction 装配"选择会话"交互：标签 = 会话名，名缺失时回退短 ID；
// 描述 = `tok:<token 数>  <MM-DD HH:MM>`（用户靠这两列认会话）。
func BuildSessionInteraction(id string, sessions []SessionChoice, openedAt time.Time) model.Interaction {
	options := make([]model.InteractionOption, 0, len(sessions))
	for _, session := range sessions {
		label := session.Name
		if label == "" && session.ShortID != nil {
			label = session.ShortID()
		}
		options = append(options, model.InteractionOption{
			ID:          session.ID,
			Label:       label,
			Description: fmt.Sprintf("tok:%d  %s", session.TokenCount, session.UpdatedAt.Format("01-02 15:04")),
		})
	}
	return model.Interaction{
		ID:       id,
		Kind:     InteractionKindSession,
		Title:    InteractionTitleSession,
		Options:  options,
		OpenedAt: openedAt,
	}
}

// BuildAccountInteraction 装配"切换账号"交互：停用的账号在标签后补 ` [禁用]`（不隐藏，
// 用户要能看见"为什么这个账号不能选"）；描述 = `provider model` 去尾空。
func BuildAccountInteraction(id string, accounts []AccountChoice, openedAt time.Time) model.Interaction {
	options := make([]model.InteractionOption, 0, len(accounts))
	for _, account := range accounts {
		label := account.Name
		if account.Disabled {
			label += " [禁用]"
		}
		options = append(options, model.InteractionOption{
			ID:          account.Name,
			Label:       label,
			Description: strings.TrimSpace(account.Provider + " " + account.Model),
		})
	}
	return model.Interaction{
		ID:       id,
		Kind:     InteractionKindAccount,
		Title:    InteractionTitleAccount,
		Options:  options,
		OpenedAt: openedAt,
	}
}
