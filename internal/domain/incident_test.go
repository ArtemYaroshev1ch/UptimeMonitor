package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEvaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		health    Health
		success   bool
		threshold int
		want      Transition
	}{
		{
			name:      "first check succeeds",
			health:    Health{Status: StatusUnknown},
			success:   true,
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUp}},
		},
		{
			name:      "failure below the threshold keeps the status",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 0},
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUp, ConsecutiveFailures: 1}},
		},
		{
			name:      "one failure before the threshold",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 1},
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUp, ConsecutiveFailures: 2}},
		},
		{
			name:      "the failure that reaches the threshold opens an incident",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 2},
			threshold: 3,
			want: Transition{
				Next:         Health{Status: StatusDown, ConsecutiveFailures: 3, IncidentOpen: true},
				OpenIncident: true,
			},
		},
		{
			name:      "failures past the threshold do not open a second incident",
			health:    Health{Status: StatusDown, ConsecutiveFailures: 3, IncidentOpen: true},
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusDown, ConsecutiveFailures: 4, IncidentOpen: true}},
		},
		{
			name:      "threshold 1: the first failure opens an incident",
			health:    Health{Status: StatusUp},
			threshold: 1,
			want: Transition{
				Next:         Health{Status: StatusDown, ConsecutiveFailures: 1, IncidentOpen: true},
				OpenIncident: true,
			},
		},
		{
			name:      "threshold 10: nine failures are not enough",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 8},
			threshold: 10,
			want:      Transition{Next: Health{Status: StatusUp, ConsecutiveFailures: 9}},
		},
		{
			name:      "threshold 10: the tenth failure opens",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 9},
			threshold: 10,
			want: Transition{
				Next:         Health{Status: StatusDown, ConsecutiveFailures: 10, IncidentOpen: true},
				OpenIncident: true,
			},
		},
		{
			name:      "unknown monitor failing below the threshold stays unknown",
			health:    Health{Status: StatusUnknown},
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUnknown, ConsecutiveFailures: 1}},
		},
		{
			// The user lowered the threshold below the current streak.
			name:      "lowered threshold opens on the next failure",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 5},
			threshold: 2,
			want: Transition{
				Next:         Health{Status: StatusDown, ConsecutiveFailures: 6, IncidentOpen: true},
				OpenIncident: true,
			},
		},
		{
			name:      "success resolves the open incident",
			health:    Health{Status: StatusDown, ConsecutiveFailures: 7, IncidentOpen: true},
			success:   true,
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUp}, ResolveIncident: true},
		},
		{
			name:      "success after failures below the threshold resolves nothing",
			health:    Health{Status: StatusUp, ConsecutiveFailures: 2},
			success:   true,
			threshold: 3,
			want:      Transition{Next: Health{Status: StatusUp}},
		},
		{
			name:      "invalid threshold counts as 1",
			health:    Health{Status: StatusUp},
			threshold: 0,
			want: Transition{
				Next:         Health{Status: StatusDown, ConsecutiveFailures: 1, IncidentOpen: true},
				OpenIncident: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Evaluate(tt.health, tt.success, tt.threshold))
		})
	}
}

// A full outage: up, three failures (threshold 3), recovery. Exactly one
// incident opens and it is resolved by the first success.
func TestEvaluateSequence(t *testing.T) {
	t.Parallel()
	h := Health{Status: StatusUnknown}
	var opened, resolved int
	for _, success := range []bool{true, false, false, false, false, true, true} {
		tr := Evaluate(h, success, 3)
		if tr.OpenIncident {
			opened++
		}
		if tr.ResolveIncident {
			resolved++
		}
		h = tr.Next
	}

	assert.Equal(t, 1, opened)
	assert.Equal(t, 1, resolved)
	assert.Equal(t, Health{Status: StatusUp}, h)
}

func TestIncidentOpen(t *testing.T) {
	t.Parallel()
	now := time.Now()
	assert.True(t, Incident{}.Open())
	assert.False(t, Incident{ResolvedAt: &now}.Open())
}
