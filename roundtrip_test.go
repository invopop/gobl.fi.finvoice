package fifinvoice_test

import (
	"encoding/json"
	"testing"

	fifinvoice "github.com/invopop/gobl.fi.finvoice"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
	"github.com/invopop/gobl/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoundTrip converts every example to Finvoice, parses it back, and
// checks the invoice content survived. What Finvoice cannot carry is folded
// on both sides by normalizeRoundTrip.
func TestRoundTrip(t *testing.T) {
	for src := range convertCases(t) {
		t.Run(src, func(t *testing.T) {
			env := loadEnvelope(t, src)
			want := env.Extract().(*bill.Invoice)

			doc, err := fifinvoice.Convert(env, testOptions()...)
			require.NoError(t, err)
			data, err := doc.Bytes()
			require.NoError(t, err)

			parsed, err := fifinvoice.Parse(data)
			require.NoError(t, err)
			require.NoError(t, parsed.Validate())
			got := parsed.Extract().(*bill.Invoice)

			normalizeRoundTrip(t, want)
			normalizeRoundTrip(t, got)
			assert.Equal(t, toJSON(t, want), toJSON(t, got))
		})
	}
}

// normalizeRoundTrip folds the fields Finvoice cannot carry as GOBL does:
// series and code travel as one number, a street number as part of the
// street, an advance without its description, notes without their key, a
// VAT rate as its percentage and a charge or discount as its UNTDID code.
// The sender's operator is the caller's to give rather than the document's
// to keep.
func normalizeRoundTrip(t *testing.T, inv *bill.Invoice) {
	t.Helper()
	inv.UUID = uuid.MustParse(testUUID)
	inv.Code = inv.Series.Join(inv.Code)
	inv.Series = ""
	for _, ref := range inv.Preceding {
		ref.Code = ref.Series.Join(ref.Code)
		ref.Series = ""
	}
	for _, p := range []*org.Party{inv.Supplier, inv.Customer} {
		for _, a := range p.Addresses {
			if a.Number != "" {
				a.Street += " " + a.Number
				a.Number = ""
			}
		}
	}
	inv.Supplier.Ext = inv.Supplier.Ext.Delete(finvoice.ExtKeyOperator)
	if inv.Payment != nil {
		for _, adv := range inv.Payment.Advances {
			adv.Description = ""
		}
	}
	for _, n := range inv.Notes {
		n.Key = org.NoteKeyGeneral
	}
	for _, line := range inv.Lines {
		clearRates(line.Taxes)
		for _, d := range line.Discounts {
			d.Key = ""
		}
		for _, c := range line.Charges {
			c.Key = ""
		}
	}
	for _, d := range inv.Discounts {
		d.Key = ""
		clearRates(d.Taxes)
	}
	for _, c := range inv.Charges {
		c.Key = ""
		clearRates(c.Taxes)
	}
	// Finvoice totals add up the rounded row amounts; recalculate both sides
	// the same way before comparing.
	inv.Tax.Rounding = ""
	require.NoError(t, inv.Calculate())
}

func clearRates(set tax.Set) {
	for _, combo := range set {
		combo.Rate = ""
	}
}

func toJSON(t *testing.T, inv *bill.Invoice) string {
	t.Helper()
	data, err := json.MarshalIndent(inv, "", "  ")
	require.NoError(t, err)
	return string(data)
}
