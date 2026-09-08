package fitnessrepo

import (
	"context"
	"errors"
)

func (r *Repository) RecordQuickReadiness(ctx context.Context, athleteID string, in CheckinWrite) (CheckinWinner, error) {
	if in.Energy == nil || *in.Energy < 1 || *in.Energy > 5 || in.LegFreshness == nil || *in.LegFreshness < 1 || *in.LegFreshness > 5 || in.SleepQuality == nil || *in.SleepQuality < 1 || *in.SleepQuality > 5 || in.PainPresent == nil {
		return CheckinWinner{}, errors.New("invalid_input: quick readiness ratings and pain_present are required")
	}
	if in.RecordedAt.IsZero() {
		return CheckinWinner{}, errors.New("invalid_input: recorded_at")
	}
	in.SchemaVersion = 2
	return r.UpsertCheckin(ctx, athleteID, in)
}
