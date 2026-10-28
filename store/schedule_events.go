package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ScheduleEventPayload 的 wire 形状唯一归 contract/dto。
type ScheduleEventPayload = dto.ScheduleEventPayload

// scheduleEventCommit 写一条 schedule.* EVENT（唯一写序 = message → event）。
func (store *storeEngine) scheduleEventCommit(key Key, kind structuralEventKind, payload ScheduleEventPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	commitID := "schedule-" + payload.ScheduleID + "-" + string(kind)
	_, err = store.structuralEventCommit(key, commitID, []structuralEvent{{
		Kind: kind, CommitID: commitID, Payload: raw, CreatedAt: time.Now().UTC(),
	}})
	return err
}

// ScheduleRegisterWorkspace 写 schedule.registered EVENT（冷启动重建 timer）。
func (router *Router) ScheduleRegisterWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error {
	return router.scheduleEvent(projectID, sessionID, structuralEventScheduleRegistered, payload)
}

// ScheduleCancelWorkspace 写 schedule.cancelled EVENT。
func (router *Router) ScheduleCancelWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error {
	return router.scheduleEvent(projectID, sessionID, structuralEventScheduleCancelled, payload)
}

// ScheduleFireWorkspace 写 schedule.fired EVENT（审计；timer 实际触发由运行期处理）。
func (router *Router) ScheduleFireWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error {
	return router.scheduleEvent(projectID, sessionID, structuralEventScheduleFired, payload)
}

func (router *Router) scheduleEvent(projectID, sessionID string, kind structuralEventKind, payload ScheduleEventPayload) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: schedule events require session layout")
		}
		return layout.layout.scheduleEventCommit(Key{ProjectID: projectID, SessionID: sessionID}, kind, payload)
	})
}
