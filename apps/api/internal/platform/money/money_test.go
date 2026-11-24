package money

import (
	"math"
	"testing"
)

func TestExactAmounts(t *testing.T) {
	for input, canonical := range map[string]string{
		"0": "0.00", "0.01": "0.01", "15.5": "15.50", "9999999999999999.99": "9999999999999999.99",
	} {
		cents, valid := Parse(input)
		if !valid || Format(cents) != canonical {
			t.Fatalf("%q: %d %v", input, cents, valid)
		}
	}
	for _, input := range []string{"", "-1", "01.00", "1.001", "1e2", "10000000000000000.00"} {
		if _, valid := Parse(input); valid {
			t.Fatalf("se aceptó %q", input)
		}
	}
	if Format(-1) != "-0.01" || Format(math.MinInt64) != "-92233720368547758.08" {
		t.Fatal("falló el formato de diferencias negativas")
	}
}
