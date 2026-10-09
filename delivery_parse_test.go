package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseDelivery checks the delivery receiver.
func TestParseDelivery(t *testing.T) {
	t.Run("a delivery party's Y-tunnus gets its scheme from its address", func(t *testing.T) {
		party := `<DeliveryPartyDetails><DeliveryPartyIdentifier>7654321-2</DeliveryPartyIdentifier><DeliveryOrganisationName>Varasto Oy</DeliveryOrganisationName><DeliveryPostalAddressDetails><DeliveryStreetName>Satamakatu 3</DeliveryStreetName><DeliveryTownName>Turku</DeliveryTownName><DeliveryPostCodeIdentifier>20100</DeliveryPostCodeIdentifier><CountryCode>FI</CountryCode></DeliveryPostalAddressDetails></DeliveryPartyDetails>`
		data := strings.Replace(string(message{}.bytes()), "<InvoiceDetails>", party+"<InvoiceDetails>", 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		receiver := env.Extract().(*bill.Invoice).Delivery.Receiver
		require.Len(t, receiver.Identities, 1)
		assert.Equal(t, cbc.Code("0212"), receiver.Identities[0].Ext.Get(iso.ExtKeySchemeID))
	})
}
