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
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/tax"
	"github.com/invopop/gobl/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, cbc.Code("23456780"), inv.Supplier.TaxID.Code)
	assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003723456780"), inv.Supplier.Endpoints[0].URI)
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003701120389"), inv.Customer.Endpoints[0].URI)
	assert.Empty(t, inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
	assert.Empty(t, inv.Customer.Ext.Get(finvoice.ExtKeyOperator))

	assert.Equal(t, "0.99", inv.Totals.TotalWithTax.String())
	assert.Equal(t, "0.79", inv.Totals.Total.String())
	require.Len(t, inv.Lines, 1)
	assert.Equal(t, "1.00", inv.Lines[0].Quantity.String())
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
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003723456780"), inv.Supplier.Endpoints[0].URI)
	assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Supplier.Ext.Get(finvoice.ExtKeyOperator))
	assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003701120389"), inv.Customer.Endpoints[0].URI)
	assert.Equal(t, cbc.Code("003723327487"), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
	assert.Equal(t, "Hoitokäynti", inv.Lines[1].Item.Name)
	assert.Equal(t, org.UnitHour, inv.Lines[0].Item.Unit)
	assert.Equal(t, tax.KeyExempt, inv.Lines[1].Taxes[0].Key)
	assert.Equal(t, cbc.Code("VATEX-EU-132-1C"), inv.Lines[1].Taxes[0].Ext.Get("cef-vatex"))
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
	assert.Equal(t, cbc.Code("560"), inv.Preceding[0].Code)
	assert.Equal(t, "2026-09-22", inv.Preceding[0].IssueDate.String())
	assert.Equal(t, "2", inv.Lines[0].Quantity.String())
	assert.Equal(t, "50.00", inv.Lines[0].Item.Price.String())
	assert.Equal(t, "125.50", inv.Totals.TotalWithTax.String())
	assert.True(t, inv.Totals.Payable.Equals(num.MakeAmount(12550, 2)))
}

// TestParseRejectsOtherDocuments guards the entry point.
func TestParseRejectsOtherDocuments(t *testing.T) {
	_, err := fifinvoice.Parse([]byte(`<?xml version="1.0"?><Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"/>`))
	require.ErrorContains(t, err, "Invoice")
	_, err = fifinvoice.Parse([]byte("not xml"))
	require.Error(t, err)
}
