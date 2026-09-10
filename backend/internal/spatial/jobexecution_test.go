package spatial

import (
	"testing"
	"time"
)

func TestJobExecutionStatus_IsValid(t *testing.T) {
	valid := []JobExecutionStatus{
		JobExecutionPending, JobExecutionProcessing, JobExecutionCompleted,
		JobExecutionFailed, JobExecutionNeedsAttention, JobExecutionCancelled,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if JobExecutionStatus("bogus").IsValid() {
		t.Error("expected an unrecognized status to be invalid")
	}
}

func TestJobExecution_LeaseState(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	cases := []struct {
		name   string
		exec   JobExecution
		expect bool // leaseIsHeld(now)
	}{
		{"no lease ever taken", JobExecution{}, false},
		{"lease expired", JobExecution{LeaseOwner: "w1", LeaseExpiresAt: &past}, false},
		{"lease active", JobExecution{LeaseOwner: "w1", LeaseExpiresAt: &future}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.exec.leaseIsHeld(now); got != c.expect {
				t.Errorf("leaseIsHeld() = %v, want %v", got, c.expect)
			}
		})
	}
}
