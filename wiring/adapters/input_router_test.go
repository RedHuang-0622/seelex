package adapters

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
)

type inputRouterTestCommand struct {
	name string
}

func (command inputRouterTestCommand) Name() string        { return command.name }
func (command inputRouterTestCommand) Description() string { return "test command" }
func (command inputRouterTestCommand) Execute(context.Context, []string) (contract.CommandResult, error) {
	return contract.CommandResult{}, nil
}

func TestInputRouterFactoriesForwardRegistryAndPrefixDispatch(t *testing.T) {
	registry := NewCommandRegistryFactory()()
	if err := registry.Register(inputRouterTestCommand{name: "status"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("STATUS"); !ok {
		t.Fatal("registry did not preserve backend case-insensitive lookup")
	}

	var got string
	dispatcher := NewInputRouterFactory()(contract.InputRouteHandlers{
		Command: func(_ context.Context, input string) error {
			got = input
			return nil
		},
	})
	if err := dispatcher.Dispatch(context.Background(), "/status"); err != nil {
		t.Fatal(err)
	}
	if got != "/status" {
		t.Fatalf("command route got %q, want /status", got)
	}
}
