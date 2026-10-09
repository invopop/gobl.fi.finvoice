package finvoice_test

import (
	"testing"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
)

func TestConvertTypes(t *testing.T) {
	tests := []struct {
		name               string
		adjust             func(inv *bill.Invoice)
		code, codeUN, text string
	}{
		{"standard", func(*bill.Invoice) {}, "INV01", "380", "INVOICE"},
		{"proforma", func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma }, "INV06", "325", "PRO FORMA INVOICE"},
		{"self-billed", func(inv *bill.Invoice) { inv.SetTags(tax.TagSelfBilled) }, "INV07", "389", "SELFBILLING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, "invoice")
			tt.adjust(inv)
			doc := convertAdjusted(t, env)
			assert.Equal(t, tt.code, doc.InvoiceDetails.TypeCode.Value)
			assert.Equal(t, tt.codeUN, doc.InvoiceDetails.TypeCodeUN)
			assert.Equal(t, tt.text, doc.InvoiceDetails.TypeText)
		})
	}
}

// TestConvertDetails pins the header fields the examples leave out.
func TestConvertDetails(t *testing.T) {
	t.Run("an agreement keeps its date", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Ordering = &bill.Ordering{Contracts: []*org.DocumentRef{{Code: "SOP-9", IssueDate: cal.NewDate(2026, 1, 15)}}}
		doc := convertAdjusted(t, env)
		assert.Equal(t, "20260115", doc.InvoiceDetails.AgreementDate.Value)
	})
	t.Run("percentages finer than three decimals", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		third := num.MakePercentage(333333, 6)
		base := num.MakeAmount(90000, 2)
		inv.Lines[0].Discounts = []*bill.LineDiscount{{Percent: &third, Base: &base, Reason: "Kolmannes"}}
		inv.Charges = []*bill.Charge{{Percent: &third, Reason: "Rahti", Taxes: tax.Set{{Category: tax.CategoryVAT, Rate: tax.RateGeneral}}}}
		doc := convertAdjusted(t, env)
		assert.Empty(t, doc.Rows[0].DiscountPercent)
		assert.Nil(t, doc.Rows[0].DiscountBaseAmount)
		assert.Equal(t, "300,00", doc.Rows[0].DiscountAmount.Value, "the amount stands")
		assert.Empty(t, doc.InvoiceDetails.Charges[0].Percent)
	})
}
