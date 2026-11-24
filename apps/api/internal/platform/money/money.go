package money

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const MaxCents int64 = 999999999999999999

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$`)

// Parse accepts nonnegative decimal text and returns exact minor units.
func Parse(amount string) (int64, bool) {
	if !decimal.MatchString(amount) {
		return 0, false
	}
	whole, fractional, _ := strings.Cut(amount, ".")
	cents, err := strconv.ParseInt(whole+(fractional + "00")[:2], 10, 64)
	return cents, err == nil
}

func Format(cents int64) string {
	if cents < 0 {
		return fmt.Sprintf("-%d.%02d", -(cents / 100), -(cents % 100))
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
