package policy

import "github.com/RedHuang-0622/seelex/kernel/limit"

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

// ApplySessionReleaseWindow validates first, then changes the process limit.
func ApplySessionReleaseWindow(window int) (int, error) {
	if err := limit.ValidateSessionReleaseWindow(window); err != nil {
		return 0, err
	}
	limit.SetSessionReleaseWindow(window)
	return limit.Get().ReleaseWindow(), nil
}

func NewKnobs(host KnobsHost) *Knobs {
	return limit.NewKnobs(host)
}
