package contract

import "testing"

func TestRetainFloorTokens(t *testing.T) {
	tests := []struct {
		name    string
		percent int
		budget  int
		want    int
	}{
		{name: "disabled percent", percent: 0, budget: 1000, want: 0},
		{name: "disabled budget", percent: 25, budget: 0, want: 0},
		{name: "percentage", percent: 25, budget: 1000, want: 250},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RetainFloorTokens(test.percent, test.budget); got != test.want {
				t.Fatalf("RetainFloorTokens(%d, %d) = %d, want %d",
					test.percent, test.budget, got, test.want)
			}
		})
	}
}
