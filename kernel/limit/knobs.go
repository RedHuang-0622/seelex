package limit

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// 本文件是 composer 底部两条「独立拉条」的落点：思考强度与循环次数。
//
// 口径（与 effort 档位的关系）：
//
//	档位 = **同时设置两条拉条的预设**（prompt 的 effortProfiles：lite/medium/high/max →
//	reasoningEffort low/medium/high/max + maxLoops 15/48/384/768）。拖一条拉条**只**改它那一项，
//	另一项一个字都不动；切档位则两项一起被预设覆盖，且快照里两条拉条的读数随之更新——
//	"档位是预设"这件事在界面上看得见，不会有"拖了 A 之后 B 偷偷变了"的暗改。
//
// 为什么两条拉条各自独立而不是只做档位：档位的两轴本来就走两条完全不同的链（"跑多少轮"写在
// 引擎上，"想多深"写在账号客户端上），把它们钉在同一档上意味着想要 384 轮 + low 思考这种组合
// 必须改代码。
//
// 运行守卫与 SwitchEffort 同一条（G0b/INV-G7）：两者都改写视图会话随后回合的行为（循环上限进
// 引擎、思考强度进账号池），所以视图会话 running 时一并拒绝——不是前端置灰就够，后端必须自己
// 挡（前端置灰只是体验）。
//
// 落点说明（M5-R-limitb）：本文件是判据与状态本体，宿主能力全部经 `KnobsHost` 注入；
// 状态（两条拉条读数 + 按会话说强度租约）随本类型一起从 `application/assembly` 归位。

// KnobsHost 是两条拉条需要的**宿主能力面**（视图锁、快照写面、引擎与档位读数）。
//
// 加锁次序（原 `application/assembly/service_state.go` 的注释逐字保留）：固定为 ViewMu → 组件锁
// （只单向）。因此"改读数"必须走 `MutateRuntime` / `PublishRuntime`：宿主在自己的视图锁内
// 调用 mutate，组件在 mutate 里取自己的锁，次序不会倒。
type KnobsHost interface {
	// ViewSession 在宿主锁下返回视图会话 ID 与它是否正在跑（运行守卫用）。
	ViewSession() (sessionID string, running bool)
	// MutateRuntime 在宿主锁下按 mutate 改快照的运行时读数（**不**推进 revision；mutate 为 nil 即不改）。
	MutateRuntime(mutate func(*model.RuntimeState))
	// PublishRuntime 在宿主锁下按 mutate（可为 nil）改运行时读数 → 推进 revision → 克隆 →
	// 在锁外发布一次整份 `runtime.changed`（会话取当前视图会话）。
	PublishRuntime(mutate func(*model.RuntimeState)) error
	// SetEngineMaxLoops 把循环上限写进引擎客户端。
	SetEngineMaxLoops(limit int)
	// CurrentEffort 返回当前生效的档位名（拉条没改过时循环上限的回退来源）。
	CurrentEffort() string
	// MaxLoopsForEffort 返回某档位预设的循环上限（预设表的唯一读法在 prompt 域）。
	MaxLoopsForEffort(level string) int
	// ReasoningEffortSink 返回"跟随会话"账号的强度下发口；nil = 宿主未装配。
	ReasoningEffortSink() contract.ReasoningEffortPort
}

// Knobs 是两条独立拉条的**状态与判据本体**（进程级）。
type Knobs struct {
	host KnobsHost

	// reasoningEffort / loopLimit 是两条拉条的进程级权威读数，由 mu 单独保护。
	//
	// 为什么不用宿主的视图锁：runtime 投影每次整份重建 Snapshot.Runtime，读数必须反哺进投影；
	// 而投影的**收集**发生在锁外、**应用**在视图锁内，用同一把锁会有"收集时再取一次"的重入风险。
	//
	// 为什么要单独存一份而不是"只写进 Snapshot.Runtime"：投影整份重建时只写快照的读数会被抹掉。
	mu              sync.RWMutex
	reasoningEffort string
	loopLimit       int

	// sessionEffort 是"某会话的下一回合要用哪条 wire 思考强度"的一笔租约（sessionMu 保护；
	// 索引 = 会话 ID）。定时任务声明的那条强度落在这里，由该会话的回合起点取走并下发一次——
	// 账号池是进程级共享的，没有"按会话的强度"这种格子，所以按会话记一笔、用完即消。
	sessionMu     sync.Mutex
	sessionEffort map[string]string
}

// NewKnobs 建一条拉条状态机；host 不可为 nil。
func NewKnobs(host KnobsHost) *Knobs {
	return &Knobs{host: host, sessionEffort: make(map[string]string)}
}

// SeedReadings 在装配期播种两条拉条的默认读数（档位默认值派生）。只该在装配期调一次：
// 否则首个快照里 reasoning_effort / loop_limit 是空/0，前端两条拉条会显示成 Low / 1，
// 与引擎实际生效的档位不一致。
func (k *Knobs) SeedReadings(reasoningEffort string, loopLimit int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reasoningEffort = reasoningEffort
	k.loopLimit = loopLimit
}

// EnsureViewIdle 是运行守卫：返回视图会话 ID，并在视图会话 running 时给出「先等回合结束」
// 的拒绝理由（action 是动作名，拼进文案）。
//
// 只守视图会话：后台会话各自保留自己的选择（SwitchEffort 自刀 4 起的既有口径），
// 一个后台会话在跑不该让用户失去调节当前视图会话的入口。
func (k *Knobs) EnsureViewIdle(action string) (string, error) {
	sessionID, running := k.host.ViewSession()
	if running {
		return sessionID, fmt.Errorf("当前会话正在运行：请等待回合结束或取消后再%s", action)
	}
	return sessionID, nil
}

// SetReasoningEffort 单独下发"想多深"（不碰循环上限、不碰档位的 prompt 层与预算）。
//
// 下发口是既有窄可选端口（contract.ReasoningEffortPort）：只有配置为
// `reasoning_effort: session` 的账号会被改（写死强度的账号——subagent 默认 low、
// goalplan 默认 high、用户显式配的——不受影响）。宿主没装配这个端口时整步退化为
// 记录读数，不报错（与 SwitchEffort 同口径）。
func (k *Knobs) SetReasoningEffort(effort string) error {
	if _, err := k.EnsureViewIdle("切换思考强度"); err != nil {
		return err
	}
	value := strings.ToLower(strings.TrimSpace(effort))
	if !ValidWireReasoningEffort(value) {
		return fmt.Errorf("无效的思考强度 %q，可选：%v", effort, ReasoningEffortChoices())
	}
	k.PushSessionReasoningEffort(value)
	k.setReasoningEffortReading(value)
	return k.Publish(nil)
}

// SetLoopLimit 单独改"跑多少轮"（不碰思考强度）。取值判据与定时任务的循环次数同源
// （ValidateLoopLimit）。
func (k *Knobs) SetLoopLimit(limit int) error {
	if _, err := k.EnsureViewIdle("调整循环次数"); err != nil {
		return err
	}
	if err := ValidateLoopLimit(limit); err != nil {
		return err
	}
	k.ApplyLoopLimit(limit)
	return k.Publish(nil)
}

// ApplyLoopLimit 是"改循环上限"的唯一写点：引擎与快照读数一起改，避免出现"引擎已经改了、
// 拉条还显示旧值"（或反过来）的半截状态。加锁次序：宿主视图锁 → 组件锁。
func (k *Knobs) ApplyLoopLimit(limit int) {
	if limit <= 0 {
		return
	}
	k.host.SetEngineMaxLoops(limit)
	k.host.MutateRuntime(func(runtime *model.RuntimeState) {
		k.mu.Lock()
		k.loopLimit = limit
		k.mu.Unlock()
		runtime.LoopLimit = limit
	})
}

// setReasoningEffortReading 记录"当前生效的 wire 思考强度"（engine/账号池那一步由调用方做：
// 档位切换走 profile 映射，拉条走用户选的值）。加锁次序：宿主视图锁 → 组件锁。
func (k *Knobs) setReasoningEffortReading(value string) {
	k.host.MutateRuntime(func(runtime *model.RuntimeState) {
		k.mu.Lock()
		k.reasoningEffort = value
		k.mu.Unlock()
		runtime.ReasoningEffort = value
	})
}

// Readings 是 runtime 投影收集两条拉条读数的唯一入口（每个回合尾、每次工具边界都会调；
// 投影整份重建 Runtime，读数必须由这里反哺）。**不取宿主视图锁**：收集在锁外发生，
// 而应用在锁内。
func (k *Knobs) Readings() (string, int) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.reasoningEffort, k.loopLimit
}

// ReasoningEfforts / LoopLimit 是投影装配用的两个单值回调。
func (k *Knobs) ReasoningEfforts() string {
	effort, _ := k.Readings()
	return effort
}

func (k *Knobs) LoopLimit() int {
	_, limit := k.Readings()
	return limit
}

// CurrentLoopLimit 返回"当前生效的循环上限"：用户用拉条改过就用改过的值，否则回退当前档位的
// 默认值。它是退栈 Skill / 任务边界恢复循环上限的**唯一读法**——旧实现直接取
// maxLoopsFor(档位)，于是"用户把循环次数拧到 600"会在一次退栈后无声回到 384。
func (k *Knobs) CurrentLoopLimit() int {
	k.mu.RLock()
	limit := k.loopLimit
	k.mu.RUnlock()
	if limit > 0 {
		return limit
	}
	return k.host.MaxLoopsForEffort(k.host.CurrentEffort())
}

// PushSessionReasoningEffort 把 wire 值下发给"跟随会话"的账号，返回被改动的账号数。
// 端口未装配（宿主没实现 ReasoningEffortPort）时为 no-op。
func (k *Knobs) PushSessionReasoningEffort(wireValue string) int {
	sink := k.host.ReasoningEffortSink()
	if sink == nil {
		return 0
	}
	return sink.SetSessionReasoningEffort(wireValue)
}

// Publish 把两条拉条的读数变更推成一次 runtime.changed：mutate 在宿主视图锁下改快照
// （nil = 不改字段，例如换循环上限那条链已经自己写过了），随后由宿主按既有口径**下发完整运行时
// 而不是 nil**（payload 为 nil 的 runtime.changed 会被前端判为不可增量应用 → 触发整份快照刷新，
// 表现为"拖一下拉条、右侧面板整块闪动"）。
func (k *Knobs) Publish(mutate func(*model.RuntimeState)) error {
	return k.host.PublishRuntime(mutate)
}

// BorrowSessionReasoningEffort 记下"某会话的下一回合要用哪条 wire 思考强度"（触发即记，
// 回合起点取走）。
func (k *Knobs) BorrowSessionReasoningEffort(sessionID, wireValue string) {
	sessionID = strings.TrimSpace(sessionID)
	wireValue = strings.TrimSpace(wireValue)
	if sessionID == "" || wireValue == "" {
		return
	}
	k.sessionMu.Lock()
	defer k.sessionMu.Unlock()
	if k.sessionEffort == nil {
		k.sessionEffort = make(map[string]string)
	}
	k.sessionEffort[sessionID] = wireValue
}

// TakeSessionReasoningEffort 取走并消掉指定会话待下发的思考强度（没有则 false）。
func (k *Knobs) TakeSessionReasoningEffort(sessionID string) (string, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", false
	}
	k.sessionMu.Lock()
	defer k.sessionMu.Unlock()
	wireValue, ok := k.sessionEffort[sessionID]
	if !ok {
		return "", false
	}
	delete(k.sessionEffort, sessionID)
	return wireValue, true
}

// ValidateLoopLimit 是"循环次数"这一项的**唯一**一份取值判据：拉条（SetLoopLimit）与定时任务
// 声明的循环次数共用它。上界取 limits.input_loop_limit（默认 9999，就是 goal skill 用的
// "≈无上限"哨兵）——一旦被超过，这个哨兵就不再表达"无上限"。
func ValidateLoopLimit(limit int) error {
	upper := Get().InputLoopLimit
	if limit < 1 || limit > upper {
		return fmt.Errorf("循环次数需在 1..%d 之间（收到 %d）", upper, limit)
	}
	return nil
}

// ValidWireReasoningEffort 判定一个值是否是 provider 词表里的思考强度。
//
// 判据本体只有一份（dto.NormalizeWireReasoningEffort），这里只补一条拉条自己的口径：
// **不收空值**——空是"没声明"，调用方（定时任务）可以按默认档位兜底，而一条拉条没有
// "跟随档位"这一格，停在空上等于没有位置。
func ValidWireReasoningEffort(value string) bool {
	normalized, err := dto.NormalizeWireReasoningEffort(value)
	return err == nil && normalized != ""
}

// ReasoningEffortChoices 是给报错提示用的可选值清单（不含 "session"：见
// ValidWireReasoningEffort 的注释）。词表本体在契约层，这里不写第二份。
func ReasoningEffortChoices() []string {
	return dto.WireReasoningEffortVocabulary()
}

// ErrKnobsLocked 是运行守卫的哨兵（调用方若需要 errors.Is 判"因为会话在跑"）。
//
// 登记：迁移前只有定义、无消费者（P8 的"僵尸面"候选），本片按"行为零变化"逐字保留，
// 是否删除留给 P8 的统一裁决。
var ErrKnobsLocked = errors.New("会话运行中，配置已上锁")
