package fifinvoice_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fifinvoice "github.com/invopop/gobl.fi.finvoice"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/catalogues/untdid"
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
	env, err := fifinvoice.Parse(data)
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
			env, err := fifinvoice.Parse(data)
			require.NoError(t, err)
			require.NoError(t, env.Validate())

			inv := env.Extract().(*bill.Invoice)
			assert.True(t, finvoice.V3.In(inv.GetAddons()...))
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

// TestParseFramelessDelivery covers the shape operators deliver: Finvoice
// 1.3, no message frame, the e-invoice addresses in the organisation unit
// numbers.
func TestParseFramelessDelivery(t *testing.T) {
	inv := parseFile(t, filepath.Join("test", "data", "parse", "operator-converted.xml"))

	assert.Equal(t, bill.InvoiceTypeStandard, inv.Type)
	assert.Equal(t, cbc.Code("00044"), inv.Code)
	assert.Equal(t, "FI", inv.Supplier.TaxID.Country.String())
	assert.Equal(t, cbc.Code("76543212"), inv.Supplier.TaxID.Code)
	assert.Empty(t, inv.Supplier.Identities, "the Y-tunnus is the VAT number's own identity")
	assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
	assert.Empty(t, inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
	assert.Empty(t, inv.Customer.Ext.Get(finvoice.ExtKeyOperator))

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
	assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
	assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
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

// delivery is a minimal frameless Finvoice 1.3, as operators deliver it, for
// the parser tests to vary one part at a time.
type delivery struct {
	decl, frame, sellerUnit, details, vat, rows, total, roundoff, currency, note string
	// noVATNumber leaves out the seller's VAT number, noBusinessID its Y-tunnus.
	noVATNumber, noBusinessID bool
}

const defaultRows = `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`

func (d delivery) bytes() []byte {
	def := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	return []byte(def(d.decl, `<?xml version="1.0" encoding="UTF-8"?>`) + `
<Finvoice Version="1.3">` + d.frame + `
<SellerPartyDetails>` + unless(d.noBusinessID, `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier>`) + `
<SellerOrganisationName>Lähettäjä Oy</SellerOrganisationName>` + unless(d.noVATNumber, `<SellerOrganisationTaxCode>FI76543212</SellerOrganisationTaxCode>`) + `
<SellerPostalAddressDetails><SellerStreetName>Esimerkkikatu 1</SellerStreetName><SellerTownName>Helsinki</SellerTownName><SellerPostCodeIdentifier>00100</SellerPostCodeIdentifier><CountryCode>FI</CountryCode></SellerPostalAddressDetails>
</SellerPartyDetails>` + d.sellerUnit + `
<BuyerPartyDetails><BuyerOrganisationName>Vastaanottaja Oy</BuyerOrganisationName><BuyerPostalAddressDetails><BuyerStreetName>Testitie 2</BuyerStreetName><BuyerTownName>Espoo</BuyerTownName><BuyerPostCodeIdentifier>02100</BuyerPostCodeIdentifier><CountryCode>FI</CountryCode></BuyerPostalAddressDetails></BuyerPartyDetails>
<InvoiceDetails>` + def(d.details, `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`) + `
<InvoiceDate Format="CCYYMMDD">20260922</InvoiceDate>
<InvoiceTotalVatIncludedAmount AmountCurrencyIdentifier="` + def(d.currency, "EUR") + `">` + def(d.total, "125,50") + `</InvoiceTotalVatIncludedAmount>
` + unless(d.roundoff == "", `<InvoiceTotalRoundoffAmount AmountCurrencyIdentifier="EUR">`+d.roundoff+`</InvoiceTotalRoundoffAmount>`) + d.vat + unless(d.note == "", `<InvoiceFreeText>`+d.note+`</InvoiceFreeText>`) + `
<PaymentTermsDetails><PaymentTermsFreeText>14 pv</PaymentTermsFreeText><PaymentTermsFreeText>netto</PaymentTermsFreeText><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails>
</InvoiceDetails>
` + def(d.rows, defaultRows) + `
<EpiDetails>
<EpiIdentificationDetails><EpiDate Format="CCYYMMDD">20260922</EpiDate><EpiReference>1042005</EpiReference></EpiIdentificationDetails>
<EpiPartyDetails><EpiBfiPartyDetails/><EpiBeneficiaryPartyDetails><EpiAccountID IdentificationSchemeName="IBAN">FI2112345600000785</EpiAccountID></EpiBeneficiaryPartyDetails></EpiPartyDetails>
<EpiPaymentInstructionDetails><EpiInstructedAmount AmountCurrencyIdentifier="EUR">125,50</EpiInstructedAmount><EpiCharge ChargeOption="SHA">SHA</EpiCharge><EpiDateOptionDate Format="CCYYMMDD">20261006</EpiDateOptionDate></EpiPaymentInstructionDetails>
</EpiDetails>
</Finvoice>`)
}

func unless(skip bool, s string) string {
	if skip {
		return ""
	}
	return s
}

func parseDelivery(t *testing.T, d delivery) *bill.Invoice {
	t.Helper()
	env, err := fifinvoice.Parse(d.bytes())
	require.NoError(t, err)
	return env.Extract().(*bill.Invoice)
}

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := `<InvoiceTypeCode>` + tt.code + `</InvoiceTypeCode>`
			if tt.codeUN != "" {
				details += `<InvoiceTypeCodeUN>` + tt.codeUN + `</InvoiceTypeCodeUN>`
			}
			details += `<InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
			inv := parseDelivery(t, delivery{details: details})
			assert.Equal(t, tt.typ, inv.Type)
			if tt.tag != "" {
				assert.True(t, inv.HasTags(tt.tag))
			}
		})
	}

	t.Run("messages that are not invoices are refused", func(t *testing.T) {
		for _, code := range []string{"TES01", "QUO01", "INV08"} {
			details := `<InvoiceTypeCode>` + code + `</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
			_, err := fifinvoice.Parse(delivery{details: details}.bytes())
			require.ErrorIs(t, err, fifinvoice.ErrUnsupportedDocumentType)
			require.ErrorContains(t, err, code)
		}
	})
	t.Run("copies are refused", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Copy</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		_, err := fifinvoice.Parse(delivery{details: details}.bytes())
		require.ErrorIs(t, err, fifinvoice.ErrUnsupportedDocumentType)
		require.ErrorContains(t, err, "copy of 77")
	})
}

func TestParseCreditNoteSign(t *testing.T) {
	details := `<InvoiceTypeCode>INV02</InvoiceTypeCode><InvoiceTypeText>HYVITYSLASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>78</InvoiceNumber><OriginalInvoiceNumber>77</OriginalInvoiceNumber>`
	inv := parseDelivery(t, delivery{details: details})
	assert.Equal(t, bill.InvoiceTypeCreditNote, inv.Type)
	assert.Equal(t, "2", inv.Lines[0].Quantity.String(), "a positive credit note keeps its signs")
	assert.Equal(t, "125.50", inv.Totals.Payable.String())

	t.Run("nothing to pay", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Korjaus</ArticleName><InvoicedQuantity>0</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		env, err := fifinvoice.Parse(delivery{details: details, rows: rows, total: "0,00"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Nil(t, inv.Payment.Terms.DueDates[0].Percent)
	})
}

func TestParseAddresses(t *testing.T) {
	tests := []struct{ name, unit, uri string }{
		{"OVT", "003776543212", "iso6523-actorid-upis::0216:003776543212"},
		{"OVT with a unit suffix", "003776543212AB1", "iso6523-actorid-upis::0216:003776543212AB1"},
		{"IBAN", "FI2112345600000785", "iso6523-actorid-upis::9918:FI2112345600000785"},
		{"Y-tunnus", "7654321-2", "iso6523-actorid-upis::0212:7654321-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := parseDelivery(t, delivery{sellerUnit: "<SellerOrganisationUnitNumber>" + tt.unit + "</SellerOrganisationUnitNumber>"})
			require.Len(t, inv.Supplier.Endpoints, 1)
			assert.Equal(t, cbc.URI(tt.uri), inv.Supplier.Endpoints[0].URI)
		})
	}
	t.Run("an address of an unknown shape is kept as an inbox", func(t *testing.T) {
		inv := parseDelivery(t, delivery{sellerUnit: "<SellerOrganisationUnitNumber>LASKUT</SellerOrganisationUnitNumber>"})
		assert.Empty(t, inv.Supplier.Endpoints)
		require.Len(t, inv.Supplier.Inboxes, 1)
		assert.Equal(t, cbc.Code("LASKUT"), inv.Supplier.Inboxes[0].Code)
	})
	t.Run("frame wins over the unit number", func(t *testing.T) {
		frame := `<MessageTransmissionDetails><MessageSenderDetails><FromIdentifier SchemeID="0216">003799999999</FromIdentifier><FromIntermediator>NDEAFIHH</FromIntermediator></MessageSenderDetails><MessageReceiverDetails><ToIdentifier>003745678907</ToIdentifier><ToIntermediator>` + receiverOperator + `</ToIntermediator></MessageReceiverDetails><MessageDetails><MessageIdentifier>1</MessageIdentifier><MessageTimeStamp>2026-09-22T11:12:12+03:00</MessageTimeStamp></MessageDetails></MessageTransmissionDetails>`
		inv := parseDelivery(t, delivery{frame: frame, sellerUnit: "<SellerOrganisationUnitNumber>003776543212</SellerOrganisationUnitNumber>"})
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003799999999"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
	})
	t.Run("a SOAP frame alone carries the routing", func(t *testing.T) {
		soap := `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eb="http://www.oasis-open.org/committees/ebxml-msg/schema/msg-header-2_0.xsd"><SOAP-ENV:Header><eb:MessageHeader>
<eb:From><eb:PartyId>003776543212</eb:PartyId><eb:Role>Sender</eb:Role></eb:From><eb:From><eb:PartyId>HELSFIHH</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:From>
<eb:To><eb:PartyId>003745678907</eb:PartyId><eb:Role>Receiver</eb:Role></eb:To><eb:To><eb:PartyId>` + receiverOperator + `</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:To>
</eb:MessageHeader></SOAP-ENV:Header><SOAP-ENV:Body/></SOAP-ENV:Envelope>
`
		env, err := fifinvoice.Parse(append([]byte(soap), delivery{}.bytes()...))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
		assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
	})
}

func TestParseIdentities(t *testing.T) {
	t.Run("Y-tunnus without a VAT number stays a legal identity", func(t *testing.T) {
		inv := parseDelivery(t, delivery{noVATNumber: true})
		assert.Nil(t, inv.Supplier.TaxID)
		require.Len(t, inv.Supplier.Identities, 1)
		assert.Equal(t, cbc.Code("7654321-2"), inv.Supplier.Identities[0].Code)
		assert.Equal(t, cbc.Code("0212"), inv.Supplier.Identities[0].Ext.Get(iso.ExtKeySchemeID))
		assert.Equal(t, "FI", inv.GetRegime().String())
	})
	t.Run("a Y-tunnus in the VAT field is no VAT number", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		d := delivery{details: details, noVATNumber: true}
		data := strings.Replace(string(d.bytes()), `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier>`, `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier><SellerOrganisationTaxCode>7654321-2</SellerOrganisationTaxCode>`, 1)
		data = strings.Replace(data, `<SellerOrganisationName>Lähettäjä Oy</SellerOrganisationName>`, ``, 1)
		data = strings.Replace(data, `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier><SellerOrganisationTaxCode>7654321-2</SellerOrganisationTaxCode>`, `<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier><SellerOrganisationName>Lähettäjä Oy</SellerOrganisationName><SellerOrganisationTaxCode>7654321-2</SellerOrganisationTaxCode>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Nil(t, inv.Supplier.TaxID)
	})
}

func TestParseRows(t *testing.T) {
	t.Run("rows and text rows", func(t *testing.T) {
		rows := defaultRows + `
<InvoiceRow><RowFreeText>Välisumma</RowFreeText></InvoiceRow>
<InvoiceRow><ArticleName>Otsikkorivi</ArticleName></InvoiceRow>
<InvoiceRow><ArticleName>Rahti</ArticleName><InvoicedQuantity QuantityUnitCodeUN="ZP">2</InvoicedQuantity><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">15,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">15,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatExemptionReasonCode>VATEX-EU-132-1C</VatExemptionReasonCode></VatSpecificationDetails>`
		inv := parseDelivery(t, delivery{rows: rows, vat: vat, total: "140,50"})

		require.Len(t, inv.Lines, 2, "text rows are not lines")
		assert.Equal(t, []string{"Välisumma", "Otsikkorivi"}, []string{inv.Notes[0].Text, inv.Notes[1].Text})
		line := inv.Lines[1]
		assert.Equal(t, "7.5000", line.Item.Price.String(), "price from the row total")
		assert.Equal(t, cbc.Code("ZP"), line.Item.Ext.Get(untdid.ExtKeyUnit))
		assert.Equal(t, tax.KeyExempt, line.Taxes[0].Key, "category from the breakdown")
		assert.Equal(t, cbc.Code("VATEX-EU-132-1C"), line.Taxes[0].Ext.Get(cef.ExtKeyVATEX))
		assert.Equal(t, "14 pv netto", inv.Payment.Terms.Notes)
	})
	t.Run("price per base quantity", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Sähkö</ArticleName><InvoicedQuantity QuantityUnitCode="kWh">2000</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><UnitPriceBaseQuantity>1000</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "0.0500", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "100.00", inv.Lines[0].Total.String())
		assert.Nil(t, inv.Totals.Rounding)
	})
	t.Run("net price over gross", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">60,00</UnitPriceAmount><UnitPriceNetAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceNetAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
	})
	t.Run("row total wins over the price", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Konsultointi</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">55,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "50.0000", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "125.50", inv.Totals.Payable.String())
	})
	t.Run("Finnish units", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<InvoicedQuantity>", `<InvoicedQuantity QuantityUnitCode="kpl">`, 1)
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, org.UnitPiece, inv.Lines[0].Item.Unit)
	})
	t.Run("exemption reason on the row", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Hoito</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Terveydenhuoltopalvelu, AVL 34 §</RowFreeText><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount></VatSpecificationDetails>`
		env, err := fifinvoice.Parse(delivery{rows: rows, vat: vat, total: "100,00"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, "Terveydenhuoltopalvelu, AVL 34 §", inv.Tax.Notes[0].Text)
	})
	t.Run("two categories at one rate cannot be told apart", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode></VatSpecificationDetails><VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>AE</VatCode></VatSpecificationDetails>`
		_, err := fifinvoice.Parse(delivery{rows: rows, vat: vat, total: "100,00"}.bytes())
		require.ErrorContains(t, err, "E and AE")
	})
	t.Run("unknown VAT code", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<RowVatCode>S</RowVatCode>", "<RowVatCode>L</RowVatCode>", 1)
		_, err := fifinvoice.Parse(delivery{rows: rows}.bytes())
		require.ErrorContains(t, err, `VAT category code "L"`)
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
			d := delivery{decl: `<?xml version="1.0" encoding="` + tt.label + `"?>`, note: tt.note}
			data, err := tt.enc.NewEncoder().Bytes(d.bytes())
			require.NoError(t, err)
			env, err := fifinvoice.Parse(data)
			require.NoError(t, err)
			inv := env.Extract().(*bill.Invoice)
			assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
			assert.Equal(t, tt.note, inv.Notes[0].Text)
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		_, err := fifinvoice.Parse(delivery{decl: `<?xml version="1.0" encoding="EBCDIC-US"?>`}.bytes())
		assert.ErrorContains(t, err, "unsupported encoding")
	})
}

func TestParseTotals(t *testing.T) {
	t.Run("a cent kept as rounding", func(t *testing.T) {
		inv := parseDelivery(t, delivery{total: "125,51"})
		assert.Equal(t, "125.51", inv.Totals.Payable.String())
		require.NotNil(t, inv.Totals.Rounding)
		assert.Equal(t, "0.01", inv.Totals.Rounding.String())
	})
	t.Run("stated roundoff", func(t *testing.T) {
		inv := parseDelivery(t, delivery{roundoff: "-0,50"})
		assert.Equal(t, "125.00", inv.Totals.Payable.String())
		assert.Equal(t, "-0.50", inv.Totals.Rounding.String())
	})
	t.Run("a total the rows do not add up to is refused", func(t *testing.T) {
		_, err := fifinvoice.Parse(delivery{total: "130,00"}.bytes())
		require.ErrorContains(t, err, "stated total 130.00 does not match")
	})
	t.Run("currency from the total", func(t *testing.T) {
		inv := parseDelivery(t, delivery{currency: "SEK"})
		assert.Equal(t, currency.SEK, inv.Currency)
	})
	t.Run("unknown currency is refused", func(t *testing.T) {
		_, err := fifinvoice.Parse(delivery{currency: "XXX"}.bytes())
		require.ErrorContains(t, err, `unknown currency "XXX"`)
	})
	t.Run("malformed dates are refused", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><OrderIdentifier>PO-1</OrderIdentifier><OrderDate Format="CCYYMMDD">2026-09-01</OrderDate>`
		_, err := fifinvoice.Parse(delivery{details: details}.bytes())
		require.ErrorContains(t, err, `date "2026-09-01"`)
	})
}

func TestParsePayment(t *testing.T) {
	t.Run("further accounts and the payee", func(t *testing.T) {
		d := delivery{}
		data := strings.Replace(string(d.bytes()), `</SellerPartyDetails>`, `</SellerPartyDetails><SellerInformationDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">FI2112345600000785</SellerAccountID><SellerBic IdentificationSchemeName="BIC">NDEAFIHH</SellerBic></SellerAccountDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">FI4250001510000023</SellerAccountID><SellerBic IdentificationSchemeName="BIC">OKOYFIHH</SellerBic></SellerAccountDetails></SellerInformationDetails>`, 1)
		data = strings.Replace(data, `<EpiBeneficiaryPartyDetails>`, `<EpiBeneficiaryPartyDetails><EpiNameAddressDetails>Perintä Oy</EpiNameAddressDetails>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		require.Len(t, inv.Payment.Instructions.CreditTransfer, 2)
		assert.Equal(t, cbc.Code("FI4250001510000023"), inv.Payment.Instructions.CreditTransfer[1].IBAN)
		assert.Equal(t, cbc.Code("OKOYFIHH"), inv.Payment.Instructions.CreditTransfer[1].BIC)
		assert.Equal(t, "Perintä Oy", inv.Payment.Payee.Name)
	})
}

// TestParseRejectsOtherDocuments guards the entry point.
func TestParseRejectsOtherDocuments(t *testing.T) {
	_, err := fifinvoice.Parse([]byte(`<?xml version="1.0"?><Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"/>`))
	require.ErrorContains(t, err, "Invoice")
	_, err = fifinvoice.Parse([]byte("not xml"))
	require.Error(t, err)
	_, err = fifinvoice.Parse([]byte(`<Finvoice Version="3.0"><SellerPartyDetails><SellerOrganisationName>X</SellerOrganisationName></SellerPartyDetails></Finvoice>`))
	require.ErrorContains(t, err, "InvoiceDetails")
}
