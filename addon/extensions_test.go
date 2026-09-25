package finvoice_test

import (
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperatorExtension(t *testing.T) {
	t.Run("registered with the addon", func(t *testing.T) {
		def := tax.ExtensionForKey(finvoice.ExtKeyOperator)
		require.NotNil(t, def)
		assert.Equal(t, "Finnish e-invoice operator", def.Name.String())
	})

	t.Run("accepted on a customer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{finvoice.ExtKeyOperator: "003700010001"})
		require.NoError(t, inv.Calculate())
		require.NoError(t, rules.Validate(inv))
		assert.Equal(t, cbc.Code("003700010001"), inv.Customer.Ext.Get(finvoice.ExtKeyOperator))
	})

	t.Run("one-character operator rejected", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{finvoice.ExtKeyOperator: "X"})
		require.NoError(t, inv.Calculate())
		assert.ErrorContains(t, rules.Validate(inv), "operator identifier")
	})
}
