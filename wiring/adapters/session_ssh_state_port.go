package adapters

// session_ssh_state_port.go —— 会话级 SSH 状态在装配侧的落地：存储 + **写穿缓存**。
//
// 两件事必须共用同一个进程内实体，否则会出现"写进去的放权，执行面读不到"：
//
//	1. 会话端口的读写（SessionSSHState / SetSessionSSHState）；
//	2. 执行面的**缺省环境**读面（SessionSSHEnvResolver，工具没写 env 时用）。
//
// 为什么执行面不能逐次回放 JSONL：决策段跑在**每一次工具调用**上（权限门之前），
// 一次调用一次文件回放，成本随文件增长而线性上升。所以这里做写穿缓存：
// 本进程的写入立刻更新缓存，读命中即返回。（跨进程写入要等下一次 miss 才看见——
// 数据根按设计是非共享的，多进程模式由使用方自担，与 rolePermission 同一条口径。）

import (
	"errors"
	"sync"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// errSessionSSHStorageNotAssembled 表示会话存储没装配（不静默当成"没有状态"）。
var errSessionSSHStorageNotAssembled = errors.New("session ssh: session storage is not assembled")

// sessionSSHBridge 是「会话 SSH 状态」的进程内共享体。
type sessionSSHBridge struct {
	store *store.SessionSSHStateStore

	mu sync.RWMutex
	// cache 是 sessionID → 最近一次读写后的状态（**不含 projectID**：一个会话只归属
	// 一个项目，换项目等于换会话）。
	cache map[string]model.SessionSSHInfo
}

// sessionSSHBridges 是「数据根（Router）→ 共享体」的实例表。
var sessionSSHBridges sync.Map // *store.Router -> *sessionSSHBridge

// sessionSSHBridgeFor 取该数据根的共享体（首次调用建实例，其后复用）。
func sessionSSHBridgeFor(router *store.Router) *sessionSSHBridge {
	if router == nil {
		return nil
	}
	if cached, ok := sessionSSHBridges.Load(router); ok {
		return cached.(*sessionSSHBridge)
	}
	created, _ := sessionSSHBridges.LoadOrStore(router, &sessionSSHBridge{
		store: store.NewSessionSSHStateStore(router),
		cache: map[string]model.SessionSSHInfo{},
	})
	return created.(*sessionSSHBridge)
}

// state 读会话状态：缓存优先，miss 才回放 JSONL 并回填。
func (bridge *sessionSSHBridge) state(projectID, sessionID string) (model.SessionSSHInfo, error) {
	if bridge == nil || bridge.store == nil {
		return model.SessionSSHInfo{}, errSessionSSHStorageNotAssembled
	}
	bridge.mu.RLock()
	cached, ok := bridge.cache[sessionID]
	bridge.mu.RUnlock()
	if ok {
		return cached, nil
	}
	state, err := bridge.store.State(projectID, sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	info := model.SessionSSHInfo{
		SessionID: state.SessionID, CurrentEnv: state.CurrentEnv, AllowedEnvs: state.AllowedEnvs,
	}
	if info.AllowedEnvs == nil {
		info.AllowedEnvs = []string{}
	}
	bridge.mu.Lock()
	bridge.cache[sessionID] = info
	bridge.mu.Unlock()
	return info, nil
}

// put 写会话状态：先落盘（权限状态必须持久），成功后再更新缓存。
//
// 顺序不能反：先更新缓存再落盘，落盘失败时缓存里就留着一条"看起来生效过"的状态，
// 而磁盘上还是旧的——重启后行为突变，且没有任何痕迹。
func (bridge *sessionSSHBridge) put(projectID string, state model.SessionSSHInfo) error {
	if bridge == nil || bridge.store == nil {
		return errSessionSSHStorageNotAssembled
	}
	if err := bridge.store.Put(projectID, store.SessionSSHState{
		SessionID: state.SessionID, CurrentEnv: state.CurrentEnv, AllowedEnvs: state.AllowedEnvs,
	}); err != nil {
		return err
	}
	normalized := model.SessionSSHInfo{
		SessionID: state.SessionID, CurrentEnv: state.CurrentEnv, AllowedEnvs: state.AllowedEnvs,
	}
	if normalized.AllowedEnvs == nil {
		normalized.AllowedEnvs = []string{}
	}
	bridge.mu.Lock()
	bridge.cache[state.SessionID] = normalized
	bridge.mu.Unlock()
	return nil
}

// sessionSSHBridge 取本端口的共享体（存储未装配时 nil）。
func (port SessionPort) sessionSSHBridge() *sessionSSHBridge {
	if port.Manager == nil {
		return nil
	}
	return sessionSSHBridgeFor(port.Manager.Router())
}

// SessionSSHState 实现 application/core 的 sessionSSHPort：读会话 SSH 状态。
func (port SessionPort) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	bridge := port.sessionSSHBridge()
	if bridge == nil {
		return model.SessionSSHInfo{}, errSessionSSHStorageNotAssembled
	}
	return bridge.state(port.rolePermissionProject(sessionID), sessionID)
}

// SetSessionSSHState 实现 sessionSSHPort：写会话 SSH 状态（整份写入，后写覆盖先写）。
func (port SessionPort) SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error {
	bridge := port.sessionSSHBridge()
	if bridge == nil {
		return errSessionSSHStorageNotAssembled
	}
	state.SessionID = sessionID
	return bridge.put(port.rolePermissionProject(sessionID), state)
}

// SessionSSHEnvGrantedResolver 给执行面用的**放权判定**：报告某环境是否在该会话
// 被放权过（读缓存，与端口写面同一个进程内实体）。
//
// 它挡的是"显式 env 参数绕过会话放权"：不在放权集合里的环境，工具调用也不许指名。
// 读不出来（存储异常）一律按**未放权**处理——失败方向收紧。
func SessionSSHEnvGrantedResolver(router *store.Router) func(sessionID, projectID, env string) bool {
	bridge := sessionSSHBridgeFor(router)
	if bridge == nil {
		return nil
	}
	return func(sessionID, projectID, env string) bool {
		if env == "" {
			return false
		}
		state, err := bridge.state(projectID, sessionID)
		if err != nil {
			return false
		}
		for _, name := range state.AllowedEnvs {
			if name == env {
				return true
			}
		}
		return false
	}
}

// SessionSSHEnvResolver 给执行面用的**缺省环境**读面：返回该会话当前限定的环境名
// （空 = 没有限定，工具就按"没指名环境"处理）。
//
// 参数顺序（sessionID 在前）与执行面的取值方向一致：执行面先知道"哪个会话"，
// 再问"那个会话在哪个项目"。读的是缓存，不逐次回放 JSONL。
func SessionSSHEnvResolver(router *store.Router) func(sessionID, projectID string) string {
	bridge := sessionSSHBridgeFor(router)
	if bridge == nil {
		return nil
	}
	return func(sessionID, projectID string) string {
		state, err := bridge.state(projectID, sessionID)
		if err != nil {
			// 读不出来 = 没有缺省环境（方向是收紧：不猜一个环境去连）。
			return ""
		}
		return state.CurrentEnv
	}
}
