package fifinvoice_test

import (
	"encoding/json"
	"testing"

	fifinvoice "github.com/invopop/gobl.fi.finvoice"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/tax"
	"github.com/invopop/gobl/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTrip converts an example, parses the result back, and checks the
// invoice content survived. The original is loaded separately since Convert
// works on its envelope in place.
func roundTrip(t *testing.T, example string, adjust func(inv *bill.Invoice)) {
	t.Helper()
	env, inv := exampleEnvelope(t, example)
	adjust(inv)
	require.NoError(t, env.Calculate())
	_, want := exampleEnvelope(t, example)
	adjust(want)
	require.NoError(t, want.Calculate())

	doc, err := fifinvoice.Convert(env, testOptions()...)
	require.NoError(t, err)
	data, err := doc.Bytes()
	require.NoError(t, err)
	parsed, err := fifinvoice.Parse(data)
	require.NoError(t, err)
	require.NoError(t, parsed.Validate())
	got := parsed.Extract().(*bill.Invoice)

	// Finvoice prices exclude VAT and its amounts fit the currency.
	require.NoError(t, want.RemoveIncludedTaxes())
	require.NoError(t, want.RoundToCurrency())
	normalizeRoundTrip(t, want)
	normalizeRoundTrip(t, got)

	assert.Equal(t, want.Totals.Payable.String(), got.Totals.Payable.String(), "payable")
	assert.Equal(t, want.Totals.Total.String(), got.Totals.Total.String(), "total before VAT")
	want.Totals, got.Totals = nil, nil
	assert.Equal(t, toJSON(t, want), toJSON(t, got))
}

// TestRoundTrip covers every example as shipped.
func TestRoundTrip(t *testing.T) {
	for _, tc := range convertCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			roundTrip(t, tc.name, func(*bill.Invoice) {})
		})
	}
}

// TestRoundTripCases covers the branches the examples leave out.
func TestRoundTripCases(t *testing.T) {
	tests := []struct {
		name, example string
		adjust        func(inv *bill.Invoice)
	}{
		{"further preceding documents", "credit-note", func(inv *bill.Invoice) {
			inv.Preceding = append(inv.Preceding, &org.DocumentRef{Code: "1000", IssueDate: cal.NewDate(2026, 8, 1)})
		}},
		{"every ordering reference", "invoice", func(inv *bill.Invoice) {
			inv.Ordering.Sales = []*org.DocumentRef{{Code: "SO-1"}}
			inv.Ordering.Contracts = []*org.DocumentRef{{Code: "SOP-9"}}
			inv.Ordering.Projects = []*org.DocumentRef{{Code: "PRJ-3"}}
			inv.Ordering.Tender = []*org.DocumentRef{{Code: "TND-5"}}
		}},
		{"line period", "invoice", func(inv *bill.Invoice) {
			inv.Lines[0].Period = &cal.Period{Start: cal.NewDate(2026, 8, 1), End: cal.NewDate(2026, 8, 31)}
		}},
		{"delivery period", "reverse-charge", func(inv *bill.Invoice) {
			inv.Delivery.Period = &cal.Period{Start: cal.NewDate(2026, 8, 1), End: cal.NewDate(2026, 8, 31)}
		}},
		{"reference that is neither SPY nor RF", "invoice", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "LASKU1001" }},
		{"unit GOBL has no key for", "invoice", func(inv *bill.Invoice) {
			inv.Lines[1].Item.Unit = ""
			inv.Lines[1].Item.Ext = inv.Lines[1].Item.Ext.Set(untdid.ExtKeyUnit, "ZP")
		}},
		{"self-billed", "invoice", func(inv *bill.Invoice) { inv.SetTags(tax.TagSelfBilled) }},
		{"proforma", "invoice", func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma }},
		{"Swedish krona", "reverse-charge", func(inv *bill.Invoice) { inv.Currency = "SEK" }},
		{"credit note with discounts, charges and an advance", "credit-note", func(inv *bill.Invoice) {
			inv.Lines[0].Discounts = []*bill.LineDiscount{{Amount: num.MakeAmount(500, 2), Reason: "Alennus"}}
			inv.Lines[0].Charges = []*bill.LineCharge{{Amount: num.MakeAmount(500, 2), Reason: "Käsittely"}}
			inv.Charges = []*bill.Charge{{Amount: num.MakeAmount(400, 2), Reason: "Rahti",
				Taxes: tax.Set{{Category: tax.CategoryVAT, Rate: tax.RateGeneral}}}}
			inv.Payment.Advances = []*pay.Record{{Amount: num.MakeAmount(1000, 2), Description: "Maksettu"}}
		}},
		{"payable rounded", "invoice", func(inv *bill.Invoice) { inv.Totals = &bill.Totals{Rounding: num.NewAmount(-36, 2)} }},
		{"payment terms longer than one line", "invoice", func(inv *bill.Invoice) {
			inv.Payment.Terms.Notes = "Maksuehto 14 päivää netto, viivästyskorko korkolain mukaan ja perintäkulut peritään erikseen"
		}},
		{"exemption note longer than one line", "reverse-charge", func(inv *bill.Invoice) {
			inv.Tax.Notes[0].Text = "Käännetty verovelvollisuus, arvonlisäverolain 8 c §, ostaja on verovelvollinen rakentamispalvelun ostajana"
		}},
		{"prices include VAT", "invoice", func(inv *bill.Invoice) { inv.Tax.PricesInclude = tax.CategoryVAT }},
		{"VAT that rounds differently per rate", "invoice", func(inv *bill.Invoice) {
			inv.Lines[1].Item.Price = num.NewAmount(2350, 2)
			inv.Charges[0].Amount = num.MakeAmount(300, 2)
		}},
		{"a payee", "invoice", func(inv *bill.Invoice) { inv.Payment.Payee = &org.Party{Name: "Perintä Oy"} }},
		{"a second account", "invoice", func(inv *bill.Invoice) {
			inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
				&pay.CreditTransfer{IBAN: "FI4250001510000023", BIC: "OKOYFIHH"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.example, tt.adjust)
		})
	}
}

// normalizeRoundTrip folds the fields Finvoice cannot carry as GOBL does:
// series and code travel as one number, a street number as part of the
// street, an advance without its description, notes without their key, a
// VAT rate as its percentage and a charge or discount as its UNTDID code.
// The sender's operator is the caller's to give rather than the document's
// to keep. Each side keeps its own rounding rule, so the totals are compared
// on their own.
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
	for _, adv := range inv.Payment.Advances {
		adv.Description = ""
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
	require.NoError(t, inv.Calculate())
	inv.Tax.Rounding = ""
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
