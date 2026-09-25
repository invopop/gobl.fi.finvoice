package fifinvoice_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/invopop/gobl"
	fifinvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/tax"
	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	senderOperator   = "003723327487"
	receiverOperator = "003708599126"
)

// testOptions keep the message frame deterministic for golden comparison.
func testOptions() []fifinvoice.Option {
	return []fifinvoice.Option{
		fifinvoice.WithSenderOperator(senderOperator),
		fifinvoice.WithMessageTime(time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)),
		fifinvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
	}
}

// convertCases are the calculated example envelopes, each with the Finvoice
// XML it should produce next to it.
func convertCases(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("examples", "out")
	found, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, found)
	cases := map[string]string{}
	for _, src := range found {
		name := strings.TrimSuffix(filepath.Base(src), ".json")
		cases[src] = filepath.Join(dir, name+".xml")
	}
	return cases
}

func loadEnvelope(t *testing.T, path string) *gobl.Envelope {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(data, env))
	return env
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

// TestConvert checks every fixture against its golden XML and the Finvoice
// 3.0 schema. Run with -update to regenerate the goldens.
func TestConvert(t *testing.T) {
	schema := loadSchema(t)
	defer schema.Free()

	for src, golden := range convertCases(t) {
		t.Run(src, func(t *testing.T) {
			env := loadEnvelope(t, src)
			doc, err := fifinvoice.Convert(env, testOptions()...)
			require.NoError(t, err)
			data, err := doc.Bytes()
			require.NoError(t, err)

			assertValidXML(t, schema, data)

			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o755))
				require.NoError(t, os.WriteFile(golden, data, 0o644))
				return
			}
			expected, err := os.ReadFile(golden)
			require.NoError(t, err, "golden %s missing, run with -update", golden)
			assert.Equal(t, string(expected), string(data), "run with -update to refresh %s", golden)
		})
	}
}

// TestConvertFrame covers the routing frame: written only for a customer
// that names an operator, and then only with the sender's operator to hand.
func TestConvertFrame(t *testing.T) {
	src := filepath.Join("examples", "out", "invoice.json")

	t.Run("receiver operator routes the message", func(t *testing.T) {
		env := loadEnvelope(t, src)
		doc, err := fifinvoice.Convert(env, testOptions()...)
		require.NoError(t, err)
		require.NotNil(t, doc.Transmission)
		assert.Equal(t, "003723456780", doc.Transmission.Sender.Identifier.Value)
		assert.Equal(t, "0216", doc.Transmission.Sender.Identifier.SchemeID)
		assert.Equal(t, senderOperator, doc.Transmission.Sender.Intermediator)
		assert.Equal(t, "003701120389", doc.Transmission.Receiver.Identifier.Value)
		assert.Equal(t, "003723327487", doc.Transmission.Receiver.Intermediator)
		assert.Equal(t, "3a4b1f4e-7b1c-4a1a-9c2e-0d5f6a7b8c9d", doc.Transmission.Message.Identifier)
		assert.Equal(t, "2026-09-01T08:30:00Z", doc.Transmission.Message.Timestamp)
	})

	t.Run("no receiver operator, no frame", func(t *testing.T) {
		env := loadEnvelope(t, src)
		inv := env.Extract().(*bill.Invoice)
		inv.Customer.Ext = inv.Customer.Ext.Delete("fi-finvoice-operator")
		doc, err := fifinvoice.Convert(env, testOptions()...)
		require.NoError(t, err)
		assert.Nil(t, doc.Transmission)
		assert.Equal(t, "003723456780", doc.SellerOrganisationUnitNumber)
		assert.Equal(t, "003701120389", doc.BuyerOrganisationUnitNumber)
	})

	t.Run("receiver operator without the sender's fails", func(t *testing.T) {
		env := loadEnvelope(t, src)
		_, err := fifinvoice.Convert(env)
		require.ErrorIs(t, err, fifinvoice.ErrSenderOperatorRequired)
	})

	t.Run("receiver operator without an address fails", func(t *testing.T) {
		env := loadEnvelope(t, src)
		inv := env.Extract().(*bill.Invoice)
		inv.Customer.Endpoints = nil
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "customer")
	})
}

// TestConvertCreditNote checks the sign convention: a credit note is a zero
// or negative document in Finvoice.
func TestConvertCreditNote(t *testing.T) {
	env := loadEnvelope(t, filepath.Join("examples", "out", "credit-note.json"))
	doc, err := fifinvoice.Convert(env, testOptions()...)
	require.NoError(t, err)
	assert.Equal(t, "INV02", doc.InvoiceDetails.TypeCode)
	assert.Equal(t, "CREDIT NOTE", doc.InvoiceDetails.TypeText)
	assert.Equal(t, "381", doc.InvoiceDetails.TypeCodeUN)
	assert.Equal(t, "-56,75", doc.InvoiceDetails.TotalVatIncludedAmount.Value)
	assert.Equal(t, "-56,75", doc.Epi.PaymentInstruction.InstructedAmount.Value)
	assert.Equal(t, "-2", doc.Rows[0].InvoicedQuantity.Value)
	assert.Equal(t, "25,00", doc.Rows[0].UnitPriceAmount.Value)
	assert.Equal(t, "-50,00", doc.Rows[0].VatExcludedAmount.Value)
	assert.Equal(t, "2026-1001", doc.InvoiceDetails.OriginalInvoiceNumber)
}

// TestConvertRejectsOtherDocuments guards the entry point.
func TestConvertRejectsOtherDocuments(t *testing.T) {
	env := gobl.NewEnvelope()
	_, err := fifinvoice.Convert(env, testOptions()...)
	require.Error(t, err)

	env = loadEnvelope(t, filepath.Join("examples", "out", "invoice.json"))
	inv := env.Extract().(*bill.Invoice)
	inv.Addons = tax.WithAddons(cbc.Key("eu-en16931-v2017"))
	_, err = fifinvoice.Convert(env, testOptions()...)
	require.ErrorContains(t, err, "fi-finvoice-v3")
}
