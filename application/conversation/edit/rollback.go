package edit

import "fmt"

// RollbackDeps supplies child deletion and parent restoration.
type RollbackDeps struct {
	Delete func(childID string) error
	Resume func(parentID string) error
}

// RollbackApplication combines rollback failures with the original cause.
type RollbackApplication struct {
	deps RollbackDeps
}

// NewRollback constructs the rollback application.
func NewRollback(deps RollbackDeps) *RollbackApplication {
	return &RollbackApplication{deps: deps}
}

// Rollback removes child and restores parent while preserving cause.
func (application *RollbackApplication) Rollback(parentID, childID string, cause error) error {
	deleteErr := application.deps.Delete(childID)
	restoreErr := application.deps.Resume(parentID)
	switch {
	case deleteErr != nil && restoreErr != nil:
		return fmt.Errorf(
			"%w（回滚 child 失败: %v；恢复父会话失败: %v）",
			cause, deleteErr, restoreErr,
		)
	case deleteErr != nil:
		return fmt.Errorf("%w（回滚 child 失败: %v）", cause, deleteErr)
	case restoreErr != nil:
		return fmt.Errorf("%w（恢复父会话失败: %v）", cause, restoreErr)
	default:
		return cause
	}
}
