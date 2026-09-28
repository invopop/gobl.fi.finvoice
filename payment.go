package finvoice

import (
	"regexp"
	"strings"

	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/pay"
)

// Payment codes: who bears the bank charges, and the schemes written beside
// a reference, an account and a bank code.
const (
	chargeOptionShared = "SHA"  // payer and payee share the charges
	chargeOptionSEPA   = "SLEV" // the service level of a SEPA transfer

	referenceSchemeSPY = "SPY"
	referenceSchemeISO = "ISO"

	schemeIBAN = "IBAN"
	schemeBBAN = "BBAN"
	schemeBIC  = "BIC"
)

// Lengths of the payment elements, and how many terms texts there may be.
const (
	beneficiaryNameMaxLength  = 35 // EpiNameAddressDetails
	bankNameMaxLength         = 35 // EpiBfiName
	paymentMeansTextMaxLength = 70 // EpiPaymentMeansText
	accountNameMaxLength      = 70 // SellerAccountName
	termsTextMaxLength        = 70 // each PaymentTermsFreeText
	termsTextLines            = 2
)

var (
	// referenceSPY is a Finnish bank reference number (viitenumero).
	referenceSPY = regexp.MustCompile(`^[0-9]{4,20}$`)
	// referenceISO is an ISO 11649 creditor reference.
	referenceISO = regexp.MustCompile(`^RF[0-9]{2}[0-9A-Za-z]{1,21}$`)
)

// EpiDetails is the payment order: reference, beneficiary account and the
// amount and date to pay.
type EpiDetails struct {
	Identification     EpiIdentificationDetails     `xml:"EpiIdentificationDetails"`
	Party              EpiPartyDetails              `xml:"EpiPartyDetails"`
	PaymentInstruction EpiPaymentInstructionDetails `xml:"EpiPaymentInstructionDetails"`
}

// EpiIdentificationDetails dates and references the payment order.
type EpiIdentificationDetails struct {
	Date      *Date  `xml:"EpiDate"`
	Reference string `xml:"EpiReference"`
}

// EpiPartyDetails names the beneficiary's bank and account.
type EpiPartyDetails struct {
	BFI         EpiBfiPartyDetails         `xml:"EpiBfiPartyDetails"`
	Beneficiary EpiBeneficiaryPartyDetails `xml:"EpiBeneficiaryPartyDetails"`
}

// EpiBfiPartyDetails identifies the beneficiary's bank.
type EpiBfiPartyDetails struct {
	Identifier *Account `xml:"EpiBfiIdentifier,omitempty"`
	Name       string   `xml:"EpiBfiName,omitempty"`
}

// EpiBeneficiaryPartyDetails identifies the payee and its account.
type EpiBeneficiaryPartyDetails struct {
	NameAddress string  `xml:"EpiNameAddressDetails,omitempty"`
	AccountID   Account `xml:"EpiAccountID"`
}

// EpiPaymentInstructionDetails says what to pay, by when, and under which
// reference.
type EpiPaymentInstructionDetails struct {
	InstructionCode          string               `xml:"EpiInstructionCode,omitempty"`
	RemittanceInfoIdentifier *RemittanceReference `xml:"EpiRemittanceInfoIdentifier,omitempty"`
	InstructedAmount         Amount               `xml:"EpiInstructedAmount"`
	Charge                   EpiCharge            `xml:"EpiCharge"`
	DateOptionDate           *Date                `xml:"EpiDateOptionDate"`
	PaymentMeansCode         string               `xml:"EpiPaymentMeansCode,omitempty"`
	PaymentMeansText         string               `xml:"EpiPaymentMeansText,omitempty"`
}

// RemittanceReference is the payment reference with its scheme, SPY or ISO.
type RemittanceReference struct {
	Value  string `xml:",chardata"`
	Scheme string `xml:"IdentificationSchemeName,attr"`
}

// EpiCharge says who bears the bank charges.
type EpiCharge struct {
	Value  string `xml:",chardata"`
	Option string `xml:"ChargeOption,attr"`
}

// SellerAccountDetails is one of the seller's bank accounts.
type SellerAccountDetails struct {
	AccountID Account `xml:"SellerAccountID"`
	BIC       Account `xml:"SellerBic"`
	Name      string  `xml:"SellerAccountName,omitempty"`
}

// PaymentTermsDetails describes when the invoice is due.
type PaymentTermsDetails struct {
	FreeText []string `xml:"PaymentTermsFreeText,omitempty"`
	DueDate  *Date    `xml:"InvoiceDueDate,omitempty"`
}

// newEpiDetails builds the payment order from the payment instructions the
// addon requires: the first credit transfer account, the reference and the
// one due date.
func (c *converter) newEpiDetails() *EpiDetails {
	p := c.inv.Payment
	instr := p.Instructions
	ct := instr.CreditTransfer[0]
	payee := c.inv.Supplier
	if p.Payee != nil {
		payee = p.Payee
	}
	charge := chargeOptionShared
	if instr.Key.Has(pay.MeansKeySEPA) {
		charge = chargeOptionSEPA
	}

	epi := &EpiDetails{
		Identification: EpiIdentificationDetails{
			Date:      newDate(c.inv.IssueDate),
			Reference: instr.Ref.String(),
		},
		Party: EpiPartyDetails{
			Beneficiary: EpiBeneficiaryPartyDetails{
				NameAddress: strings.TrimSpace(cut(payee.Name, beneficiaryNameMaxLength)),
				AccountID:   *newAccountID(ct),
			},
		},
		PaymentInstruction: EpiPaymentInstructionDetails{
			RemittanceInfoIdentifier: newRemittanceInfo(instr),
			InstructedAmount: Amount{
				Value:    formatEpiAmount(c.signed(c.instructedAmount())),
				Currency: c.cur.String(),
			},
			Charge:           EpiCharge{Value: charge, Option: charge},
			DateOptionDate:   newDate(*p.Terms.DueDates[0].Date),
			PaymentMeansCode: instr.Ext.Get(untdid.ExtKeyPaymentMeans).String(),
			PaymentMeansText: cut(instr.Detail, paymentMeansTextMaxLength),
		},
	}
	if ct.BIC != cbc.CodeEmpty {
		epi.Party.BFI.Identifier = &Account{Value: ct.BIC.String(), Scheme: schemeBIC}
		epi.Party.BFI.Name = strings.TrimSpace(cut(ct.Name, bankNameMaxLength))
	}
	return epi
}

// instructedAmount is what remains to be paid: the amount due, or the
// payable total when nothing was paid in advance.
func (c *converter) instructedAmount() num.Amount {
	t := c.inv.Totals
	if t.Due != nil {
		return *t.Due
	}
	return t.Payable
}

// newSellerAccounts lists the accounts a transfer may be paid to, leaving
// out one without a BIC, which only the payment order's account may lack.
func (c *converter) newSellerAccounts() []*SellerAccountDetails {
	var out []*SellerAccountDetails
	for _, ct := range c.inv.Payment.Instructions.CreditTransfer {
		if ct == nil || ct.BIC == cbc.CodeEmpty {
			continue
		}
		id := newAccountID(ct)
		if id == nil {
			continue
		}
		out = append(out, &SellerAccountDetails{
			AccountID: *id,
			BIC:       Account{Value: ct.BIC.String(), Scheme: schemeBIC},
			Name:      cut(ct.Name, accountNameMaxLength),
		})
	}
	return out
}

func (c *converter) newPaymentTerms() []*PaymentTermsDetails {
	terms := c.inv.Payment.Terms
	out := &PaymentTermsDetails{
		FreeText: split(terms.Notes, termsTextMaxLength, termsTextLines),
		DueDate:  newDatePtr(terms.DueDates[0].Date),
	}
	return []*PaymentTermsDetails{out}
}

// newAccountID writes the account's IBAN, or its BBAN, or nil when it has
// neither.
func newAccountID(ct *pay.CreditTransfer) *Account {
	switch {
	case ct.IBAN != cbc.CodeEmpty:
		return &Account{Value: ct.IBAN.String(), Scheme: schemeIBAN}
	case ct.Number != cbc.CodeEmpty:
		return &Account{Value: ct.Number.String(), Scheme: schemeBBAN}
	}
	return nil
}

// newRemittanceInfo writes the payment reference with its scheme: SPY for a
// Finnish bank reference number, ISO for an RF creditor reference. Banks
// only take references whose check digits hold, so any other reference
// stays in EpiReference only.
func newRemittanceInfo(instr *pay.Instructions) *RemittanceReference {
	ref := instr.Ref.String()
	switch {
	case isCreditorReference(ref):
		return &RemittanceReference{Value: ref, Scheme: referenceSchemeISO}
	case isFinnishReference(ref):
		return &RemittanceReference{Value: ref, Scheme: referenceSchemeSPY}
	}
	return nil
}

// isFinnishReference checks the last digit against the rest weighted 7, 3,
// 1 from the right.
func isFinnishReference(ref string) bool {
	if !referenceSPY.MatchString(ref) {
		return false
	}
	sum := 0
	weights := []int{7, 3, 1}
	for i, j := len(ref)-2, 0; i >= 0; i, j = i-1, j+1 {
		sum += int(ref[i]-'0') * weights[j%3]
	}
	return (10-sum%10)%10 == int(ref[len(ref)-1]-'0')
}

// isCreditorReference checks an ISO 11649 reference: moved to the end and
// read with letters as numbers, it leaves 1 modulo 97.
func isCreditorReference(ref string) bool {
	if !referenceISO.MatchString(ref) {
		return false
	}
	rest := 0
	for _, r := range strings.ToUpper(ref[4:] + ref[:4]) {
		switch {
		case r >= '0' && r <= '9':
			rest = (rest*10 + int(r-'0')) % 97
		default:
			rest = (rest*100 + int(r-'A') + 10) % 97
		}
	}
	return rest == 1
}
