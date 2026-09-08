package service

import (
	"math"
	"testing"
)

func TestMasqueTrafficValueSaturates(t *testing.T) {
	if got := masqueTrafficValue(42); got != 42 {
		t.Fatalf("unexpected regular traffic value: %d", got)
	}
	if got := masqueTrafficValue(math.MaxUint64); got != math.MaxInt64 {
		t.Fatalf("overflowing traffic was not saturated: %d", got)
	}
}
