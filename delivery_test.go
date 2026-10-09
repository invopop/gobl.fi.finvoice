package finvoice_test

import (
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertDelivery checks the delivery receiver is written whole, and
// refused before conversion when Finvoice could not hold it.
func TestConvertDelivery(t *testing.T) {
	t.Run("delivery receiver with its address", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		doc := convertAdjusted(t, env)
		require.NotNil(t, doc.DeliveryParty)
		assert.Equal(t, []string{inv.Delivery.Receiver.Name}, doc.DeliveryParty.Name)
		assert.Equal(t, inv.Delivery.Receiver.Addresses[0].Locality, doc.DeliveryParty.Address.TownName)
	})
	t.Run("delivery receiver without an address is refused", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Delivery.Receiver.Addresses = nil
		_, err := finvoice.ConvertInvoice(env, testOptions()...)
		assert.ErrorContains(t, err, "delivery receiver needs a name and an address")
	})
}
