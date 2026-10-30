package intelligence

import (
	"testing"
	"time"
)

func TestUntilNextHour(t *testing.T) {
	d := untilNextHour(20, 0)
	if d <= 0 {
		t.Errorf("expected positive duration, got %v", d)
	}
	if d > 24*time.Hour {
		t.Errorf("duration should never exceed 24 hours, got %v", d)
	}
}

func TestUntilNextMonday(t *testing.T) {
	s := &Scheduler{}
	d := s.untilNextMonday()
	if d <= 0 {
		t.Errorf("expected positive duration, got %v", d)
	}
	if d > 7*24*time.Hour {
		t.Errorf("duration should never exceed 7 days, got %v", d)
	}
}
