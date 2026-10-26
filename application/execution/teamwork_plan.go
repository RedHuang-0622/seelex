package execution

import "github.com/RedHuang-0622/seelex/store"

// TeamworkPlan and TeamworkState are transitional identity aliases for the
// project-board read application. Store remains the sole owner of the plan
// state machine and persistence payload until the plan is fully DTO-shaped.
type TeamworkPlan = store.TeamworkPlan
type TeamworkState = store.TeamworkState
