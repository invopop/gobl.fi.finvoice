package fifinvoice

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
)

// Finvoice writes dates as CCYYMMDD and decimals with a comma.
const (
	dateFormat  = "CCYYMMDD"
	dateLayout  = "20060102"
	decimalMark = ","
	// minTextLength is the shortest text most Finvoice string elements take,
	// which the addon enforces on the names it requires.
	minTextLength = 2

	// amountMinExp and amountMaxExp bound the decimals of a Finvoice amount:
	// two to five, per the monetaryAmount pattern.
	amountMinExp uint32 = 2
	amountMaxExp uint32 = 5
	// percentMinExp and percentMaxExp bound the decimals of a percentage:
	// one to three, which num.Percentage renders from exponents three to five.
	percentMinExp uint32 = 3
	percentMaxExp uint32 = 5
)

// Amount is a monetary value with its currency.
type Amount struct {
	Value    string `xml:",chardata"`
	Currency string `xml:"AmountCurrencyIdentifier,attr"`
}

// UnitAmount is a unit price with its currency and optional unit.
type UnitAmount struct {
	Value      string `xml:",chardata"`
	Currency   string `xml:"AmountCurrencyIdentifier,attr"`
	UnitCode   string `xml:"UnitPriceUnitCode,attr,omitempty"`
	UnitCodeUN string `xml:"QuantityUnitCodeUN,attr,omitempty"`
}

// Quantity is a number of units, with the unit as free text and, when
// known, as its UN/ECE code.
type Quantity struct {
	Value      string `xml:",chardata"`
	UnitCode   string `xml:"QuantityUnitCode,attr,omitempty"`
	UnitCodeUN string `xml:"QuantityUnitCodeUN,attr,omitempty"`
}

// Date is a calendar date in CCYYMMDD form.
type Date struct {
	Value  string `xml:",chardata"`
	Format string `xml:"Format,attr"`
}

// Identifier is a party or address identifier with an optional ISO 6523
// scheme.
type Identifier struct {
	Value    string `xml:",chardata"`
	SchemeID string `xml:"SchemeID,attr,omitempty"`
}

// Account is a bank account or bank identifier with its scheme (IBAN, BBAN,
// BIC, SPY, ISO).
type Account struct {
	Value  string `xml:",chardata"`
	Scheme string `xml:"IdentificationSchemeName,attr,omitempty"`
}

func newAmount(a num.Amount, cur currency.Code) *Amount {
	return &Amount{Value: formatAmount(a), Currency: cur.String()}
}

func formatAmount(a num.Amount) string {
	a = a.RescaleRange(amountMinExp, amountMaxExp)
	return strings.Replace(a.String(), ".", decimalMark, 1)
}

// formatEpiAmount renders with exactly two decimals, as the payment order
// requires.
func formatEpiAmount(a num.Amount) string {
	return strings.Replace(a.Rescale(amountMinExp).String(), ".", decimalMark, 1)
}

func formatQuantity(a num.Amount) string {
	return strings.Replace(a.String(), ".", decimalMark, 1)
}

func formatPercent(p num.Percentage) string {
	if p.Exp() < percentMinExp {
		p = p.Rescale(percentMinExp)
	}
	if p.Exp() > percentMaxExp {
		p = p.Rescale(percentMaxExp)
	}
	return strings.Replace(p.StringWithoutSymbol(), ".", decimalMark, 1)
}

func newDate(d cal.Date) *Date {
	return &Date{Value: formatDate(d), Format: dateFormat}
}

func newDatePtr(d *cal.Date) *Date {
	if d == nil {
		return nil
	}
	return newDate(*d)
}

func formatDate(d cal.Date) string {
	return fmt.Sprintf("%04d%02d%02d", d.Year, d.Month, d.Day)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

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

// tooLong reports whether s exceeds the n characters an element allows.
func tooLong(s string, n int) bool {
	return utf8.RuneCountInString(s) > n
}

// tooShort reports whether s has text but not the two characters most
// Finvoice text elements require.
func tooShort(s string) bool {
	return s != "" && utf8.RuneCountInString(s) < minTextLength
}

// identifier returns s for the element named, refusing one longer than the n
// characters it takes since a cut identifier points at nothing.
func identifier(name, s string, n int) (string, error) {
	if tooLong(s, n) {
		return "", fmt.Errorf("%s %q is longer than the %d characters Finvoice allows", name, s, n)
	}
	return s, nil
}

// cut trims s to at most n characters, for the display texts Finvoice caps
// and cannot repeat.
func cut(s string, n int) string {
	if !tooLong(s, n) {
		return s
	}
	return string([]rune(s)[:n])
}

// fit is cut for an optional element, left out when s is too short for it.
func fit(s string, n int) string {
	if tooShort(s) {
		return ""
	}
	return cut(s, n)
}

// split breaks s into pieces of at most n characters at word boundaries
// where it can, keeping at most limit of them, for the texts Finvoice lets
// an element repeat for.
func split(s string, n, limit int) []string {
	var out []string
	rest := strings.TrimSpace(s)
	for rest != "" && len(out) < limit {
		if !tooLong(rest, n) {
			if !tooShort(rest) {
				out = append(out, rest)
			}
			break
		}
		r := []rune(rest)
		end := wordBoundary(r, n)
		if tooShort(strings.TrimSpace(string(r[end:]))) {
			end = wordBoundary(r[:end], end-1)
		}
		out = append(out, strings.TrimSpace(string(r[:end])))
		rest = strings.TrimSpace(string(r[end:]))
	}
	return out
}

// wordBoundary is the last space within the first n runes of r, or n when
// there is none.
func wordBoundary(r []rune, n int) int {
	head := string(r[:n+1])
	if i := strings.LastIndex(head, " "); i > 0 {
		return utf8.RuneCountInString(head[:i])
	}
	return n
}
