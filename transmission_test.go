package finvoice_test

import (
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertTransmission covers the transmission details: written only
// when the receiver names an operator, and then only with the sender's
// operator to hand.
func TestConvertTransmission(t *testing.T) {
	t.Run("both operators route the message", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.NoError(t, err)
		require.NotNil(t, doc.Transmission)
		assert.Equal(t, "003776543212", doc.Transmission.Sender.Identifier.Value)
		assert.Equal(t, "0216", doc.Transmission.Sender.Identifier.SchemeID)
		assert.Equal(t, senderOperator, doc.Transmission.Sender.Intermediator)
		assert.Equal(t, "003745678907", doc.Transmission.Receiver.Identifier.Value)
		assert.Equal(t, receiverOperator, doc.Transmission.Receiver.Intermediator)
		assert.Equal(t, "3a4b1f4e-7b1c-4a1a-9c2e-0d5f6a7b8c9d", doc.Transmission.Message.Identifier)
		assert.Equal(t, "2026-09-01T08:30:00Z", doc.Transmission.Message.Timestamp)
	})
	t.Run("a self-billed invoice goes from the customer to the supplier", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.SetTags(tax.TagSelfBilled)
		inv.Supplier.Ext = tax.ExtensionsOf(cbc.CodeMap{addon.ExtKeyOperator: "003700030003"})
		doc := convertAdjusted(t, env)
		require.NotNil(t, doc.Transmission)
		assert.Equal(t, "003745678907", doc.Transmission.Sender.Identifier.Value)
		assert.Equal(t, "003776543212", doc.Transmission.Receiver.Identifier.Value)
		assert.Equal(t, "003700030003", doc.Transmission.Receiver.Intermediator)

		inv.Supplier.Ext = inv.Supplier.Ext.Delete(addon.ExtKeyOperator)
		assert.Nil(t, convertAdjusted(t, env).Transmission, "the receiver names no operator")

		inv.Supplier.Endpoints = nil
		require.NoError(t, env.Calculate())
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.ErrorContains(t, err, "supplier needs an e-invoice address")
	})
	t.Run("no receiver operator, no transmission details", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Ext = inv.Customer.Ext.Delete(addon.ExtKeyOperator)
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.Transmission)
		assert.Equal(t, "003776543212", doc.SellerOrganisationUnitNumber)
		assert.Equal(t, "003745678907", doc.BuyerOrganisationUnitNumber)
	})
	t.Run("message identifier", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc := convertAdjusted(t, env, finvoice.WithMessageID("MSG-1"))
		assert.Equal(t, "MSG-1", doc.Transmission.Message.Identifier)
	})
	t.Run("a coded inbox serves as the e-invoice address", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Endpoints = nil
		inv.Customer.Inboxes = []*org.Inbox{{Scheme: "0216", Code: "003745678907"}}
		doc := convertAdjusted(t, env)
		assert.Equal(t, "003745678907", doc.BuyerOrganisationUnitNumber)
		assert.Equal(t, finvoice.Identifier{Value: "003745678907", SchemeID: "0216"}, doc.Transmission.Receiver.Identifier)
	})
	t.Run("receiver operator without the sender's fails", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		_, err := finvoice.ConvertInvoice(env)
		require.ErrorIs(t, err, finvoice.ErrSenderOperatorRequired)
	})
	t.Run("customer without an address fails", func(t *testing.T) {
		for _, withOperator := range []bool{true, false} {
			env, inv := exampleEnvelope(t, "invoice")
			inv.Customer.Endpoints = nil
			if !withOperator {
				inv.Customer.Ext = inv.Customer.Ext.Delete(addon.ExtKeyOperator)
			}
			require.NoError(t, env.Calculate())
			_, err := finvoice.ConvertInvoice(env, testOptions()...)
			require.ErrorContains(t, err, "customer needs an e-invoice address")
		}
	})
	t.Run("a supplier without an e-invoice address fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Endpoints = nil
		require.NoError(t, env.Calculate())
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		require.ErrorContains(t, err, "supplier needs an e-invoice address")
	})
}
