package mib

import (
	"strings"
	"testing"
)

// An instance of ifStackStatus — IF-MIB's RowStatus — is known as one, with its
// column; an instance of another column, the column itself and an OID no MIB
// describes are not.
func TestARowStatusColumnIsFoundInTheMIB(t *testing.T) {
	s := loadedService(t)
	col, ok := s.RowStatusColumn(".1.3.6.1.2.1.31.1.2.1.3.5.6")
	if !ok || strings.TrimPrefix(col, ".") != "1.3.6.1.2.1.31.1.2.1.3" {
		t.Errorf("ifStackStatus.5.6: %q, %v", col, ok)
	}
	for _, not := range []string{"1.3.6.1.2.1.2.2.1.2.1", "1.3.6.1.2.1.31.1.2.1.3", "1.3.6.1.4.1.32473.1.2.3"} {
		if col, ok := s.RowStatusColumn(not); ok {
			t.Errorf("%s was taken for an instance of the RowStatus %s", not, col)
		}
	}
}
