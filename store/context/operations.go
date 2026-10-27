package context

import (
	"encoding/json"
	"fmt"
	"time"
)

func newStackInput(
	itemID string,
	kind StackKind,
	status string,
	payload any,
	enteredAt time.Time,
	now time.Time,
	roleName string,
	roleSessionID string,
) (StackItemInput, error) {
	if itemID == "" {
		return StackItemInput{}, fmt.Errorf("session context: %s frame requires %s", kind, kind)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return StackItemInput{}, err
	}
	if status == "" {
		status = "active"
	}
	if enteredAt.IsZero() {
		enteredAt = now
	}
	return StackItemInput{
		ItemID: itemID, Kind: kind, Status: status, Payload: raw,
		EnteredAt: enteredAt, RoleName: roleName, RoleSessionID: roleSessionID,
	}, nil
}

func NewPlanStackInput(frame PlanFrame, now time.Time) (StackItemInput, error) {
	if frame.PlanID == "" {
		return StackItemInput{}, fmt.Errorf("session context: plan frame requires plan_id")
	}
	return newStackInput(frame.PlanID, StackKindPlan, frame.Status, frame, frame.EnteredAt, now, "", "")
}

func NewTaskStackInput(frame TaskFrame, now time.Time) (StackItemInput, error) {
	if frame.TaskID == "" {
		return StackItemInput{}, fmt.Errorf("session context: task frame requires task_id")
	}
	return newStackInput(frame.TaskID, StackKindTask, frame.Status, frame, frame.EnteredAt, now, "", "")
}

func NewGoalStackInput(frame GoalFrame, now time.Time) (StackItemInput, error) {
	if frame.GoalID == "" {
		return StackItemInput{}, fmt.Errorf("session context: goal frame requires goal_id")
	}
	return newStackInput(frame.GoalID, StackKindGoal, frame.Status, frame, frame.EnteredAt, now,
		frame.RoleName, frame.RoleSessionID)
}

func RequireTopGoal(frames []GoalFrame, goalID string) error {
	if len(frames) == 0 {
		return fmt.Errorf("session context: goal %q is not on the goal stack (empty)", goalID)
	}
	top := frames[len(frames)-1].GoalID
	if top != goalID {
		return fmt.Errorf("session context: goal %q is not the stack top (top=%q)", goalID, top)
	}
	return nil
}

func PushSkillFrame(frames []SkillFrame, frame SkillFrame, now time.Time) ([]SkillFrame, error) {
	if frame.SkillID == "" {
		return nil, fmt.Errorf("session context: skill frame requires skill_id")
	}
	if frame.ActivatedAt.IsZero() {
		frame.ActivatedAt = now
	}
	return append(frames, frame), nil
}

func PopSkillFrame(frames []SkillFrame, skillID string) ([]SkillFrame, error) {
	for index := len(frames) - 1; index >= 0; index-- {
		if frames[index].SkillID == skillID {
			return append(frames[:index], frames[index+1:]...), nil
		}
	}
	return nil, fmt.Errorf("session context: skill %q is not on the skill stack", skillID)
}
