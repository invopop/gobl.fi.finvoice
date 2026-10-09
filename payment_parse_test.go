package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/pay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePayment(t *testing.T) {
	t.Run("payment means codes", func(t *testing.T) {
		tests := []struct {
			code, ext string
			key       cbc.Key
		}{
			{"58", "58", pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA)},
			{"30", "30", pay.MeansKeyCreditTransfer},
			{"49", "49", pay.MeansKeyDirectDebit},
			{"42", "30", pay.MeansKeyCreditTransfer},
		}
		for _, tt := range tests {
			t.Run(tt.code, func(t *testing.T) {
				epi := epiInstruction("125,50", "20261006", `<EpiPaymentMeansCode>`+tt.code+`</EpiPaymentMeansCode><EpiPaymentMeansText>Tilisiirto</EpiPaymentMeansText>`)
				env, err := finvoice.Parse(message{epi: epi}.bytes())
				require.NoError(t, err)
				inv := env.Extract().(*bill.Invoice)
				assert.Equal(t, tt.key, inv.Payment.Instructions.Key)
				assert.Equal(t, cbc.Code(tt.ext), inv.Payment.Instructions.Ext.Get(untdid.ExtKeyPaymentMeans), "GOBL pairs the code with the key")
				assert.Equal(t, "Tilisiirto", inv.Payment.Instructions.Detail)
				if tt.key.Has(pay.MeansKeyCreditTransfer) {
					assert.NoError(t, env.Validate())
				} else {
					assert.ErrorContains(t, env.Validate(), "payment instructions key must be credit-transfer", "the addon takes credit transfers only")
				}
			})
		}
	})
	t.Run("the legacy instruction code names the payment means", func(t *testing.T) {
		epi := strings.Replace(epiInstruction("125,50", "20261006", ""), "<EpiInstructedAmount", "<EpiInstructionCode>58</EpiInstructionCode><EpiInstructedAmount", 1)
		inv := parseMessage(t, message{epi: epi})
		assert.Equal(t, pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA), inv.Payment.Instructions.Key)
	})
	t.Run("a BBAN account", func(t *testing.T) {
		data := strings.Replace(string(message{}.bytes()), `<EpiAccountID IdentificationSchemeName="IBAN">FI2112345600000785</EpiAccountID>`, `<EpiAccountID IdentificationSchemeName="BBAN">12345600000785</EpiAccountID>`, 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		ct := env.Extract().(*bill.Invoice).Payment.Instructions.CreditTransfer[0]
		assert.Equal(t, cbc.Code("12345600000785"), ct.Number)
		assert.Empty(t, ct.IBAN)
	})
	t.Run("the payment order's date is the due date", func(t *testing.T) {
		inv := parseMessage(t, message{epi: epiInstruction("125,50", "20261020", "")})
		assert.Equal(t, "2026-10-20", inv.Payment.Terms.DueDates[0].Date.String())
	})
	t.Run("the payment order's amount is the sender's to state", func(t *testing.T) {
		inv := parseMessage(t, message{epi: epiInstruction("100,00", "20261006", "")})
		assert.Equal(t, "125.50", inv.Totals.Payable.String())
	})
	t.Run("a seller name cut to the payment order is no payee", func(t *testing.T) {
		name := "Helsingin Kaupungin Asuntotuotanto Oy Ab"
		data := strings.Replace(string(message{}.bytes()), "<SellerOrganisationName>Lähettäjä Oy<", "<SellerOrganisationName>"+name+"<", 1)
		data = strings.Replace(data, "<EpiBeneficiaryPartyDetails>", "<EpiBeneficiaryPartyDetails><EpiNameAddressDetails>Helsingin Kaupungin Asuntotuotanto</EpiNameAddressDetails>", 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		assert.Nil(t, env.Extract().(*bill.Invoice).Payment.Payee)
	})
	t.Run("further accounts and the payee", func(t *testing.T) {
		d := message{}
		data := strings.Replace(string(d.bytes()), `</SellerPartyDetails>`, `</SellerPartyDetails><SellerInformationDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">fi21 1234 5600 0007 85</SellerAccountID><SellerBic IdentificationSchemeName="BIC">NDEAFIHH</SellerBic></SellerAccountDetails><SellerAccountDetails><SellerAccountID IdentificationSchemeName="IBAN">FI4250001510000023</SellerAccountID><SellerBic IdentificationSchemeName="BIC">OKOYFIHH</SellerBic></SellerAccountDetails></SellerInformationDetails>`, 1)
		data = strings.Replace(data, `<EpiBeneficiaryPartyDetails>`, `<EpiBeneficiaryPartyDetails><EpiNameAddressDetails>Perintä Oy</EpiNameAddressDetails>`, 1)
		env, err := finvoice.Parse([]byte(data))
		require.NoError(t, err)
		inv := env.Extract().(*bill.Invoice)
		require.Len(t, inv.Payment.Instructions.CreditTransfer, 2)
		assert.Equal(t, cbc.Code("NDEAFIHH"), inv.Payment.Instructions.CreditTransfer[0].BIC, "the BIC the seller's list gives the payment order's account")
		assert.Equal(t, cbc.Code("FI4250001510000023"), inv.Payment.Instructions.CreditTransfer[1].IBAN)
		assert.Equal(t, cbc.Code("OKOYFIHH"), inv.Payment.Instructions.CreditTransfer[1].BIC)
		assert.Equal(t, "Perintä Oy", inv.Payment.Payee.Name)
	})
	t.Run("a second due date is refused", func(t *testing.T) {
		terms := `<PaymentTermsDetails><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails><PaymentTermsDetails><InvoiceDueDate Format="CCYYMMDD">20261106</InvoiceDueDate></PaymentTermsDetails>`
		data := strings.Replace(string(message{}.bytes()), `<PaymentTermsDetails><PaymentTermsFreeText>14 pv</PaymentTermsFreeText><PaymentTermsFreeText>netto</PaymentTermsFreeText><InvoiceDueDate Format="CCYYMMDD">20261006</InvoiceDueDate></PaymentTermsDetails>`, terms, 1)
		_, err := finvoice.Parse([]byte(data))
		require.ErrorContains(t, err, "payment terms give 2 due dates, the payment order takes one")
	})
}
