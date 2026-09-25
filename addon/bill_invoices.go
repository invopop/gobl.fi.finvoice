package finvoice

import (
	"fmt"
	"unicode/utf8"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
	"github.com/invopop/gobl/tax"
)

// Lengths of the Finvoice elements that hold identifiers.
const (
	invoiceNumberMaxLength     = 20
	referenceMaxLength         = 70
	articleIdentifierMaxLength = 70
	paymentReferenceMaxLength  = 35
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
		rules.Field("supplier",
			rules.Field("ext",
				rules.Assert("13", fmt.Sprintf("supplier '%s' extension must be an operator identifier (Finvoice FromIntermediator)", ExtKeyOperator),
					tax.ExtensionHasValidCode(ExtKeyOperator),
				),
			),
		),
		rules.Field("customer",
			rules.Assert("01", "customer is required (Finvoice BuyerPartyDetails)", is.Present),
			rules.Field("name",
				rules.Assert("02", "customer name is required (Finvoice BuyerOrganisationName)", is.Present),
			),
			rules.Field("ext",
				rules.Assert("14", fmt.Sprintf("customer '%s' extension must be an operator identifier (Finvoice ToIntermediator)", ExtKeyOperator),
					tax.ExtensionHasValidCode(ExtKeyOperator),
				),
			),
		),
		rules.Assert("15", fmt.Sprintf("invoice number with its series must be at most %d characters (Finvoice InvoiceNumber)", invoiceNumberMaxLength),
			is.Func("invoice number fits", invoiceNumberFits),
		),
		rules.Field("preceding",
			rules.Each(
				rules.Assert("16", fmt.Sprintf("preceding document number must be at most %d characters (Finvoice OriginalInvoiceNumber)", invoiceNumberMaxLength),
					is.Func("document number fits", precedingNumberFits),
				),
			),
		),
		rules.Field("ordering",
			rules.Assert("17", fmt.Sprintf("ordering references must be at most %d characters (Finvoice BuyerReferenceIdentifier, OrderIdentifier and the other ordering references)", referenceMaxLength),
				is.Func("ordering references fit", orderingReferencesFit),
			),
		),
		rules.Field("lines",
			rules.Each(
				rules.Field("item",
					rules.Field("ref",
						rules.Assert("18", fmt.Sprintf("item reference must be at most %d characters (Finvoice ArticleIdentifier)", articleIdentifierMaxLength),
							is.RuneLength(0, articleIdentifierMaxLength),
						),
					),
				),
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
					rules.Assert("19", fmt.Sprintf("payment reference must be at most %d characters (Finvoice EpiReference)", paymentReferenceMaxLength),
						is.RuneLength(0, paymentReferenceMaxLength),
					),
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
					rules.Assert("20", "at most one due date is allowed (Finvoice EpiDateOptionDate)", is.Length(0, 1)),
				),
			),
		),
	)
}

func invoiceNumberFits(val any) bool {
	inv, ok := val.(*bill.Invoice)
	return !ok || inv == nil || fits(inv.Series.Join(inv.Code), invoiceNumberMaxLength)
}

func precedingNumberFits(val any) bool {
	ref, ok := val.(*org.DocumentRef)
	return !ok || ref == nil || fits(ref.Series.Join(ref.Code), invoiceNumberMaxLength)
}

// orderingReferencesFit checks the buyer's reference and every document the
// ordering points at.
func orderingReferencesFit(val any) bool {
	o, ok := val.(*bill.Ordering)
	if !ok || o == nil {
		return true
	}
	if !fits(o.Code, referenceMaxLength) {
		return false
	}
	for _, refs := range [][]*org.DocumentRef{o.Sales, o.Purchases, o.Contracts, o.Projects, o.Tender} {
		for _, ref := range refs {
			if ref != nil && !fits(ref.Series.Join(ref.Code), referenceMaxLength) {
				return false
			}
		}
	}
	return true
}

func fits(code cbc.Code, n int) bool {
	return utf8.RuneCountInString(code.String()) <= n
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
