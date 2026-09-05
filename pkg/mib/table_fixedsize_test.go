package mib

import (
	"math"
	"testing"

	"github.com/sleepinggenius2/gosmi/models"
)

// A declared SIZE is a number out of a MIB, and a MIB is a file the user drops
// in. `models.Range.MinValue` is an int64 while the size this returns is an
// `int`, which is 32 bits on a 32-bit build: without the upper bound, a SIZE
// above 2^31 comes back NEGATIVE and reaches make([]byte, n).
//
// The last case is the one that mattered, and it fails without the bound on a
// 32-bit build only — hence the explicit MaxInt32 rather than MaxInt, so the
// same file is refused everywhere rather than on one platform.
func TestFixedSizeRefusesWhatItCannotHold(t *testing.T) {
	size := func(min, max int64) int {
		return fixedSize(&models.Type{Ranges: []models.Range{{MinValue: min, MaxValue: max}}})
	}

	if got := size(4, 4); got != 4 {
		t.Errorf("a fixed SIZE (4) should be 4, got %d", got)
	}
	if got := size(1, 255); got != -1 {
		t.Errorf("a RANGE is not a fixed size, got %d", got)
	}
	if got := size(-1, -1); got != -1 {
		t.Errorf("a negative size is not a size, got %d", got)
	}
	if got := size(math.MaxInt32+1, math.MaxInt32+1); got != -1 {
		t.Errorf("a size that does not fit an int32 must be refused, got %d", got)
	}
	if got := size(math.MaxInt64, math.MaxInt64); got != -1 {
		t.Errorf("MaxInt64 must be refused, got %d", got)
	}
}
