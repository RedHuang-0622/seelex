// draft.go —— 草稿会话的**身份判据**归位（plan §2.4 的 `session_draft.go` → `middle/switch/`）。
//
// 落点沿革：2026-10-17 随 **M5-R6-draft** 接收者转移从 `application/assembly/session_draft.go` 归位。
// 本文件只装"谁是草稿、草稿 SID 长什么样"这两条判据；草稿的**状态机**（进入草稿要清哪些字段、
// 物化时绑哪个工作区、发哪些事件）仍留在宿主——那是视图过渡锁下的编排，不是判据。
package switching

import (
	"fmt"
	"time"
)

// DraftSessionName 是草稿会话的显示名。
//
// 「草稿」是会话的**状态**（Snapshot.Session.Draft / 会话记录的 Status=draft），不是会话的身份，
// 所以显示名可以是一句话：新建会话时右侧列表先按这个名字占一行，首次提交物化后再由会话目录
// 刷新成首条输入派生的标题。
const DraftSessionName = "新会话"

// DraftSessionIDPrefix 是宿主**早分配**会话 ID 的前缀。
//
// 前缀必须读作"一个会话"，而不是"一份草稿"：这个 ID 在首次提交物化后会继续当真实会话 ID 用——
// 引擎 bundle、会话记录键、权限档位槽、工作区绑定全按它寻址，`RoleSessionID` 还拿它派生角色
// 子代理会话号。此前这里是 `draft_`：于是每个经「新建会话」产生的会话终生带 draft 前缀，
// 工作表格的「会话」列永远是 `draft_…`（2026-10-01 现场：明明是 session，却显示 draft）。
const DraftSessionIDPrefix = "seelex"

// NewDraftSessionID 生成早分配的会话 ID：`<前缀>-<UnixNano>-<序号>`。
//
// 用独立前缀与序号的原因：Windows 时间戳低位分辨率下，同一 tick 内多次进入草稿也不能撞——
// `seq` 由宿主在 Core.ViewMu 下自增（单调，不复用）。宿主按该显式 ID 建引擎 bundle
// （HasSession=false 阶段不建，首次提交物化时由 ActivateSession 创建）。
func NewDraftSessionID(now time.Time, seq uint64) string {
	return fmt.Sprintf("%s-%d-%d", DraftSessionIDPrefix, now.UnixNano(), seq)
}

// DraftTarget 是「显式提交的目标是不是那份尚未物化的草稿」的判据来源：
// 进程单例的**草稿槽**（宿主侧 `service.draft`）与**共享视图镜像**（Snapshot.Session）。
//
// 两个来源必须分开记，因为它们的语义不同：草稿槽是进程单例，切走再回来仍保留同一份早分配
// SID；视图镜像只反映"此刻前端看到的是哪份会话"。归属判定因此取二者的或（见 Matches），
// 而物化闸门只认视图（见 InView）。
type DraftTarget struct {
	// HasSlot 报告草稿槽是否存在（宿主侧 `service.draft != nil`）。
	HasSlot bool
	// SlotID 是草稿槽里的早分配 SID（HasSlot=false 时无意义）。
	SlotID string
	// ViewDraft 报告视图镜像此刻是否停在草稿状态。
	ViewDraft bool
	// ViewID 是视图镜像的会话 ID。
	ViewID string
}

// Matches 预判显式提交目标 targetID 是否就是那份尚未物化的草稿：廉价闸门，避免每次后台提交
// 都去抢视图过渡锁；真正的归属复判在锁内走 InView。
//
// 空 ID 边界与前身逐字一致：草稿槽存在且 SID 为空串时，targetID 为空串也算命中
// （槽的 SID 只在物化时被消费，"还没分配过 SID"的槽按空串登记）。
func (target DraftTarget) Matches(sessionID string) bool {
	if target.HasSlot && target.SlotID == sessionID {
		return true
	}
	return target.ViewDraft && target.ViewID == sessionID
}

// InView 报告「视图仍停在这份草稿上」——物化前的**锁内复判**，也是它与 Matches 的判据差别：
// 视图必须先停在草稿上（否则一律 false），再看草稿槽是否还认这份 SID。
//
// 为什么视图这条是必要条件：草稿槽是进程单例，物化会把**共享视图镜像**切到该 SID，因此只有
// 视图仍停在它上面时才允许物化；持着过期快照提交时要明确失败（宿主返回 ErrDraftNotInView），
// 比把这条输入投给别的会话安全。槽已空（nil）表示还没物化过，仍属本次。
func (target DraftTarget) InView(sessionID string) bool {
	if !target.ViewDraft || target.ViewID != sessionID {
		return false
	}
	return !target.HasSlot || target.SlotID == sessionID
}
