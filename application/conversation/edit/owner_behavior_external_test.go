package edit_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

type editTestLocker struct {
	mu     sync.Mutex
	locked int
}

func (locker *editTestLocker) Lock() {
	locker.mu.Lock()
	locker.locked++
}

func (locker *editTestLocker) Unlock() {
	locker.locked--
	locker.mu.Unlock()
}

func TestOperationRegistryDeduplicatesMatchingRequest(t *testing.T) {
	registry := applicationedit.NewOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}

	first, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("first Begin = owner %v, err %v", owner, err)
	}
	second, owner, err := registry.Begin("parent-a", request)
	if err != nil || owner {
		t.Fatalf("duplicate Begin = owner %v, err %v", owner, err)
	}
	if first != second {
		t.Fatal("相同 operation_id 和请求必须复用同一 operation")
	}
}

func TestOperationRegistryRejectsReusedIDWithDifferentRequest(t *testing.T) {
	registry := applicationedit.NewOperationRegistry(5*time.Minute, time.Now)
	if _, _, err := registry.Begin("parent-a", model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "one"}); err != nil {
		t.Fatal(err)
	}

	_, _, err := registry.Begin("parent-a", model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "two"})
	if err == nil || err.Error() != "同一 operation_id 不能复用于不同的编辑请求" {
		t.Fatalf("Begin error = %v", err)
	}
}

func TestOperationRegistryExpiresSuccessfulOperations(t *testing.T) {
	now := time.Unix(100, 0)
	registry := applicationedit.NewOperationRegistry(5*time.Minute, func() time.Time { return now })
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	registry.Finish(operation, model.EditUserMessageForkResult{ChildSessionID: "child-a"}, nil)

	now = now.Add(5*time.Minute + time.Second)
	replacement, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("expired Begin = owner %v, err %v", owner, err)
	}
	if replacement == operation {
		t.Fatal("TTL 过期后必须创建新 operation")
	}
}

func TestOperationRegistryDropsFailedOperation(t *testing.T) {
	registry := applicationedit.NewOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	runErr := errors.New("fork failed")
	registry.Finish(operation, model.EditUserMessageForkResult{}, runErr)

	select {
	case <-operation.Done():
	default:
		t.Fatal("Finish 必须关闭 operation.done")
	}
	_, operationErr := operation.Outcome()
	if !errors.Is(operationErr, runErr) {
		t.Fatalf("operation err = %v, want %v", operationErr, runErr)
	}
	replacement, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("failed request rerun = owner %v, err %v", owner, err)
	}
	if replacement == operation {
		t.Fatal("失败 operation 必须从表里删除")
	}
}

func TestOperationRegistryWaiterSeesCompletedResult(t *testing.T) {
	registry := applicationedit.NewOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	want := model.EditUserMessageForkResult{ChildSessionID: "child-a"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-operation.Done()
	}()
	registry.Finish(operation, want, nil)
	wg.Wait()

	got, _ := operation.Outcome()
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
	if operation.CompletedAt().IsZero() {
		t.Fatal("成功 operation 必须记录 completedAt")
	}
}

func TestValidatorRejectsNonCurrentParent(t *testing.T) {
	validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
		Snapshot: func(string) applicationedit.ParentState {
			return applicationedit.ParentState{CurrentSessionID: "session-b", HasWindow: true}
		},
	})

	err := validator.Validate("session-a", 7, 7, 3)
	if err == nil || err.Error() != "只能编辑当前会话中的用户消息" {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestValidatorRejectsBusyAndQueuedParent(t *testing.T) {
	runningErr := errors.New("chat running")
	tests := []struct {
		name  string
		state applicationedit.ParentState
		want  error
		text  string
	}{
		{
			name:  "running",
			state: applicationedit.ParentState{CurrentSessionID: "session-a", Running: true, HasWindow: true},
			want:  runningErr,
		},
		{
			name:  "queued",
			state: applicationedit.ParentState{CurrentSessionID: "session-a", Queued: true, HasWindow: true},
			text:  "父会话还有排队输入",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
				Snapshot:         func(string) applicationedit.ParentState { return test.state },
				ChatRunningError: runningErr,
			})
			err := validator.Validate("session-a", 7, 7, 3)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Validate error = %v, want %v", err, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Validate error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestValidatorRejectsRevisionWindowAndCompression(t *testing.T) {
	tests := []struct {
		name             string
		state            applicationedit.ParentState
		parentRevision   uint64
		expectedRevision uint64
		targetSeq        uint64
		text             string
	}{
		{
			name: "revision", state: applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true},
			parentRevision: 8, expectedRevision: 7, targetSeq: 3, text: "父会话已推进",
		},
		{
			name: "no-window", state: applicationedit.ParentState{CurrentSessionID: "session-a"},
			parentRevision: 7, expectedRevision: 7, targetSeq: 3, text: applicationedit.NoWindowReason,
		},
		{
			name: "compressed", state: applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true, Floor: 5},
			parentRevision: 7, expectedRevision: 7, targetSeq: 3, text: applicationedit.CompressedReason,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
				Snapshot: func(string) applicationedit.ParentState { return test.state },
			})
			err := validator.Validate("session-a", test.parentRevision, test.expectedRevision, test.targetSeq)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Validate error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestValidatorAcceptsEditableCurrentWindow(t *testing.T) {
	validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
		Snapshot: func(sessionID string) applicationedit.ParentState {
			if sessionID != "session-a" {
				t.Fatalf("snapshot session = %q", sessionID)
			}
			return applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true, Floor: 3}
		},
	})

	if err := validator.Validate("session-a", 7, 7, 3); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := validator.Validate("session-a", 7, 0, 4); err != nil {
		t.Fatalf("expectedRevision=0 should accept current revision: %v", err)
	}
}

func TestTargetResolverRejectsMissingStore(t *testing.T) {
	resolver := applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
		LoadEvents: func(string) ([]model.TranscriptEvent, bool, error) {
			return nil, false, nil
		},
		IsUserText: func(message model.Message) bool { return message.Content != "" },
	})

	_, _, _, err := resolver.Resolve("parent", 1)
	if err == nil || err.Error() != "会话 fork 存储不可用" {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestTargetResolverComputesRevisionAndParagraphCut(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "first", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "answer", MessageID: "m2"},
		{Seq: 3, TaskID: "chat-2", Role: "user", Content: "edit me", MessageID: "m3"},
		{Seq: 4, TaskID: "chat-2", Role: "assistant", Content: "old branch", MessageID: "m4"},
		{Seq: 5, TaskID: "chat-3", Role: "user", Content: "latest", MessageID: "m5"},
	}
	resolver := applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
		LoadEvents: func(parentID string) ([]model.TranscriptEvent, bool, error) {
			if parentID != "parent" {
				t.Fatalf("parentID = %q", parentID)
			}
			return events, true, nil
		},
		IsUserText: func(message model.Message) bool { return message.Content != "" },
	})

	target, cut, revision, err := resolver.Resolve("parent", 3)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if target.Seq != 3 || target.MessageID != "m3" {
		t.Fatalf("target = %+v, want seq 3", target)
	}
	if cut != 2 || revision != 5 {
		t.Fatalf("cut/revision = %d/%d, want 2/5", cut, revision)
	}
}

func TestTargetResolverRejectsIneligibleTargets(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "first", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "answer", MessageID: "m2"},
		{Seq: 3, TaskID: "chat-2", Role: "user", Kind: model.TranscriptEventKindInternal, Content: "internal", MessageID: "m3"},
	}
	tests := []struct {
		name   string
		target uint64
		text   string
	}{
		{name: "missing", target: 99, text: "找不到 EventSeq=99"},
		{name: "assistant", target: 2, text: "目标事件不是可编辑的用户输入"},
		{name: "internal", target: 3, text: "目标事件不是可编辑的用户输入"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
				LoadEvents: func(string) ([]model.TranscriptEvent, bool, error) {
					return events, true, nil
				},
				IsUserText: func(message model.Message) bool { return message.Content != "" },
			})
			_, _, _, err := resolver.Resolve("parent", test.target)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Resolve error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestTargetResolverWrapsLoadFailure(t *testing.T) {
	loadErr := errors.New("read failed")
	resolver := applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
		LoadEvents: func(string) ([]model.TranscriptEvent, bool, error) {
			return nil, true, loadErr
		},
		IsUserText: func(message model.Message) bool { return message.Content != "" },
	})

	_, _, _, err := resolver.Resolve("parent", 1)
	if !errors.Is(err, loadErr) {
		t.Fatalf("Resolve error = %v, want load error", err)
	}
}

func TestRollbackReturnsCauseWhenBothActionsSucceed(t *testing.T) {
	cause := errors.New("submit failed")
	component := applicationedit.NewRollback(applicationedit.RollbackDeps{
		Delete: func(childID string) error {
			if childID != "child" {
				t.Fatalf("delete child = %q", childID)
			}
			return nil
		},
		Resume: func(parentID string) error {
			if parentID != "parent" {
				t.Fatalf("resume parent = %q", parentID)
			}
			return nil
		},
	})

	if err := component.Rollback("parent", "child", cause); err != cause {
		t.Fatalf("Rollback error = %v, want original cause", err)
	}
}

func TestRollbackCombinesChildAndParentFailures(t *testing.T) {
	tests := []struct {
		name      string
		deleteErr error
		resumeErr error
		contains  []string
	}{
		{name: "delete", deleteErr: errors.New("delete failed"), contains: []string{"回滚 child 失败"}},
		{name: "resume", resumeErr: errors.New("resume failed"), contains: []string{"恢复父会话失败"}},
		{name: "both", deleteErr: errors.New("delete failed"), resumeErr: errors.New("resume failed"), contains: []string{"回滚 child 失败", "恢复父会话失败"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("submit failed")
			component := applicationedit.NewRollback(applicationedit.RollbackDeps{
				Delete: func(string) error { return test.deleteErr },
				Resume: func(string) error { return test.resumeErr },
			})

			err := component.Rollback("parent", "child", cause)
			if !errors.Is(err, cause) {
				t.Fatalf("Rollback error = %v, want cause identity", err)
			}
			for _, text := range test.contains {
				if !strings.Contains(err.Error(), text) {
					t.Fatalf("Rollback error = %v, want %q", err, text)
				}
			}
		})
	}
}

func TestProcessStopsBeforeValidateWhenResolveFails(t *testing.T) {
	resolveErr := errors.New("missing target")
	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(string, uint64) (model.TranscriptEvent, uint64, uint64, error) {
			return model.TranscriptEvent{}, 0, 0, resolveErr
		},
		Validate: func(string, uint64, uint64, uint64) error {
			t.Fatal("解析失败后不得继续校验")
			return nil
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{})
	if !errors.Is(err, resolveErr) {
		t.Fatalf("Run error = %v, want resolve error", err)
	}
}

func TestProcessStopsBeforeForkWhenValidateFails(t *testing.T) {
	validateErr := errors.New("stale")
	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(string, uint64) (model.TranscriptEvent, uint64, uint64, error) {
			return model.TranscriptEvent{Seq: 3}, 2, 7, nil
		},
		Validate: func(parentID string, parentRevision, expectedRevision, targetSeq uint64) error {
			if parentID != "parent" || parentRevision != 7 || expectedRevision != 6 || targetSeq != 3 {
				t.Fatalf("validate args = %s/%d/%d/%d", parentID, parentRevision, expectedRevision, targetSeq)
			}
			return validateErr
		},
		Fork: func(string, model.ForkRequest) (string, error) {
			t.Fatal("校验失败后不得 fork")
			return "", nil
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, ExpectedRevision: 6,
	})
	if !errors.Is(err, validateErr) {
		t.Fatalf("Run error = %v, want validate error", err)
	}
}

func TestProcessForksUnderTransitionAndStartsChild(t *testing.T) {
	locker := &editTestLocker{}
	var calls []string
	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(string, uint64) (model.TranscriptEvent, uint64, uint64, error) {
			return model.TranscriptEvent{Seq: 3, MessageID: "m3"}, 2, 7, nil
		},
		Validate: func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(parentID string) sync.Locker {
			if parentID != "parent" {
				t.Fatalf("transition parent = %q", parentID)
			}
			return locker
		},
		Fork: func(parentID string, request model.ForkRequest) (string, error) {
			calls = append(calls, "fork:"+parentID)
			if request.EventSeq != 2 || request.SourceEventSeq != 3 ||
				request.SourceMessageID != "m3" || request.ExpectedEventSeq != 7 {
				t.Fatalf("fork request = %+v", request)
			}
			if locker.locked != 1 {
				t.Fatalf("fork called with %d transition locks, want 1", locker.locked)
			}
			return "child", nil
		},
		Resume: func(childID string) error {
			calls = append(calls, "resume:"+childID)
			if locker.locked != 0 {
				t.Fatalf("resume called with %d transition locks, want 0", locker.locked)
			}
			return nil
		},
		Submit: func(_ context.Context, childID, text string) error {
			calls = append(calls, "submit:"+childID+":"+text)
			return nil
		},
	})

	result, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, ExpectedRevision: 7, NewText: "edited",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != (model.EditUserMessageForkResult{ChildSessionID: "child", ForkPoint: 2}) {
		t.Fatalf("result = %+v", result)
	}
	if got := strings.Join(calls, ","); got != "fork:parent,resume:child,submit:child:edited" {
		t.Fatalf("calls = %q", got)
	}
}

func TestProcessRollsBackWhenChildResumeFails(t *testing.T) {
	locker := &editTestLocker{}
	resumeErr := errors.New("resume failed")
	var rollbackCause error
	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(string, uint64) (model.TranscriptEvent, uint64, uint64, error) {
			return model.TranscriptEvent{Seq: 3}, 2, 7, nil
		},
		Validate:         func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(string) sync.Locker { return locker },
		Fork:             func(string, model.ForkRequest) (string, error) { return "child", nil },
		Resume:           func(string) error { return resumeErr },
		Submit:           func(context.Context, string, string) error { t.Fatal("resume 失败后不得提交"); return nil },
		Rollback: func(parentID, childID string, cause error) error {
			rollbackCause = cause
			if parentID != "parent" || childID != "child" {
				t.Fatalf("rollback args = %s/%s", parentID, childID)
			}
			return cause
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{TargetEventSeq: 3})
	if !errors.Is(err, resumeErr) || !strings.Contains(rollbackCause.Error(), "切换编辑分支失败") {
		t.Fatalf("Run error = %v, rollback cause = %v", err, rollbackCause)
	}
}

func TestProcessRollsBackWhenSubmitFails(t *testing.T) {
	locker := &editTestLocker{}
	submitErr := errors.New("submit failed")
	var rollbackCalled bool
	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(string, uint64) (model.TranscriptEvent, uint64, uint64, error) {
			return model.TranscriptEvent{Seq: 3}, 2, 7, nil
		},
		Validate:         func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(string) sync.Locker { return locker },
		Fork:             func(string, model.ForkRequest) (string, error) { return "child", nil },
		Resume:           func(string) error { return nil },
		Submit:           func(context.Context, string, string) error { return submitErr },
		Rollback: func(_, _ string, cause error) error {
			rollbackCalled = strings.Contains(cause.Error(), "提交编辑后的输入失败") && errors.Is(cause, submitErr)
			return cause
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{TargetEventSeq: 3})
	if !errors.Is(err, submitErr) || !rollbackCalled {
		t.Fatalf("Run error = %v, rollback = %v", err, rollbackCalled)
	}
}

func TestAnnotationBackfillsSeqAndMarksEditable(t *testing.T) {
	component := applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
		Input: func(sessionID string) applicationedit.AnnotationInput {
			if sessionID != "session-a" {
				t.Fatalf("input session = %q", sessionID)
			}
			return applicationedit.AnnotationInput{
				Transcript: []model.TranscriptEvent{
					{Role: "user", MessageID: "m1", Seq: 4},
				},
				Floor: 3, HasWindow: true,
			}
		},
		IsUserText: func(message model.Message) bool { return message.Role == "user" },
	})
	messages := []model.Message{
		{ID: "m1", Role: "user", Content: "editable"},
		{ID: "m2", Role: "assistant", Content: "not editable"},
	}

	annotated := component.Annotate("session-a", messages, false)
	if !annotated[0].Editable || annotated[0].EventSeq != 4 || annotated[0].EditDisabledReason != "" {
		t.Fatalf("user annotation = %+v", annotated[0])
	}
	if annotated[1].Editable || annotated[1].EditDisabledReason != "" {
		t.Fatalf("assistant annotation = %+v", annotated[1])
	}
	if messages[0].EventSeq != 0 {
		t.Fatal("Annotate 不得原地修改输入切片")
	}
}

func TestAnnotationExplainsDisabledReasons(t *testing.T) {
	tests := []struct {
		name    string
		input   applicationedit.AnnotationInput
		running bool
		text    string
	}{
		{name: "running", input: applicationedit.AnnotationInput{HasWindow: true}, running: true, text: applicationedit.RunningReason},
		{name: "missing", input: applicationedit.AnnotationInput{HasWindow: true}, text: applicationedit.MissingSeqReason},
		{
			name:  "no-window",
			input: applicationedit.AnnotationInput{Transcript: []model.TranscriptEvent{{Role: "user", MessageID: "m1", Seq: 4}}},
			text:  applicationedit.NoWindowReason,
		},
		{
			name:  "compressed",
			input: applicationedit.AnnotationInput{Transcript: []model.TranscriptEvent{{Role: "user", MessageID: "m1", Seq: 4}}, Floor: 5, HasWindow: true},
			text:  applicationedit.CompressedReason,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			component := applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
				Input:      func(string) applicationedit.AnnotationInput { return test.input },
				IsUserText: func(message model.Message) bool { return message.Role == "user" },
			})
			annotated := component.Annotate("session-a", []model.Message{{ID: "m1", Role: "user", Content: "input"}}, test.running)
			if annotated[0].Editable || !strings.Contains(annotated[0].EditDisabledReason, test.text) {
				t.Fatalf("annotation = %+v, want reason %q", annotated[0], test.text)
			}
		})
	}
}
