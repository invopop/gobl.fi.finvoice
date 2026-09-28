package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

// TestParseAddresses checks the e-invoice addresses read from the
// organisation unit numbers and the transmission details.
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
			inv := parseMessage(t, message{sellerUnit: "<SellerOrganisationUnitNumber>" + tt.unit + "</SellerOrganisationUnitNumber>"})
			require.Len(t, inv.Supplier.Endpoints, 1)
			assert.Equal(t, cbc.URI(tt.uri), inv.Supplier.Endpoints[0].URI)
		})
	}
	t.Run("an IBAN whose check digits fail is kept as an inbox", func(t *testing.T) {
		inv := parseMessage(t, message{sellerUnit: "<SellerOrganisationUnitNumber>FI2112345600000786</SellerOrganisationUnitNumber>"})
		assert.Empty(t, inv.Supplier.Endpoints)
		require.Len(t, inv.Supplier.Inboxes, 1)
	})
	t.Run("a Finnish shape with a wrong check digit is kept as an inbox", func(t *testing.T) {
		for _, unit := range []string{"12345674", "003712345674"} {
			inv := parseMessage(t, message{sellerUnit: "<SellerOrganisationUnitNumber>" + unit + "</SellerOrganisationUnitNumber>"})
			assert.Empty(t, inv.Supplier.Endpoints, unit)
			require.Len(t, inv.Supplier.Inboxes, 1, unit)
			assert.Equal(t, cbc.Code(unit), inv.Supplier.Inboxes[0].Code)
		}
	})
	t.Run("an address of an unknown shape is kept as an inbox", func(t *testing.T) {
		inv := parseMessage(t, message{sellerUnit: "<SellerOrganisationUnitNumber>LASKUT</SellerOrganisationUnitNumber>"})
		assert.Empty(t, inv.Supplier.Endpoints)
		require.Len(t, inv.Supplier.Inboxes, 1)
		assert.Equal(t, cbc.Code("LASKUT"), inv.Supplier.Inboxes[0].Code)
	})
	t.Run("transmission details win over the unit number", func(t *testing.T) {
		td := `<MessageTransmissionDetails><MessageSenderDetails><FromIdentifier SchemeID="0216">003799999999</FromIdentifier><FromIntermediator>NDEAFIHH</FromIntermediator></MessageSenderDetails><MessageReceiverDetails><ToIdentifier>003745678907</ToIdentifier><ToIntermediator>` + receiverOperator + `</ToIntermediator></MessageReceiverDetails><MessageDetails><MessageIdentifier>1</MessageIdentifier><MessageTimeStamp>2026-09-22T11:12:12+03:00</MessageTimeStamp></MessageDetails></MessageTransmissionDetails>`
		inv := parseMessage(t, message{transmission: td, sellerUnit: "<SellerOrganisationUnitNumber>003776543212</SellerOrganisationUnitNumber>"})
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003799999999"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(addon.ExtKeyOperator))

		t.Run("even with an address of an unknown shape", func(t *testing.T) {
			td := strings.Replace(td, `<FromIdentifier SchemeID="0216">003799999999</FromIdentifier>`, `<FromIdentifier>LASKUT</FromIdentifier>`, 1)
			inv := parseMessage(t, message{transmission: td, sellerUnit: "<SellerOrganisationUnitNumber>003776543212</SellerOrganisationUnitNumber>"})
			assert.Empty(t, inv.Supplier.Endpoints)
			require.Len(t, inv.Supplier.Inboxes, 1)
			assert.Equal(t, cbc.Code("LASKUT"), inv.Supplier.Inboxes[0].Code)
		})
	})
	t.Run("a self-billed invoice is sent by the customer", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV07</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		td := `<MessageTransmissionDetails><MessageSenderDetails><FromIdentifier SchemeID="0216">003745678907</FromIdentifier><FromIntermediator>` + receiverOperator + `</FromIntermediator></MessageSenderDetails><MessageReceiverDetails><ToIdentifier SchemeID="0216">003776543212</ToIdentifier><ToIntermediator>HELSFIHH</ToIntermediator></MessageReceiverDetails><MessageDetails><MessageIdentifier>1</MessageIdentifier><MessageTimeStamp>2026-09-22T11:12:12+03:00</MessageTimeStamp></MessageDetails></MessageTransmissionDetails>`
		inv := parseMessage(t, message{details: details, transmission: td})
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(addon.ExtKeyOperator))
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
		assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(addon.ExtKeyOperator))
	})
}

// TestParseFrames checks the routing a SOAP frame gives and the charset its
// declaration names.
func TestParseFrames(t *testing.T) {
	t.Run("a SOAP frame alone carries the routing", func(t *testing.T) {
		soap := `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eb="http://www.oasis-open.org/committees/ebxml-msg/schema/msg-header-2_0.xsd"><SOAP-ENV:Header><eb:MessageHeader>
<eb:From><eb:PartyId>003776543212</eb:PartyId><eb:Role>Sender</eb:Role></eb:From><eb:From><eb:PartyId>HELSFIHH</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:From>
<eb:To><eb:PartyId>003745678907</eb:PartyId><eb:Role>Receiver</eb:Role></eb:To><eb:To><eb:PartyId>` + receiverOperator + `</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:To>
</eb:MessageHeader></SOAP-ENV:Header><SOAP-ENV:Body/></SOAP-ENV:Envelope>
`
		env, err := finvoice.Parse(append([]byte(soap), message{}.bytes()...))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003776543212"), inv.Supplier.Endpoints[0].URI)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(addon.ExtKeyOperator))
		assert.Equal(t, cbc.URI("iso6523-actorid-upis::0216:003745678907"), inv.Customer.Endpoints[0].URI)
		assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(addon.ExtKeyOperator))

		t.Run("with the declaration in front of the frame", func(t *testing.T) {
			decl := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
			data := decl + soap + strings.TrimPrefix(string(message{}.bytes()), decl)
			env, err := finvoice.Parse([]byte(data))
			require.NoError(t, err)
			inv := env.Extract().(*bill.Invoice)
			assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(addon.ExtKeyOperator))
			assert.Equal(t, cbc.Code(receiverOperator), inv.Customer.Ext.Get(addon.ExtKeyOperator))
		})
	})
	t.Run("one declaration in front of a frame serves both documents", func(t *testing.T) {
		decl := `<?xml version="1.0" encoding="ISO-8859-15"?>` + "\n"
		frame := `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:eb="http://www.oasis-open.org/committees/ebxml-msg/schema/msg-header-2_0.xsd"><SOAP-ENV:Header><eb:MessageHeader>` +
			`<eb:From><eb:PartyId>003776543212</eb:PartyId><eb:Role>Sender</eb:Role></eb:From><eb:From><eb:PartyId>HELSFIHH</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:From>` +
			`<eb:To><eb:PartyId>003745678907</eb:PartyId><eb:Role>Receiver</eb:Role></eb:To><eb:To><eb:PartyId>` + receiverOperator + `</eb:PartyId><eb:Role>Intermediator</eb:Role></eb:To>` +
			`<eb:Service>Lähetys €</eb:Service></eb:MessageHeader></SOAP-ENV:Header><SOAP-ENV:Body/></SOAP-ENV:Envelope>` + "\n"
		body := strings.TrimPrefix(string(message{note: "Hinta 10 €"}.bytes()), `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
		data, err := charmap.ISO8859_15.NewEncoder().Bytes([]byte(decl + frame + body))
		require.NoError(t, err)
		env, err := finvoice.Parse(data)
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, "Lähettäjä Oy", inv.Supplier.Name)
		assert.Equal(t, "Hinta 10 €", inv.Notes[0].Text)
		assert.Equal(t, cbc.Code("HELSFIHH"), inv.Supplier.Ext.Get(addon.ExtKeyOperator))
	})
}
