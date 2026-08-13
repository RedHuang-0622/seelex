package plan

import (
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// PlanBranchEvent is the Seelex-owned representation of a branch lifecycle
// event. It intentionally contains no Seele runtime types.
type PlanBranchEvent struct {
	Type     string
	BranchID string
	NodeID   string
	Error    string
	At       time.Time
}

// PlanBranchBinding freezes the request-scoped values used to construct
// branch runtimes. Empty AccountID delegates selection to the role router.
// PlanBranchBinding freezes the request-scoped values used to construct
// a plan branch???????? application/contract/dto??
type PlanBranchBinding = dto.PlanBranchBinding
