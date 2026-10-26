package session

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// CatalogRefresher requests one catalog refresh and returns its completion signal.
type CatalogRefresher interface {
	RequestCatalogRefresh() <-chan struct{}
}

// EvictionSnapshot is one consistent residency-LRU reading.
type EvictionSnapshot struct {
	Order       []string
	Protect     string
	Occupied    map[string]bool
	Protected   map[string]bool
	LegacyLimit int
	Window      int
}

// EvictionDeps supplies one LRU reconciliation pass.
type EvictionDeps struct {
	Snapshot     func() EvictionSnapshot
	Evict        func(sessionID string) error
	OnEvictError func(sessionID string, err error)
}

// ArchiveGate is the application-owned archive admission reading.
type ArchiveGate struct {
	Closed      bool
	Draining    bool
	Busy        bool
	Restoring   bool
	ErrClosed   error
	ErrDraining error
	ErrBusy     error
}

// ArchiveStore is the archive persistence face.
type ArchiveStore struct {
	Load           func(sessionID string) (model.SessionRecord, bool, error)
	Flush          func(sessionID string) error
	MarkArchived   func(sessionID string) error
	RefreshProject func(sessionID string)
}

// ArchiveEngine is the archive engine face.
type ArchiveEngine struct {
	Resident       func(sessionID string) bool
	Loaded         func(sessionID string) bool
	Unload         func(sessionID string) error
	ForgetResident func(sessionID string)
}

// ArchiveUseCaseDeps supplies one archive use case.
type ArchiveUseCaseDeps struct {
	Gate         func() ArchiveGate
	Store        ArchiveStore
	Engine       ArchiveEngine
	ReleaseAsync func(sessionID string)
}

// ResidencyPort is the consumer port for Middle residency decisions.
//
// Implementations own ordering, input-index projection, eviction and archive
// ordering. Application supplies state readings and concrete side-effect
// callbacks; the adapter only forwards and maps value shapes.
type ResidencyPort interface {
	Preview(content string, limit int) (string, bool)
	Touch(order []string, sessionID string) []string
	Forget(order []string, sessionID string) []string
	Prune(order []string, loaded func(string) bool, unitIDs []string) []string
	Budget(legacyLimit, protectedCount, window int) int
	InputIndexSummaryLimit() int
	BuildInputIndex(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow
	WaitCatalogRefresh(ctx context.Context, refresher CatalogRefresher) error
	Evict(deps EvictionDeps)
	Archive(deps ArchiveUseCaseDeps, sessionID string) error
}

// ErrArchiveNotFound preserves the archive error identity across layers.
var ErrArchiveNotFound = contract.ErrSessionNotFoundArchive

// ErrResidencyUnavailable reports an unassembled residency port.
var ErrResidencyUnavailable = errors.New("residency port is not assembled")
