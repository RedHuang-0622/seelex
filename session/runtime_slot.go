package session

import (
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
)

// ComposerText 返回本会话未发送输入草稿正文。
func (unit *SessionUnit) ComposerText() string {
	if unit == nil {
		return ""
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Composer.Text
}

// SetComposerText 写入本会话未发送输入草稿（调用方负责持久化；空正文
// 仍更新时间戳，表示草稿曾存在后清空）。
func (unit *SessionUnit) SetComposerText(text string, updatedAt time.Time) {
	if unit == nil {
		return
	}
	unit.mu.Lock()
	unit.Composer.Text = text
	unit.Composer.UpdatedAt = updatedAt
	unit.mu.Unlock()
}

// EffortLevel 返回本会话选择的 effort 级别（空 = 未选择，回退进程默认）。
func (unit *SessionUnit) EffortLevel() string {
	if unit == nil {
		return ""
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.Effort
}

// SetEffortLevel 写入本会话的 effort 选择（运行守卫由调用方持有：目标会话
// idle 时才允许变更）。
func (unit *SessionUnit) SetEffortLevel(level string) {
	if unit == nil {
		return
	}
	unit.mu.Lock()
	unit.Effort = level
	unit.mu.Unlock()
}

// FullAccessMode 返回本会话的全权模式选择（ok=false = 未选择，回退进程
// 默认/引擎门值）。G4：每个会话保存自己的选择，切换/新建后互不覆盖。
func (unit *SessionUnit) FullAccessMode() (on bool, ok bool) {
	if unit == nil {
		return false, false
	}
	unit.mu.Lock()
	defer unit.mu.Unlock()
	return unit.FullAccess, unit.fullAccessSet
}

// SetFullAccessMode 写入本会话的全权模式选择（chat 起点按生效模式同步
// 引擎门；未选择会话回退进程默认，不继承其它会话的遗留开关）。
func (unit *SessionUnit) SetFullAccessMode(on bool) {
	if unit == nil {
		return
	}
	unit.mu.Lock()
	unit.FullAccess = on
	unit.fullAccessSet = true
	unit.mu.Unlock()
}

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
