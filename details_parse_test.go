package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTypeCodes(t *testing.T) {
	tests := []struct {
		name, code, codeUN string
		typ, tag           cbc.Key
	}{
		{"self-billed by UNTDID code", "INV01", "389", bill.InvoiceTypeStandard, tax.TagSelfBilled},
		{"self-billed by Finvoice code", "INV07", "", bill.InvoiceTypeStandard, tax.TagSelfBilled},
		{"partial", "INV01", "326", bill.InvoiceTypeStandard, tax.TagPartial},
		{"corrective", "INV01", "384", bill.InvoiceTypeCorrective, ""},
		{"proforma by Finvoice code", "INV06", "", bill.InvoiceTypeProforma, ""},
		{"debit note", "INV01", "383", bill.InvoiceTypeDebitNote, ""},
		{"self-billed credit note", "INV02", "261", bill.InvoiceTypeCreditNote, tax.TagSelfBilled},
		{"prepayment", "INV01", "386", bill.InvoiceTypeStandard, tax.TagPrepayment},
		{"factored", "INV01", "393", bill.InvoiceTypeStandard, tax.TagFactoring},
		{"factored credit note", "INV02", "396", bill.InvoiceTypeCreditNote, tax.TagFactoring},
		{"an unmapped UNTDID code leaves the Finvoice code to decide", "INV01", "388", bill.InvoiceTypeStandard, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := `<InvoiceTypeCode>` + tt.code + `</InvoiceTypeCode>`
			if tt.codeUN != "" {
				details += `<InvoiceTypeCodeUN>` + tt.codeUN + `</InvoiceTypeCodeUN>`
			}
			details += `<InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
			inv := parseMessage(t, message{details: details})
			assert.Equal(t, tt.typ, inv.Type)
			if tt.tag == "" {
				assert.Empty(t, inv.GetTags())
			} else {
				assert.Equal(t, []cbc.Key{tt.tag}, inv.GetTags())
			}
		})
	}

	t.Run("self-billing adds its tag to the UNTDID code's", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV07</InvoiceTypeCode><InvoiceTypeCodeUN>326</InvoiceTypeCodeUN><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		inv := parseMessage(t, message{details: details})
		assert.Equal(t, []cbc.Key{tax.TagPartial, tax.TagSelfBilled}, inv.GetTags())
	})
	t.Run("messages that are not invoices are refused", func(t *testing.T) {
		for _, code := range []string{"TES01", "QUO01", "INV08"} {
			details := `<InvoiceTypeCode>` + code + `</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
			_, err := finvoice.Parse(message{details: details}.bytes())
			require.ErrorIs(t, err, finvoice.ErrUnsupportedDocumentType)
			require.ErrorContains(t, err, code)
		}
	})
	t.Run("copies are refused", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Copy</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		_, err := finvoice.Parse(message{details: details}.bytes())
		require.ErrorIs(t, err, finvoice.ErrUnsupportedDocumentType)
		require.ErrorContains(t, err, "copy of 77")
	})
}

// TestParseTypeCodesWithWhitespace checks padded codes still set the tags.
func TestParseTypeCodesWithWhitespace(t *testing.T) {
	details := "<InvoiceTypeCode>\n INV07\n</InvoiceTypeCode><InvoiceTypeCodeUN>\n 389\n</InvoiceTypeCodeUN><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>"
	inv := parseMessage(t, message{details: details})
	assert.Equal(t, []cbc.Key{tax.TagSelfBilled}, inv.GetTags())
}

// TestParseOrdering checks the references and the invoicing period.
func TestParseOrdering(t *testing.T) {
	t.Run("a contract reference keeps its date", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><AgreementIdentifier>SOP-9</AgreementIdentifier><AgreementDate Format="CCYYMMDD">20260115</AgreementDate>`
		inv := parseMessage(t, message{details: details})
		require.Len(t, inv.Ordering.Contracts, 1)
		assert.Equal(t, cbc.Code("SOP-9"), inv.Ordering.Contracts[0].Code)
		assert.Equal(t, "2026-01-15", inv.Ordering.Contracts[0].IssueDate.String())
	})
	t.Run("an invoicing period with one date is kept", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><InvoicingPeriodStartDate Format="CCYYMMDD">20260901</InvoicingPeriodStartDate>`
		inv := parseMessage(t, message{details: details})
		require.NotNil(t, inv.Ordering)
		require.NotNil(t, inv.Ordering.Period)
		assert.Equal(t, "2026-09-01", inv.Ordering.Period.Start.String())
		assert.Nil(t, inv.Ordering.Period.End)
	})
	t.Run("malformed dates are refused", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><OrderIdentifier>PO-1</OrderIdentifier><OrderDate Format="CCYYMMDD">20261301</OrderDate>`
		_, err := finvoice.Parse(message{details: details}.bytes())
		require.ErrorContains(t, err, `date "20261301"`)
	})
}

// TestParseAdjustments checks document discounts and charges.
func TestParseAdjustments(t *testing.T) {
	t.Run("a document discount per VAT rate keeps its amounts", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Kirja</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>14</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		discount := func(rate string) string {
			return `<DiscountDetails><FreeText>Alennus</FreeText><Percent>5</Percent><Amount AmountCurrencyIdentifier="EUR">5,00</Amount><VatCategoryCode>S</VatCategoryCode><VatRatePercent>` + rate + `</VatRatePercent></DiscountDetails>`
		}
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>` + discount("25,5") + discount("14")
		env, err := finvoice.Parse(message{details: details, rows: rows, total: "227,53"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		for _, d := range inv.Discounts {
			assert.Nil(t, d.Percent)
			assert.Equal(t, "5.00", d.Amount.String())
		}
	})
}

// TestParseVAT checks the categories and exemption reasons read from the
// rows and the VAT breakdown.
func TestParseVAT(t *testing.T) {
	t.Run("rows without a code take the category the breakdown gives at their rate", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>` +
			`<InvoiceRow><ArticleName>B</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>25,5</VatRatePercent><VatCode>S</VatCode></VatSpecificationDetails><VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatFreeText>Terveydenhuoltopalvelu</VatFreeText></VatSpecificationDetails>`
		inv := parseMessage(t, message{rows: rows, vat: vat, total: "225,50"})
		assert.Equal(t, tax.KeyStandard, inv.Lines[0].Taxes[0].Key)
		assert.Equal(t, tax.KeyExempt, inv.Lines[1].Taxes[0].Key)
	})
	t.Run("a zero rate with no VAT code anywhere is zero-rated", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Palvelu</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseMessage(t, message{rows: rows, total: "100,00"})
		assert.Equal(t, tax.KeyZero, inv.Lines[0].Taxes[0].Key)
	})
	t.Run("an exemption text comes from a row of that category", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Asennus asiakkaan tiloissa</RowFreeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>` +
			`<InvoiceRow><ArticleName>B</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Terveydenhuoltopalvelu, AVL 34 §</RowFreeText><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode></VatSpecificationDetails>`
		inv := parseMessage(t, message{rows: rows, vat: vat, total: "225,50"})
		require.Len(t, inv.Tax.Notes, 1)
		assert.Equal(t, "Terveydenhuoltopalvelu, AVL 34 §", inv.Tax.Notes[0].Text)
	})
	t.Run("an exemption text the breakdown repeats is one note", func(t *testing.T) {
		row := func(name string) string {
			return `<InvoiceRow><ArticleName>` + name + `</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		}
		spec := func(text string) string {
			return `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatFreeText>` + text + `</VatFreeText></VatSpecificationDetails>`
		}
		inv := parseMessage(t, message{rows: row("Hoito") + row("Koulutus"), vat: spec("AVL 34 §") + spec("AVL 34 §"), total: "200,00"})
		require.Len(t, inv.Tax.Notes, 1)
		assert.Equal(t, "AVL 34 §", inv.Tax.Notes[0].Text)

		env, err := finvoice.Parse(message{rows: row("Hoito") + row("Koulutus"), vat: spec("Terveydenhuolto") + spec("Koulutus"), total: "200,00"}.bytes())
		require.NoError(t, err)
		require.Len(t, env.Extract().(*bill.Invoice).Tax.Notes, 2)
		require.ErrorContains(t, env.Validate(), "one exemption reason per VAT category")
	})
	t.Run("exemption reason on the row", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Hoito</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Terveydenhuoltopalvelu, AVL 34 §</RowFreeText><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount></VatSpecificationDetails>`
		env, err := finvoice.Parse(message{rows: rows, vat: vat, total: "100,00"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, "Terveydenhuoltopalvelu, AVL 34 §", inv.Tax.Notes[0].Text)
	})
	t.Run("two exemption reasons in one category cannot be told apart", func(t *testing.T) {
		row := func(name string) string {
			return `<InvoiceRow><ArticleName>` + name + `</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		}
		spec := func(vatex string) string {
			return `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatExemptionReasonCode>` + vatex + `</VatExemptionReasonCode></VatSpecificationDetails>`
		}
		_, err := finvoice.Parse(message{rows: row("Hoito") + row("Koulutus"), vat: spec("VATEX-EU-132") + spec("VATEX-EU-135"), total: "200,00"}.bytes())
		require.ErrorContains(t, err, "VAT breakdown gives E two exemption reasons, VATEX-EU-132 and VATEX-EU-135")
	})
	t.Run("two categories at one rate cannot be told apart", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode></VatSpecificationDetails><VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>AE</VatCode></VatSpecificationDetails>`
		_, err := finvoice.Parse(message{rows: rows, vat: vat, total: "100,00"}.bytes())
		require.ErrorContains(t, err, "E and AE")
	})
	t.Run("unknown VAT code in the breakdown", func(t *testing.T) {
		vat := `<VatSpecificationDetails><VatRatePercent>25,5</VatRatePercent><VatCode>L</VatCode></VatSpecificationDetails>`
		_, err := finvoice.Parse(message{vat: vat}.bytes())
		require.ErrorContains(t, err, `VAT breakdown: unknown VAT category code "L"`)
	})
	t.Run("unknown VAT code", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<RowVatCode>S</RowVatCode>", "<RowVatCode>L</RowVatCode>", 1)
		_, err := finvoice.Parse(message{rows: rows}.bytes())
		require.ErrorContains(t, err, `VAT category code "L"`)
	})
}
