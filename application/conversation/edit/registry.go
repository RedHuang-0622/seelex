// Package edit owns edit-user-message-and-fork application orchestration.
package edit

import (
	"errors"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	// DefaultOperationTTL is the success deduplication window.
	DefaultOperationTTL = 5 * time.Minute
	MissingSeqReason    = "这条消息缺少可定位的持久事件序号"
	CompressedReason    = "该消息已在上下文压缩前缀中，不能编辑"
	RunningReason       = "父会话正在运行；等待本轮结束后再编辑"
	NoWindowReason      = "当前上下文没有可编辑的保留窗口"
)

// Operation is one idempotent edit/fork invocation.
type Operation struct {
	done        chan struct{}
	id          string
	parentID    string
	request     model.EditUserMessageForkRequest
	result      model.EditUserMessageForkResult
	err         error
	completedAt time.Time
}

// Done closes when the operation completes.
func (operation *Operation) Done() <-chan struct{} {
	return operation.done
}

// Outcome returns the completed result and error. Callers must first wait for
// Done.
func (operation *Operation) Outcome() (model.EditUserMessageForkResult, error) {
	return operation.result, operation.err
}

// CompletedAt returns the successful completion time.
func (operation *Operation) CompletedAt() time.Time {
	return operation.completedAt
}

// OperationRegistry owns edit/fork operation bookkeeping.
type OperationRegistry struct {
	mu  sync.Mutex
	ops map[string]*Operation
	ttl time.Duration
	now func() time.Time
}

// NewOperationRegistry constructs the idempotency registry.
func NewOperationRegistry(ttl time.Duration, now func() time.Time) *OperationRegistry {
	return &OperationRegistry{
		ops: make(map[string]*Operation),
		ttl: ttl,
		now: now,
	}
}

// Begin creates or reuses an operation.
func (registry *OperationRegistry) Begin(parentID string, request model.EditUserMessageForkRequest) (*Operation, bool, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	now := registry.now()
	for id, existing := range registry.ops {
		if existing.completedAt.IsZero() || now.Sub(existing.completedAt) <= registry.ttl {
			continue
		}
		delete(registry.ops, id)
	}
	if existing := registry.ops[request.OperationID]; existing != nil {
		if existing.parentID != parentID || existing.request != request {
			return nil, false, errors.New("同一 operation_id 不能复用于不同的编辑请求")
		}
		return existing, false, nil
	}
	operation := &Operation{
		done: make(chan struct{}), id: request.OperationID,
		parentID: parentID, request: request,
	}
	registry.ops[request.OperationID] = operation
	return operation, true, nil
}

// Finish publishes one operation outcome.
func (registry *OperationRegistry) Finish(operation *Operation, result model.EditUserMessageForkResult, err error) {
	registry.mu.Lock()
	operation.result = result
	operation.err = err
	if err == nil {
		operation.completedAt = registry.now()
	} else {
		delete(registry.ops, operation.id)
	}
	registry.mu.Unlock()
	close(operation.done)
}
