package addon_test

import (
	"strings"
	"testing"

	_ "github.com/invopop/gobl"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/tax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInvoiceStandard(t *testing.T) *bill.Invoice {
	t.Helper()
	return &bill.Invoice{
		Regime:    tax.WithRegime("FI"),
		Addons:    tax.WithAddons(addon.V3),
		IssueDate: cal.MakeDate(2026, 7, 1),
		Type:      bill.InvoiceTypeStandard,
		Currency:  currency.EUR,
		Series:    "2026",
		Code:      "1000",
		Supplier: &org.Party{
			Name: "Myyjä Oy",
			TaxID: &tax.Identity{
				Country: "FI",
				Code:    "76543212",
			},
			Endpoints: []*org.Endpoint{{URI: "iso6523-actorid-upis::0216:003776543212"}},
			Addresses: []*org.Address{
				{
					Street:   "Esimerkkikatu 1",
					Locality: "Helsinki",
					Code:     "00100",
					Country:  "FI",
				},
			},
		},
		Customer: &org.Party{
			Name: "Ostaja Oy",
			TaxID: &tax.Identity{
				Country: "FI",
				Code:    "45678907",
			},
			Endpoints: []*org.Endpoint{{URI: "iso6523-actorid-upis::0216:003745678907"}},
			Addresses: []*org.Address{
				{
					Street:   "Testitie 2",
					Locality: "Espoo",
					Code:     "02100",
					Country:  "FI",
				},
			},
		},
		Payment: &bill.PaymentDetails{
			Instructions: &pay.Instructions{
				Key: pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA),
				Ref: "RF18539007547034",
				CreditTransfer: []*pay.CreditTransfer{
					{
						IBAN: "FI2112345600000785",
					},
				},
			},
			Terms: &pay.Terms{
				DueDates: []*pay.DueDate{
					{
						Date:   cal.NewDate(2026, 7, 31),
						Amount: num.NewAmount(12550, 2),
					},
				},
			},
		},
		Lines: []*bill.Line{
			{
				Quantity: num.MakeAmount(10, 0),
				Item: &org.Item{
					Name:  "Test Item",
					Price: num.NewAmount(1000, 2),
					Unit:  "item",
				},
				Taxes: tax.Set{
					{
						Category: tax.CategoryVAT,
						Rate:     tax.RateGeneral,
					},
				},
			},
		},
	}
}

func TestBillInvoiceRules(t *testing.T) {
	t.Run("valid invoice", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		require.NoError(t, inv.Calculate())
		assert.NoError(t, rules.Validate(inv))
	})

	t.Run("valid invoice without BIC", func(t *testing.T) {
		// Finvoice recommends the BIC alongside the IBAN but the schema does
		// not require it, so its absence must not fail validation.
		inv := testInvoiceStandard(t)
		require.NoError(t, inv.Calculate())
		require.Empty(t, inv.Payment.Instructions.CreditTransfer[0].BIC)
		assert.NoError(t, rules.Validate(inv))
	})

	t.Run("missing customer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.SetTags(tax.TagSimplified)
		inv.Customer = nil
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "customer is required")
	})

	t.Run("missing customer name", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Customer.Name = ""
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "customer name is required")
	})

	t.Run("missing payment", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment = nil
		require.NoError(t, inv.Calculate())
		faults := rules.Validate(inv)
		require.NotNil(t, faults)
		assert.True(t, faults.HasCode("GOBL-FI-FINVOICE-BILL-INVOICE-03"))
	})

	t.Run("credit note requires payment too", func(t *testing.T) {
		// en16931 only requires payment details when an amount is due, which
		// exempts credit notes. Finvoice's EpiDetails is mandatory on every
		// invoice, so the addon must fault here on its own.
		inv := testInvoiceStandard(t)
		inv.Type = bill.InvoiceTypeCreditNote
		inv.Preceding = []*org.DocumentRef{
			{
				Series:    "2026",
				Code:      "0999",
				IssueDate: cal.NewDate(2026, 6, 1),
			},
		}
		inv.Payment = nil
		require.NoError(t, inv.Calculate())
		faults := rules.Validate(inv)
		require.NotNil(t, faults)
		assert.True(t, faults.HasCode("GOBL-FI-FINVOICE-BILL-INVOICE-03"))
	})

	t.Run("missing payment instructions", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions = nil
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "payment instructions are required")
	})

	t.Run("instructions key must be credit transfer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.Key = pay.MeansKeyCash
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "credit-transfer")
	})

	t.Run("missing payment reference", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.Ref = ""
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "payment reference is required")
	})

	t.Run("missing credit transfer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer = nil
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "credit transfer details are required")
	})

	t.Run("missing IBAN in first credit transfer", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].IBAN = ""
		inv.Payment.Instructions.CreditTransfer[0].Number = "12345-678"
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "IBAN is required")
	})

	t.Run("invalid IBAN checksum", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].IBAN = "FI2112345600000786"
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "IBAN is not valid")
	})

	t.Run("malformed IBAN", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].IBAN = "NOT-AN-IBAN"
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "IBAN is not valid")
	})

	t.Run("IBAN shorter than the ISO 13616 minimum", func(t *testing.T) {
		// "FI2100021" satisfies the mod-97 checksum, so only the length
		// floor (15 characters, BBAN >= 11) rejects it.
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].IBAN = "FI2100021"
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "IBAN is not valid")
	})

	t.Run("malformed BIC", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].BIC = "NDEA"
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "BIC is not valid")
	})

	t.Run("valid BIC", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].BIC = "NDEAFIHH"
		require.NoError(t, inv.Calculate())
		assert.NoError(t, rules.Validate(inv))
	})

	t.Run("due date entry requires a date", func(t *testing.T) {
		// Guaranteed by the pay package's own DueDate rules, which the
		// combined rule set applies to every entry — the addon only has to
		// require the list to be non-empty for EpiDateOptionDate.
		inv := testInvoiceStandard(t)
		inv.Payment.Terms.DueDates[0].Date = nil
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "date is required")
	})

	t.Run("missing payment terms", func(t *testing.T) {
		// A credit note has no amount due, so en16931's BR-CO-25 does not
		// require terms here — only the addon does.
		inv := testInvoiceStandard(t)
		inv.Type = bill.InvoiceTypeCreditNote
		inv.Preceding = []*org.DocumentRef{
			{
				Series:    "2026",
				Code:      "0999",
				IssueDate: cal.NewDate(2026, 6, 1),
			},
		}
		inv.Payment.Terms = nil
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "payment terms are required")
	})

	t.Run("terms with notes but no due dates", func(t *testing.T) {
		// en16931 accepts terms with notes only (BR-CO-25); Finvoice needs a
		// dated entry to fill EpiDateOptionDate.
		inv := testInvoiceStandard(t)
		inv.Payment.Terms = &pay.Terms{
			Notes: "Payable on receipt",
		}
		require.NoError(t, inv.Calculate())
		err := rules.Validate(inv)
		assert.ErrorContains(t, err, "due date is required")
	})
}

func TestNormalization(t *testing.T) {
	t.Run("strips spaces and uppercases IBAN", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.CreditTransfer[0].IBAN = "fi21 1234 5600 0007 85"
		require.NoError(t, inv.Calculate())
		assert.Equal(t, "FI2112345600000785", inv.Payment.Instructions.CreditTransfer[0].IBAN.String())
		assert.NoError(t, rules.Validate(inv))
	})

	t.Run("strips spaces from payment reference", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Payment.Instructions.Ref = "RF18 5390 0754 7034"
		require.NoError(t, inv.Calculate())
		assert.Equal(t, "RF18539007547034", inv.Payment.Instructions.Ref.String())
	})
}

// ruleCase adjusts the standard invoice and names the fault it should raise, or none.
type ruleCase struct {
	name   string
	adjust func(inv *bill.Invoice)
	want   string
}

func runRuleCases(t *testing.T, tests []ruleCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := testInvoiceStandard(t)
			tt.adjust(inv)
			require.NoError(t, inv.Calculate())
			err := rules.Validate(inv)
			if tt.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func digits(n int) cbc.Code {
	return cbc.Code(strings.Repeat("9", n))
}

// payInFull sets one due date for the whole amount, whatever the totals come to.
func payInFull(inv *bill.Invoice) {
	full := num.MakePercentage(1, 0)
	inv.Payment.Terms.DueDates = []*pay.DueDate{{Date: cal.NewDate(2026, 7, 31), Percent: &full}}
}

func TestEInvoiceAddressRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"a customer with a coded inbox instead of an endpoint", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = nil
			inv.Customer.Inboxes = []*org.Inbox{{Scheme: "0216", Code: "003745678907"}}
		}, ""},
		{"a supplier without an e-invoice address", func(inv *bill.Invoice) {
			inv.Supplier.Endpoints = nil
		}, "supplier needs an e-invoice address"},
		{"a customer without an e-invoice address", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = nil
		}, "customer needs an e-invoice address"},
	})
}

func TestEInvoiceAddressLengthRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"e-invoice address of 35", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::0216:" + digits(35))}}
		}, ""},
		{"e-invoice address of 2", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::0216:" + digits(2))}}
		}, ""},
		{"e-invoice address of 36", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::0216:" + digits(36))}}
		}, "($.customer) e-invoice address code must be 2 to 35 characters"},
		{"e-invoice address of 1", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::0216:" + digits(1))}}
		}, "($.customer) e-invoice address code must be 2 to 35 characters"},
		{"e-invoice address scheme of 11", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::" + digits(11) + ":003745678907")}}
		}, "e-invoice address code must be 2 to 35 characters and its scheme at most 10"},
		{"a customer inbox code of 36", func(inv *bill.Invoice) {
			inv.Customer.Endpoints = nil
			inv.Customer.Inboxes = []*org.Inbox{{Scheme: "0216", Code: digits(36)}}
		}, "e-invoice address code must be 2 to 35 characters"},
	})
}

func TestPartyNameRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"names of two characters", func(inv *bill.Invoice) {
			inv.Supplier.Name = "Oy"
			inv.Customer.Name = "Ab"
			inv.Delivery = &bill.DeliveryDetails{Receiver: &org.Party{Name: "Ky",
				Addresses: []*org.Address{{Locality: "Ii", Code: "91", Country: "FI"}}}}
		}, ""},
		{"a supplier name of one character", func(inv *bill.Invoice) { inv.Supplier.Name = "M" },
			"supplier name must be at least 2 characters"},
		{"a customer name of one character", func(inv *bill.Invoice) { inv.Customer.Name = "O" },
			"customer name must be at least 2 characters"},
		{"a delivery receiver name of one character", func(inv *bill.Invoice) {
			inv.Delivery = &bill.DeliveryDetails{Receiver: &org.Party{Name: "V"}}
		}, "delivery receiver name must be at least 2 characters"},
	})
}

func TestLegalIdentityRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"legal identity of 35", func(inv *bill.Invoice) {
			inv.Customer.Identities = []*org.Identity{{Scope: org.IdentityScopeLegal, Code: digits(35)}}
		}, ""},
		{"identity of another scope of 80", func(inv *bill.Invoice) {
			inv.Customer.Identities = []*org.Identity{{Scope: org.IdentityScopeTax, Code: digits(80)}}
		}, ""},
		{"a payee with a long legal identity, which is never written", func(inv *bill.Invoice) {
			inv.Payment.Payee = &org.Party{Name: "Perintä Oy", Identities: []*org.Identity{{Scope: org.IdentityScopeLegal, Code: digits(36)}}}
		}, ""},
		{"legal identity of 36", func(inv *bill.Invoice) {
			inv.Customer.Identities = []*org.Identity{{Scope: org.IdentityScopeLegal, Code: digits(36)}}
		}, "($.customer.identities[0].code) legal identity code must be at most 35 characters"},
	})
}

func TestInvoiceNumberRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"invoice number of 20 with its series", func(inv *bill.Invoice) { inv.Code = digits(15) }, ""},
		{"preceding number of 20", func(inv *bill.Invoice) {
			inv.Type = bill.InvoiceTypeCreditNote
			inv.Preceding = []*org.DocumentRef{{Code: digits(20), IssueDate: cal.NewDate(2026, 6, 1)}}
		}, ""},
		{"no invoice code", func(inv *bill.Invoice) { inv.Code = "" }, "invoice code is required"},
		{"invoice number of 21 with its series", func(inv *bill.Invoice) { inv.Code = digits(16) },
			"invoice number with its series must be at most 20 characters"},
		{"preceding number of 21", func(inv *bill.Invoice) {
			inv.Type = bill.InvoiceTypeCreditNote
			inv.Preceding = []*org.DocumentRef{{Code: digits(21), IssueDate: cal.NewDate(2026, 6, 1)}}
		}, "preceding document number must be at most 20 characters"},
	})
}

func TestAttachmentRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"an attachment, which travels as a separate message", func(inv *bill.Invoice) {
			inv.Attachments = []*org.Attachment{{Name: "erittely.pdf", URL: "https://myyja.fi/erittely.pdf"}}
		}, "attachments are not supported"},
	})
}

func TestOrderingRules(t *testing.T) {
	ref := func(code cbc.Code) []*org.DocumentRef { return []*org.DocumentRef{{Code: code}} }
	runRuleCases(t, []ruleCase{
		{"references of 70", func(inv *bill.Invoice) {
			inv.Ordering = &bill.Ordering{Code: digits(70), Sales: ref(digits(70)), Purchases: ref(digits(70)),
				Contracts: ref(digits(70)), Projects: ref(digits(70)), Tender: ref(digits(70))}
		}, ""},
		{"two purchase orders", func(inv *bill.Invoice) {
			inv.Ordering = &bill.Ordering{Purchases: []*org.DocumentRef{{Code: "PO-1"}, {Code: "PO-2"}}}
		}, "at most one reference of each kind is allowed"},
	})

	t.Run("a reference of 71 of each kind", func(t *testing.T) {
		long := digits(71)
		want := "ordering references must be at most 70 characters"
		runRuleCases(t, []ruleCase{
			{"buyer", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Code: long} }, want},
			{"sales", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Sales: ref(long)} }, want},
			{"order", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Purchases: ref(long)} }, want},
			{"contract", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Contracts: ref(long)} }, want},
			{"project", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Projects: ref(long)} }, want},
			{"tender", func(inv *bill.Invoice) { inv.Ordering = &bill.Ordering{Tender: ref(long)} }, want},
		})
	})
}

func TestDeliveryRules(t *testing.T) {
	receiver := func() *org.Party {
		return &org.Party{Name: "Varasto Oy", Addresses: []*org.Address{{Street: "Satamakatu 3", Locality: "Turku", Code: "20100", Country: "FI"}}}
	}
	runRuleCases(t, []ruleCase{
		{"a delivery receiver with a name and an address", func(inv *bill.Invoice) {
			inv.Delivery = &bill.DeliveryDetails{Receiver: receiver()}
		}, ""},
		{"a delivery receiver with a long endpoint, which is never written", func(inv *bill.Invoice) {
			r := receiver()
			r.Endpoints = []*org.Endpoint{{URI: cbc.URI("iso6523-actorid-upis::0216:" + digits(36))}}
			inv.Delivery = &bill.DeliveryDetails{Receiver: r}
		}, ""},
		{"a delivery receiver with a long legal identity", func(inv *bill.Invoice) {
			r := receiver()
			r.Identities = []*org.Identity{{Scope: org.IdentityScopeLegal, Code: digits(36)}}
			inv.Delivery = &bill.DeliveryDetails{Receiver: r}
		}, "legal identity code must be at most 35 characters"},
		{"a delivery receiver without an address", func(inv *bill.Invoice) {
			inv.Delivery = &bill.DeliveryDetails{Receiver: &org.Party{Name: "Varasto Oy"}}
		}, "delivery receiver needs a name and an address with a town and post code"},
		{"a delivery receiver whose address has no post code", func(inv *bill.Invoice) {
			r := receiver()
			r.Addresses[0].Code = ""
			inv.Delivery = &bill.DeliveryDetails{Receiver: r}
		}, "delivery receiver needs a name and an address with a town and post code"},
		{"a delivery receiver without a name", func(inv *bill.Invoice) {
			r := receiver()
			r.Name = ""
			inv.Delivery = &bill.DeliveryDetails{Receiver: r}
		}, "delivery receiver needs a name and an address with a town and post code"},
		{"delivery period with one date", func(inv *bill.Invoice) {
			inv.Delivery = &bill.DeliveryDetails{Period: &cal.Period{Start: cal.NewDate(2026, 6, 1)}}
		}, "delivery period must have both dates"},
	})
}

func TestExemptionRules(t *testing.T) {
	exempt := func(reason cbc.Code) tax.Set {
		return tax.Set{{Category: tax.CategoryVAT, Key: tax.KeyExempt, Ext: tax.ExtensionsOf(cbc.CodeMap{cef.ExtKeyVATEX: reason})}}
	}
	runRuleCases(t, []ruleCase{
		{"a charge exempt for the same reason as the lines", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = exempt("VATEX-EU-132")
			inv.Charges = []*bill.Charge{{Amount: num.MakeAmount(500, 2), Reason: "Rahti", Taxes: exempt("VATEX-EU-132")}}
			payInFull(inv)
		}, ""},
		{"two exemption reasons in one category", func(inv *bill.Invoice) {
			line := func(reason cbc.Code) *bill.Line {
				return &bill.Line{Quantity: num.MakeAmount(1, 0), Item: &org.Item{Name: "Hoito", Price: num.NewAmount(10000, 2)}, Taxes: exempt(reason)}
			}
			inv.Lines = []*bill.Line{line("VATEX-EU-132"), line("VATEX-EU-135")}
		}, "one exemption reason per VAT category"},
		{"two exemption notes in one category", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = tax.Set{{Category: tax.CategoryVAT, Key: tax.KeyExempt}}
			inv.Tax = &bill.Tax{Notes: []*tax.Note{
				{Category: tax.CategoryVAT, Key: tax.KeyExempt, Text: "Terveydenhuolto"},
				{Category: tax.CategoryVAT, Key: tax.KeyExempt, Text: "Koulutus"},
			}}
		}, "one exemption reason per VAT category"},
		{"a charge exempt for another reason than the lines", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = exempt("VATEX-EU-132")
			inv.Charges = []*bill.Charge{{Amount: num.MakeAmount(500, 2), Reason: "Rahti", Taxes: exempt("VATEX-EU-135")}}
		}, "one exemption reason per VAT category"},
	})
}

func TestVATRateRules(t *testing.T) {
	rate := func(p num.Percentage) tax.Set {
		return tax.Set{{Category: tax.CategoryVAT, Key: tax.KeyStandard, Percent: &p}}
	}
	runRuleCases(t, []ruleCase{
		{"a rate of three decimals", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = rate(num.MakePercentage(12125, 5))
			payInFull(inv)
		}, ""},
		{"a rate whose further decimals are zeros", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = rate(num.MakePercentage(1212500, 7))
			payInFull(inv)
		}, ""},
		{"a rate of four decimals", func(inv *bill.Invoice) {
			inv.Lines[0].Taxes = rate(num.MakePercentage(121255, 6))
			payInFull(inv)
		}, "VAT percentage must have at most 3 decimals"},
		{"a charge at a rate of four decimals", func(inv *bill.Invoice) {
			inv.Charges = []*bill.Charge{{Amount: num.MakeAmount(500, 2), Reason: "Rahti", Taxes: rate(num.MakePercentage(121255, 6))}}
			payInFull(inv)
		}, "VAT percentage must have at most 3 decimals"},
	})
}

func TestLineRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"item reference of 70", func(inv *bill.Invoice) { inv.Lines[0].Item.Ref = digits(70) }, ""},
		{"a quantity of 14 characters", func(inv *bill.Invoice) {
			inv.Lines[0].Quantity = num.MakeAmount(1234567890123, 2)
			payInFull(inv)
		}, ""},
		{"a negative quantity of 14 characters on a credit note, written without its sign", func(inv *bill.Invoice) {
			inv.Type = bill.InvoiceTypeCreditNote
			inv.Lines[0].Quantity = num.MakeAmount(-123456789012, 2)
			inv.Lines = append(inv.Lines, &bill.Line{Quantity: num.MakeAmount(2000, 0), Item: inv.Lines[0].Item, Taxes: inv.Lines[0].Taxes})
			payInFull(inv)
		}, ""},
		{"item reference of 71", func(inv *bill.Invoice) { inv.Lines[0].Item.Ref = digits(71) },
			"item reference must be at most 70 characters"},
		{"a quantity of 14 characters on a credit note, which writes the sign", func(inv *bill.Invoice) {
			inv.Type = bill.InvoiceTypeCreditNote
			inv.Lines[0].Quantity = num.MakeAmount(1234567890123, 2)
			payInFull(inv)
		}, "quantity must be at most 14 characters"},
		{"a quantity of 15 characters", func(inv *bill.Invoice) {
			inv.Lines[0].Quantity = num.MakeAmount(12345678901234, 2)
			payInFull(inv)
		}, "quantity must be at most 14 characters"},
	})
}

func TestPaymentInstructionRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"payment reference of 35", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = digits(35) }, ""},
		{"a second account as a BBAN", func(inv *bill.Invoice) {
			inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
				&pay.CreditTransfer{Number: "12345-678", BIC: "OKOYFIHH"})
		}, ""},
		{"currency without decimals", func(inv *bill.Invoice) {
			inv.Currency = "JPY"
			inv.ExchangeRates = []*currency.ExchangeRate{{From: "JPY", To: "EUR", Amount: num.MakeAmount(6, 3)}}
		}, ""},
		{"payment reference of 36", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = digits(36) },
			"payment reference must be at most 35 characters"},
		{"a second account with a one-character number", func(inv *bill.Invoice) {
			inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
				&pay.CreditTransfer{Number: "7", BIC: "OKOYFIHH"})
		}, "account number must be 2 to 35 characters"},
		{"a second account without a BIC", func(inv *bill.Invoice) {
			inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
				&pay.CreditTransfer{IBAN: "FI4250001510000023"})
		}, "credit transfers after the first need a BIC"},
		{"currency with three decimals", func(inv *bill.Invoice) {
			inv.Currency = "BHD"
			inv.ExchangeRates = []*currency.ExchangeRate{{From: "BHD", To: "EUR", Amount: num.MakeAmount(24, 1)}}
		}, "currency must use at most 2 decimals"},
	})
}

func TestPaymentTermRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"a due date for what remains after an advance", func(inv *bill.Invoice) {
			inv.Payment.Advances = []*pay.Record{{Description: "Ennakko", Amount: num.MakeAmount(2750, 2)}}
			inv.Payment.Terms.DueDates = []*pay.DueDate{{Date: cal.NewDate(2026, 7, 31), Amount: num.NewAmount(9800, 2)}}
		}, ""},
		{"a due date for part of the amount", func(inv *bill.Invoice) {
			half := num.MakePercentage(50, 2)
			inv.Payment.Terms.DueDates = []*pay.DueDate{{Date: cal.NewDate(2026, 7, 31), Percent: &half}}
		}, "the due date must cover the whole amount"},
		{"a due date for part of the amount given as an amount", func(inv *bill.Invoice) {
			inv.Payment.Terms.DueDates = []*pay.DueDate{{Date: cal.NewDate(2026, 7, 31), Amount: num.NewAmount(5000, 2)}}
		}, "the due date must cover the whole amount"},
		{"instalments", func(inv *bill.Invoice) {
			half := num.MakePercentage(50, 2)
			inv.Payment.Terms.DueDates = []*pay.DueDate{
				{Date: cal.NewDate(2026, 7, 15), Percent: &half},
				{Date: cal.NewDate(2026, 7, 31), Percent: &half},
			}
		}, "at most one due date is allowed"},
	})
}
