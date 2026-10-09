package finvoice_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/tax"
	"github.com/invopop/gobl/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

const testUUID = "00000000-0000-0000-0000-000000000000"

func parseFile(t *testing.T, path string) *bill.Invoice {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	env, err := finvoice.Parse(data)
	require.NoError(t, err)
	inv, ok := env.Extract().(*bill.Invoice)
	require.True(t, ok)
	return inv
}

// TestParse turns every Finvoice under test/data/parse into a calculated
// invoice, checks it validates under fi-finvoice-v3, and compares it with
// its golden JSON. Run with -update to regenerate the goldens.
func TestParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("test", "data", "parse", "*.xml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, src := range files {
		t.Run(filepath.Base(src), func(t *testing.T) {
			data, err := os.ReadFile(src)
			require.NoError(t, err)
			env, err := finvoice.Parse(data)
			require.NoError(t, err)
			require.NoError(t, env.Validate())

			inv := env.Extract().(*bill.Invoice)
			assert.True(t, addon.V3.In(inv.GetAddons()...))
			inv.UUID = uuid.MustParse(testUUID)
			out, err := json.MarshalIndent(inv, "", "\t")
			require.NoError(t, err)

			golden := filepath.Join("test", "data", "parse", "out", strings.TrimSuffix(filepath.Base(src), ".xml")+".json")
			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o755))
				require.NoError(t, os.WriteFile(golden, out, 0o644))
				return
			}
			expected, err := os.ReadFile(golden)
			require.NoError(t, err, "golden %s missing, run with -update", golden)
			assert.Equal(t, string(expected), string(out), "run with -update to refresh %s", golden)
		})
	}
}

// TestParseOperatorConverted covers the shape operators deliver: Finvoice
// 1.3, no transmission details, the e-invoice addresses in the organisation
// unit numbers.
func TestParseOperatorConverted(t *testing.T) {
	inv := parseFile(t, filepath.Join("test", "data", "parse", "operator-converted.xml"))

	assert.Equal(t, bill.InvoiceTypeStandard, inv.Type)
	assert.Equal(t, cbc.Code("00044"), inv.Code)
	assert.Equal(t, "FI", inv.Supplier.TaxID.Country.String())
	assert.Equal(t, cbc.Code("76543212"), inv.Supplier.TaxID.Code)
	assert.Empty(t, inv.Supplier.Identities, "the Y-tunnus is the VAT number's own identity")
	assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
	assert.Empty(t, inv.Supplier.Ext.Get(addon.ExtKeyOperator))
	assert.Empty(t, inv.Customer.Ext.Get(addon.ExtKeyOperator))

	assert.Equal(t, "0.99", inv.Totals.TotalWithTax.String())
	assert.Equal(t, "0.79", inv.Totals.Total.String())
	require.Len(t, inv.Lines, 1)
	assert.Equal(t, "1.00", inv.Lines[0].Quantity.String())
	assert.Equal(t, org.UnitPiece, inv.Lines[0].Item.Unit)
	assert.Equal(t, tax.KeyStandard, inv.Lines[0].Taxes[0].Key)
	assert.Equal(t, "25.5%", inv.Lines[0].Taxes[0].Percent.String())

	assert.Equal(t, pay.MeansKeyCreditTransfer, inv.Payment.Instructions.Key)
	assert.Equal(t, cbc.Code("000440"), inv.Payment.Instructions.Ref)
	assert.Equal(t, cbc.Code("FI2112345600000785"), inv.Payment.Instructions.CreditTransfer[0].IBAN)
	assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Payment.Instructions.CreditTransfer[0].BIC)
	assert.Equal(t, "2026-09-22", inv.Payment.Terms.DueDates[0].Date.String())
	assert.Equal(t, "Test for e-invoicing", inv.Notes[0].Text)
}

// TestParseFramedMessage covers a transport frame: ISO-8859-15 bytes behind
// a SOAP envelope, with both operators on the parties.
func TestParseFramedMessage(t *testing.T) {
	inv := parseFile(t, filepath.Join("test", "data", "parse", "soap-framed.xml"))

	assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
	assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Supplier.Ext.Get(addon.ExtKeyOperator))
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
	assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(addon.ExtKeyOperator))
	assert.Equal(t, "Hoitokäynti", inv.Lines[1].Item.Name)
	assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
	assert.Equal(t, tax.KeyExempt, inv.Lines[1].Taxes[0].Key)
	assert.Equal(t, cbc.Code("VATEX-EU-132-1C"), inv.Lines[1].Taxes[0].Ext.Get(cef.ExtKeyVATEX))
	assert.Equal(t, pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA), inv.Payment.Instructions.Key)
	assert.Equal(t, cbc.Code("PO-1"), inv.Ordering.Purchases[0].Code)
	assert.Equal(t, cbc.Code("OSTAJAN VIITE"), inv.Ordering.Code)
	assert.Equal(t, "325.50", inv.Totals.TotalWithTax.String())
}

// TestParseCreditNote flips the negative document back to positive GOBL
// amounts.
func TestParseCreditNote(t *testing.T) {
	inv := parseFile(t, filepath.Join("test", "data", "parse", "credit-note.xml"))

	assert.Equal(t, bill.InvoiceTypeCreditNote, inv.Type)
	assert.Equal(t, cbc.Code("1042"), inv.Preceding[0].Code)
	assert.Equal(t, "2026-09-22", inv.Preceding[0].IssueDate.String())
	assert.Equal(t, "2", inv.Lines[0].Quantity.String())
	assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
	assert.Equal(t, "125.50", inv.Totals.TotalWithTax.String())
	assert.True(t, inv.Totals.Payable.Equals(num.MakeAmount(12550, 2)))
}

// message is a minimal Finvoice 1.3 without transmission details, as
// operators deliver it, for the parser tests to vary one part at a time.
type message struct {
	decl, transmission, sellerUnit, buyerUnit, details, vat, rows, total, roundoff, currency, note string
	// epi replaces the payment order instruction, whose amount otherwise
	// follows the total.
	epi string
	// noVATNumber leaves out the seller's VAT number, noBusinessID its Y-tunnus.
	noVATNumber, noBusinessID bool
}

const defaultRows = `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`

func (d message) bytes() []byte {
	def := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	return []byte(def(d.decl, `<?xml version="1.0" encoding="UTF-8"?>`) + `
<Finvoice Version="1.3">` + d.transmission + `
<SellerPartyDetails>` + unless(d.noBusinessID, `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier>`) + `
<SellerOrganisationName>Lähettäjä Oy</SellerOrganisationName>` + unless(d.noVATNumber, `<SellerOrganisationTaxCode>FI76543212</SellerOrganisationTaxCode>`) + `
<SellerPostalAddressDetails><SellerStreetName>Esimerkkikatu 1</SellerStreetName><SellerTownName>Helsinki</SellerTownName><SellerPostCodeIdentifier>00100</SellerPostCodeIdentifier><CountryCode>FI</CountryCode></SellerPostalAddressDetails>
</SellerPartyDetails>` + def(d.sellerUnit, "<SellerOrganisationUnitNumber>003776543212</SellerOrganisationUnitNumber>") + `
<BuyerPartyDetails><BuyerOrganisationName>Vastaanottaja Oy</BuyerOrganisationName><BuyerPostalAddressDetails><BuyerStreetName>Testitie 2</BuyerStreetName><BuyerTownName>Espoo</BuyerTownName><BuyerPostCodeIdentifier>02100</BuyerPostCodeIdentifier><CountryCode>FI</CountryCode></BuyerPostalAddressDetails></BuyerPartyDetails>` + def(d.buyerUnit, "<BuyerOrganisationUnitNumber>003745678907</BuyerOrganisationUnitNumber>") + `
<InvoiceDetails>` + def(d.details, `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`) + `
<InvoiceDate Format="CCYYMMDD">20260922</InvoiceDate>
<InvoiceTotalVatIncludedAmount AmountCurrencyIdentifier="` + def(d.currency, "EUR") + `">` + def(d.total, "125,50") + `</InvoiceTotalVatIncludedAmount>
` + unless(d.roundoff == "", `<InvoiceTotalRoundoffAmount AmountCurrencyIdentifier="EUR">`+d.roundoff+`</InvoiceTotalRoundoffAmount>`) + d.vat + unless(d.note == "", `<InvoiceFreeText>`+d.note+`</InvoiceFreeText>`) + `
<PaymentTermsDetails><PaymentTermsFreeText>14 pv</PaymentTermsFreeText><PaymentTermsFreeText>netto</PaymentTermsFreeText><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails>
</InvoiceDetails>
` + strings.ReplaceAll(def(d.rows, defaultRows), `AmountCurrencyIdentifier="EUR"`, `AmountCurrencyIdentifier="`+def(d.currency, "EUR")+`"`) + `
<EpiDetails>
<EpiIdentificationDetails><EpiDate Format="CCYYMMDD">20260922</EpiDate><EpiReference>1042005</EpiReference></EpiIdentificationDetails>
<EpiPartyDetails><EpiBfiPartyDetails/><EpiBeneficiaryPartyDetails><EpiAccountID IdentificationSchemeName="IBAN">FI2112345600000785</EpiAccountID></EpiBeneficiaryPartyDetails></EpiPartyDetails>
` + def(d.epi, `<EpiPaymentInstructionDetails><EpiInstructedAmount AmountCurrencyIdentifier="`+def(d.currency, "EUR")+`">`+def(d.total, "125,50")+`</EpiInstructedAmount><EpiCharge ChargeOption="SHA">SHA</EpiCharge><EpiDateOptionDate Format="CCYYMMDD">20261006</EpiDateOptionDate></EpiPaymentInstructionDetails>`) + `
</EpiDetails>
</Finvoice>`)
}

// epiInstruction writes a payment order instruction with the given amount,
// date and further elements.
func epiInstruction(amount, date, more string) string {
	return `<EpiPaymentInstructionDetails><EpiInstructedAmount AmountCurrencyIdentifier="EUR">` + amount + `</EpiInstructedAmount><EpiCharge ChargeOption="SHA">SHA</EpiCharge><EpiDateOptionDate Format="CCYYMMDD">` + date + `</EpiDateOptionDate>` + more + `</EpiPaymentInstructionDetails>`
}

func unless(skip bool, s string) string {
	if skip {
		return ""
	}
	return s
}

func parseMessage(t *testing.T, d message) *bill.Invoice {
	t.Helper()
	env, err := finvoice.Parse(d.bytes())
	require.NoError(t, err)
	require.NoError(t, env.Validate())
	return env.Extract().(*bill.Invoice)
}

func TestParseCreditNoteSign(t *testing.T) {
	details := `<InvoiceTypeCode>INV02</InvoiceTypeCode><InvoiceTypeText>HYVITYSLASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>78</InvoiceNumber><OriginalInvoiceNumber>77</OriginalInvoiceNumber>`
	inv := parseMessage(t, message{details: details})
	assert.Equal(t, bill.InvoiceTypeCreditNote, inv.Type)
	assert.Equal(t, "2", inv.Lines[0].Quantity.String(), "a positive credit note keeps its signs")
	assert.Equal(t, "125.50", inv.Totals.Payable.String())

	t.Run("a zero total keeps the rows' own signs", func(t *testing.T) {
		rows := defaultRows + strings.Replace(strings.Replace(strings.Replace(defaultRows, "Konsultointi", "Hyvitys", 1), "<InvoicedQuantity>2<", "<InvoicedQuantity>-2<", 1), ">100,00<", ">-100,00<", 1)
		inv := parseMessage(t, message{details: details, rows: rows, total: "0,00"})
		assert.Equal(t, "-2", inv.Lines[0].Quantity.String(), "a positive Finvoice row on a credit note is a charge")
		assert.Equal(t, "2", inv.Lines[1].Quantity.String(), "a negative one is the credit")
	})
	t.Run("nothing to pay", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Korjaus</ArticleName><InvoicedQuantity>0</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		env, err := finvoice.Parse(message{details: details, rows: rows, total: "0,00"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Nil(t, inv.Payment.Terms.DueDates[0].Percent)
	})
	t.Run("a negative credit note signing only the amounts or the price", func(t *testing.T) {
		tests := []struct{ name, quantity, price string }{
			{"amounts", "2", "50,00"},
			{"price", "2", "-50,00"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rows := `<InvoiceRow><ArticleName>Palautus</ArticleName><InvoicedQuantity>` + tt.quantity + `</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">` + tt.price + `</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">-100,00</RowVatExcludedAmount></InvoiceRow>`
				inv := parseMessage(t, message{details: details, rows: rows, total: "-125,50"})
				assert.Equal(t, "2", inv.Lines[0].Quantity.String())
				assert.Zero(t, inv.Lines[0].Item.Price.Compare(num.MakeAmount(5000, 2)), "price %s", inv.Lines[0].Item.Price)
				assert.Equal(t, "125.50", inv.Totals.Payable.String())
			})
		}
	})
	t.Run("an invoice's UNTDID code reads as an invoice with a negative total", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV02</InvoiceTypeCode><InvoiceTypeCodeUN>380</InvoiceTypeCodeUN><InvoiceTypeText>HYVITYSLASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>78</InvoiceNumber>`
		rows := strings.Replace(strings.Replace(defaultRows, "<InvoicedQuantity>2<", "<InvoicedQuantity>-2<", 1), ">100,00<", ">-100,00<", 1)
		inv := parseMessage(t, message{details: details, rows: rows, total: "-125,50"})
		assert.Equal(t, bill.InvoiceTypeStandard, inv.Type)
		assert.Equal(t, "-2", inv.Lines[0].Quantity.String())
		assert.Equal(t, "-125.50", inv.Totals.Payable.String())
	})
	t.Run("a mismatch is reported with the document's signs", func(t *testing.T) {
		rows := strings.Replace(strings.Replace(defaultRows, "<InvoicedQuantity>2<", "<InvoicedQuantity>-2<", 1), ">100,00<", ">-100,00<", 1)
		_, err := finvoice.Parse(message{details: details, rows: rows, total: "-999,00"}.bytes())
		require.ErrorContains(t, err, "stated total -999.00 does not match the rows, which add up to -125.50")
	})
}

func TestParseCharsets(t *testing.T) {
	tests := []struct {
		label string
		enc   *charmap.Charmap
		note  string
	}{
		{"ISO-8859-1", charmap.ISO8859_1, "Hinta 10 ¤"},
		{"ISO-8859-15", charmap.ISO8859_15, "Hinta 10 € Šumava"},
		{"windows-1252", charmap.Windows1252, "Hinta 10 € Šumava"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			d := message{decl: `<?xml version="1.0" encoding="` + tt.label + `"?>`, note: tt.note}
			data, err := tt.enc.NewEncoder().Bytes(d.bytes())
			require.NoError(t, err)
			env, err := finvoice.Parse(data)
			require.NoError(t, err)
			inv := env.Extract().(*bill.Invoice)
			assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
			assert.Equal(t, tt.note, inv.Notes[0].Text)
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		_, err := finvoice.Parse(message{decl: `<?xml version="1.0" encoding="EBCDIC-US"?>`}.bytes())
		assert.ErrorContains(t, err, "unsupported encoding")
	})
}

func TestParseTotals(t *testing.T) {
	t.Run("a difference within a subunit per row and one more is rounding", func(t *testing.T) {
		tests := []struct {
			name, rows, total, rounding string
		}{
			{"one row, two subunits", defaultRows, "125,52", "0.02"},
			{"two rows, three subunits", defaultRows + defaultRows, "251,03", "0.03"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				inv := parseMessage(t, message{rows: tt.rows, total: tt.total})
				assert.Equal(t, strings.Replace(tt.total, ",", ".", 1), inv.Totals.Payable.String())
				assert.Equal(t, tt.rounding, inv.Totals.Rounding.String())
			})
		}
	})
	t.Run("stated roundoff", func(t *testing.T) {
		inv := parseMessage(t, message{roundoff: "-0,50", epi: epiInstruction("125,00", "20261006", "")})
		assert.Equal(t, "125.00", inv.Totals.Payable.String())
		assert.Equal(t, "-0.50", inv.Totals.Rounding.String())
	})
	t.Run("stated roundoff with a difference on top", func(t *testing.T) {
		inv := parseMessage(t, message{total: "125,51", roundoff: "-0,50", epi: epiInstruction("125,01", "20261006", "")})
		assert.Equal(t, "125.01", inv.Totals.Payable.String())
		assert.Equal(t, "-0.49", inv.Totals.Rounding.String())
	})
	t.Run("currency from the total", func(t *testing.T) {
		inv := parseMessage(t, message{currency: "SEK"})
		assert.Equal(t, currency.SEK, inv.Currency)
	})
	t.Run("text rows alone parse but make no invoice", func(t *testing.T) {
		rows := `<InvoiceRow><RowFreeText>Ei laskutettavaa</RowFreeText></InvoiceRow>`
		env, err := finvoice.Parse(message{rows: rows, total: "0,00"}.bytes())
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Empty(t, inv.Lines)
		assert.Equal(t, "Ei laskutettavaa", inv.Notes[0].Text)
		assert.ErrorContains(t, env.Validate(), "lines")
	})
	t.Run("a difference past the tolerance is refused", func(t *testing.T) {
		tests := []struct{ name, rows, total string }{
			{"one row, three subunits", defaultRows, "125,53"},
			{"two rows, four subunits", defaultRows + defaultRows, "251,04"},
			{"far off", defaultRows, "130,00"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := finvoice.Parse(message{rows: tt.rows, total: tt.total}.bytes())
				require.ErrorContains(t, err, "stated total "+strings.Replace(tt.total, ",", ".", 1)+" does not match")
			})
		}
	})
	t.Run("text rows alone with a stated total are refused", func(t *testing.T) {
		rows := `<InvoiceRow><RowFreeText>Ei laskutettavaa</RowFreeText></InvoiceRow>`
		_, err := finvoice.Parse(message{rows: rows, total: "10,00"}.bytes())
		require.ErrorContains(t, err, "stated total 10.00 does not match")
	})
	t.Run("unknown currency is refused", func(t *testing.T) {
		_, err := finvoice.Parse(message{currency: "XXX"}.bytes())
		require.ErrorContains(t, err, `unknown currency "XXX"`)
	})
}

// TestParseRefusals pins what the parser refuses and how it says so.
func TestParseRefusals(t *testing.T) {
	valid := string(message{}.bytes())
	soap := `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"><SOAP-ENV:Header>`
	tests := []struct {
		name, data, want string
	}{
		{"not XML", "not xml", "unmarshal document"},
		{"another format", `<?xml version="1.0"?><Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"/>`, "unmarshal document: expected element type <Finvoice>"},
		{"a blank invoice number", strings.Replace(valid, "<InvoiceNumber>77</InvoiceNumber>", "<InvoiceNumber> </InvoiceNumber>", 1), "document has no InvoiceNumber"},
		{"a cancellation", strings.Replace(valid, "<OriginCode>Original</OriginCode>", "<OriginCode>Cancel</OriginCode>", 1), "a cancellation of 77"},
		{"two messages in one file", valid + valid, "more than one Finvoice message"},
		{"a malformed transport frame", soap + valid, "unmarshal transport frame"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := finvoice.Parse([]byte(tt.data))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
