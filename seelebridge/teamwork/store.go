package teamwork

// store.go — 计划持久面的适配：把 sessionstore 的 moduleTeamwork 读/写面
// （TeamworkRepository）折成本包的 PlanStore 端口。
//
// 为什么用适配而不是让 sessionstore 直接实现 PlanStore：PlanStore 是**编排面**的
// 端口（它说"写一份计划、追加一条审计"），sessionstore 是**存储面**（它说"发布一个
// 模块 head、追加一行 JSONL"）。两层说同一件事但用各自的词汇；把它们绑在一起会让
// 存储层依赖上层的编排词汇。适配器是唯一知道两套词汇的地方。

import (
	"context"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// NewPlanStore 把 sessionstore.TeamworkRepository 适配成 PlanStore。
func NewPlanStore(repository sessionstore.TeamworkRepository) PlanStore {
	if repository == nil {
		return nil
	}
	return planStore{repository: repository}
}

type planStore struct {
	repository sessionstore.TeamworkRepository
}

func (s planStore) WritePlan(ctx context.Context, key sessionstore.Key, plan sessionstore.TeamworkPlan, maxTeammates int) error {
	return s.repository.WriteTeamworkPlan(ctx, key, plan, maxTeammates)
}

func (s planStore) ReadPlan(ctx context.Context, key sessionstore.Key) (sessionstore.TeamworkPlan, error) {
	return s.repository.ReadTeamworkPlan(ctx, key)
}

func (s planStore) AppendEvent(ctx context.Context, key sessionstore.Key, event sessionstore.TeamworkEvent) error {
	return s.repository.AppendTeamworkEvent(ctx, key, event)
}

func (s planStore) ReadEvents(ctx context.Context, key sessionstore.Key) ([]sessionstore.TeamworkEvent, error) {
	return s.repository.ReadTeamworkEvents(ctx, key)
}

func (s planStore) AppendBinding(ctx context.Context, key sessionstore.Key, binding sessionstore.TeamworkBinding) error {
	return s.repository.AppendTeamworkBinding(ctx, key, binding)
}

func (s planStore) ReadBindings(ctx context.Context, key sessionstore.Key) ([]sessionstore.TeamworkBinding, error) {
	return s.repository.ReadTeamworkBindings(ctx, key)
}
