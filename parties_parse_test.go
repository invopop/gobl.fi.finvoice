package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseIdentities checks the VAT numbers, legal identities and emails
// read for the parties.
func TestParseIdentities(t *testing.T) {
	t.Run("a VAT number in any common form is kept for validation", func(t *testing.T) {
		tests := []struct {
			name, code, want string
			valid            bool
		}{
			{"compact", "FI76543212", "FI76543212", true},
			{"spaced", "FI 76543212", "FI76543212", true},
			{"hyphenated", "FI7654321-2", "FI76543212", true},
			{"bad check digit", "FI76543213", "FI76543213", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				data := strings.Replace(string(message{}.bytes()), "<SellerOrganisationTaxCode>FI76543212<", "<SellerOrganisationTaxCode>"+tt.code+"<", 1)
				env, err := finvoice.Parse([]byte(data))
				require.NoError(t, err)
				inv := env.Extract().(*bill.Invoice)
				require.NotNil(t, inv.Supplier.TaxID)
				assert.Equal(t, tt.want, inv.Supplier.TaxID.String())
				if tt.valid {
					assert.NoError(t, env.Validate())
				} else {
					assert.ErrorContains(t, env.Validate(), "supplier.tax_id")
				}
			})
		}
	})
	t.Run("padded codes in attributes are read collapsed", func(t *testing.T) {
		data := string(message{noVATNumber: true}.bytes())
		data = strings.Replace(data, `AmountCurrencyIdentifier="EUR">125,50</InvoiceTotalVatIncludedAmount>`, `AmountCurrencyIdentifier=" EUR ">125,50</InvoiceTotalVatIncludedAmount>`, 1)
		data = strings.Replace(data, "<CountryCode>FI</CountryCode>", "<CountryCode> FI </CountryCode>", 1)
		data = strings.Replace(data, "<SellerPartyIdentifier>7654321-2</SellerPartyIdentifier>", `<SellerPartyIdentifier SchemeID=" 0212 ">7654321-2</SellerPartyIdentifier>`, 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		require.NoError(t, env.Validate())
		inv := env.Extract().(*bill.Invoice)
		assert.Equal(t, currency.EUR, inv.Currency)
		assert.Equal(t, "FI", inv.Supplier.Addresses[0].Country.String())
		assert.Equal(t, cbc.Code("0212"), inv.Supplier.Identities[0].Ext.Get(iso.ExtKeySchemeID))
	})
	t.Run("a contact email and a common email are both kept", func(t *testing.T) {
		seller := func(common string) string {
			return `</SellerPartyDetails><SellerCommunicationDetails><SellerEmailaddressIdentifier>matti@myyja.fi</SellerEmailaddressIdentifier></SellerCommunicationDetails><SellerInformationDetails><SellerCommonEmailaddressIdentifier>` + common + `</SellerCommonEmailaddressIdentifier></SellerInformationDetails>`
		}
		for common, want := range map[string][]string{
			"laskutus@myyja.fi": {"matti@myyja.fi", "laskutus@myyja.fi"},
			"matti@myyja.fi":    {"matti@myyja.fi"},
		} {
			data := strings.Replace(string(message{}.bytes()), "</SellerPartyDetails>", seller(common), 1)
			env, err := finvoice.Parse([]byte(data))
			require.NoError(t, err)
			var got []string
			for _, e := range env.Extract().(*bill.Invoice).Supplier.Emails {
				got = append(got, e.Address)
			}
			assert.Equal(t, want, got)
		}
	})
	t.Run("Y-tunnus without a VAT number stays a legal identity", func(t *testing.T) {
		inv := parseMessage(t, message{noVATNumber: true})
		assert.Nil(t, inv.Supplier.TaxID)
		require.Len(t, inv.Supplier.Identities, 1)
		assert.Equal(t, cbc.Code("7654321-2"), inv.Supplier.Identities[0].Code)
		assert.Equal(t, cbc.Code("0212"), inv.Supplier.Identities[0].Ext.Get(iso.ExtKeySchemeID))
		assert.Equal(t, "FI", inv.GetRegime().String())
	})
	t.Run("a Y-tunnus is labelled unless the party is placed abroad", func(t *testing.T) {
		address := `<SellerPostalAddressDetails><SellerStreetName>Esimerkkikatu 1</SellerStreetName><SellerTownName>Helsinki</SellerTownName><SellerPostCodeIdentifier>00100</SellerPostCodeIdentifier><CountryCode>FI</CountryCode></SellerPostalAddressDetails>`
		tests := []struct {
			name     string
			old, new []string
			want     cbc.Code
		}{
			{"no address", []string{address}, []string{""}, "0212"},
			{"an address without a country", []string{"<CountryCode>FI</CountryCode>"}, []string{""}, "0212"},
			{"a check digit that does not hold", []string{"7654321-2"}, []string{"7654321-3"}, ""},
			{"a Swedish address", []string{"<CountryCode>FI</CountryCode>"}, []string{"<CountryCode>SE</CountryCode>"}, ""},
			{"a Danish VAT number and no address", []string{address, "</SellerOrganisationName>"},
				[]string{"", "</SellerOrganisationName><SellerOrganisationTaxCode>DK13585628</SellerOrganisationTaxCode>"}, ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				data := string(message{noVATNumber: true}.bytes())
				for i := range tt.old {
					data = strings.Replace(data, tt.old[i], tt.new[i], 1)
				}
				env, err := finvoice.Parse([]byte(data))
				require.NoError(t, err)
				ids := env.Extract().(*bill.Invoice).Supplier.Identities
				require.Len(t, ids, 1)
				assert.Equal(t, tt.want, ids[0].Ext.Get(iso.ExtKeySchemeID))
			})
		}
	})
	t.Run("a Y-tunnus in the VAT field is no VAT number", func(t *testing.T) {
		details := `<InvoiceTypeCode>INV01</InvoiceTypeCode><InvoiceTypeText>X</InvoiceTypeText><OriginCode>Original</OriginCode><InvoiceNumber>77</InvoiceNumber>`
		d := message{details: details, noVATNumber: true}
		data := strings.Replace(string(d.bytes()), `</SellerOrganisationName>`, `</SellerOrganisationName><SellerOrganisationTaxCode>7654321-2</SellerOrganisationTaxCode>`, 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		assert.Nil(t, inv.Supplier.TaxID)
	})
}
