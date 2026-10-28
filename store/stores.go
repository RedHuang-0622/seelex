package store

import (
	"context"
	"fmt"
	"sync"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storerecord "github.com/RedHuang-0622/seelex/store/record"
	storerole "github.com/RedHuang-0622/seelex/store/role"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// Stores is the composition-root value object. Every field is a concrete,
// already-scoped owner; consumers receive the narrow field they use instead of
// discovering capabilities from Router at runtime.
type Stores struct {
	Router      *Router
	Artifacts   *storeartifact.Store
	Context     *storecontext.Store
	Boards      *storeboard.SessionStore
	Teamwork    *storeteamwork.Store
	Roles       *storerole.Store
	ToolRecords *storerecord.Store
}

// OpenStores opens the single JSON data root once and returns the shared owner
// bundle. Opening twice against the same root is rejected by the data-root lock.
func OpenStores(defaultPath string, limits ...storeroot.Settings) (*Stores, error) {
	config := Config{SessionStorage: storeroot.ResolveSettings(limits...)}
	config, err := config.Normalize(defaultPath)
	if err != nil {
		return nil, err
	}
	repository, err := Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	jsonRepository, ok := repository.(*jsonRepository)
	if !ok {
		_ = repository.Close()
		return nil, fmt.Errorf("session storage: unexpected repository implementation %T", repository)
	}
	router := &Router{repository: repository}
	router.opsCond = sync.NewCond(&router.opsMu)
	layout := jsonRepository.layout
	return &Stores{
		Router:      router,
		Artifacts:   layout.artifacts,
		Context:     layout.contexts,
		Boards:      layout.boards,
		Teamwork:    layout.teamwork,
		Roles:       layout.roles,
		ToolRecords: layout.records,
	}, nil
}

func (stores *Stores) Close() error {
	if stores == nil || stores.Router == nil {
		return nil
	}
	return stores.Router.Close()
}
