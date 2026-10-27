package teamwork

import "testing"

func TestBindingsFoldLastRow(t *testing.T) {
	rows := []Binding{
		{WorkItem: "a", SessionID: "s1"},
		{WorkItem: "b", SessionID: "s2"},
		{WorkItem: "a", SessionID: "s1b"},
		{WorkItem: "b", Released: true},
		{WorkItem: ""},
	}
	current := Bindings(rows)
	if len(current) != 1 || current["a"].SessionID != "s1b" {
		t.Fatalf("bindings=%+v", current)
	}
}

func TestValidatePlanRejectsGraphAndCrossMilestoneDependency(t *testing.T) {
	plan := Plan{
		TeamID: "t", Version: 1,
		Members: []Member{{Role: "exec", RoleSessionID: "s"}},
		Milestones: []Milestone{
			{ID: "m1", Items: []WorkItem{{ID: "a", Milestone: "m1", Role: "exec", Name: "a"}}},
			{ID: "m2", DependsOn: []string{"m1"}, Items: []WorkItem{{
				ID: "b", Milestone: "m2", Role: "exec", Name: "b", DependsOn: []string{"a"},
			}}},
		},
	}
	if err := ValidatePlan(plan, 6); err == nil {
		t.Fatal("cross-milestone item dependency must fail")
	}
	plan.Milestones[1].Items[0].DependsOn = nil
	plan.Milestones[0].DependsOn = []string{"m2"}
	if err := ValidatePlan(plan, 6); err == nil {
		t.Fatal("milestone cycle must fail")
	}
}
