package task_context

import (
	"context"
	"errors"
	"strings"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/draft"
)

// AppendContextDraftFor persists one sent wire fragment through the session draft
// actor. Seq is allocated from the transcript stream by the caller.
func (c *Coordinator) AppendContextDraftFor(ctx context.Context, sessionID string, seq uint64, wire string, final bool) error {
	if c == nil || strings.TrimSpace(sessionID) == "" || seq == 0 || wire == "" {
		return nil
	}
	actor, err := c.contextDraftActor(sessionID)
	if err != nil {
		return err
	}
	_, err = actor.Append(ctx, draft.Item{Seq: seq, Bytes: []byte(wire), Final: final})
	return err
}

// CommitContextDraftFor promotes the current work set through the request's
// transcript seq. No actor or no pending bytes is a successful no-op.
func (c *Coordinator) CommitContextDraftFor(ctx context.Context, sessionID string, through uint64) error {
	if c == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	c.draftMu.Lock()
	actor := c.draftActors[sessionID]
	c.draftMu.Unlock()
	if actor == nil {
		return nil
	}
	if through == 0 {
		through = c.LatestTranscriptSeqFor(sessionID)
	}
	if through == 0 {
		return nil
	}
	_, err := actor.Commit(ctx, through)
	if errors.Is(err, draft.ErrSequence) {
		return nil
	}
	return err
}

// CommitSessionContextDraft 是回合收尾使用的一行窄适配，through 取会话最新
// transcript Seq。
func (c *Coordinator) CommitSessionContextDraft(ctx context.Context, sessionID string) error {
	return c.CommitContextDraftFor(ctx, sessionID, 0)
}

func (c *Coordinator) contextDraftActor(sessionID string) (*draft.Actor, error) {
	c.draftMu.Lock()
	defer c.draftMu.Unlock()
	if actor := c.draftActors[sessionID]; actor != nil {
		return actor, nil
	}
	actor, err := draft.NewActor(context.Background(), sessionID, draft.DefaultQueueCapacity, taskDraftDurable{coordinator: c})
	if err != nil {
		return nil, err
	}
	if c.draftActors == nil {
		c.draftActors = make(map[string]*draft.Actor)
	}
	c.draftActors[sessionID] = actor
	return actor, nil
}

func (c *Coordinator) closeContextDraftActor(sessionID string) {
	c.draftMu.Lock()
	actor := c.draftActors[sessionID]
	delete(c.draftActors, sessionID)
	c.draftMu.Unlock()
	actor.Close()
}

type taskDraftDurable struct {
	coordinator *Coordinator
}

func (durable taskDraftDurable) Load(_ context.Context, sessionID string) (draft.State, error) {
	state := durable.coordinator.ContextDraftStateFor(sessionID)
	items := make([]draft.Item, 0, len(state.Items))
	for _, item := range state.Items {
		items = append(items, draft.Item{
			SessionID: sessionID, Seq: item.Seq, Bytes: []byte(item.Bytes), Final: item.Final,
		})
	}
	return draft.State{Items: items, Watermark: state.Watermark, Uncommitted: state.Uncommitted}, nil
}

func (durable taskDraftDurable) Save(_ context.Context, sessionID string, state draft.State) error {
	record := model.ContextDraftState{Watermark: state.Watermark, Uncommitted: state.Uncommitted}
	record.Items = make([]model.ContextDraftItem, 0, len(state.Items))
	for _, item := range state.Items {
		record.Items = append(record.Items, model.ContextDraftItem{
			Seq: item.Seq, Bytes: string(item.Bytes), Final: item.Final,
		})
	}
	durable.coordinator.ReplaceContextDraftStateFor(sessionID, record)
	return nil
}

func (durable taskDraftDurable) Commit(ctx context.Context, batch draft.CommitBatch) (draft.Ack, error) {
	if err := ctx.Err(); err != nil {
		return draft.Ack{}, err
	}
	state := durable.coordinator.ContextDraftStateFor(batch.SessionID)
	remaining := make([]model.ContextDraftItem, 0, len(state.Items))
	for _, item := range state.Items {
		if item.Seq > batch.Through {
			remaining = append(remaining, item)
		}
	}
	durable.coordinator.ReplaceContextDraftStateFor(batch.SessionID, model.ContextDraftState{
		Items: remaining, Watermark: batch.Through,
	})
	return draft.Ack{SessionID: batch.SessionID, Through: batch.Through, Digest: batch.Digest}, nil
}
