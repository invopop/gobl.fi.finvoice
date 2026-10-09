package finvoice_test

import (
	"testing"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertRows checks prices and row discounts.
func TestConvertRows(t *testing.T) {
	t.Run("a price finer than five decimals is given per a base quantity", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Lines[0].Quantity = num.MakeAmount(1000000, 0)
		inv.Lines[0].Item.Price = num.NewAmount(12345, 7)
		inv.Lines[0].Discounts = nil
		doc := convertAdjusted(t, env)
		row := doc.Rows[0]
		assert.Equal(t, "0,12345", row.UnitPriceAmount.Value)
		require.NotNil(t, row.UnitPriceBaseQuantity)
		assert.Equal(t, "100", row.UnitPriceBaseQuantity.Value)
		assert.Equal(t, "1234,50", row.VatExcludedAmount.Value)
	})
	t.Run("several row discounts are all progressive", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		five := num.MakePercentage(5, 2)
		inv.Lines[0].Discounts = []*bill.LineDiscount{
			{Percent: &five, Reason: "Määräalennus"},
			{Amount: num.MakeAmount(1000, 2), Reason: "Kampanja"},
		}
		doc := convertAdjusted(t, env)
		row := doc.Rows[0]
		assert.Empty(t, row.DiscountPercent)
		assert.Nil(t, row.DiscountAmount)
		require.Len(t, row.ProgressiveDiscount, 2)
		assert.Equal(t, "Kampanja", row.ProgressiveDiscount[1].TypeText)
	})
}
