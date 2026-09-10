package sessionstore

import (
	"encoding/json"
	"fmt"
	"time"
)

// ScheduleEventPayload 是定时任务式插话 EVENT 的 payload（§8.3）。冷启动按
// schedule.registered/cancelled/fired 重放重建 timer。
type ScheduleEventPayload struct {
	ScheduleID    string `json:"schedule_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Cron          string `json:"cron,omitempty"`
	Interval      string `json:"interval,omitempty"`
	NextFireAt    string `json:"next_fire_at,omitempty"`
	PayloadRef    string `json:"payload_ref,omitempty"`
}

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
