package contract

import "github.com/RedHuang-0622/seelex/contract/dto"

// ScheduledTaskPersistence 是定时任务定义的**全局持久化端口**（调度器只依赖它，
// 不认识文件系统）：Append 追加一次变更；Load 返回重放后的**当前任务集**
// （已丢弃墓碑与残尾）。
//
// 物理位置：2026-10-16 按 plan §2.12.1 从 `backend/engine/schedule/persistence.go` 归位到这里；
// 实现是 `store/schedule.FileStore`（结构满足，编译期断言写在它自己的包里，
// 见该文件头）。
type ScheduledTaskPersistence interface {
	Append(record dto.ScheduledTaskRecord) error
	Load() ([]dto.ScheduledTaskRecord, error)
}
