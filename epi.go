package fifinvoice

import (
	"errors"

	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/pay"
)

// Finvoice payment order (EPI) constants.
const (
	// chargeOptionShared means bank charges are shared by payer and payee,
	// the usual choice for SEPA transfers.
	chargeOptionShared = "SHA"

	referenceSchemeSPY = "SPY"
	referenceSchemeISO = "ISO"

	// beneficiaryNameMaxLength bounds EpiNameAddressDetails.
	beneficiaryNameMaxLength = 35
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
	RemittanceInfoIdentifier *Account   `xml:"EpiRemittanceInfoIdentifier,omitempty"`
	InstructedAmount         Amount     `xml:"EpiInstructedAmount"`
	Charge                   EpiCharge  `xml:"EpiCharge"`
	DateOptionDate           *Date      `xml:"EpiDateOptionDate"`
	PaymentMeansCode         string     `xml:"EpiPaymentMeansCode,omitempty"`
	PaymentMeansText         string     `xml:"EpiPaymentMeansText,omitempty"`
}

// EpiCharge says who bears the bank charges.
type EpiCharge struct {
	Value  string `xml:",chardata"`
	Option string `xml:"ChargeOption,attr"`
}

var (
	errPaymentInstructionsRequired = errors.New("payment instructions with a credit transfer account are required (Finvoice EpiDetails)")
	errDueDateRequired             = errors.New("a payment due date is required (Finvoice EpiDateOptionDate)")
)

// newEpiDetails builds the payment order from the payment instructions the
// addon requires: the first credit transfer account, the reference and the
// first due date.
func (c *converter) newEpiDetails() (*EpiDetails, error) {
	p := c.inv.Payment
	if p == nil || p.Instructions == nil || len(p.Instructions.CreditTransfer) == 0 {
		return nil, errPaymentInstructionsRequired
	}
	instr := p.Instructions
	ct := instr.CreditTransfer[0]
	account := newAccountID(ct)
	if ct == nil || account == nil {
		return nil, errPaymentInstructionsRequired
	}
	due := firstDueDate(p.Terms)
	if due == nil {
		return nil, errDueDateRequired
	}

	epi := &EpiDetails{
		Identification: EpiIdentificationDetails{
			Date:      newDate(c.inv.IssueDate),
			Reference: instr.Ref.String(),
		},
		Party: EpiPartyDetails{
			Beneficiary: EpiBeneficiaryPartyDetails{
				NameAddress: cut(c.inv.Supplier.Name, beneficiaryNameMaxLength),
				AccountID:   *account,
			},
		},
		PaymentInstruction: EpiPaymentInstructionDetails{
			RemittanceInfoIdentifier: newRemittanceInfo(instr),
			InstructedAmount: Amount{
				Value:    formatEpiAmount(c.signed(c.instructedAmount())),
				Currency: c.cur.String(),
			},
			Charge:           EpiCharge{Value: chargeOptionShared, Option: chargeOptionShared},
			DateOptionDate:   newDate(*due),
			PaymentMeansCode: instr.Ext.Get(untdid.ExtKeyPaymentMeans).String(),
		},
	}
	if ct.BIC != "" {
		epi.Party.BFI.Identifier = &Account{Value: ct.BIC.String(), Scheme: schemeBIC}
		epi.Party.BFI.Name = cut(ct.Name, beneficiaryNameMaxLength)
	}
	return epi, nil
}

// instructedAmount is what remains to be paid: the amount due, or the
// payable total when nothing was paid in advance.
func (c *converter) instructedAmount() num.Amount {
	t := c.inv.Totals
	if t == nil {
		return num.MakeAmount(0, c.cur.Def().Subunits)
	}
	if t.Due != nil {
		return *t.Due
	}
	return t.Payable
}

// newAccountID writes the account as an IBAN, or a BBAN when only a plain
// account number is given.
func newAccountID(ct *pay.CreditTransfer) *Account {
	switch {
	case ct == nil:
		return nil
	case ct.IBAN != "":
		return &Account{Value: ct.IBAN.String(), Scheme: schemeIBAN}
	case ct.Number != "":
		return &Account{Value: ct.Number.String(), Scheme: schemeBBAN}
	}
	return nil
}

// newRemittanceInfo writes the payment reference with its scheme: SPY for a
// Finnish bank reference number, ISO for an RF creditor reference. Other
// references stay in EpiReference only.
func newRemittanceInfo(instr *pay.Instructions) *Account {
	ref := instr.Ref.String()
	switch {
	case referenceISO.MatchString(ref):
		return &Account{Value: ref, Scheme: referenceSchemeISO}
	case referenceSPY.MatchString(ref):
		return &Account{Value: ref, Scheme: referenceSchemeSPY}
	}
	return nil
}

func firstDueDate(terms *pay.Terms) *cal.Date {
	if terms == nil {
		return nil
	}
	for _, dd := range terms.DueDates {
		if dd != nil && dd.Date != nil {
			return dd.Date
		}
	}
	return nil
}
