package finvoice_test

import (
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertPaymentOrder(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(inv *bill.Invoice)
		check  func(t *testing.T, doc *finvoice.Invoice)
	}{
		{"RF creditor reference", func(*bill.Invoice) {}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "ISO", doc.Epi.PaymentInstruction.RemittanceInfoIdentifier.Scheme)
			assert.Equal(t, "SLEV", doc.Epi.PaymentInstruction.Charge.Option)
		}},
		{"Finnish reference", func(inv *bill.Invoice) {
			inv.Payment.Instructions.Ref = "1232"
			inv.Payment.Instructions.Key = pay.MeansKeyCreditTransfer
		}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "SPY", doc.Epi.PaymentInstruction.RemittanceInfoIdentifier.Scheme)
			assert.Equal(t, "SHA", doc.Epi.PaymentInstruction.Charge.Option)
			assert.Equal(t, "30", doc.Epi.PaymentInstruction.PaymentMeansCode)
		}},
		{"other reference", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "LASKU1001" }, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
			assert.Equal(t, "LASKU1001", doc.Epi.Identification.Reference)
		}},
		{"Finnish reference with a wrong check digit", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "1001" }, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
			assert.Equal(t, "1001", doc.Epi.Identification.Reference)
		}},
		{"RF reference with a wrong checksum", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "RF00539007547034" }, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
		}},
		{"payment means text", func(inv *bill.Invoice) { inv.Payment.Instructions.Detail = "Tilisiirto" }, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "Tilisiirto", doc.Epi.PaymentInstruction.PaymentMeansText)
		}},
		{"payee name cut at a space", func(inv *bill.Invoice) {
			inv.Payment.Payee = &org.Party{Name: "Helsingin Kaupungin Asuntotuotanto Oy Ab"}
		}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "Helsingin Kaupungin Asuntotuotanto", doc.Epi.Party.Beneficiary.NameAddress)
		}},
		{"no BIC, with a bank name", func(inv *bill.Invoice) {
			inv.Payment.Instructions.CreditTransfer[0].BIC = ""
			inv.Payment.Instructions.CreditTransfer[0].Name = "Myyjä Oy tili"
		}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Nil(t, doc.Epi.Party.BFI.Identifier)
			assert.Equal(t, "Myyjä Oy tili", doc.Epi.Party.BFI.Name)
			assert.Nil(t, doc.SellerInformation.Accounts)
		}},
		{"payee", func(inv *bill.Invoice) {
			inv.Payment.Payee = &org.Party{Name: "Perintä Oy"}
		}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "Perintä Oy", doc.Epi.Party.Beneficiary.NameAddress)
		}},
		{"advance paid", func(inv *bill.Invoice) {
			inv.Payment.Advances = []*pay.Record{{Amount: num.MakeAmount(8836, 2), Description: "Ennakko"}}
		}, func(t *testing.T, doc *finvoice.Invoice) {
			assert.Equal(t, "88,36", doc.InvoiceDetails.PaidAmount.Value)
			assert.Equal(t, "1000,00", doc.Epi.PaymentInstruction.InstructedAmount.Value)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, "invoice")
			tt.adjust(inv)
			tt.check(t, convertAdjusted(t, env))
		})
	}
	t.Run("further accounts and the bank name", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Instructions.CreditTransfer[0].Name = "Myyjä Oy tili"
		inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
			&pay.CreditTransfer{Number: "12345-678", BIC: "OKOYFIHH"})
		doc := convertAdjusted(t, env)
		assert.Equal(t, "Myyjä Oy tili", doc.SellerInformation.Accounts[0].Name)
		assert.Equal(t, "Myyjä Oy tili", doc.Epi.Party.BFI.Name)
		require.Len(t, doc.SellerInformation.Accounts, 2)
		assert.Equal(t, finvoice.Account{Value: "12345-678", Scheme: "BBAN"}, doc.SellerInformation.Accounts[1].AccountID)
	})
}
