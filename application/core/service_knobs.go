package core

// service_knobs.go — composer 底部两条「独立拉条」的落点：思考强度与循环次数。
//
// 口径（与 effort 档位的关系）：
//
//	档位 = **同时设置两条拉条的预设**（application/prompt 的 effortProfiles：
//	lite/medium/high/max → reasoningEffort low/medium/high/max + maxLoops
//	15/48/384/768）。拖一条拉条**只**改它那一项，另一项一个字都不动；切档位则
//	两项一起被预设覆盖，且快照里两条拉条的读数随之更新——"档位是预设"这件事在
//	界面上看得见，不会有"拖了 A 之后 B 偷偷变了"的暗改。
//
// 为什么两条拉条要各自独立而不是只做档位：档位的两轴本来就走两条完全不同的链
// （"跑多少轮"写在引擎上，"想多深"写在账号客户端上），把它们钉在同一档上意味着
// 想要 384 轮 + low 思考这种组合必须改代码。
//
// 运行守卫与 SwitchEffort 同一条（G0b/INV-G7）：两者都改写视图会话随后回合的行为
// （循环上限进引擎、思考强度进账号池），所以视图会话 running 时一并拒绝——不是
// 前端置灰就够，后端必须自己挡（前端置灰只是体验）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ensureViewIdle 是本文件与 SwitchEffort 共用的运行守卫：返回视图会话 ID，并在
// 视图会话 running 时给出「先等回合结束」的拒绝理由（action 是动作名，拼进文案）。
//
// 只守视图会话：后台会话各自保留自己的选择（SwitchEffort 自刀 4 起的既有口径），
// 一个后台会话在跑不该让用户失去调节当前视图会话的入口。
func (service *Service) ensureViewIdle(action string) (string, error) {
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	viewRunning := false
	if unit := service.sessions.Unit(viewSessionID); unit != nil {
		viewRunning = unit.ChatState().Running
	}
	service.ViewMu.RUnlock()
	if viewRunning {
		return viewSessionID, fmt.Errorf("当前会话正在运行：请等待回合结束或取消后再%s", action)
	}
	return viewSessionID, nil
}

// validWireReasoningEffort 判定一个值是否是 provider 词表里的思考强度。
//
// 判据本体只有一份（dto.NormalizeWireReasoningEffort），这里只补一条拉条自己的
// 口径：**不收空值**——空是"没声明"，调用方（定时任务）可以按默认档位兜底，而一条
// 拉条没有"跟随档位"这一格，停在空上等于没有位置。
func validWireReasoningEffort(value string) bool {
	normalized, err := dto.NormalizeWireReasoningEffort(value)
	return err == nil && normalized != ""
}

// SetReasoningEffort 单独下发"想多深"（不碰循环上限、不碰档位的 prompt 层与预算）。
//
// 下发口是既有的窄可选端口（contract.ReasoningEffortPort）：只有配置为
// `reasoning_effort: session` 的账号会被改（写死强度的账号——subagent 默认 low、
// goalplan 默认 high、用户显式配的——不受影响）。宿主没装配这个端口时整步退化为
// 记录读数，不报错（与 SwitchEffort 同口径）。
func (service *Service) SetReasoningEffort(_ context.Context, effort string) error {
	if _, err := service.ensureViewIdle("切换思考强度"); err != nil {
		return err
	}
	value := strings.ToLower(strings.TrimSpace(effort))
	if !validWireReasoningEffort(value) {
		return fmt.Errorf("无效的思考强度 %q，可选：%v", effort, reasoningEffortChoices())
	}
	service.pushSessionReasoningEffort(value)
	service.setReasoningEffortReading(value)
	return service.publishKnobs(nil)
}

// SetLoopLimit 单独改"跑多少轮"（不碰思考强度）。取值判据与定时任务的循环次数
// 同源（validateLoopLimit）。
func (service *Service) SetLoopLimit(_ context.Context, limit int) error {
	if _, err := service.ensureViewIdle("调整循环次数"); err != nil {
		return err
	}
	if err := validateLoopLimit(limit); err != nil {
		return err
	}
	service.applyLoopLimit(limit)
	return service.publishKnobs(nil)
}

// applyLoopLimit 是"改循环上限"的唯一写点：引擎与快照读数一起改，避免出现
// "引擎已经改了、拉条还显示旧值"（或反过来）的半截状态。加锁次序：ViewMu → knobMu。
func (service *Service) applyLoopLimit(limit int) {
	if limit <= 0 {
		return
	}
	if service.Deps.Engine != nil {
		service.Deps.Engine.SetMaxLoops(limit)
	}
	service.ViewMu.Lock()
	service.knobMu.Lock()
	service.knobLoopLimit = limit
	service.knobMu.Unlock()
	service.Core.Snapshot.Runtime.LoopLimit = limit
	service.ViewMu.Unlock()
}

// setReasoningEffortReading 记录"当前生效的 wire 思考强度"（engine/账号池那一步由
// 调用方做：档位切换走 profile 映射，拉条走用户选的值）。加锁次序：ViewMu → knobMu。
func (service *Service) setReasoningEffortReading(value string) {
	service.ViewMu.Lock()
	service.knobMu.Lock()
	service.knobReasoningEffort = value
	service.knobMu.Unlock()
	service.Core.Snapshot.Runtime.ReasoningEffort = value
	service.ViewMu.Unlock()
}

// knobReadings 是 runtime 投影收集两条拉条读数的唯一入口（每个回合尾、每次工具
// 边界都会调；投影整份重建 Runtime，读数必须由这里反哺）。**不取 ViewMu**：收集在
// 锁外发生，而应用在锁内（见 serviceState 的注释）。
func (service *Service) knobReadings() (string, int) {
	service.knobMu.RLock()
	defer service.knobMu.RUnlock()
	return service.knobReasoningEffort, service.knobLoopLimit
}

// knobReasonings / knobLimit 是投影装配用的两个单值回调。
func (service *Service) knobReasonings() string {
	effort, _ := service.knobReadings()
	return effort
}

func (service *Service) knobLimit() int {
	_, limit := service.knobReadings()
	return limit
}

// currentLoopLimit 返回"当前生效的循环上限"：用户用拉条改过就用改过的值，否则回退
// 当前档位的默认值。它是退栈 Skill / 任务边界恢复循环上限的**唯一读法**——旧实现
// 直接取 maxLoopsFor(档位)，于是"用户把循环次数拧到 600"会在一次退栈后无声回到
// 384。
func (service *Service) currentLoopLimit() int {
	service.knobMu.RLock()
	limit := service.knobLoopLimit
	service.knobMu.RUnlock()
	if limit > 0 {
		return limit
	}
	return maxLoopsFor(service.effortManager.Current())
}

// pushSessionReasoningEffort 把 wire 值下发给"跟随会话"的账号，返回被改动的账号数。
// 端口未装配（宿主没实现 ReasoningEffortPort）时为 no-op。
func (service *Service) pushSessionReasoningEffort(wireValue string) int {
	if service == nil || service.Deps.Runtime == nil {
		return 0
	}
	sink, ok := service.Deps.Runtime.(contract.ReasoningEffortPort)
	if !ok {
		return 0
	}
	return sink.SetSessionReasoningEffort(wireValue)
}

// publishKnobs 把两条拉条的读数变更推成一次 runtime.changed：mutate 在 ViewMu 下
// 改快照（nil = 不改字段，例如换循环上限那条链已经自己写过了），随后按既有口径
// **下发完整运行时而不是 nil**（payload 为 nil 的 runtime.changed 会被前端判为不可
// 增量应用 → 触发整份快照刷新，表现为"拖一下拉条、右侧面板整块闪动"）。
func (service *Service) publishKnobs(mutate func(*RuntimeState)) error {
	service.ViewMu.Lock()
	if mutate != nil {
		mutate(&service.Core.Snapshot.Runtime)
	}
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
	return nil
}

// reasoningEffortChoices 是给报错提示用的可选值清单（不含 "session"：见
// validWireReasoningEffort 的注释）。词表本体在契约层，这里不写第二份。
func reasoningEffortChoices() []string {
	return dto.WireReasoningEffortVocabulary()
}

// validateLoopLimit 是"循环次数"这一项的**唯一**一份取值判据：拉条（SetLoopLimit）
// 与定时任务声明的循环次数共用它。上界取 limits.input_loop_limit（默认 9999，就是
// goal skill 用的"≈无上限"哨兵）——一旦被超过，这个哨兵就不再表达"无上限"。
func validateLoopLimit(limit int) error {
	upper := Limits().InputLoopLimit
	if limit < 1 || limit > upper {
		return fmt.Errorf("循环次数需在 1..%d 之间（收到 %d）", upper, limit)
	}
	return nil
}

// —— 定时任务的按会话思考强度（"租一笔"）——
//
// 思考强度落在**账号客户端**上，账号池是进程级共享的：没有"这个会话用自己的强度"
// 这种格子。于是定时任务声明的那条强度按**会话**记一笔，在该会话的回合起点下发一次
// （runChat 起点，与 syncPlanPolicyFor / syncFullAccessFor 同一个同步点），下发即
// 消掉：它属于**那一次运行**，不是这个会话的长期设置。
//
// 为什么到回合起点才下发、而不是触发那一刻就下发：触发与回合起点之间，别的会话的
// 回合可能已经按自己的强度下发过（账号池就是"后切者生效"）。落在起点 = 这一回合
// 一定按它自己声明的那条跑。为什么下发即消掉、而不是长期压着：用户随后在该会话里
// 拖拉条、切档位必须照旧生效——不能出现"拉条被一个看不见的东西压住"。
//
// 已知边界（账号池进程级共享，不是本次引入）：这一笔下发后到下一个会话说自己的强度
// 之前，**别的会话**的请求也走这条强度。要彻底按会话分，需要 Seele 侧按会话的
// ChatClient；这条链只保证"每个回合起点都按自己的声明下发一次"。
func (service *Service) borrowSessionReasoningEffort(sessionID, wireValue string) {
	sessionID = strings.TrimSpace(sessionID)
	wireValue = strings.TrimSpace(wireValue)
	if sessionID == "" || wireValue == "" {
		return
	}
	service.sessionEffortMu.Lock()
	defer service.sessionEffortMu.Unlock()
	if service.sessionEffort == nil {
		service.sessionEffort = make(map[string]string)
	}
	service.sessionEffort[sessionID] = wireValue
}

// takeSessionReasoningEffort 取走并消掉指定会话待下发的思考强度（没有则 false）。
func (service *Service) takeSessionReasoningEffort(sessionID string) (string, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", false
	}
	service.sessionEffortMu.Lock()
	defer service.sessionEffortMu.Unlock()
	wireValue, ok := service.sessionEffort[sessionID]
	if !ok {
		return "", false
	}
	delete(service.sessionEffort, sessionID)
	return wireValue, true
}

// errKnobsLocked 是运行守卫的哨兵（调用方若需要 errors.Is 判"因为会话在跑"）。
var errKnobsLocked = errors.New("会话运行中，配置已上锁")
