package addon_test

import (
	"strings"
	"testing"

	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperatorExtension(t *testing.T) {
	t.Run("registered with the addon", func(t *testing.T) {
		def := tax.ExtensionForKey(addon.ExtKeyOperator)
		require.NotNil(t, def)
		assert.Equal(t, "Finnish e-invoice operator", def.Name.String())
	})

	t.Run("accepted on a customer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: "003700010001"})
		require.NoError(t, inv.Calculate())
		require.NoError(t, rules.Validate(inv))
		assert.Equal(t, cbc.Code("003700010001"), inv.Customer.Ext.Get(addon.ExtKeyOperator))
	})

	t.Run("operator of 35 characters accepted", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: cbc.Code(strings.Repeat("9", 35))})
		require.NoError(t, inv.Calculate())
		assert.NoError(t, rules.Validate(inv))
	})

	t.Run("one-character operator rejected", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: "X"})
		require.NoError(t, inv.Calculate())
		assert.ErrorContains(t, rules.Validate(inv), "extension must be 2 to 35 letters or digits")
	})

	t.Run("operator of 36 characters rejected", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: cbc.Code(strings.Repeat("9", 36))})
		require.NoError(t, inv.Calculate())
		assert.ErrorContains(t, rules.Validate(inv), "customer 'fi-finvoice-operator' extension must be 2 to 35 letters or digits")
	})

	t.Run("operator with a hyphen rejected", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: "0037-0001"})
		require.NoError(t, inv.Calculate())
		assert.ErrorContains(t, rules.Validate(inv), "customer 'fi-finvoice-operator' extension must be 2 to 35 letters or digits")
	})

	t.Run("one-character operator rejected on a supplier", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Supplier.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: "X"})
		require.NoError(t, inv.Calculate())
		assert.ErrorContains(t, rules.Validate(inv), "supplier 'fi-finvoice-operator' extension must be 2 to 35 letters or digits")
	})
}
