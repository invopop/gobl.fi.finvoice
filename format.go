package fifinvoice

import (
	"fmt"
	"regexp"
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

	// amountMinExp and amountMaxExp bound the decimals of a Finvoice amount:
	// two to five, per the monetaryAmount pattern.
	amountMinExp uint32 = 2
	amountMaxExp uint32 = 5
	// percentMaxExp is the largest exponent a Finvoice percentage can carry:
	// a num.Percentage of exponent 5 renders with three decimals.
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

// cut trims s to at most n runes, for the elements Finvoice caps.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// chunks splits s into pieces of at most n runes each, keeping at most max
// of them.
func chunks(s string, n, max int) []string {
	var out []string
	r := []rune(s)
	for len(r) > 0 && len(out) < max {
		end := min(n, len(r))
		out = append(out, string(r[:end]))
		r = r[end:]
	}
	return out
}

var (
	// referenceSPY is a Finnish bank reference number (viitenumero).
	referenceSPY = regexp.MustCompile(`^[0-9]{2,20}$`)
	// referenceISO is an ISO 11649 creditor reference.
	referenceISO = regexp.MustCompile(`^RF[0-9]{2}[0-9A-Za-z]{1,21}$`)
)
