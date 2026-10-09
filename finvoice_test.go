package finvoice_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/invopop/gobl"
	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/pkg/examples"
	"github.com/invopop/gobl/tax"
	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Made-up operator codes: the sender's, and the one the example customer
// names.
const (
	senderOperator   = "003700010001"
	receiverOperator = "003700020002"
)

// testOptions keep the transmission details deterministic for golden comparison.
func testOptions() []finvoice.Option {
	return []finvoice.Option{
		finvoice.WithSenderOperator(senderOperator),
		finvoice.WithMessageTime(time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)),
		finvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
	}
}

// convertCase is an example document and the Finvoice XML it should produce.
type convertCase struct {
	name, src, golden string
}

// convertCases lists the example sources; each converts from a freshly
// calculated envelope so the goldens never lag the examples.
func convertCases(t *testing.T) []convertCase {
	t.Helper()
	sources, err := examples.Sources("examples")
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	var cases []convertCase
	for _, src := range sources {
		name := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		cases = append(cases, convertCase{
			name:   name,
			src:    src,
			golden: filepath.Join("examples", "out", name+".xml"),
		})
	}
	return cases
}

// exampleEnvelope calculates an example document into an envelope.
func exampleEnvelope(t *testing.T, name string) (*gobl.Envelope, *bill.Invoice) {
	t.Helper()
	src := filepath.Join("examples", name+".yaml")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	out, err := examples.Convert(data, examples.IsEnvelope(src))
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(out, env))
	return env, env.Extract().(*bill.Invoice)
}

func loadSchema(t *testing.T) *xsd.Schema {
	t.Helper()
	schema, err := xsd.ParseFromFile(filepath.Join("schemas", "Finvoice3.0.xsd"))
	require.NoError(t, err)
	return schema
}

func assertValidXML(t *testing.T, schema *xsd.Schema, data []byte) {
	t.Helper()
	doc, err := libxml2.Parse(data)
	require.NoError(t, err)
	defer doc.Free()
	if err := schema.Validate(doc); err != nil {
		for _, e := range err.(xsd.SchemaValidationError).Errors() {
			t.Error(e)
		}
	}
}

// convertAdjusted recalculates an adjusted example, converts it and checks
// the XML against the schema.
func convertAdjusted(t *testing.T, env *gobl.Envelope, opts ...finvoice.Option) *finvoice.Invoice {
	t.Helper()
	require.NoError(t, env.Calculate())
	doc, err := finvoice.ConvertInvoice(env, append(testOptions(), opts...)...)
	require.NoError(t, err)
	validXML(t, doc)
	return doc
}

// validXML renders the document and checks it against the Finvoice schema.
func validXML(t *testing.T, doc *finvoice.Invoice) []byte {
	t.Helper()
	data, err := doc.Bytes()
	require.NoError(t, err)
	schema := loadSchema(t)
	defer schema.Free()
	assertValidXML(t, schema, data)
	return data
}

// TestConvert checks every example against its golden XML and the Finvoice
// 3.0 schema. Run with -update to regenerate the goldens.
func TestConvert(t *testing.T) {
	schema := loadSchema(t)
	defer schema.Free()

	for _, tc := range convertCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := exampleEnvelope(t, tc.name)
			doc, err := finvoice.Convert(env, testOptions()...)
			require.NoError(t, err)
			data, err := finvoice.Bytes(doc)
			require.NoError(t, err)

			assertValidXML(t, schema, data)

			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(tc.golden), 0o755))
				require.NoError(t, os.WriteFile(tc.golden, data, 0o644))
				return
			}
			expected, err := os.ReadFile(tc.golden)
			require.NoError(t, err, "golden %s missing, run with -update", tc.golden)
			assert.Equal(t, string(expected), string(data), "run with -update to refresh %s", tc.golden)
		})
	}
}

// TestConvertCreditNote checks the sign convention: a credit note is a zero
// or negative document in Finvoice.
func TestConvertCreditNote(t *testing.T) {
	env, _ := exampleEnvelope(t, "credit-note")
	doc, err := finvoice.ConvertInvoice(env, testOptions()...)
	require.NoError(t, err)
	assert.Equal(t, "INV02", doc.InvoiceDetails.TypeCode.Value)
	assert.Equal(t, "SPY", doc.InvoiceDetails.TypeCode.CodeList)
	assert.Equal(t, "CREDIT NOTE", doc.InvoiceDetails.TypeText)
	assert.Equal(t, "381", doc.InvoiceDetails.TypeCodeUN)
	assert.Equal(t, "-56,75", doc.InvoiceDetails.TotalVatIncludedAmount.Value)
	assert.Equal(t, "-56,75", doc.Epi.PaymentInstruction.InstructedAmount.Value)
	assert.Equal(t, "-2", doc.Rows[0].InvoicedQuantity[0].Value)
	assert.Equal(t, "25,00", doc.Rows[0].UnitPriceAmount.Value)
	assert.Equal(t, "-50,00", doc.Rows[0].VatExcludedAmount.Value)
	assert.Equal(t, "2026-1001", doc.InvoiceDetails.OriginalInvoiceNumber)
}

// TestConvertTotalsAddUp checks the identities the totals must satisfy: row
// amounts at the currency's precision (BR-DEC-23), rows adding up to the
// rows total (BR-CO-10) and the VAT-inclusive total being the sum of the
// exclusive total and the VAT (BR-CO-15).
func TestConvertTotalsAddUp(t *testing.T) {
	tests := []struct {
		name, example string
		adjust        func(inv *bill.Invoice)
	}{
		{"invoice", "invoice", func(*bill.Invoice) {}},
		{"credit note", "credit-note", func(*bill.Invoice) {}},
		{"reverse charge", "reverse-charge", func(*bill.Invoice) {}},
		{"line total with more decimals than the currency", "invoice", func(inv *bill.Invoice) {
			inv.Lines[1].Quantity = num.MakeAmount(3, 0)
			inv.Lines[1].Item.Price = num.NewAmount(10555, 3)
		}},
		{"prices include VAT", "invoice", func(inv *bill.Invoice) { inv.Tax.PricesInclude = tax.CategoryVAT }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, tt.example)
			tt.adjust(inv)
			doc := convertAdjusted(t, env)
			det := doc.InvoiceDetails

			rows := num.MakeAmount(0, 2)
			for _, r := range doc.Rows {
				_, decimals, _ := strings.Cut(r.VatExcludedAmount.Value, ",")
				assert.LessOrEqual(t, len(decimals), 2, "BR-DEC-23: %s", r.VatExcludedAmount.Value)
				rows = rows.Add(amountOf(t, r.VatExcludedAmount))
			}
			assert.Equal(t, amountOf(t, det.RowsTotalVatExcludedAmount).String(), rows.String(), "BR-CO-10")
			inclusive := amountOf(t, det.TotalVatExcludedAmount).Add(amountOf(t, det.TotalVatAmount))
			assert.Equal(t, amountOf(t, det.TotalVatIncludedAmount).String(), inclusive.String(), "BR-CO-15")
		})
	}
}

func amountOf(t *testing.T, a *finvoice.Amount) num.Amount {
	t.Helper()
	require.NotNil(t, a)
	v, err := num.AmountFromString(strings.Replace(a.Value, ",", ".", 1))
	require.NoError(t, err)
	return v
}

// TestConvertAddsTheAddon checks Convert declares the addon, calculates the
// invoice and applies the rules before writing.
func TestConvertAddsTheAddon(t *testing.T) {
	t.Run("the addon is added to an invoice without it", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Addons = tax.WithAddons(en16931.V2017)
		require.NoError(t, env.Calculate())
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.NoError(t, err)
		assert.True(t, addon.V3.In(inv.GetAddons()...))
	})
	t.Run("a null list entry is pruned before writing", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Terms.DueDates = append(inv.Payment.Terms.DueDates, nil)
		inv.Lines = append(inv.Lines, nil)
		doc, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.NoError(t, err)
		assert.Len(t, doc.Rows, len(inv.Lines))
	})
	t.Run("and its rules applied", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Addons = tax.WithAddons(en16931.V2017)
		inv.Payment.Instructions.Ref = ""
		require.NoError(t, env.Calculate())
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.ErrorContains(t, err, "GOBL-FI-FINVOICE-BILL-INVOICE-06")
	})
	t.Run("an invoice number too long fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Series = "MYYNTI-2026-HELSINKI"
		require.NoError(t, env.Calculate())
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.ErrorContains(t, err, "invoice number with its series must be at most 20 characters")
	})
}

// TestConvertRejectsOtherDocuments guards the entry point.
func TestConvertRejectsOtherDocuments(t *testing.T) {
	env := gobl.NewEnvelope()
	doc, err := finvoice.Convert(env, testOptions()...)
	require.ErrorIs(t, err, finvoice.ErrUnsupportedDocumentType)
	assert.True(t, doc == nil, "a refused document is a nil interface, not a nil *Invoice inside one")

	_, err = finvoice.Bytes(env)
	require.ErrorIs(t, err, finvoice.ErrUnsupportedDocumentType)
}
