package domain

import (
	"testing"
	"time"
)

func TestComputeFireIDDeterministic(t *testing.T) {
	window := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	id1 := ComputeFireID("sch_123", window)
	id2 := ComputeFireID("sch_123", window)
	if id1 != id2 {
		t.Fatalf("fire id is not deterministic")
	}
	if id1 == ComputeFireID("sch_123", window.Add(time.Minute)) {
		t.Fatalf("fire id should change for different window")
	}
}

func TestNextCronFireUsesTimezone(t *testing.T) {
	after := time.Date(2026, 3, 10, 8, 59, 59, 0, time.UTC)
	next, err := NextCronFire("0 10 * * *", "UTC", after)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("unexpected next fire: got=%s want=%s", next, want)
	}
}
