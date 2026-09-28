package finvoice_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/invopop/gobl/org"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertTexts checks long texts are cut or split to the schema's
// lengths in characters.
func TestConvertTexts(t *testing.T) {
	t.Run("names and notes take as many elements as they need", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Name = strings.TrimSpace(strings.Repeat("Rakennuspalvelu ", 12))
		note := strings.TrimSpace(strings.Repeat("Toimitus sisältää asennuksen ja käyttöönoton. ", 150))
		inv.Notes = []*org.Note{{Key: org.NoteKeyGeneral, Text: note}}
		doc := convertAdjusted(t, env)
		assert.Len(t, doc.Seller.Name, 3)
		assert.Equal(t, inv.Supplier.Name, strings.Join(doc.Seller.Name, " "))
		assert.Equal(t, note, strings.Join(doc.InvoiceDetails.FreeText, " "))
	})
	t.Run("names and streets", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Name = strings.Repeat("ä", 80) + " " + strings.Repeat("ö", 30)
		inv.Customer.Addresses[0].Street = strings.Repeat("ö", 40)
		inv.Lines[0].Item.Name = strings.Repeat("å", 120)
		doc := convertAdjusted(t, env)
		require.Len(t, doc.Seller.Name, 2)
		assert.Equal(t, 70, utf8.RuneCountInString(doc.Seller.Name[0]))
		assert.Equal(t, strings.Repeat("ä", 10)+" "+strings.Repeat("ö", 30), doc.Seller.Name[1])
		assert.Equal(t, 35, utf8.RuneCountInString(doc.Buyer.Address.StreetName[0]))
		assert.Equal(t, 100, utf8.RuneCountInString(doc.Rows[0].ArticleName))
	})
	t.Run("a one-character last word stays with the word before it", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Delivery.Receiver.Name = "Asunto Oy Helsingin Mannerheimintie 5"
		inv.Customer.Addresses[0].Street = "Kuninkaankartanonkatu Pohjoinen"
		inv.Customer.Addresses[0].Number = "12 B"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Asunto Oy Helsingin", "Mannerheimintie 5"}, doc.DeliveryParty.Name)
		assert.Equal(t, []string{"Kuninkaankartanonkatu Pohjoinen", "12 B"}, doc.Buyer.Address.StreetName[:2])
	})
	t.Run("texts split at words", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Terms.Notes = "Maksuehto 14 päivää netto, viivästyskorko korkolain mukaan ja perintäkulut peritään erikseen"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{
			"Maksuehto 14 päivää netto, viivästyskorko korkolain mukaan ja",
			"perintäkulut peritään erikseen",
		}, doc.InvoiceDetails.PaymentTerms[0].FreeText)
	})
	t.Run("display texts are cut to their elements", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Lines[0].Discounts[0].Reason = strings.Repeat("alennus ", 6)
		inv.Tax.Notes[0].Text = strings.TrimSpace(strings.Repeat("Käännetty verovelvollisuus ", 10))
		doc := convertAdjusted(t, env)
		assert.Equal(t, 35, utf8.RuneCountInString(doc.Rows[0].ProgressiveDiscount[0].TypeText))
		assert.Len(t, doc.InvoiceDetails.VatSpecifications[0].FreeText, 3)
	})
}
