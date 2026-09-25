package finvoice

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
)

// Finvoice's EpiDetails payment block is mandatory on every invoice,
// including credit notes, so the payment rules below apply unconditionally
// rather than only when an amount is due (en16931 BR-CO-25).
//
// EpiBfiIdentifier (the BIC) is optional in the Finvoice schema and SEPA
// practice no longer requires it for domestic transfers, so the addon
// recommends but does not require it.
func billInvoiceRules() *rules.Set {
	return rules.For(new(bill.Invoice),
		rules.Field("customer",
			rules.Assert("01", "customer is required (Finvoice BuyerPartyDetails)", is.Present),
			rules.Field("name",
				rules.Assert("02", "customer name is required (Finvoice BuyerOrganisationName)", is.Present),
			),
		),
		rules.Field("payment",
			rules.Assert("03", "payment details are required (Finvoice EpiDetails)", is.Present),
			rules.Field("instructions",
				rules.Assert("04", "payment instructions are required (Finvoice EpiDetails)", is.Present),
				rules.Field("key",
					rules.Assert("05", "payment instructions key must be credit-transfer",
						cbc.HasValidKeyIn(pay.MeansKeyCreditTransfer),
					),
				),
				rules.Field("ref",
					rules.Assert("06", "payment reference is required (Finvoice EpiReference)", is.Present),
				),
				rules.Field("credit_transfer",
					rules.Assert("07", "credit transfer details are required (Finvoice EpiAccountID)", is.Present),
					rules.Assert("08", "first credit transfer IBAN is required (Finvoice EpiAccountID)",
						is.Func("first entry has IBAN", firstCreditTransferHasIBAN),
					),
					rules.Each(
						rules.Field("iban",
							rules.AssertIfPresent("09", "credit transfer IBAN is not valid",
								pay.IsIBAN,
							),
						),
						rules.Field("bic",
							rules.AssertIfPresent("10", "credit transfer BIC is not valid (Finvoice EpiBfiIdentifier)",
								pay.IsBIC,
							),
						),
					),
				),
			),
			rules.Field("terms",
				rules.Assert("11", "payment terms are required (Finvoice EpiDateOptionDate)", is.Present),
				rules.Field("due_dates",
					rules.Assert("12", "at least one due date is required (Finvoice EpiDateOptionDate)", is.Present),
				),
			),
		),
	)
}

// firstCreditTransferHasIBAN checks the entry that becomes EpiAccountID.
// An empty list is handled by the presence assertion.
func firstCreditTransferHasIBAN(val any) bool {
	cts, ok := val.([]*pay.CreditTransfer)
	if !ok || len(cts) == 0 {
		return true
	}
	return cts[0] != nil && cts[0].IBAN != ""
}

// normalizePayInstructions drops the grouping spaces conventionally used when
// displaying Finnish reference numbers and RF references.
func normalizePayInstructions(instr *pay.Instructions) {
	instr.Ref = cbc.NormalizeAlphanumericalCode(instr.Ref)
}

// normalizePayCreditTransfer converts the IBAN to its machine form: no
// grouping spaces, upper case.
func normalizePayCreditTransfer(ct *pay.CreditTransfer) {
	ct.IBAN = cbc.NormalizeAlphanumericalCode(ct.IBAN)
}
