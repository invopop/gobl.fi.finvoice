package fifinvoice

import (
	"fmt"
	"regexp"

	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/pay"
)

// Finvoice payment order (EPI) constants.
const (
	// chargeOptionShared means bank charges are shared by payer and payee;
	// chargeOptionSEPA is the service-level option of a SEPA transfer.
	chargeOptionShared = "SHA"
	chargeOptionSEPA   = "SLEV"

	referenceSchemeSPY = "SPY"
	referenceSchemeISO = "ISO"

	// beneficiaryNameMaxLength bounds EpiNameAddressDetails.
	beneficiaryNameMaxLength = 35
	// referenceMaxLengthEpi bounds EpiReference; a reference is never cut.
	referenceMaxLengthEpi = 35
)

var (
	// referenceSPY is a Finnish bank reference number (viitenumero).
	referenceSPY = regexp.MustCompile(`^[0-9]{2,20}$`)
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
	RemittanceInfoIdentifier *Account  `xml:"EpiRemittanceInfoIdentifier,omitempty"`
	InstructedAmount         Amount    `xml:"EpiInstructedAmount"`
	Charge                   EpiCharge `xml:"EpiCharge"`
	DateOptionDate           *Date     `xml:"EpiDateOptionDate"`
	PaymentMeansCode         string    `xml:"EpiPaymentMeansCode,omitempty"`
	PaymentMeansText         string    `xml:"EpiPaymentMeansText,omitempty"`
}

// EpiCharge says who bears the bank charges.
type EpiCharge struct {
	Value  string `xml:",chardata"`
	Option string `xml:"ChargeOption,attr"`
}

// newEpiDetails builds the payment order from the payment instructions the
// addon requires: the first credit transfer account, the reference and the
// one due date.
func (c *converter) newEpiDetails() (*EpiDetails, error) {
	p := c.inv.Payment
	instr := p.Instructions
	ct := instr.CreditTransfer[0]
	if tooLong(instr.Ref.String(), referenceMaxLengthEpi) {
		return nil, fmt.Errorf("payment reference %q is longer than the %d characters Finvoice allows", instr.Ref, referenceMaxLengthEpi)
	}
	due, err := dueDate(p.Terms)
	if err != nil {
		return nil, err
	}
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
				NameAddress: cut(payee.Name, beneficiaryNameMaxLength),
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
			DateOptionDate:   newDate(due),
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
	if t.Due != nil {
		return *t.Due
	}
	return t.Payable
}

// newAccountID writes the account's IBAN, which the addon requires on every
// credit transfer it validates.
func newAccountID(ct *pay.CreditTransfer) *Account {
	if ct == nil || ct.IBAN == "" {
		return nil
	}
	return &Account{Value: ct.IBAN.String(), Scheme: schemeIBAN}
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

// dueDate is the payment order's one date. Finvoice pays the whole amount
// on it, so instalments over several dates cannot be expressed and are
// refused.
func dueDate(terms *pay.Terms) (cal.Date, error) {
	var dates []cal.Date
	for _, dd := range terms.DueDates {
		if dd != nil && dd.Date != nil {
			dates = append(dates, *dd.Date)
		}
	}
	if len(dates) > 1 {
		return cal.Date{}, fmt.Errorf("payment terms have %d due dates, Finvoice carries one", len(dates))
	}
	return dates[0], nil
}

// firstDueDate is the due date the payment terms name, if any.
func firstDueDate(terms *pay.Terms) *cal.Date {
	for _, dd := range terms.DueDates {
		if dd != nil && dd.Date != nil {
			return dd.Date
		}
	}
	return nil
}
