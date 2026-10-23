package adapters

// session_ssh_state_port.go —— 会话级 SSH 状态在装配侧的落地：把
// store.SessionSSHStateStore 接到会话端口上（application/core 用类型断言发现）。
//
// 与 rolePermissionStores 同一条理由：store 内部有一把**按项目分配**的追加锁，
// 同一个数据根必须共用**同一个** store 实例，否则两个实例各持一把锁，并发追加会
// 交错成半行（JSONL 的一行被劈开 = 一行坏数据）。

import (
	"errors"
	"sync"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// errSessionSSHStorageNotAssembled 表示会话存储没装配（不静默当成没有状态）。
var errSessionSSHStorageNotAssembled = errors.New("session ssh: session storage is not assembled")

// sessionSSHStateStores 是「数据根（Router）→ 会话 SSH 状态存储」的实例表。
var sessionSSHStateStores sync.Map // *store.Router -> *store.SessionSSHStateStore

// sessionSSHStateStoreFor 取该数据根的会话 SSH 状态存储（首次调用建实例，其后复用）。
func sessionSSHStateStoreFor(router *store.Router) *store.SessionSSHStateStore {
	if router == nil {
		return nil
	}
	if cached, ok := sessionSSHStateStores.Load(router); ok {
		return cached.(*store.SessionSSHStateStore)
	}
	created, _ := sessionSSHStateStores.LoadOrStore(router, store.NewSessionSSHStateStore(router))
	return created.(*store.SessionSSHStateStore)
}

// sessionSSHStateStore 返回本端口的会话 SSH 状态存储（存储未装配时 nil）。
func (port SessionPort) sessionSSHStateStore() *store.SessionSSHStateStore {
	if port.Manager == nil {
		return nil
	}
	return sessionSSHStateStoreFor(port.Manager.Router())
}

// SessionSSHState 实现 application/core 的 sessionSSHPort：读会话 SSH 状态。
func (port SessionPort) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	storage := port.sessionSSHStateStore()
	if storage == nil {
		return model.SessionSSHInfo{}, errSessionSSHStorageNotAssembled
	}
	state, err := storage.State(port.rolePermissionProject(sessionID), sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	return model.SessionSSHInfo{
		SessionID:   state.SessionID,
		CurrentEnv:  state.CurrentEnv,
		AllowedEnvs: state.AllowedEnvs,
	}, nil
}

// SetSessionSSHState 实现 sessionSSHPort：追加一条状态（整份写入，后写覆盖先写）。
func (port SessionPort) SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error {
	storage := port.sessionSSHStateStore()
	if storage == nil {
		return errSessionSSHStorageNotAssembled
	}
	return storage.Put(port.rolePermissionProject(sessionID), store.SessionSSHState{
		SessionID:   sessionID,
		CurrentEnv:  state.CurrentEnv,
		AllowedEnvs: state.AllowedEnvs,
	})
}
