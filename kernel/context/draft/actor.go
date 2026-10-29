// Package draft owns the append-only, per-session draft queue in front of the
// durable context truth. Sequence numbers are allocated by the caller; the
// actor is the only writer and commits only at explicit turn barriers.
package draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
)

const DefaultQueueCapacity = 256

var (
	ErrClosed       = errors.New("context draft: actor closed")
	ErrSequence     = errors.New("context draft: sequence conflict")
	ErrInvalidItem  = errors.New("context draft: invalid item")
	ErrCommitFailed = errors.New("context draft: commit failed")
)

// Item is one already-sequenced wire fragment. Bytes must be the exact bytes
// that were sent to the provider; Final closes one model request iteration.
type Item struct {
	SessionID string
	Seq       uint64
	Bytes     []byte
	Final     bool
}

// State is the recoverable draft state. Watermark is the largest seq whose
// commit was acknowledged and durably recorded.
type State struct {
	Items       []Item
	Watermark   uint64
	Uncommitted bool
}

// CommitBatch is passed to the durable callback on a turn barrier.
type CommitBatch struct {
	SessionID string
	Items     []Item
	Through   uint64
	Digest    string
}

// Ack is the successful durable acknowledgement for an append or commit.
type Ack struct {
	SessionID string
	Through   uint64
	Digest    string
}

// Durable is the persistence seam. Save persists pending draft bytes before an
// append is acknowledged. Commit must atomically persist truth and watermark.
type Durable interface {
	Load(context.Context, string) (State, error)
	Save(context.Context, string, State) error
	Commit(context.Context, CommitBatch) (Ack, error)
}

type appendCommand struct {
	ctx  context.Context
	item Item
	done chan appendResult
}

type appendResult struct {
	ack Ack
	err error
}

type commitCommand struct {
	ctx     context.Context
	through uint64
	done    chan appendResult
}

type closeCommand struct{ done chan struct{} }

// Actor is the single writer for one session. It is deliberately one-session
// scoped so backpressure and idempotency are local and observable.
type Actor struct {
	sessionID string
	durable   Durable
	commands  chan any

	closeOnce sync.Once
	done      chan struct{}

	stateMu sync.RWMutex
	state   State
}

// NewActor loads any durable pending state and starts the writer. Queue
// capacity is a correctness parameter: a full queue blocks the caller rather
// than dropping bytes.
func NewActor(ctx context.Context, sessionID string, capacity int, durable Durable) (*Actor, error) {
	if sessionID == "" || durable == nil {
		return nil, ErrInvalidItem
	}
	if capacity <= 0 {
		capacity = DefaultQueueCapacity
	}
	state, err := durable.Load(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load draft: %w", err)
	}
	state.Items = cloneItems(state.Items)
	actor := &Actor{
		sessionID: sessionID,
		durable:   durable,
		commands:  make(chan any, capacity),
		done:      make(chan struct{}),
		state:     state,
	}
	go actor.run()
	return actor, nil
}

// Append persists one item before acknowledging it. Repeating the same
// (session, seq) and bytes is idempotent; conflicting bytes fail.
func (actor *Actor) Append(ctx context.Context, item Item) (Ack, error) {
	if actor == nil {
		return Ack{}, ErrClosed
	}
	item.SessionID = actor.sessionID
	item.Bytes = append([]byte(nil), item.Bytes...)
	done := make(chan appendResult, 1)
	select {
	case actor.commands <- appendCommand{ctx: ctx, item: item, done: done}:
	case <-actor.done:
		return Ack{}, ErrClosed
	case <-ctx.Done():
		return Ack{}, ctx.Err()
	}
	select {
	case result := <-done:
		return result.ack, result.err
	case <-actor.done:
		return Ack{}, ErrClosed
	case <-ctx.Done():
		return Ack{}, ctx.Err()
	}
}

// Commit asks the writer to durably promote every item through seq. The
// working set is cleared only after the durable callback acknowledges it.
func (actor *Actor) Commit(ctx context.Context, through uint64) (Ack, error) {
	if actor == nil {
		return Ack{}, ErrClosed
	}
	done := make(chan appendResult, 1)
	select {
	case actor.commands <- commitCommand{ctx: ctx, through: through, done: done}:
	case <-actor.done:
		return Ack{}, ErrClosed
	case <-ctx.Done():
		return Ack{}, ctx.Err()
	}
	select {
	case result := <-done:
		return result.ack, result.err
	case <-actor.done:
		return Ack{}, ErrClosed
	case <-ctx.Done():
		return Ack{}, ctx.Err()
	}
}

// Snapshot returns a deep copy for replay/diagnostics.
func (actor *Actor) Snapshot() State {
	actor.stateMu.RLock()
	defer actor.stateMu.RUnlock()
	state := actor.state
	state.Items = cloneItems(actor.state.Items)
	return state
}

// Close stops the writer. Pending state remains in the durable store for the
// next actor instance.
func (actor *Actor) Close() {
	if actor == nil {
		return
	}
	actor.closeOnce.Do(func() {
		done := make(chan struct{})
		select {
		case actor.commands <- closeCommand{done: done}:
		case <-actor.done:
			return
		}
		<-done
	})
}

func (actor *Actor) run() {
	defer close(actor.done)
	for command := range actor.commands {
		switch value := command.(type) {
		case appendCommand:
			actor.handleAppend(value)
		case commitCommand:
			actor.handleCommit(value)
		case closeCommand:
			close(value.done)
			return
		}
	}
}

func (actor *Actor) handleAppend(command appendCommand) {
	if err := command.ctx.Err(); err != nil {
		command.done <- appendResult{err: err}
		return
	}
	item := command.item
	if item.SessionID != actor.sessionID || item.Seq == 0 || len(item.Bytes) == 0 {
		command.done <- appendResult{err: ErrInvalidItem}
		return
	}
	actor.stateMu.Lock()
	defer actor.stateMu.Unlock()
	for _, existing := range actor.state.Items {
		if existing.Seq != item.Seq {
			continue
		}
		if !bytes.Equal(existing.Bytes, item.Bytes) || existing.Final != item.Final {
			command.done <- appendResult{err: fmt.Errorf("%w: seq=%d", ErrSequence, item.Seq)}
			return
		}
		command.done <- appendResult{ack: ackFor(existing)}
		return
	}
	if item.Seq <= actor.state.Watermark {
		command.done <- appendResult{ack: Ack{SessionID: actor.sessionID, Through: item.Seq, Digest: digest(item.Bytes)}}
		return
	}
	next := append(cloneItems(actor.state.Items), item)
	sort.SliceStable(next, func(i, j int) bool { return next[i].Seq < next[j].Seq })
	candidate := State{Items: next, Watermark: actor.state.Watermark, Uncommitted: true}
	if err := actor.durable.Save(command.ctx, actor.sessionID, candidate); err != nil {
		command.done <- appendResult{err: fmt.Errorf("save draft: %w", err)}
		return
	}
	actor.state = candidate
	command.done <- appendResult{ack: ackFor(item)}
}

func (actor *Actor) handleCommit(command commitCommand) {
	if err := command.ctx.Err(); err != nil {
		command.done <- appendResult{err: err}
		return
	}
	actor.stateMu.Lock()
	defer actor.stateMu.Unlock()
	if command.through <= actor.state.Watermark {
		command.done <- appendResult{ack: Ack{SessionID: actor.sessionID, Through: actor.state.Watermark}}
		return
	}
	batch := make([]Item, 0, len(actor.state.Items))
	for _, item := range actor.state.Items {
		if item.Seq <= command.through {
			batch = append(batch, cloneItem(item))
		}
	}
	if len(batch) == 0 {
		command.done <- appendResult{err: fmt.Errorf("%w: no items through seq=%d", ErrSequence, command.through)}
		return
	}
	commit := CommitBatch{
		SessionID: actor.sessionID,
		Items:     batch,
		Through:   batch[len(batch)-1].Seq,
		Digest:    digestItems(batch),
	}
	ack, err := actor.durable.Commit(command.ctx, commit)
	if err != nil {
		actor.state.Uncommitted = true
		_ = actor.durable.Save(context.Background(), actor.sessionID, actor.state)
		command.done <- appendResult{err: fmt.Errorf("%w: %v", ErrCommitFailed, err)}
		return
	}
	if ack.SessionID == "" {
		ack.SessionID = actor.sessionID
	}
	if ack.Through == 0 {
		ack.Through = commit.Through
	}
	if ack.Digest == "" {
		ack.Digest = commit.Digest
	}
	remaining := make([]Item, 0, len(actor.state.Items))
	for _, item := range actor.state.Items {
		if item.Seq > ack.Through {
			remaining = append(remaining, item)
		}
	}
	actor.state = State{Items: remaining, Watermark: ack.Through}
	command.done <- appendResult{ack: ack}
}

func ackFor(item Item) Ack {
	return Ack{SessionID: item.SessionID, Through: item.Seq, Digest: digest(item.Bytes)}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func digestItems(items []Item) string {
	hash := sha256.New()
	for _, item := range items {
		hash.Write([]byte(fmt.Sprintf("%d:", item.Seq)))
		hash.Write(item.Bytes)
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneItems(items []Item) []Item {
	out := make([]Item, len(items))
	for index := range items {
		out[index] = cloneItem(items[index])
	}
	return out
}

func cloneItem(item Item) Item {
	item.Bytes = append([]byte(nil), item.Bytes...)
	return item
}
