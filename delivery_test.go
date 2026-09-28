package finvoice_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConvertDelivery checks a delivery receiver without an address is left
// out.
func TestConvertDelivery(t *testing.T) {
	t.Run("delivery receiver without an address", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Delivery.Receiver.Addresses = nil
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.DeliveryParty)
		assert.NotNil(t, doc.DeliveryDetails)
	})
}
