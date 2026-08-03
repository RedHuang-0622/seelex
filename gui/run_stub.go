//go:build !gui

package gui

import "fmt"

func Available() bool { return false }

// Run reports how to enable the desktop frontend in non-GUI builds.
func Run(Application, Options) error {
	return fmt.Errorf(`GUI support is not included in this build; rebuild with -tags "gui,desktop,production"`)
}
