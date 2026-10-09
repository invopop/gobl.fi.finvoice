package addon

import (
	"fmt"
	"unicode/utf8"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/num"
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
	quantityMaxLength          = 14
	nameMinLength              = 2
	accountNumberMinLength     = 2
	accountNumberMaxLength     = 35
)

// epiAmountDecimals is the exact precision of the payment order's amount,
// vatPercentDecimals the most a VAT rate may have; a GOBL percentage holds
// the fraction, so its exponent is two more than the rate's decimals.
const (
	epiAmountDecimals  = 2
	vatPercentDecimals = 3
	vatPercentExp      = vatPercentDecimals + 2
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
			rules.Field("name",
				rules.Assert("38", fmt.Sprintf("supplier name must be at least %d characters (Finvoice SellerOrganisationName)", nameMinLength),
					is.RuneLength(nameMinLength, 0),
				),
			),
			rules.Assert("13", "supplier needs an e-invoice address, an ISO 6523 endpoint or a coded inbox (Finvoice SellerOrganisationUnitNumber)",
				is.Func("e-invoice address", hasEInvoiceAddress),
			),
			rules.Field("identities",
				rules.Each(
					rules.When(is.Func("legal scope", isLegalIdentity),
						rules.Field("code",
							rules.Assert("14", fmt.Sprintf("legal identity code must be at most %d characters (Finvoice SellerPartyIdentifier)", identityMaxLength),
								is.RuneLength(0, identityMaxLength),
							),
						),
					),
				),
			),
			rules.Assert("15", fmt.Sprintf("e-invoice address code must be %d to %d characters and its scheme at most %d (Finvoice SellerOrganisationUnitNumber, FromIdentifier, ToIdentifier)", einvoiceAddressMinLength, einvoiceAddressMaxLength, einvoiceSchemeMaxLength),
				is.Func("e-invoice address fits", einvoiceAddressFits),
			),
			rules.Field("ext",
				rules.Assert("16", fmt.Sprintf("supplier '%s' extension must be 2 to 35 letters or digits", ExtKeyOperator),
					tax.ExtensionHasValidCode(ExtKeyOperator),
				),
			),
		),
		rules.Field("customer",
			rules.Assert("01", "customer is required (Finvoice BuyerPartyDetails)", is.Present),
			rules.Field("name",
				rules.Assert("02", "customer name is required (Finvoice BuyerOrganisationName)", is.Present),
				rules.Assert("39", fmt.Sprintf("customer name must be at least %d characters (Finvoice BuyerOrganisationName)", nameMinLength),
					is.RuneLength(nameMinLength, 0),
				),
			),
			rules.Assert("17", "customer needs an e-invoice address, an ISO 6523 endpoint or a coded inbox (Finvoice BuyerOrganisationUnitNumber)",
				is.Func("e-invoice address", hasEInvoiceAddress),
			),
			rules.Field("identities",
				rules.Each(
					rules.When(is.Func("legal scope", isLegalIdentity),
						rules.Field("code",
							rules.Assert("18", fmt.Sprintf("legal identity code must be at most %d characters (Finvoice BuyerPartyIdentifier)", identityMaxLength),
								is.RuneLength(0, identityMaxLength),
							),
						),
					),
				),
			),
			rules.Assert("19", fmt.Sprintf("e-invoice address code must be %d to %d characters and its scheme at most %d (Finvoice BuyerOrganisationUnitNumber, FromIdentifier, ToIdentifier)", einvoiceAddressMinLength, einvoiceAddressMaxLength, einvoiceSchemeMaxLength),
				is.Func("e-invoice address fits", einvoiceAddressFits),
			),
			rules.Field("ext",
				rules.Assert("20", fmt.Sprintf("customer '%s' extension must be 2 to 35 letters or digits (Finvoice ToIntermediator)", ExtKeyOperator),
					tax.ExtensionHasValidCode(ExtKeyOperator),
				),
			),
		),
		rules.Field("code",
			rules.Assert("21", "invoice code is required (Finvoice InvoiceNumber)", is.Present),
		),
		rules.Field("attachments",
			rules.Assert("22", "attachments are not supported, Finvoice carries them as separate attachment messages", is.Empty),
		),
		rules.Assert("23", fmt.Sprintf("invoice number with its series must be at most %d characters (Finvoice InvoiceNumber)", invoiceNumberMaxLength),
			is.Func("invoice number fits", invoiceNumberFits),
		),
		rules.Field("currency",
			rules.Assert("24", fmt.Sprintf("currency must use at most %d decimals (Finvoice EpiInstructedAmount)", epiAmountDecimals),
				is.Func("currency fits the payment order", currencyFitsPaymentOrder),
			),
		),
		rules.Field("preceding",
			rules.Each(
				rules.Assert("25", fmt.Sprintf("preceding document number must be at most %d characters (Finvoice OriginalInvoiceNumber)", invoiceNumberMaxLength),
					is.Func("document number fits", precedingNumberFits),
				),
			),
		),
		rules.Field("ordering",
			rules.Assert("26", fmt.Sprintf("ordering references must be at most %d characters (Finvoice BuyerReferenceIdentifier, SellerReferenceIdentifier, OrderIdentifier, AgreementIdentifier, ProjectReferenceIdentifier, TenderReference)", referenceMaxLength),
				is.Func("ordering references fit", orderingReferencesFit),
			),
			rules.Assert("27", "at most one reference of each kind is allowed (Finvoice SellerReferenceIdentifier, OrderIdentifier, AgreementIdentifier, ProjectReferenceIdentifier, TenderReference)",
				is.Func("one reference of each kind", orderingReferencesSingle),
			),
		),
		rules.Field("delivery",
			rules.Field("receiver",
				rules.Field("name",
					rules.AssertIfPresent("40", fmt.Sprintf("delivery receiver name must be at least %d characters (Finvoice DeliveryOrganisationName)", nameMinLength),
						is.RuneLength(nameMinLength, 0),
					),
				),
				rules.Field("identities",
					rules.Each(
						rules.When(is.Func("legal scope", isLegalIdentity),
							rules.Field("code",
								rules.Assert("28", fmt.Sprintf("legal identity code must be at most %d characters (Finvoice DeliveryPartyIdentifier)", identityMaxLength),
									is.RuneLength(0, identityMaxLength),
								),
							),
						),
					),
				),
			),
			rules.Field("period",
				rules.Assert("29", "delivery period must have both dates (Finvoice DeliveryPeriodDetails)",
					is.Func("both dates", periodHasBothDates),
				),
			),
		),
		rules.Assert("30", "the due date must cover the whole amount (Finvoice EpiInstructedAmount)",
			is.Func("whole amount", dueDateCoversAll),
		),
		rules.Assert("31", "one exemption reason per VAT category (Finvoice VatExemptionReasonCode, VatFreeText)",
			is.Func("one reason per category", oneExemptionReasonPerCategory),
		),
		rules.Assert("37", fmt.Sprintf("VAT percentage must have at most %d decimals (Finvoice RowVatRatePercent, VatRatePercent)", vatPercentDecimals),
			is.Func("VAT percentages fit", vatPercentsFit),
		),
		rules.Assert("32", fmt.Sprintf("quantity must be at most %d characters (Finvoice InvoicedQuantity)", quantityMaxLength),
			is.Func("quantities fit", quantitiesFit),
		),
		rules.Field("lines",
			rules.Each(
				rules.Field("item",
					rules.Field("ref",
						rules.Assert("33", fmt.Sprintf("item reference must be at most %d characters (Finvoice ArticleIdentifier)", articleIdentifierMaxLength),
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
					rules.Assert("34", fmt.Sprintf("payment reference must be at most %d characters (Finvoice EpiReference)", paymentReferenceMaxLength),
						is.RuneLength(0, paymentReferenceMaxLength),
					),
				),
				rules.Field("credit_transfer",
					rules.Assert("07", "credit transfer details are required (Finvoice EpiAccountID)", is.Present),
					rules.Assert("08", "first credit transfer IBAN is required (Finvoice EpiAccountID)",
						is.Func("first entry has IBAN", firstCreditTransferHasIBAN),
					),
					rules.Assert("35", "credit transfers after the first need a BIC (Finvoice SellerBic)",
						is.Func("later entries have a BIC", laterCreditTransfersHaveBIC),
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
						rules.Field("number",
							rules.AssertIfPresent("41", fmt.Sprintf("credit transfer account number must be %d to %d characters (Finvoice SellerAccountID)", accountNumberMinLength, accountNumberMaxLength),
								is.RuneLength(accountNumberMinLength, accountNumberMaxLength),
							),
						),
					),
				),
			),
			rules.Field("terms",
				rules.Assert("11", "payment terms are required (Finvoice EpiDateOptionDate)", is.Present),
				rules.Field("due_dates",
					rules.Assert("12", "at least one due date is required (Finvoice EpiDateOptionDate)", is.Present),
					rules.Assert("36", "at most one due date is allowed (Finvoice EpiDateOptionDate)", is.Length(0, 1)),
				),
			),
		),
	)
}

func invoiceNumberFits(val any) bool {
	inv, ok := val.(*bill.Invoice)
	return !ok || inv == nil || fits(inv.Series.Join(inv.Code), invoiceNumberMaxLength)
}

// quantitiesFit measures each quantity as Finvoice writes it, negated on a
// credit note.
func quantitiesFit(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv == nil {
		return true
	}
	credit := inv.Type.In(bill.InvoiceTypeCreditNote)
	for _, line := range inv.Lines {
		if line == nil {
			continue
		}
		q := line.Quantity
		if credit {
			q = q.Negate()
		}
		if len(q.String()) > quantityMaxLength {
			return false
		}
	}
	return true
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

// orderingReferencesSingle checks each list holds one document, as each
// Finvoice element holds one identifier.
func orderingReferencesSingle(val any) bool {
	o, ok := val.(*bill.Ordering)
	if !ok || o == nil {
		return true
	}
	for _, refs := range [][]*org.DocumentRef{o.Sales, o.Purchases, o.Contracts, o.Projects, o.Tender} {
		if len(refs) > 1 {
			return false
		}
	}
	return true
}

// dueDateCoversAll checks the due date asks for the whole amount, as the
// payment order pays everything on its date: all of it, or what remains
// after the advances, which is what the order carries.
func dueDateCoversAll(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv == nil || inv.Payment == nil || inv.Payment.Terms == nil || inv.Totals == nil {
		return true
	}
	for _, dd := range inv.Payment.Terms.DueDates {
		switch {
		case dd == nil:
		case dd.Percent != nil:
			if !dd.Percent.Equals(num.MakePercentage(1, 0)) {
				return false
			}
		case dd.Amount == nil || dd.Amount.Equals(inv.Totals.Payable):
		case inv.Totals.Due != nil && dd.Amount.Equals(*inv.Totals.Due):
		default:
			return false
		}
	}
	return true
}

// taxSets lists the taxes of every line, discount and charge.
func taxSets(inv *bill.Invoice) []tax.Set {
	var sets []tax.Set
	for _, line := range inv.Lines {
		if line != nil {
			sets = append(sets, line.Taxes)
		}
	}
	for _, d := range inv.Discounts {
		if d != nil {
			sets = append(sets, d.Taxes)
		}
	}
	for _, c := range inv.Charges {
		if c != nil {
			sets = append(sets, c.Taxes)
		}
	}
	return sets
}

// vatPercentsFit checks every VAT rate can be written whole.
func vatPercentsFit(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv == nil {
		return true
	}
	for _, set := range taxSets(inv) {
		vat := set.Get(tax.CategoryVAT)
		if vat == nil || vat.Percent == nil {
			continue
		}
		if vat.Percent.Rescale(vatPercentExp).Compare(*vat.Percent) != 0 {
			return false
		}
	}
	return true
}

// oneExemptionReasonPerCategory checks the lines, discounts and charges of
// a VAT category share one exemption code, and the category has at most one
// note, since the breakdown gives one reason per category.
func oneExemptionReasonPerCategory(val any) bool {
	inv, ok := val.(*bill.Invoice)
	if !ok || inv == nil {
		return true
	}
	reasons := map[cbc.Key]cbc.Code{}
	for _, set := range taxSets(inv) {
		vat := set.Get(tax.CategoryVAT)
		if vat == nil || vat.Key == cbc.KeyEmpty {
			continue
		}
		code := vat.Ext.Get(cef.ExtKeyVATEX)
		if code == cbc.CodeEmpty {
			continue
		}
		if seen, ok := reasons[vat.Key]; ok && seen != code {
			return false
		}
		reasons[vat.Key] = code
	}
	if inv.Tax == nil {
		return true
	}
	notes := map[cbc.Key]bool{}
	for _, n := range inv.Tax.Notes {
		if n == nil || n.Category != tax.CategoryVAT {
			continue
		}
		if notes[n.Key] {
			return false
		}
		notes[n.Key] = true
	}
	return true
}

func periodHasBothDates(val any) bool {
	period, ok := val.(*cal.Period)
	return !ok || period == nil || (period.Start != nil && period.End != nil)
}

func currencyFitsPaymentOrder(val any) bool {
	code, ok := val.(currency.Code)
	if !ok || code == currency.CodeEmpty {
		return true
	}
	def := code.Def()
	return def == nil || def.Subunits <= epiAmountDecimals
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
	return cts[0] != nil && cts[0].IBAN != cbc.CodeEmpty
}

// laterCreditTransfersHaveBIC checks the accounts listed beside the payment
// order's, which Finvoice lists with their BIC.
func laterCreditTransfersHaveBIC(val any) bool {
	cts, ok := val.([]*pay.CreditTransfer)
	if !ok {
		return true
	}
	for _, ct := range cts[min(1, len(cts)):] {
		if ct != nil && ct.BIC == cbc.CodeEmpty {
			return false
		}
	}
	return true
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
