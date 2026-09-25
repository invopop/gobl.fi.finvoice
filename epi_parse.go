package fifinvoice

import (
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/pay"
)

// paymentMeansSEPA is the UNTDID 4461 code for a SEPA credit transfer.
const paymentMeansSEPA = "58"

// payment reads the payment order into GOBL's instructions and terms.
func (p *parser) payment() (*bill.PaymentDetails, error) {
	details := &bill.PaymentDetails{}
	if epi := p.doc.Epi; epi != nil {
		details.Instructions = parseInstructions(epi)
	}
	terms, err := p.terms()
	if err != nil {
		return nil, err
	}
	details.Terms = terms
	if paid, err := p.amount(p.det.PaidAmount); err != nil {
		return nil, err
	} else if paid != nil && !paid.IsZero() {
		details.Advances = []*pay.Record{{Description: "Paid", Amount: *paid}}
	}
	if details.Instructions == nil && details.Terms == nil && details.Advances == nil {
		return nil, nil
	}
	return details, nil
}

func parseInstructions(epi *EpiDetails) *pay.Instructions {
	instr := &pay.Instructions{Key: pay.MeansKeyCreditTransfer}
	pi := epi.PaymentInstruction
	if strings.TrimSpace(pi.PaymentMeansCode) == paymentMeansSEPA {
		instr.Key = instr.Key.With(pay.MeansKeySEPA)
	}
	if pi.RemittanceInfoIdentifier != nil && strings.TrimSpace(pi.RemittanceInfoIdentifier.Value) != "" {
		instr.Ref = cbc.Code(strings.TrimSpace(pi.RemittanceInfoIdentifier.Value))
	} else {
		instr.Ref = cbc.Code(strings.TrimSpace(epi.Identification.Reference))
	}
	ct := &pay.CreditTransfer{}
	account := epi.Party.Beneficiary.AccountID
	switch strings.ToUpper(strings.TrimSpace(account.Scheme)) {
	case schemeBBAN:
		ct.Number = cbc.Code(strings.TrimSpace(account.Value))
	default:
		ct.IBAN = cbc.Code(strings.TrimSpace(account.Value))
	}
	if bfi := epi.Party.BFI.Identifier; bfi != nil {
		ct.BIC = cbc.Code(strings.TrimSpace(bfi.Value))
	}
	ct.Name = strings.TrimSpace(epi.Party.BFI.Name)
	if ct.IBAN != "" || ct.Number != "" {
		instr.CreditTransfer = []*pay.CreditTransfer{ct}
	}
	return instr
}

// terms reads the due date, preferring the payment order's, and the terms
// text.
func (p *parser) terms() (*pay.Terms, error) {
	terms := &pay.Terms{}
	var texts []string
	var due *Date
	for _, t := range p.det.PaymentTerms {
		if t == nil {
			continue
		}
		texts = append(texts, t.FreeText...)
		if due == nil {
			due = t.DueDate
		}
	}
	if epi := p.doc.Epi; epi != nil && epi.PaymentInstruction.DateOptionDate != nil {
		due = epi.PaymentInstruction.DateOptionDate
	}
	date, err := parseDatePtr(due)
	if err != nil {
		return nil, err
	}
	if date != nil {
		full := num.MakePercentage(1, 0)
		terms.DueDates = []*pay.DueDate{{Date: date, Percent: &full}}
	}
	terms.Notes = strings.TrimSpace(strings.Join(texts, " "))
	if terms.DueDates == nil && terms.Notes == "" {
		return nil, nil
	}
	return terms, nil
}
