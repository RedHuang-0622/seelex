package sessionstore

import (
	"context"
	"testing"
)

func TestTeamworkAccessorNarrowsRepository(t *testing.T) {
	repository, key := teamworkFixture(t)
	narrowed, ok := Teamwork(repository)
	if !ok {
		t.Fatal("jsonRepository must expose the teamwork read/write face")
	}
	ctx := context.Background()
	if err := narrowed.WriteTeamworkPlan(ctx, key, samplePlan(), 6); err != nil {
		t.Fatalf("WriteTeamworkPlan through the narrowed face: %v", err)
	}
	plan, err := narrowed.ReadTeamworkPlan(ctx, key)
	if err != nil || plan.TeamID != "v-model" {
		t.Fatalf("ReadTeamworkPlan through the narrowed face: %+v / %v", plan, err)
	}
	if err := narrowed.AppendTeamworkEvent(ctx, key, TeamworkEvent{Kind: TeamworkEventPlan}); err != nil {
		t.Fatalf("AppendTeamworkEvent through the narrowed face: %v", err)
	}
	events, err := narrowed.ReadTeamworkEvents(ctx, key)
	if err != nil || len(events) != 1 {
		t.Fatalf("ReadTeamworkEvents through the narrowed face: %+v / %v", events, err)
	}
}
