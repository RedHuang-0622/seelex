//go:build !race

package testutil

import "time"

// Budget 见 budget_race.go：非 race 构建下原样返回，普通测试的数字不变。
func Budget(d time.Duration) time.Duration { return d }
