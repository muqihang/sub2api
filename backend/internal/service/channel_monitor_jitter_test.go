package service

import (
	"testing"
	"time"
)

func TestScheduledMonitorNextDelayNoJitterKeepsFixedInterval(t *testing.T) {
	task := &scheduledMonitor{interval: 60 * time.Second}
	for i := 0; i < 10; i++ {
		if got := task.nextDelay(); got != 60*time.Second {
			t.Fatalf("expected fixed 60s delay without jitter, got %s", got)
		}
	}
}

func TestScheduledMonitorNextDelayJitterStaysWithinBounds(t *testing.T) {
	task := &scheduledMonitor{interval: 60 * time.Second, jitter: 20 * time.Second}
	seenDifferent := false
	first := task.nextDelay()
	for i := 0; i < 200; i++ {
		got := task.nextDelay()
		if got < 40*time.Second || got > 80*time.Second {
			t.Fatalf("expected delay in [40s,80s], got %s", got)
		}
		if got != first {
			seenDifferent = true
		}
	}
	if !seenDifferent {
		t.Fatal("expected jittered delay to vary across samples")
	}
}

func TestScheduledMonitorNextDelayClampsBelowMinimum(t *testing.T) {
	task := &scheduledMonitor{interval: 20 * time.Second, jitter: 20 * time.Second}
	for i := 0; i < 200; i++ {
		if got := task.nextDelay(); got < monitorMinIntervalSeconds*time.Second {
			t.Fatalf("expected delay >= minimum interval, got %s", got)
		}
	}
}

func TestValidateJitterRejectsIntervalsThatCouldDropBelowMinimum(t *testing.T) {
	if err := validateJitter(0, 15); err != nil {
		t.Fatalf("jitter=0 at minimum interval should be valid: %v", err)
	}
	if err := validateJitter(45, 60); err != nil {
		t.Fatalf("interval-jitter equal to minimum should be valid: %v", err)
	}
	if err := validateJitter(46, 60); err != ErrChannelMonitorInvalidJitter {
		t.Fatalf("expected invalid jitter when interval-jitter < minimum, got %v", err)
	}
	if err := validateJitter(-1, 60); err != ErrChannelMonitorInvalidJitter {
		t.Fatalf("expected invalid negative jitter, got %v", err)
	}
}
