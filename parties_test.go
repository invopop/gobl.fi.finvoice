package finvoice_test

import (
	"strings"
	"testing"

	"github.com/invopop/gobl/org"
	"github.com/stretchr/testify/assert"
)

// TestConvertParties pins the party fields the examples leave out, and what
// becomes of an address Finvoice has no room for.
func TestConvertParties(t *testing.T) {
	t.Run("trading name, region and the organisation's email", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Alias = "Myyjä"
		inv.Supplier.Addresses[0].Region = "Uusimaa"
		inv.Supplier.Emails = append(inv.Supplier.Emails, &org.Email{Address: "laskutus@myyja.fi"})
		doc := convertAdjusted(t, env)
		assert.Equal(t, "Myyjä", doc.Seller.TradingName)
		assert.Equal(t, "Uusimaa", doc.Seller.Address.Subdivision)
		assert.Equal(t, inv.Supplier.Emails[0].Address, doc.SellerCommunication.Email)
		assert.Equal(t, "laskutus@myyja.fi", doc.SellerInformation.Email, "the second email is the organisation's common address")
	})
	t.Run("an email and a web address of 70 are kept, of 71 left out", func(t *testing.T) {
		for _, n := range []int{70, 71} {
			env, inv := exampleEnvelope(t, "invoice")
			email := strings.Repeat("a", n-len("@myyja.fi")) + "@myyja.fi"
			website := "https://myyja.fi/" + strings.Repeat("a", n-len("https://myyja.fi/"))
			inv.Supplier.Emails[0].Address = email
			inv.Supplier.Websites[0].URL = website
			doc := convertAdjusted(t, env)
			if n == 70 {
				assert.Equal(t, email, doc.SellerCommunication.Email)
				assert.Equal(t, website, doc.SellerInformation.Website)
			} else {
				assert.Empty(t, doc.SellerCommunication.Email)
				assert.Empty(t, doc.SellerInformation.Website)
			}
		}
	})
	t.Run("address without a street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Espoo"}, doc.Buyer.Address.StreetName)
	})
	t.Run("address with a PO box and no street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		inv.Customer.Addresses[0].PostOfficeBox = "PL 123"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"PL 123"}, doc.Buyer.Address.StreetName)
	})
	t.Run("address with a one-digit PO box and no street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		inv.Customer.Addresses[0].PostOfficeBox = "5"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Espoo"}, doc.Buyer.Address.StreetName)
		assert.Equal(t, "5", doc.Buyer.Address.PostOfficeBox)
	})
	t.Run("address without a post code", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Customer.Addresses[0].Code = ""
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.Buyer.Address)
	})
}
