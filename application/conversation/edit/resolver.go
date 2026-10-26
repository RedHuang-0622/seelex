package edit

import (
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// TargetResolverDeps loads parent events and identifies user text.
type TargetResolverDeps struct {
	LoadEvents func(parentID string) ([]store.Event, bool, error)
	IsUserText func(model.Message) bool
}

// TargetResolver resolves editable user events and fork cuts.
type TargetResolver struct {
	deps TargetResolverDeps
}

// NewTargetResolver constructs the target resolver.
func NewTargetResolver(deps TargetResolverDeps) *TargetResolver {
	return &TargetResolver{deps: deps}
}

// Resolve returns target, fork cut and parent revision.
func (resolver *TargetResolver) Resolve(parentID string, targetSeq uint64) (store.Event, uint64, uint64, error) {
	events, available, err := resolver.deps.LoadEvents(parentID)
	if !available {
		return store.Event{}, 0, 0, errors.New("会话 fork 存储不可用")
	}
	if err != nil {
		return store.Event{}, 0, 0, fmt.Errorf("读取父会话事件失败: %w", err)
	}
	var parentRevision uint64
	for _, event := range events {
		if event.Seq > parentRevision {
			parentRevision = event.Seq
		}
	}
	var target *store.Event
	for index := range events {
		if events[index].Seq == targetSeq {
			target = &events[index]
			break
		}
	}
	if target == nil {
		return store.Event{}, 0, parentRevision, fmt.Errorf("找不到 EventSeq=%d 的用户消息", targetSeq)
	}
	if target.Role != "user" || target.WireMaterial || target.Kind == model.TranscriptEventKindInternal ||
		!resolver.deps.IsUserText(model.Message{Role: target.Role, Content: target.Content}) {
		return store.Event{}, 0, parentRevision, errors.New("目标事件不是可编辑的用户输入")
	}
	var cut uint64
	for _, paragraph := range store.EventParagraphs(events) {
		if target.Seq < paragraph.EventFrom || target.Seq > paragraph.EventTo {
			if paragraph.EventTo < target.Seq {
				cut = paragraph.EventTo
			}
			continue
		}
		if target.Seq != paragraph.EventFrom {
			return store.Event{}, 0, parentRevision, errors.New("目标用户消息不是完整段落起点")
		}
		return *target, cut, parentRevision, nil
	}
	return store.Event{}, 0, parentRevision, errors.New("目标用户消息不属于完整段落")
}
