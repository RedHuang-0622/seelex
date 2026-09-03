package session

import (
	"github.com/RedHuang-0622/seelex/application/model"
)

// SetRuntimeState 把一次运行时投影写入本会话槽（深拷贝：投影的 slice
// 与调用方后续写入互不 alias）。
func (unit *SessionUnit) SetRuntimeState(state model.RuntimeState) {
	if unit == nil {
		return
	}
	unit.mu.Lock()
	unit.Runtime = model.CloneRuntimeState(state)
	unit.mu.Unlock()
}

// RuntimeState 返回本会话运行时投影槽的深拷贝（无槽内容时返回零值）。
func (unit *SessionUnit) RuntimeState() model.RuntimeState {
	if unit == nil {
		return model.RuntimeState{}
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return model.CloneRuntimeState(unit.Runtime)
}

// RuntimeStateLoaded 报告本会话运行时投影槽是否已写入过（区分「槽为空」
// 与「尚未投影」；判断依据是任意会话专属/投影字段非零，而非零值字段语义）。
func (unit *SessionUnit) RuntimeStateLoaded() bool {
	if unit == nil {
		return false
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	runtime := unit.Runtime
	return runtime.Model != "" || runtime.Plugin != "" ||
		runtime.Effort != "" || len(runtime.VisibleTools) > 0 ||
		len(runtime.Skills) > 0 || runtime.Tokens != "" ||
		runtime.Plan != nil || len(runtime.WorkTable) > 0
}

// SnapshotRevision 返回本会话快照修订号（未发生任何会话事件时为 0）。
func (unit *SessionUnit) SnapshotRevision() uint64 {
	if unit == nil {
		return 0
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Revision
}

// BumpSnapshotRevision 递增并返回本会话快照修订号（INV-G5：会话级修订推进，与
// 进程 Snapshot.Revision 分离；调用方负责在发布会话事件时同步使用返回值）。
func (unit *SessionUnit) BumpSnapshotRevision() uint64 {
	if unit == nil {
		return 0
	}
	unit.mu.Lock()
	unit.Revision++
	revision := unit.Revision
	unit.mu.Unlock()
	return revision
}
