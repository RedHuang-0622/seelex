package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type stubMessageEditForkApplication struct {
	*fakeApplication
	parentID string
	request  model.EditUserMessageForkRequest
	result   model.EditUserMessageForkResult
	err      error
}

func (stub *stubMessageEditForkApplication) EditUserMessageAndFork(_ context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	stub.parentID = parentID
	stub.request = request
	return stub.result, stub.err
}

func TestBridgeForwardsEditUserMessageAndFork(t *testing.T) {
	stub := &stubMessageEditForkApplication{
		fakeApplication: newFakeApplication(),
		result:          model.EditUserMessageForkResult{ChildSessionID: "child-1", ForkPoint: 8},
	}
	bridge, err := NewBridge(stub, Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := model.EditUserMessageForkRequest{
		TargetEventSeq: 9, NewText: "edited", ExpectedRevision: 12, OperationID: "op-1",
	}
	result, err := bridge.EditUserMessageAndFork("parent-1", request)
	if err != nil {
		t.Fatalf("EditUserMessageAndFork: %v", err)
	}
	if stub.parentID != "parent-1" || stub.request != request {
		t.Fatalf("forwarded parent/request = %q/%#v", stub.parentID, stub.request)
	}
	if result.ChildSessionID != "child-1" || result.ForkPoint != 8 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBridgeEditUserMessageAndForkUnavailableIsExplicit(t *testing.T) {
	bridge, err := NewBridge(newFakeApplication(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.EditUserMessageAndFork("parent-1", model.EditUserMessageForkRequest{})
	if err == nil || !strings.Contains(err.Error(), "未装配") {
		t.Fatalf("unavailable optional surface error = %v", err)
	}
}
