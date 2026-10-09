package finvoice

import (
	"fmt"
	"strings"
	"time"

	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/num"
)

func parseAmount(s string) (num.Amount, error) {
	return num.AmountFromString(normalizeNumber(s))
}

// parsePercent reads a percentage, dropping the trailing zeros senders pad
// rates with ("25,50", "0,000") so the rate compares equal to its GOBL
// definition.
func parsePercent(s string) (num.Percentage, error) {
	s = normalizeNumber(s)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return num.PercentageFromString(s + "%")
}

func normalizeNumber(s string) string {
	return strings.Replace(strings.TrimSpace(s), decimalMark, ".", 1)
}

func parseDate(s string) (cal.Date, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return cal.Date{}, err
	}
	return cal.DateOf(t), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseDatePtr(d *Date) (*cal.Date, error) {
	if d == nil {
		return nil, nil
	}
	v, err := parseDate(d.Value)
	if err != nil {
		return nil, fmt.Errorf("date %q: %w", d.Value, err)
	}
	return &v, nil
}

func parsePeriod(start, end *Date) (*cal.Period, error) {
	s, err := parseDatePtr(start)
	if err != nil {
		return nil, err
	}
	e, err := parseDatePtr(end)
	if err != nil {
		return nil, err
	}
	if s == nil && e == nil {
		return nil, nil
	}
	return &cal.Period{Start: s, End: e}, nil
}
