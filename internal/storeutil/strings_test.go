package storeutil

import "testing"

func TestUniqueNonEmptyStrings(t *testing.T) {
	values := []string{"  alpha ", "", "beta", "alpha", " beta "}
	got := UniqueNonEmptyStrings(values)
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got=%d want=%d values=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected value at %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}
