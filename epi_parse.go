package fifinvoice

import (
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
)

// paymentMeansSEPA is the UNTDID 4461 code for a SEPA credit transfer.
const paymentMeansSEPA = "58"

// advanceDescription names the amount the document says was already paid.
const advanceDescription = "Paid"

// payment reads the payment order into GOBL's instructions and terms.
func (p *parser) payment() (*bill.PaymentDetails, error) {
	details := &bill.PaymentDetails{}
	if epi := p.doc.Epi; epi != nil {
		details.Instructions = p.instructions(epi)
		details.Payee = p.payee(epi)
	}
	terms, err := p.terms()
	if err != nil {
		return nil, err
	}
	details.Terms = terms
	if paid, err := p.amount(p.det.PaidAmount); err != nil {
		return nil, err
	} else if paid != nil && !paid.IsZero() {
		details.Advances = []*pay.Record{{Description: advanceDescription, Amount: *paid}}
	}
	if details.Instructions == nil && details.Terms == nil && details.Advances == nil && details.Payee == nil {
		return nil, nil
	}
	return details, nil
}

// instructions reads the payment order's account, then any further account
// the seller lists.
func (p *parser) instructions(epi *EpiDetails) *pay.Instructions {
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
	ct := creditTransfer(epi.Party.Beneficiary.AccountID)
	if bfi := epi.Party.BFI.Identifier; bfi != nil {
		ct.BIC = cbc.Code(strings.TrimSpace(bfi.Value))
	}
	ct.Name = strings.TrimSpace(epi.Party.BFI.Name)
	if ct.IBAN != "" || ct.Number != "" {
		instr.CreditTransfer = []*pay.CreditTransfer{ct}
	}
	if info := p.doc.SellerInformation; info != nil {
		for _, acc := range info.Accounts {
			if acc == nil {
				continue
			}
			extra := creditTransfer(acc.AccountID)
			extra.BIC = cbc.Code(strings.TrimSpace(acc.BIC.Value))
			extra.Name = strings.TrimSpace(acc.Name)
			if extra.IBAN == ct.IBAN && extra.Number == ct.Number {
				continue
			}
			instr.CreditTransfer = append(instr.CreditTransfer, extra)
		}
	}
	return instr
}

func creditTransfer(account Account) *pay.CreditTransfer {
	ct := &pay.CreditTransfer{}
	switch strings.ToUpper(strings.TrimSpace(account.Scheme)) {
	case schemeBBAN:
		ct.Number = cbc.Code(strings.TrimSpace(account.Value))
	default:
		ct.IBAN = cbc.Code(strings.TrimSpace(account.Value))
	}
	return ct
}

// payee is the beneficiary when the payment order names someone other than
// the seller.
func (p *parser) payee(epi *EpiDetails) *org.Party {
	name := strings.TrimSpace(epi.Party.Beneficiary.NameAddress)
	var seller string
	if p.doc.Seller != nil {
		seller = joinNames(p.doc.Seller.Name)
	}
	if name == "" || name == seller || name == cut(seller, beneficiaryNameMaxLength) {
		return nil
	}
	return &org.Party{Name: name}
}

// terms reads the due date, preferring the payment order's, and the terms
// text. The whole amount falls due on that date; a document with nothing
// to pay gets the date alone.
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
		dd := &pay.DueDate{Date: date}
		if !p.stated.IsZero() {
			full := num.MakePercentage(1, 0)
			dd.Percent = &full
		}
		terms.DueDates = []*pay.DueDate{dd}
	}
	terms.Notes = strings.TrimSpace(strings.Join(texts, " "))
	if terms.DueDates == nil && terms.Notes == "" {
		return nil, nil
	}
	return terms, nil
}
