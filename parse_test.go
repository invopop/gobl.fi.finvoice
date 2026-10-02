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
	decl, frame, sellerUnit, buyerUnit, details, vat, rows, total, roundoff, currency, note string
	// epi replaces the payment order instruction, whose amount otherwise
	// follows the total.
	epi string
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

func parseDelivery(t *testing.T, d delivery) *bill.Invoice {
	t.Helper()
	env, err := fifinvoice.Parse(d.bytes())
	require.NoError(t, err)
	require.NoError(t, env.Validate())
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
		{"debit note", "INV01", "383", bill.InvoiceTypeDebitNote, ""},
		{"self-billed credit note", "INV02", "261", bill.InvoiceTypeCreditNote, tax.TagSelfBilled},
		{"prepayment", "INV01", "386", bill.InvoiceTypeStandard, tax.TagPrepayment},
		{"factored", "INV01", "393", bill.InvoiceTypeStandard, tax.TagFactoring},
		{"factored credit note", "INV02", "396", bill.InvoiceTypeCreditNote, tax.TagFactoring},
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
			if tt.tag == "" {
				assert.Empty(t, inv.GetTags())
			} else {
				assert.Equal(t, []cbc.Key{tt.tag}, inv.GetTags())
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

	t.Run("a zero total keeps the rows' own signs", func(t *testing.T) {
		rows := defaultRows + strings.Replace(strings.Replace(strings.Replace(defaultRows, "Konsultointi", "Hyvitys", 1), "<InvoicedQuantity>2<", "<InvoicedQuantity>-2<", 1), ">100,00<", ">-100,00<", 1)
		inv := parseDelivery(t, delivery{details: details, rows: rows, total: "0,00"})
		assert.Equal(t, "-2", inv.Lines[0].Quantity.String(), "a positive Finvoice row on a credit note is a charge")
		assert.Equal(t, "2", inv.Lines[1].Quantity.String(), "a negative one is the credit")
	})
	t.Run("a mismatch is reported with the document's signs", func(t *testing.T) {
		rows := strings.Replace(strings.Replace(defaultRows, "<InvoicedQuantity>2<", "<InvoicedQuantity>-2<", 1), ">100,00<", ">-100,00<", 1)
		_, err := fifinvoice.Parse(delivery{details: details, rows: rows, total: "-999,00"}.bytes())
		require.ErrorContains(t, err, "stated total -999.00 does not match the rows, which add up to -125.50")
	})
	t.Run("nothing to pay", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Korjaus</ArticleName><InvoicedQuantity>0</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		env, err := fifinvoice.Parse(delivery{details: details, rows: rows, total: "0,00"}.bytes())
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
				inv := parseDelivery(t, delivery{details: details, rows: rows, total: "-125,50"})
				assert.Equal(t, "2", inv.Lines[0].Quantity.String())
				assert.Zero(t, inv.Lines[0].Item.Price.Compare(num.MakeAmount(5000, 2)), "price %s", inv.Lines[0].Item.Price)
				assert.Equal(t, "125.50", inv.Totals.Payable.String())
			})
		}
	})
}

func TestParseAddresses(t *testing.T) {
	tests := []struct{ name, unit, uri string }{
		{"OVT", "003776543212", "iso6523-actorid-upis::0216:003776543212"},
		{"OVT with a unit suffix", "003776543212AB1", "iso6523-actorid-upis::0216:003776543212AB1"},
		{"IBAN", "FI2112345600000785", "iso6523-actorid-upis::9918:FI2112345600000785"},
		{"Y-tunnus", "7654321-2", "iso6523-actorid-upis::0212:7654321-2"},
		{"Y-tunnus without its hyphen", "76543212", "iso6523-actorid-upis::0212:76543212"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := parseDelivery(t, delivery{sellerUnit: "<SellerOrganisationUnitNumber>" + tt.unit + "</SellerOrganisationUnitNumber>"})
			require.Len(t, inv.Supplier.Endpoints, 1)
			assert.Equal(t, cbc.URI(tt.uri), inv.Supplier.Endpoints[0].URI)
		})
	}
	t.Run("a Finnish shape with a wrong check digit is kept as an inbox", func(t *testing.T) {
		for _, unit := range []string{"12345674", "003712345674"} {
			inv := parseDelivery(t, delivery{sellerUnit: "<SellerOrganisationUnitNumber>" + unit + "</SellerOrganisationUnitNumber>"})
			assert.Empty(t, inv.Supplier.Endpoints, unit)
			require.Len(t, inv.Supplier.Inboxes, 1, unit)
			assert.Equal(t, cbc.Code(unit), inv.Supplier.Inboxes[0].Code)
		}
	})
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

		t.Run("even with an address of an unknown shape", func(t *testing.T) {
			frame := strings.Replace(frame, `<FromIdentifier SchemeID="0216">003799999999</FromIdentifier>`, `<FromIdentifier>LASKUT</FromIdentifier>`, 1)
			inv := parseDelivery(t, delivery{frame: frame, sellerUnit: "<SellerOrganisationUnitNumber>003776543212</SellerOrganisationUnitNumber>"})
			assert.Empty(t, inv.Supplier.Endpoints)
			require.Len(t, inv.Supplier.Inboxes, 1)
			assert.Equal(t, cbc.Code("LASKUT"), inv.Supplier.Inboxes[0].Code)
		})
	})
	t.Run("a self-billed invoice is sent by the customer", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV07</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		frame := `<MessageTransmissionDetails><MessageSenderDetails><FromIdentifier SchemeID="0216">003745678907</FromIdentifier><FromIntermediator>` + receiverOperator + `</FromIntermediator></MessageSenderDetails><MessageReceiverDetails><ToIdentifier SchemeID="0216">003776543212</ToIdentifier><ToIntermediator>HELSFIHH</ToIntermediator></MessageReceiverDetails><MessageDetails><MessageIdentifier>1</MessageIdentifier><MessageTimeStamp>2026-09-22T11:12:12+03:00</MessageTimeStamp></MessageDetails></MessageTransmissionDetails>`
		inv := parseDelivery(t, delivery{details: details, frame: frame})
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
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

		t.Run("with the declaration in front of the frame", func(t *testing.T) {
			decl := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
			data := decl + soap + strings.TrimPrefix(string(delivery{}.bytes()), decl)
			env, err := fifinvoice.Parse([]byte(data))
			require.NoError(t, err)
			inv := env.Extract().(*bill.Invoice)
			assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
			assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
		})
	})
}

func TestParseIdentities(t *testing.T) {
	t.Run("a VAT number in any common form is kept for validation", func(t *testing.T) {
		tests := []struct {
			name, code, want string
			valid            bool
		}{
			{"compact", "FI76543212", "FI76543212", true},
			{"spaced", "FI 76543212", "FI76543212", true},
			{"hyphenated", "FI7654321-2", "FI76543212", true},
			{"bad check digit", "FI76543213", "FI76543213", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				data := strings.Replace(string(delivery{}.bytes()), "<SellerOrganisationTaxCode>FI76543212<", "<SellerOrganisationTaxCode>"+tt.code+"<", 1)
				env, err := fifinvoice.Parse([]byte(data))
				require.NoError(t, err)
				inv := env.Extract().(*bill.Invoice)
				require.NotNil(t, inv.Supplier.TaxID)
				assert.Equal(t, tt.want, inv.Supplier.TaxID.String())
				if tt.valid {
					assert.NoError(t, env.Validate())
				} else {
					assert.ErrorContains(t, env.Validate(), "supplier.tax_id")
				}
			})
		}
	})
	t.Run("padded codes in attributes are read collapsed", func(t *testing.T) {
		data := string(delivery{noVATNumber: true}.bytes())
		data = strings.Replace(data, `AmountCurrencyIdentifier="EUR">125,50</InvoiceTotalVatIncludedAmount>`, `AmountCurrencyIdentifier=" EUR ">125,50</InvoiceTotalVatIncludedAmount>`, 1)
		data = strings.Replace(data, "<CountryCode>FI</CountryCode>", "<CountryCode> FI </CountryCode>", 1)
		data = strings.Replace(data, "<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier>", `<SellerPartyIdentifier SchemeID=" 0212 ">7654321-2</SellerPartyIdentifier>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, currency.EUR, inv.Currency)
		assert.Equal(t, "FI", inv.Supplier.Addresses[0].Country.String())
		assert.Equal(t, cbc.Code("0212"), inv.Supplier.Identities[0].Ext.Get(iso.ExtKeySchemeID))
	})
	t.Run("a contact email and a common email are both kept", func(t *testing.T) {
		seller := func(common string) string {
			return `</SellerPartyDetails><SellerCommunicationDetails><SellerEmailaddressIdentifier>matti@myyja.fi</SellerEmailaddressIdentifier></SellerCommunicationDetails><SellerInformationDetails><SellerCommonEmailaddressIdentifier>` + common + `</SellerCommonEmailaddressIdentifier></SellerInformationDetails>`
		}
		for common, want := range map[string][]string{
			"laskutus@myyja.fi": {"matti@myyja.fi", "laskutus@myyja.fi"},
			"matti@myyja.fi":    {"matti@myyja.fi"},
		} {
			data := strings.Replace(string(delivery{}.bytes()), "</SellerPartyDetails>", seller(common), 1)
			env, err := fifinvoice.Parse([]byte(data))
			require.NoError(t, err)
			var got []string
			for _, e := range env.Extract().(*bill.Invoice).Supplier.Emails {
				got = append(got, e.Address)
			}
			assert.Equal(t, want, got)
		}
	})
	t.Run("a delivery party's Y-tunnus gets its scheme from its address", func(t *testing.T) {
		party := `<DeliveryPartyDetails><DeliveryPartyIdentifier>7654321-2</DeliveryPartyIdentifier><DeliveryOrganisationName>Varasto Oy</DeliveryOrganisationName><DeliveryPostalAddressDetails><DeliveryStreetName>Satamakatu 3</DeliveryStreetName><DeliveryTownName>Turku</DeliveryTownName><DeliveryPostCodeIdentifier>20100</DeliveryPostCodeIdentifier><CountryCode>FI</CountryCode></DeliveryPostalAddressDetails></DeliveryPartyDetails>`
		data := strings.Replace(string(delivery{}.bytes()), "<InvoiceDetails>", party+"<InvoiceDetails>", 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		receiver := env.Extract().(*bill.Invoice).Delivery.Receiver
		require.Len(t, receiver.Identities, 1)
		assert.Equal(t, cbc.Code("0212"), receiver.Identities[0].Ext.Get(iso.ExtKeySchemeID))
	})
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
		data := strings.Replace(string(d.bytes()), `</SellerOrganisationName>`, `</SellerOrganisationName><SellerOrganisationTaxCode>7654321-2</SellerOrganisationTaxCode>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Nil(t, inv.Supplier.TaxID)
	})
}

// TestParsePeriods checks a period with only one of its dates is kept, as
// both are optional in Finvoice and in GOBL.
// TestParseTypeCodesWithWhitespace checks padded codes still set the tags.
func TestParseTypeCodesWithWhitespace(t *testing.T) {
	details := "<InvoiceTypeCode>\n INV07\n</InvoiceTypeCode><InvoiceTypeCodeUN>\n 389\n</InvoiceTypeCodeUN><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>"
	inv := parseDelivery(t, delivery{details: details})
	assert.Equal(t, []cbc.Key{tax.TagSelfBilled}, inv.GetTags())
}

// TestParseAgreement checks a contract reference keeps its date.
func TestParseAgreement(t *testing.T) {
	details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><AgreementIdentifier>SOP-9</AgreementIdentifier><AgreementDate Format="CCYYMMDD">20260115</AgreementDate>`
	inv := parseDelivery(t, delivery{details: details})
	require.Len(t, inv.Ordering.Contracts, 1)
	assert.Equal(t, cbc.Code("SOP-9"), inv.Ordering.Contracts[0].Code)
	assert.Equal(t, "2026-01-15", inv.Ordering.Contracts[0].IssueDate.String())
}

func TestParsePeriods(t *testing.T) {
	details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><InvoicingPeriodStartDate Format="CCYYMMDD">20260901</InvoicingPeriodStartDate>`
	rows := strings.Replace(defaultRows, "<RowVatRatePercent>", `<EndDate Format="CCYYMMDD">20260930</EndDate><RowVatRatePercent>`, 1)
	inv := parseDelivery(t, delivery{details: details, rows: rows})

	require.NotNil(t, inv.Ordering)
	require.NotNil(t, inv.Ordering.Period)
	assert.Equal(t, "2026-09-01", inv.Ordering.Period.Start.String())
	assert.Nil(t, inv.Ordering.Period.End)
	require.NotNil(t, inv.Lines[0].Period)
	assert.Nil(t, inv.Lines[0].Period.Start)
	assert.Equal(t, "2026-09-30", inv.Lines[0].Period.End.String())
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
		assert.Equal(t, "7.50", line.Item.Price.String(), "price from the row total")
		assert.Equal(t, cbc.Code("ZP"), line.Item.Ext.Get(untdid.ExtKeyUnit))
		assert.Equal(t, tax.KeyExempt, line.Taxes[0].Key, "category from the breakdown")
		assert.Equal(t, cbc.Code("VATEX-EU-132-1C"), line.Taxes[0].Ext.Get(cef.ExtKeyVATEX))
		assert.Equal(t, "14 pv netto", inv.Payment.Terms.Notes)
	})
	t.Run("price per base quantity", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Sähkö</ArticleName><InvoicedQuantity QuantityUnitCode="kWh">2000</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><UnitPriceBaseQuantity>1000</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "0.05", inv.Lines[0].Item.Price.String())
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
		assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "125.50", inv.Totals.Payable.String())
	})
	t.Run("rows without a code take the category the breakdown gives at their rate", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>` +
			`<InvoiceRow><ArticleName>B</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>25,5</VatRatePercent><VatCode>S</VatCode></VatSpecificationDetails><VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatFreeText>Terveydenhuoltopalvelu</VatFreeText></VatSpecificationDetails>`
		inv := parseDelivery(t, delivery{rows: rows, vat: vat, total: "225,50"})
		assert.Equal(t, tax.KeyStandard, inv.Lines[0].Taxes[0].Key)
		assert.Equal(t, tax.KeyExempt, inv.Lines[1].Taxes[0].Key)
	})
	t.Run("an unknown UN unit code falls back to the unit word", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Työ</ArticleName><InvoicedQuantity QuantityUnitCode="h" QuantityUnitCodeUN="ZP">2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "125,50"})
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
		assert.Empty(t, inv.Lines[0].Item.Ext.Get(untdid.ExtKeyUnit))
	})
	t.Run("a subtotal row made of sub-rows is left out, as the totals leave it out", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><SubInvoiceRow><SubArticleName>Yhteensä</SubArticleName><SubRowAmount AmountCurrencyIdentifier="EUR">100,00</SubRowAmount></SubInvoiceRow></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		require.Len(t, inv.Lines, 1)
		assert.Empty(t, inv.Notes)
	})
	t.Run("a quantity without a unit takes the price's", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Työ</ArticleName><InvoicedQuantity>2</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" QuantityUnitCodeUN="HUR">50,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "125,50"})
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)

		rows = `<InvoiceRow><ArticleName>Työ</ArticleName><UnitPriceAmount AmountCurrencyIdentifier="EUR" UnitPriceUnitCode="h">100,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv = parseDelivery(t, delivery{rows: rows, total: "125,50"})
		assert.Equal(t, "1", inv.Lines[0].Quantity.String())
		assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
	})
	t.Run("several quantities take the one in the price's unit word", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Hiekka</ArticleName><InvoicedQuantity QuantityUnitCode="h">2</InvoicedQuantity><InvoicedQuantity QuantityUnitCode="kg">10</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" UnitPriceUnitCode="kg">5,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">50,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "62,75"})
		assert.Equal(t, "10", inv.Lines[0].Quantity.String())
		assert.Equal(t, "5.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, org.UnitKilogram, inv.Lines[0].Item.Unit)
	})
	t.Run("several quantities take the one in the price's unit", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Kynä</ArticleName><InvoicedQuantity QuantityUnitCode="pkt" QuantityUnitCodeUN="XPK">1</InvoicedQuantity><InvoicedQuantity QuantityUnitCode="kpl" QuantityUnitCodeUN="C62">10</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR" QuantityUnitCodeUN="C62">5,00</UnitPriceAmount><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">50,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "62,75"})
		assert.Equal(t, "10", inv.Lines[0].Quantity.String())
		assert.Equal(t, "5.00", inv.Lines[0].Item.Price.String())
		assert.Equal(t, org.UnitOne, inv.Lines[0].Item.Unit)
	})
	t.Run("an exemption text comes from a row of that category", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>A</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Asennus asiakkaan tiloissa</RowFreeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>` +
			`<InvoiceRow><ArticleName>B</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowFreeText>Terveydenhuoltopalvelu, AVL 34 §</RowFreeText><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		vat := `<VatSpecificationDetails><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode></VatSpecificationDetails>`
		inv := parseDelivery(t, delivery{rows: rows, vat: vat, total: "225,50"})
		require.Len(t, inv.Tax.Notes, 1)
		assert.Equal(t, "Terveydenhuoltopalvelu, AVL 34 §", inv.Tax.Notes[0].Text)
	})
	t.Run("a free item row has a price of zero", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Kaupan päälle</ArticleName><InvoicedQuantity>2</InvoicedQuantity><RowDiscountPercent>100</RowDiscountPercent><RowDiscountTypeText>Lahja</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "0.00", inv.Lines[1].Item.Price.String())
	})
	t.Run("a row without a name takes its reference or its position", func(t *testing.T) {
		unnamed := strings.Replace(defaultRows, "<ArticleName>Konsultointi</ArticleName>", "", 1)
		referenced := strings.Replace(defaultRows, "<ArticleName>Konsultointi</ArticleName>", "<ArticleIdentifier>K-1</ArticleIdentifier>", 1)
		inv := parseDelivery(t, delivery{rows: unnamed + referenced, total: "251,00"})
		assert.Equal(t, "Row 1", inv.Lines[0].Item.Name)
		assert.Equal(t, "K-1", inv.Lines[1].Item.Name)
	})
	t.Run("a stated amount wins over a percentage that cannot give it", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Laite</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3000,00</UnitPriceAmount><RowDiscountPercent>3,333</RowDiscountPercent><RowDiscountAmount AmountCurrencyIdentifier="EUR">100,00</RowDiscountAmount><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">2900,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "3639,50"})
		line := inv.Lines[0]
		assert.Equal(t, "3000.00", line.Item.Price.String())
		assert.Nil(t, line.Discounts[0].Percent)
		assert.Equal(t, "100.00", line.Discounts[0].Amount.String())
	})
	t.Run("a percentage that gives the stated amount is kept", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Laite</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3000,00</UnitPriceAmount><RowDiscountPercent>10</RowDiscountPercent><RowDiscountAmount AmountCurrencyIdentifier="EUR">300,00</RowDiscountAmount><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">2700,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "3388,50"})
		require.NotNil(t, inv.Lines[0].Discounts[0].Percent)
		assert.Equal(t, "10%", inv.Lines[0].Discounts[0].Percent.String())
	})
	t.Run("a price is checked the way GOBL rounds the line", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Osa</ArticleName><InvoicedQuantity>3</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">3,335</UnitPriceAmount><RowDiscountPercent>10</RowDiscountPercent><RowDiscountTypeText>Alennus</RowDiscountTypeText><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">9,01</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "11,31"})
		assert.Equal(t, "3.335", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "9.01", inv.Lines[0].Total.String())
	})
	t.Run("a row with a quantity and nothing to price is kept as a note", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Sisältää suodattimen</ArticleName><DeliveredQuantity>1</DeliveredQuantity></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		require.Len(t, inv.Lines, 1)
		assert.Equal(t, "Sisältää suodattimen", inv.Notes[0].Text)
	})
	t.Run("a lump sum keeps the currency's decimals", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Asennus</ArticleName><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "100.00", inv.Lines[0].Item.Price.String())
	})
	t.Run("a zero rate with no VAT code anywhere is zero-rated", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Palvelu</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "100,00"})
		assert.Equal(t, tax.KeyZero, inv.Lines[0].Taxes[0].Key)
	})
	t.Run("an amount in another currency is refused", func(t *testing.T) {
		tests := []struct{ name, rows string }{
			{"row total", strings.Replace(defaultRows, `<RowVatExcludedAmount AmountCurrencyIdentifier="EUR">`, `<RowVatExcludedAmount AmountCurrencyIdentifier="USD">`, 1)},
			{"unit price", strings.Replace(defaultRows, `<UnitPriceAmount AmountCurrencyIdentifier="EUR">`, `<UnitPriceAmount AmountCurrencyIdentifier="USD">`, 1)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := fifinvoice.Parse(delivery{rows: tt.rows}.bytes())
				require.ErrorContains(t, err, "is in USD, the document in EUR")
			})
		}
		t.Run("payment order", func(t *testing.T) {
			epi := strings.Replace(epiInstruction("125,50", "20261006", ""), `AmountCurrencyIdentifier="EUR"`, `AmountCurrencyIdentifier="USD"`, 1)
			_, err := fifinvoice.Parse(delivery{epi: epi}.bytes())
			require.ErrorContains(t, err, "is in USD, the document in EUR")
		})
	})
	t.Run("a description alone is kept as a note", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleDescription>Toimitus sisältää asennuksen</ArticleDescription></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		require.Len(t, inv.Notes, 1)
		assert.Equal(t, "Toimitus sisältää asennuksen", inv.Notes[0].Text)
	})
	t.Run("a price finer than four decimals comes from the row total", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>API-kutsut</ArticleName><InvoicedQuantity>100000</InvoicedQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">1,00</RowVatExcludedAmount></InvoiceRow>`
		env, err := fifinvoice.Parse(delivery{rows: rows, total: "1,26"}.bytes())
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
				env, err := fifinvoice.Parse(delivery{rows: rows, total: tt.total}.bytes())
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
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, org.UnitPiece, inv.Lines[0].Item.Unit)
	})
	t.Run("the unit t could be hours or tonnes and stays generic", func(t *testing.T) {
		rows := strings.Replace(defaultRows, "<InvoicedQuantity>", `<InvoicedQuantity QuantityUnitCode="t">`, 1)
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, org.UnitOne, inv.Lines[0].Item.Unit)
	})
	t.Run("a row total with no price and no quantity prices at nothing", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Otsikko</ArticleName><InvoicedQuantity>0</InvoicedQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">0,00</RowVatExcludedAmount></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "0.00", inv.Lines[1].Item.Price.String())

		_, err := fifinvoice.Parse(delivery{rows: strings.Replace(rows, ">0,00<", ">5,00<", 1), total: "131,78"}.bytes())
		require.ErrorContains(t, err, "stated total 131.78 does not match")
	})
	t.Run("a price per a base that is no power of ten", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Pullot</ArticleName><InvoicedQuantity>300</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">10,00</UnitPriceAmount><UnitPriceBaseQuantity>3</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "1255,00"})
		assert.Equal(t, "1000.00", inv.Lines[0].Total.String())
	})
	t.Run("a price per a base quantity keeps its digits", func(t *testing.T) {
		rows := `<InvoiceRow><ArticleName>Nastat</ArticleName><InvoicedQuantity>10000</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">1,23</UnitPriceAmount><UnitPriceBaseQuantity>1000</UnitPriceBaseQuantity><RowVatRatePercent>25,5</RowVatRatePercent><RowVatCode>S</RowVatCode></InvoiceRow>`
		inv := parseDelivery(t, delivery{rows: rows, total: "15,44"})
		assert.Equal(t, "0.00123", inv.Lines[0].Item.Price.String())
		assert.Equal(t, "12.30", inv.Lines[0].Total.String())
	})
	t.Run("two exemption reasons in one category cannot be told apart", func(t *testing.T) {
		row := func(name string) string {
			return `<InvoiceRow><ArticleName>` + name + `</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		}
		spec := func(vatex string) string {
			return `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatExemptionReasonCode>` + vatex + `</VatExemptionReasonCode></VatSpecificationDetails>`
		}
		_, err := fifinvoice.Parse(delivery{rows: row("Hoito") + row("Koulutus"), vat: spec("VATEX-EU-132") + spec("VATEX-EU-135"), total: "200,00"}.bytes())
		require.ErrorContains(t, err, "VAT breakdown gives E two exemption reasons, VATEX-EU-132 and VATEX-EU-135")
	})
	t.Run("an exemption text the breakdown repeats is one note", func(t *testing.T) {
		row := func(name string) string {
			return `<InvoiceRow><ArticleName>` + name + `</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>0</RowVatRatePercent><RowVatCode>E</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		}
		spec := func(text string) string {
			return `<VatSpecificationDetails><VatBaseAmount AmountCurrencyIdentifier="EUR">100,00</VatBaseAmount><VatRatePercent>0</VatRatePercent><VatCode>E</VatCode><VatRateAmount AmountCurrencyIdentifier="EUR">0,00</VatRateAmount><VatFreeText>` + text + `</VatFreeText></VatSpecificationDetails>`
		}
		inv := parseDelivery(t, delivery{rows: row("Hoito") + row("Koulutus"), vat: spec("AVL 34 §") + spec("AVL 34 §"), total: "200,00"})
		require.Len(t, inv.Tax.Notes, 1)
		assert.Equal(t, "AVL 34 §", inv.Tax.Notes[0].Text)

		env, err := fifinvoice.Parse(delivery{rows: row("Hoito") + row("Koulutus"), vat: spec("Terveydenhuolto") + spec("Koulutus"), total: "200,00"}.bytes())
		require.NoError(t, err)
		require.Len(t, env.Extract().(*bill.Invoice).Tax.Notes, 2)
		require.ErrorContains(t, env.Validate(), "one exemption reason per VAT category")
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
	t.Run("unknown VAT code in the breakdown", func(t *testing.T) {
		vat := `<VatSpecificationDetails><VatRatePercent>25,5</VatRatePercent><VatCode>L</VatCode></VatSpecificationDetails>`
		_, err := fifinvoice.Parse(delivery{vat: vat}.bytes())
		require.ErrorContains(t, err, `VAT breakdown: unknown VAT category code "L"`)
	})
	t.Run("a priced row without a row total is quantity times price", func(t *testing.T) {
		rows := strings.Replace(defaultRows, `<RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount>`, "", 1)
		inv := parseDelivery(t, delivery{rows: rows})
		assert.Equal(t, "100.00", inv.Lines[0].Total.String())
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
	t.Run("one declaration in front of a frame serves both documents", func(t *testing.T) {
		decl := `<?xml version="1.0" encoding="ISO-8859-15"?>` + "\n"
		frame := `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eb="http://www.oasis-open.org/committees/ebxml-msg/schema/msg-header-2_0.xsd"><SOAP-ENV:Header><eb:MessageHeader>` +
			`<eb:From><eb:PartyId>003776543212</eb:PartyId><eb:Role>Sender</eb:Role></eb:From><eb:From><eb:PartyId>HELSFIHH</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:From>` +
			`<eb:To><eb:PartyId>003745678907</eb:PartyId><eb:Role>Receiver</eb:Role></eb:To><eb:To><eb:PartyId>` + receiverOperator + `</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:To>` +
			`<eb:Service>Lähetys €</eb:Service></eb:MessageHeader></SOAP-ENV:Header><SOAP-ENV:Body/></SOAP-ENV:Envelope>` + "\n"
		body := strings.TrimPrefix(string(delivery{note: "Hinta 10 €"}.bytes()), `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
		data, err := charmap.ISO8859_15.NewEncoder().Bytes([]byte(decl + frame + body))
		require.NoError(t, err)
		env, err := fifinvoice.Parse(data)
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
		assert.Equal(t, "Hinta 10 €", inv.Notes[0].Text)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
	})
}

func TestParseTotals(t *testing.T) {
	t.Run("a document discount per VAT rate keeps its amounts", func(t *testing.T) {
		rows := defaultRows + `<InvoiceRow><ArticleName>Kirja</ArticleName><InvoicedQuantity>1</InvoicedQuantity><UnitPriceAmount AmountCurrencyIdentifier="EUR">100,00</UnitPriceAmount><RowVatRatePercent>14</RowVatRatePercent><RowVatCode>S</RowVatCode><RowVatExcludedAmount AmountCurrencyIdentifier="EUR">100,00</RowVatExcludedAmount></InvoiceRow>`
		discount := func(rate string) string {
			return `<DiscountDetails><FreeText>Alennus</FreeText><Percent>5</Percent><Amount AmountCurrencyIdentifier="EUR">5,00</Amount><VatCategoryCode>S</VatCategoryCode><VatRatePercent>` + rate + `</VatRatePercent></DiscountDetails>`
		}
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>LASKU</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>` + discount("25,5") + discount("14")
		env, err := fifinvoice.Parse(delivery{details: details, rows: rows, total: "227,53"}.bytes())
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		for _, d := range inv.Discounts {
			assert.Nil(t, d.Percent)
			assert.Equal(t, "5.00", d.Amount.String())
		}
	})
	t.Run("a difference within a subunit per row and one more is rounding", func(t *testing.T) {
		tests := []struct {
			name, rows, total, rounding string
		}{
			{"one row, two subunits", defaultRows, "125,52", "0.02"},
			{"two rows, three subunits", defaultRows + defaultRows, "251,03", "0.03"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				inv := parseDelivery(t, delivery{rows: tt.rows, total: tt.total})
				assert.Equal(t, strings.Replace(tt.total, ",", ".", 1), inv.Totals.Payable.String())
				assert.Equal(t, tt.rounding, inv.Totals.Rounding.String())
			})
		}
	})
	t.Run("stated roundoff", func(t *testing.T) {
		inv := parseDelivery(t, delivery{roundoff: "-0,50", epi: epiInstruction("125,00", "20261006", "")})
		assert.Equal(t, "125.00", inv.Totals.Payable.String())
		assert.Equal(t, "-0.50", inv.Totals.Rounding.String())
	})
	t.Run("stated roundoff with a difference on top", func(t *testing.T) {
		inv := parseDelivery(t, delivery{total: "125,51", roundoff: "-0,50", epi: epiInstruction("125,01", "20261006", "")})
		assert.Equal(t, "125.01", inv.Totals.Payable.String())
		assert.Equal(t, "-0.49", inv.Totals.Rounding.String())
	})
	t.Run("currency from the total", func(t *testing.T) {
		inv := parseDelivery(t, delivery{currency: "SEK"})
		assert.Equal(t, currency.SEK, inv.Currency)
	})
	t.Run("text rows alone parse but make no invoice", func(t *testing.T) {
		rows := `<InvoiceRow><RowFreeText>Ei laskutettavaa</RowFreeText></InvoiceRow>`
		env, err := fifinvoice.Parse(delivery{rows: rows, total: "0,00"}.bytes())
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
				_, err := fifinvoice.Parse(delivery{rows: tt.rows, total: tt.total}.bytes())
				require.ErrorContains(t, err, "stated total "+strings.Replace(tt.total, ",", ".", 1)+" does not match")
			})
		}
	})
	t.Run("text rows alone with a stated total are refused", func(t *testing.T) {
		rows := `<InvoiceRow><RowFreeText>Ei laskutettavaa</RowFreeText></InvoiceRow>`
		_, err := fifinvoice.Parse(delivery{rows: rows, total: "10,00"}.bytes())
		require.ErrorContains(t, err, "stated total 10.00 does not match")
	})
	t.Run("unknown currency is refused", func(t *testing.T) {
		_, err := fifinvoice.Parse(delivery{currency: "XXX"}.bytes())
		require.ErrorContains(t, err, `unknown currency "XXX"`)
	})
	t.Run("malformed dates are refused", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber><OrderIdentifier>PO-1</OrderIdentifier><OrderDate Format="CCYYMMDD">20261301</OrderDate>`
		_, err := fifinvoice.Parse(delivery{details: details}.bytes())
		require.ErrorContains(t, err, `date "20261301"`)
	})
}

func TestParsePayment(t *testing.T) {
	t.Run("payment means codes", func(t *testing.T) {
		tests := []struct {
			code, ext string
			key       cbc.Key
		}{
			{"58", "58", pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA)},
			{"30", "30", pay.MeansKeyCreditTransfer},
			{"49", "49", pay.MeansKeyDirectDebit},
			{"42", "30", pay.MeansKeyCreditTransfer},
		}
		for _, tt := range tests {
			t.Run(tt.code, func(t *testing.T) {
				epi := epiInstruction("125,50", "20261006", `<EpiPaymentMeansCode>`+tt.code+`</EpiPaymentMeansCode><EpiPaymentMeansText>Tilisiirto</EpiPaymentMeansText>`)
				env, err := fifinvoice.Parse(delivery{epi: epi}.bytes())
				require.NoError(t, err)
				inv := env.Extract().(*bill.Invoice)
				assert.Equal(t, tt.key, inv.Payment.Instructions.Key)
				assert.Equal(t, cbc.Code(tt.ext), inv.Payment.Instructions.Ext.Get(untdid.ExtKeyPaymentMeans), "GOBL pairs the code with the key")
				assert.Equal(t, "Tilisiirto", inv.Payment.Instructions.Detail)
				if tt.key.Has(pay.MeansKeyCreditTransfer) {
					assert.NoError(t, env.Validate())
				} else {
					assert.ErrorContains(t, env.Validate(), "payment instructions key must be credit-transfer", "the addon takes credit transfers only")
				}
			})
		}
	})
	t.Run("the legacy instruction code names the payment means", func(t *testing.T) {
		epi := strings.Replace(epiInstruction("125,50", "20261006", ""), "<EpiInstructedAmount", "<EpiInstructionCode>58</EpiInstructionCode><EpiInstructedAmount", 1)
		inv := parseDelivery(t, delivery{epi: epi})
		assert.Equal(t, pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA), inv.Payment.Instructions.Key)
	})
	t.Run("a BBAN account", func(t *testing.T) {
		data := strings.Replace(string(delivery{}.bytes()), `<EpiAccountID IdentificationSchemeName="IBAN">FI2112345600000785</EpiAccountID>`, `<EpiAccountID IdentificationSchemeName="BBAN">12345600000785</EpiAccountID>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		ct := env.Extract().(*bill.Invoice).Payment.Instructions.CreditTransfer[0]
		assert.Equal(t, cbc.Code("12345600000785"), ct.Number)
		assert.Empty(t, ct.IBAN)
	})
	t.Run("the payment order's date is the due date", func(t *testing.T) {
		inv := parseDelivery(t, delivery{epi: epiInstruction("125,50", "20261020", "")})
		assert.Equal(t, "2026-10-20", inv.Payment.Terms.DueDates[0].Date.String())
	})
	t.Run("the payment order's amount is the sender's to state", func(t *testing.T) {
		inv := parseDelivery(t, delivery{epi: epiInstruction("100,00", "20261006", "")})
		assert.Equal(t, "125.50", inv.Totals.Payable.String())
	})
	t.Run("a second due date is refused", func(t *testing.T) {
		terms := `<PaymentTermsDetails><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails><PaymentTermsDetails><InvoiceDueDate Format="CCYYMMDD">20261106</InvoiceDueDate></PaymentTermsDetails>`
		data := strings.Replace(string(delivery{}.bytes()), `<PaymentTermsDetails><PaymentTermsFreeText>14 pv</PaymentTermsFreeText><PaymentTermsFreeText>netto</PaymentTermsFreeText><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails>`, terms, 1)
		_, err := fifinvoice.Parse([]byte(data))
		require.ErrorContains(t, err, "payment terms give 2 due dates, the payment order takes one")
	})
	t.Run("a seller name cut to the payment order is no payee", func(t *testing.T) {
		name := "Helsingin Kaupungin Asuntotuotanto Oy Ab"
		data := strings.Replace(string(delivery{}.bytes()), "<SellerOrganisationName>Lähettäjä Oy<", "<SellerOrganisationName>"+name+"<", 1)
		data = strings.Replace(data, "<EpiBeneficiaryPartyDetails>", "<EpiBeneficiaryPartyDetails><EpiNameAddressDetails>Helsingin Kaupungin Asuntotuotanto</EpiNameAddressDetails>", 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		assert.Nil(t, env.Extract().(*bill.Invoice).Payment.Payee)
	})
	t.Run("further accounts and the payee", func(t *testing.T) {
		d := delivery{}
		data := strings.Replace(string(d.bytes()), `</SellerPartyDetails>`, `</SellerPartyDetails><SellerInformationDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">fi21 1234 5600 0007 85</SellerAccountID><SellerBic IdentificationSchemeName="BIC">NDEAFIHH</SellerBic></SellerAccountDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">FI4250001510000023</SellerAccountID><SellerBic IdentificationSchemeName="BIC">OKOYFIHH</SellerBic></SellerAccountDetails></SellerInformationDetails>`, 1)
		data = strings.Replace(data, `<EpiBeneficiaryPartyDetails>`, `<EpiBeneficiaryPartyDetails><EpiNameAddressDetails>Perintä Oy</EpiNameAddressDetails>`, 1)
		env, err := fifinvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		require.Len(t, inv.Payment.Instructions.CreditTransfer, 2)
		assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Payment.Instructions.CreditTransfer[0].BIC, "the BIC the seller's list gives the payment order's account")
		assert.Equal(t, cbc.Code("FI4250001510000023"), inv.Payment.Instructions.CreditTransfer[1].IBAN)
		assert.Equal(t, cbc.Code("OKOYFIHH"), inv.Payment.Instructions.CreditTransfer[1].BIC)
		assert.Equal(t, "Perintä Oy", inv.Payment.Payee.Name)
	})
}

// TestParseRefusals pins what the parser refuses and how it says so.
func TestParseRefusals(t *testing.T) {
	valid := string(delivery{}.bytes())
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
			_, err := fifinvoice.Parse([]byte(tt.data))
			require.ErrorContains(t, err, tt.want)
		})
	}
}
