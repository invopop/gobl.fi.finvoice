package finvoice

import (
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
)

// paymentMeansKeys maps UNTDID 4461 payment means codes to GOBL keys, as the
// EN 16931 addon pairs them. Any other code is read as the credit transfer
// a Finvoice payment order describes.
var paymentMeansKeys = map[string]cbc.Key{
	"1":  pay.MeansKeyAny,
	"10": pay.MeansKeyCash,
	"20": pay.MeansKeyCheque,
	"21": pay.MeansKeyBankDraft,
	"30": pay.MeansKeyCreditTransfer,
	"31": pay.MeansKeyDebitTransfer,
	"48": pay.MeansKeyCard,
	"49": pay.MeansKeyDirectDebit,
	"55": pay.MeansKeyCard.With(pay.MeansKeyDebit),
	"58": pay.MeansKeyCreditTransfer.With(pay.MeansKeySEPA),
	"59": pay.MeansKeyDirectDebit.With(pay.MeansKeySEPA),
	"60": pay.MeansKeyPromissoryNote,
	"68": pay.MeansKeyOnline,
	"97": pay.MeansKeyNetting,
}

// advanceDescription names the amount the document says was already paid.
const advanceDescription = "Paid"

// payment reads the payment order into GOBL's instructions and terms.
func (p *parser) payment() (*bill.PaymentDetails, error) {
	details := &bill.PaymentDetails{}
	if epi := p.doc.Epi; epi != nil {
		// The amount to pay is the sender's to state and is not mapped, but
		// it must be in the document's currency like every other amount.
		if _, err := p.amount(&epi.PaymentInstruction.InstructedAmount); err != nil {
			return nil, fmt.Errorf("payment order: %w", err)
		}
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
	pi := epi.PaymentInstruction
	instr := &pay.Instructions{
		Key:    pay.MeansKeyCreditTransfer,
		Detail: strings.TrimSpace(pi.PaymentMeansText),
	}
	// The EN 16931 addon records the code for the key. Finvoice 1.3 gave
	// the code as EpiInstructionCode.
	code := firstNonEmpty(strings.TrimSpace(pi.PaymentMeansCode), strings.TrimSpace(pi.InstructionCode))
	if key, ok := paymentMeansKeys[code]; ok {
		instr.Key = key
	}
	if pi.RemittanceInfoIdentifier != nil && strings.TrimSpace(pi.RemittanceInfoIdentifier.Value) != "" {
		instr.Ref = cbc.Code(strings.TrimSpace(pi.RemittanceInfoIdentifier.Value))
	} else {
		instr.Ref = cbc.Code(strings.TrimSpace(epi.Identification.Reference))
	}
	ct := goblCreditTransfer(epi.Party.Beneficiary.AccountID)
	if bfi := epi.Party.BFI.Identifier; bfi != nil {
		ct.BIC = cbc.Code(strings.TrimSpace(bfi.Value))
	}
	ct.Name = strings.TrimSpace(epi.Party.BFI.Name)
	if ct.IBAN != cbc.CodeEmpty || ct.Number != cbc.CodeEmpty {
		instr.CreditTransfer = []*pay.CreditTransfer{ct}
	}
	if info := p.doc.SellerInformation; info != nil {
		for _, acc := range info.Accounts {
			extra := goblCreditTransfer(acc.AccountID)
			extra.BIC = cbc.Code(strings.TrimSpace(acc.BIC.Value))
			extra.Name = strings.TrimSpace(acc.Name)
			if extra.IBAN == ct.IBAN && extra.Number == ct.Number {
				// The payment order's account listed again, with what the
				// order may have left out.
				if ct.BIC == cbc.CodeEmpty {
					ct.BIC = extra.BIC
				}
				if ct.Name == "" {
					ct.Name = extra.Name
				}
				continue
			}
			instr.CreditTransfer = append(instr.CreditTransfer, extra)
		}
	}
	return instr
}

// goblCreditTransfer reads an account in its machine form, as the addon
// normalizes it, so the same account spelled two ways compares equal.
func goblCreditTransfer(account Account) *pay.CreditTransfer {
	ct := &pay.CreditTransfer{}
	code := cbc.NormalizeAlphanumericalCode(cbc.Code(account.Value))
	switch strings.TrimSpace(account.Scheme) {
	case schemeBBAN:
		ct.Number = code
	default:
		ct.IBAN = code
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
	if name == "" || name == seller || name == strings.TrimSpace(cut(seller, beneficiaryNameMaxLength)) {
		return nil
	}
	return &org.Party{Name: name}
}

// terms reads the payment order's due date, for the whole amount, and the
// terms text, refusing instalments, which the payment order cannot hold.
func (p *parser) terms() (*pay.Terms, error) {
	terms := &pay.Terms{}
	var texts []string
	var dates []cal.Date
	for _, t := range p.det.PaymentTerms {
		texts = append(texts, t.FreeText...)
		d, err := parseDatePtr(t.DueDate)
		if err != nil {
			return nil, err
		}
		if d != nil && !slices.Contains(dates, *d) {
			dates = append(dates, *d)
		}
	}
	if len(dates) > 1 {
		return nil, fmt.Errorf("payment terms give %d due dates, the payment order takes one", len(dates))
	}
	var date *cal.Date
	if epi := p.doc.Epi; epi != nil {
		var err error
		if date, err = parseDatePtr(epi.PaymentInstruction.DateOptionDate); err != nil {
			return nil, err
		}
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
