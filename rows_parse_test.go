package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRows(t *testing.T) {
	t.Run("rows and text rows", func(t *testing.T) {
		rows := defaultRows + `
<InvoiceRow><RowFreeText>Välisumma</RowFreeText></InvoiceRow>
<InvoiceRow><ArticleName>Otsikkorivi</ArticleName></InvoiceRow>
<InvoiceRow><ArticleName>Rahti</ArticleName><InvoicedQuantity QuantityUnitCodeUN="ZP">2</InvoicedQuantity><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">15,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">15,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatExemptionReasonCode>VATEX-EU-132-1C</VatExemptionReasonCode></VatSpecificationDetails>`
		inv := parseMessage(t, message{rows: rows, vat: vat, total: "140,50"})

		require.Len(t, inv.Lines, 2, "text rows are not lines")
		assert.Equal(t, []string{"Välisumma", "Otsikkorivi"}, []string{inv.Notes[0].Text, inv.Notes[1].Text})
		line := inv.Lines[1]
		assert.Equal(t, "7.50", line.Item.Price.String(), "price from the row total")
		assert.Equal(t, cbc.Code("ZP"), line.Item.Ext.Get(untdid.ExtKeyUnit))
		assert.Equal(t, tax.KeyExempt, line.Taxes[0].Key, "category from the breakdown")
		assert.Equal(t, cbc.Code("VATEX-EU-132-1C"), line.Taxes[0].Ext.Get(cef.ExtKeyVATEX))
		assert.Equal(t, "14 pv netto", inv.Payment.Terms.Notes)
	})
	t.Run("price per base quantity", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Sähkö</ArticleName><InvoicedQuantity QuantityUnitCode="kWh">2000</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><UnitPriceBaseQuantity>1000</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "0.05", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "100.00", inv.Lines[0].Total.String())
		assert.Nil(t, inv.Totals.Rounding)
	})
	t.Run("net price over gross", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">60,00</UnitPriceAmount><UnitPriceNetAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceNetAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
	})
	t.Run("row total wins over the price", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">55,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "125.50", inv.Totals.Payable.String())
	})
	t.Run("an unknown UN unit code falls back to the unit word", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Työ</ArticleName><InvoicedQuantity QuantityUnitCode="h" QuantityUnitCodeUN="ZP">2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "125,50"})
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
		assert.Empty(t, inv.Lines[0].Item.Ext.Get(untdid.ExtKeyUnit))
	})
	t.Run("a subtotal row made of sub-rows is left out, as the totals leave it out", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><SubInvoiceRow><SubArticleName>Yhteensä</SubArticleName><SubRowAmount AmountCurrencyIdentifier="EUR">100,00</SubRowAmount></SubInvoiceRow></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		require.Len(t, inv.Lines, 1)
		assert.Empty(t, inv.Notes)
	})
	t.Run("a quantity without a unit takes the price's", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Työ</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" QuantityUnitCodeUN="HUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "125,50"})
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)

		rows = `<InvoiceRow><ArticleName>Työ</ArticleName><UnitPriceAmount AmountCurrencyIdentifier="EUR" UnitPriceUnitCode="h">100,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv = parseMessage(t, message{rows: rows, total: "125,50"})
		assert.Equal(t, "1", inv.Lines[0].Quantity.String())
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
	})
	t.Run("several quantities take the one in the price's unit word", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Hiekka</ArticleName><InvoicedQuantity QuantityUnitCode="h">2</InvoicedQuantity><InvoicedQuantity QuantityUnitCode="kg">10</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" UnitPriceUnitCode="kg">5,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">50,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "62,75"})
		assert.Equal(t, "10", inv.Lines[0].Quantity.String())
		assert.Equal(t, "5.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, org.UnitKilogram, inv.Lines[0].Item.Unit)
	})
	t.Run("several quantities take the one in the price's unit", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Kynä</ArticleName><InvoicedQuantity QuantityUnitCode="pkt" QuantityUnitCodeUN="XPK">1</InvoicedQuantity><InvoicedQuantity QuantityUnitCode="kpl" QuantityUnitCodeUN="C62">10</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" QuantityUnitCodeUN="C62">5,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">50,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "62,75"})
		assert.Equal(t, "10", inv.Lines[0].Quantity.String())
		assert.Equal(t, "5.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, org.UnitOne, inv.Lines[0].Item.Unit)
	})
	t.Run("a free item row has a price of zero", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Kaupan päälle</ArticleName><InvoicedQuantity>2</InvoicedQuantity><RowDiscountPercent>100</RowDiscountPercent><RowDiscountTypeText>Lahja</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "0.00", inv.Lines[1].Item.Price.String())
	})
	t.Run("a row without a name takes its reference or its position", func(t *testing.T) {
		unnamed := strings.Replace(defaultRows, "<ArticleName>Konsultointi</ArticleName>", "", 1)
		referenced := strings.Replace(defaultRows, "<ArticleName>Konsultointi</ArticleName>", "<ArticleIdentifier>K-1</ArticleIdentifier>", 1)
		inv := parseMessage(t, message{rows: unnamed + referenced, total: "251,00"})
		assert.Equal(t, "Row 1", inv.Lines[0].Item.Name)
		assert.Equal(t, "K-1", inv.Lines[1].Item.Name)
	})
	t.Run("a stated amount wins over a percentage that cannot give it", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Laite</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3000,00</UnitPriceAmount><RowDiscountPercent>3,333</RowDiscountPercent><RowDiscountAmount AmountCurrencyIdentifier="EUR">100,00</RowDiscountAmount><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">2900,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "3639,50"})
		line := inv.Lines[0]
		assert.Equal(t, "3000.00", line.Item.Price.String())
		assert.Nil(t, line.Discounts[0].Percent)
		assert.Equal(t, "100.00", line.Discounts[0].Amount.String())
	})
	t.Run("a percentage that gives the stated amount is kept", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Laite</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3000,00</UnitPriceAmount><RowDiscountPercent>10</RowDiscountPercent><RowDiscountAmount AmountCurrencyIdentifier="EUR">300,00</RowDiscountAmount><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">2700,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "3388,50"})
		require.NotNil(t, inv.Lines[0].Discounts[0].Percent)
		assert.Equal(t, "10%", inv.Lines[0].Discounts[0].Percent.String())
	})
	t.Run("a price is checked the way GOBL rounds the line", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Osa</ArticleName><InvoicedQuantity>3</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3,335</UnitPriceAmount><RowDiscountPercent>10</RowDiscountPercent><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">9,01</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "11,31"})
		assert.Equal(t, "3.335", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "9.01", inv.Lines[0].Total.String())
	})
	t.Run("a row with a quantity and nothing to price is kept as a note", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Sisältää suodattimen</ArticleName><DeliveredQuantity>1</DeliveredQuantity></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		require.Len(t, inv.Lines, 1)
		assert.Equal(t, "Sisältää suodattimen", inv.Notes[0].Text)
	})
	t.Run("a lump sum keeps the currency's decimals", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Asennus</ArticleName><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "100.00", inv.Lines[0].Item.Price.String())
	})
	t.Run("a description alone is kept as a note", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleDescription>Toimitus sisältää asennuksen</ArticleDescription></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		require.Len(t, inv.Notes, 1)
		assert.Equal(t, "Toimitus sisältää asennuksen", inv.Notes[0].Text)
	})
	t.Run("a price finer than four decimals comes from the row total", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>API-kutsut</ArticleName><InvoicedQuantity>100000</InvoicedQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">1,00</RowVatExcludedAmount></InvoiceRow>`
		env, err := finvoice.Parse(message{rows: rows, total: "1,26"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		line := env.Extract().(*bill.Invoice).Lines[0]
		assert.Equal(t, "0.00001", line.Item.Price.String())
		assert.Equal(t, "1.00", line.Total.String())
	})
	t.Run("percentage adjustments the row total accounts for", func(t *testing.T) {
		tests := []struct{ name, price, adjustment, rowTotal, total string }{
			{"discount without a unit price", "", `<RowDiscountPercent>10</RowDiscountPercent><RowDiscountTypeText>Alennus</RowDiscountTypeText>`, "90,00", "112,95"},
			{"charge without a unit price", "", `<RowChargeDetails><ReasonText>Käsittely</ReasonText><Percent>10</Percent></RowChargeDetails>`, "110,00", "138,05"},
			{"discount on a base amount", `<UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount>`,
				`<RowDiscountPercent>10</RowDiscountPercent><RowDiscountBaseAmount AmountCurrencyIdentifier="EUR">50,00</RowDiscountBaseAmount><RowDiscountTypeText>Alennus</RowDiscountTypeText>`, "95,00", "119,23"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rows := `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity>` + tt.price + tt.adjustment + `<RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">` + tt.rowTotal + `</RowVatExcludedAmount></InvoiceRow>`
				env, err := finvoice.Parse(message{rows: rows, total: tt.total}.bytes())
				require.NoError(t, err)
				require.NoError(t, env.Validate())
				line := env.Extract().(*bill.Invoice).Lines[0]
				assert.Zero(t, line.Item.Price.Compare(num.MakeAmount(5000, 2)), "price %s", line.Item.Price)
				assert.Equal(t, strings.Replace(tt.rowTotal, ",", ".", 1), line.Total.String())
			})
		}
	})
	t.Run("Finnish units", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<InvoicedQuantity>", `<InvoicedQuantity QuantityUnitCode="kpl">`, 1)
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, org.UnitPiece, inv.Lines[0].Item.Unit)
	})
	t.Run("the unit t could be hours or tonnes and stays generic", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<InvoicedQuantity>", `<InvoicedQuantity QuantityUnitCode="t">`, 1)
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, org.UnitOne, inv.Lines[0].Item.Unit)
	})
	t.Run("a row total with no price and no quantity prices at nothing", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Otsikko</ArticleName><InvoicedQuantity>0</InvoicedQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "0.00", inv.Lines[1].Item.Price.String())

		_, err := finvoice.Parse(message{rows: strings.Replace(rows, ">0,00<", ">5,00<", 1), total: "131,78"}.bytes())
		require.ErrorContains(t, err, "stated total 131.78 does not match")
	})
	t.Run("a price per a base that is no power of ten", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Pullot</ArticleName><InvoicedQuantity>300</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">10,00</UnitPriceAmount><UnitPriceBaseQuantity>3</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "1255,00"})
		assert.Equal(t, "1000.00", inv.Lines[0].Total.String())
	})
	t.Run("a price per a base quantity keeps its digits", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Nastat</ArticleName><InvoicedQuantity>10000</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">1,23</UnitPriceAmount><UnitPriceBaseQuantity>1000</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "15,44"})
		assert.Equal(t, "0.00123", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "12.30", inv.Lines[0].Total.String())
	})
	t.Run("a priced row without a row total is quantity times price", func(t *testing.T) {
		rows := strings.Replace(defaultRows, `<RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount>`, "", 1)
		inv := parseMessage(t, message{rows: rows})
		assert.Equal(t, "100.00", inv.Lines[0].Total.String())
	})
	t.Run("a row period with one date is kept", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<RowVatRatePercent>", `<EndDate Format="CCYYMMDD">20260930</EndDate><RowVatRatePercent>`, 1)
		inv := parseMessage(t, message{rows: rows})
		require.NotNil(t, inv.Lines[0].Period)
		assert.Nil(t, inv.Lines[0].Period.Start)
		assert.Equal(t, "2026-09-30", inv.Lines[0].Period.End.String())
	})
	t.Run("an amount in another currency is refused", func(t *testing.T) {
		tests := []struct{ name, rows string }{
			{"row total", strings.Replace(defaultRows, `<RowVatExcludedAmount AmountCurrencyIdentifier="EUR">`, `<RowVatExcludedAmount AmountCurrencyIdentifier="USD">`, 1)},
			{"unit price", strings.Replace(defaultRows, `<UnitPriceAmount AmountCurrencyIdentifier="EUR">`, `<UnitPriceAmount AmountCurrencyIdentifier="USD">`, 1)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := finvoice.Parse(message{rows: tt.rows}.bytes())
				require.ErrorContains(t, err, "is in USD, the document in EUR")
			})
		}
		t.Run("payment order", func(t *testing.T) {
			epi := strings.Replace(epiInstruction("125,50", "20261006", ""), `AmountCurrencyIdentifier="EUR"`, `AmountCurrencyIdentifier="USD"`, 1)
			_, err := finvoice.Parse(message{epi: epi}.bytes())
			require.ErrorContains(t, err, "is in USD, the document in EUR")
		})
	})
}
