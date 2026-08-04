package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
)

func TestResolveAllReleasesPendingApprovals(t *testing.T) {
	broker := NewApprovalBroker(nil)
	opened := make(chan string, 2)
	broker.SetObserver(func(interaction *Interaction) {
		if interaction != nil {
			opened <- interaction.ID
		}
	})
	results := make(chan ApprovalDecision, 2)
	for index := 0; index < 2; index++ {
		id := fmt.Sprintf("approval-%d", index)
		go func() {
			decision, err := broker.Request(context.Background(), ApprovalRequest{
				ID: id, Question: "continue?", Options: []model.InteractionOption{{ID: "allow", Label: "Allow"}},
			})
			if err == nil {
				results <- decision
			}
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-opened:
		case <-time.After(time.Second):
			t.Fatal("approval did not become pending")
		}
	}
	if count := broker.ResolveAll(ApprovalDecision{OptionID: "always"}); count != 2 {
		t.Fatalf("ResolveAll count = %d, want 2", count)
	}
	for index := 0; index < 2; index++ {
		select {
		case decision := <-results:
			if decision.OptionID != "always" {
				t.Fatalf("decision = %#v, want always", decision)
			}
		case <-time.After(time.Second):
			t.Fatal("pending approval was not released")
		}
	}
}

func TestResolveAllRacesSingleResolveWithoutDoubleCompletion(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		broker := NewApprovalBroker(nil)
		opened := make(chan struct{}, 1)
		broker.SetObserver(func(interaction *Interaction) {
			if interaction != nil {
				opened <- struct{}{}
			}
		})
		result := make(chan ApprovalDecision, 1)
		go func() {
			decision, err := broker.Request(context.Background(), ApprovalRequest{ID: "approval-race", Question: "continue?"})
			if err == nil {
				result <- decision
			}
		}()
		select {
		case <-opened:
		case <-time.After(time.Second):
			t.Fatal("approval did not become pending")
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var resolveErr error
		var resolveAllCount int
		go func() {
			defer wg.Done()
			<-start
			resolveErr = broker.Resolve("approval-race", ApprovalDecision{OptionID: "allow"})
		}()
		go func() {
			defer wg.Done()
			<-start
			resolveAllCount = broker.ResolveAll(ApprovalDecision{OptionID: "always"})
		}()
		close(start)
		wg.Wait()

		resolved := 0
		if resolveErr == nil {
			resolved++
		} else if !errors.Is(resolveErr, ErrInteractionNotFound) {
			t.Fatalf("Resolve error = %v", resolveErr)
		}
		resolved += resolveAllCount
		if resolved != 1 {
			t.Fatalf("completion winners = %d, want exactly 1", resolved)
		}
		select {
		case decision := <-result:
			if decision.OptionID != "allow" && decision.OptionID != "always" {
				t.Fatalf("unexpected decision %#v", decision)
			}
		case <-time.After(time.Second):
			t.Fatal("approval result was not delivered")
		}
	}
}
