// Package session contains application-level session use cases that do not
// own chat execution. The package depends on consumer-defined deps only.
package session

import (
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

// MetaDeps supplies session display metadata read/write capabilities.
type MetaDeps struct {
	Meta     func() switching.MetaDeps
	ReadMeta func(sessionID string) (model.SessionMeta, error)
}

// MetaApplication owns the application boundary for session display metadata.
// Persistence and validation remain in middle/switch and SessionQuery.
type MetaApplication struct {
	deps MetaDeps
}

// NewMetaApplication constructs the metadata application.
func NewMetaApplication(deps MetaDeps) *MetaApplication {
	return &MetaApplication{deps: deps}
}

// SetSessionMeta writes display metadata and wakes the catalog through MetaDeps.
func (application *MetaApplication) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	return switching.SetMeta(application.deps.Meta(), sessionID, meta)
}

// SessionMeta reads display metadata through the injected read port.
func (application *MetaApplication) SessionMeta(sessionID string) (model.SessionMeta, error) {
	if application == nil {
		return switching.ReadMeta(switching.MetaDeps{}, sessionID)
	}
	return application.deps.ReadMeta(sessionID)
}
