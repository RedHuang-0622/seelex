package policy

import "github.com/RedHuang-0622/seelex/backend/limit"

type Limits = limit.Limits
type Knobs = limit.Knobs
type KnobsHost = limit.KnobsHost

func Apply(l Limits) {
	limit.Apply(l)
}

func Get() Limits {
	return limit.Get()
}

func ValidateSessionReleaseWindow(window int) error {
	return limit.ValidateSessionReleaseWindow(window)
}

func SetSessionReleaseWindow(window int) {
	limit.SetSessionReleaseWindow(window)
}

func NewKnobs(host KnobsHost) *Knobs {
	return limit.NewKnobs(host)
}
