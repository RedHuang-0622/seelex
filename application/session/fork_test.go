package session

import (
	"errors"
	"strings"
	"sync"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestForkApplicationCreatesAndSwitchesSession(t *testing.T) {
	var lock sync.Mutex
	var forkedRequest protocol.ForkRequest
	var resumed string
	application := NewFork(ForkDeps{
		TransitionForKey: func(string) sync.Locker { return &lock },
		ForkLocked: func(parent string, request protocol.ForkRequest) (string, error) {
			if parent != "parent" {
				t.Fatalf("parent = %q", parent)
			}
			forkedRequest = request
			return "child", nil
		},
		LatestForkCut: func(string) (uint64, error) { return 17, nil },
		Resume: func(sessionID string) error {
			resumed = sessionID
			return nil
		},
	})

	child, err := application.Fork(" parent ", protocol.ForkRequest{EventSeq: 9})
	if err != nil || child != "child" || forkedRequest.EventSeq != 9 || resumed != "child" {
		t.Fatalf("fork = %q request=%#v resumed=%q err=%v", child, forkedRequest, resumed, err)
	}
	forkedRequest = protocol.ForkRequest{}
	if _, err := application.ForkLatest("parent"); err != nil {
		t.Fatal(err)
	}
	if forkedRequest.EventSeq != 17 {
		t.Fatalf("latest cut = %d", forkedRequest.EventSeq)
	}
	if _, err := application.Fork("  ", protocol.ForkRequest{}); err == nil {
		t.Fatal("empty parent must fail")
	}
}

func TestForkApplicationReturnsChildWhenSwitchFails(t *testing.T) {
	switchErr := errors.New("resume failed")
	var lock sync.Mutex
	application := NewFork(ForkDeps{
		TransitionForKey: func(string) sync.Locker { return &lock },
		ForkLocked:       func(string, protocol.ForkRequest) (string, error) { return "child", nil },
		LatestForkCut:    func(string) (uint64, error) { return 1, nil },
		Resume:           func(string) error { return switchErr },
	})
	child, err := application.Fork("parent", protocol.ForkRequest{})
	if child != "child" || !errors.Is(err, switchErr) || !strings.Contains(err.Error(), "fork created session") {
		t.Fatalf("child=%q err=%v", child, err)
	}
}
