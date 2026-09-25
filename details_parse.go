package fifinvoice

import (
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// vatCategoryKeys maps the UNTDID 5305 categories EN 16931 uses to GOBL tax
// keys.
var vatCategoryKeys = map[cbc.Code]cbc.Key{
	en16931.TaxCategoryStandard:       tax.KeyStandard,
	en16931.TaxCategoryZero:           tax.KeyZero,
	en16931.TaxCategoryExempt:         tax.KeyExempt,
	en16931.TaxCategoryReverseCharge:  tax.KeyReverseCharge,
	en16931.TaxCategoryIntraCommunity: tax.KeyIntraCommunity,
	en16931.TaxCategoryExport:         tax.KeyExport,
	en16931.TaxCategoryOutsideScope:   tax.KeyOutsideScope,
}

func (p *parser) preceding() ([]*org.DocumentRef, error) {
	det := p.det
	var refs []*org.DocumentRef
	if det.OriginalInvoiceNumber != "" {
		ref, err := documentRef(det.OriginalInvoiceNumber, det.OriginalInvoiceDate)
		if err != nil {
			return nil, fmt.Errorf("original invoice: %w", err)
		}
		refs = append(refs, ref)
	}
	for _, r := range det.OriginalInvoiceReference {
		if r == nil || strings.TrimSpace(r.InvoiceNumber) == "" {
			continue
		}
		ref, err := documentRef(r.InvoiceNumber, r.InvoiceDate)
		if err != nil {
			return nil, fmt.Errorf("original invoice reference: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func documentRef(code string, date *Date) (*org.DocumentRef, error) {
	ref := &org.DocumentRef{Code: cbc.Code(strings.TrimSpace(code))}
	var err error
	if ref.IssueDate, err = parseDatePtr(date); err != nil {
		return nil, err
	}
	return ref, nil
}

func (p *parser) ordering() (*bill.Ordering, error) {
	det := p.det
	o := &bill.Ordering{Code: cbc.Code(strings.TrimSpace(det.BuyerReference))}
	var err error
	if o.Period, err = parsePeriod(det.PeriodStartDate, det.PeriodEndDate); err != nil {
		return nil, fmt.Errorf("invoicing period: %w", err)
	}
	if det.OrderIdentifier != "" {
		ref, err := documentRef(det.OrderIdentifier, det.OrderDate)
		if err != nil {
			return nil, fmt.Errorf("order: %w", err)
		}
		o.Purchases = []*org.DocumentRef{ref}
	}
	if det.SellerReference != "" {
		o.Sales = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.SellerReference))}}
	}
	if det.AgreementIdentifier != "" {
		o.Contracts = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.AgreementIdentifier))}}
	}
	if det.ProjectReference != "" {
		o.Projects = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.ProjectReference))}}
	}
	if det.TenderReference != "" {
		o.Tender = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.TenderReference))}}
	}
	if o.Code == "" && o.Period == nil && o.Purchases == nil && o.Sales == nil && o.Contracts == nil && o.Projects == nil && o.Tender == nil {
		return nil, nil
	}
	return o, nil
}

func (p *parser) delivery() (*bill.DeliveryDetails, error) {
	d := &bill.DeliveryDetails{}
	if dd := p.doc.DeliveryDetails; dd != nil {
		var err error
		if d.Date, err = parseDatePtr(dd.Date); err != nil {
			return nil, fmt.Errorf("delivery: %w", err)
		}
		if per := dd.Period; per != nil {
			if d.Period, err = parsePeriod(per.StartDate, per.EndDate); err != nil {
				return nil, fmt.Errorf("delivery period: %w", err)
			}
		}
	}
	if dp := p.doc.DeliveryParty; dp != nil && joinNames(dp.Name) != "" {
		receiver := &org.Party{Name: joinNames(dp.Name)}
		if dp.Address != nil {
			receiver.Addresses = []*org.Address{parseAddress(dp.Address.StreetName, dp.Address.TownName,
				dp.Address.PostCode, dp.Address.Subdivision, dp.Address.CountryCode, dp.Address.PostOfficeBox)}
		}
		receiver.TaxID, receiver.Identities = parseIdentification(dp.TaxCode, dp.Identifier, "")
		d.Receiver = receiver
	}
	if d.Date == nil && d.Period == nil && d.Receiver == nil {
		return nil, nil
	}
	return d, nil
}

func parsePeriod(start, end *Date) (*cal.Period, error) {
	s, err := parseDatePtr(start)
	if err != nil {
		return nil, err
	}
	e, err := parseDatePtr(end)
	if err != nil {
		return nil, err
	}
	if s == nil || e == nil {
		return nil, nil
	}
	return &cal.Period{Start: s, End: e}, nil
}

func (p *parser) discounts() ([]*bill.Discount, error) {
	var out []*bill.Discount
	for _, d := range p.det.Discounts {
		if d == nil {
			continue
		}
		dis := &bill.Discount{Reason: strings.TrimSpace(d.FreeText)}
		var err error
		if dis.Percent, err = p.percent(d.Percent); err != nil {
			return nil, err
		}
		amount, err := p.amount(d.Amount)
		if err != nil {
			return nil, err
		}
		if amount != nil {
			dis.Amount = *amount
		}
		if dis.Base, err = p.amount(d.BaseAmount); err != nil {
			return nil, err
		}
		if d.ReasonCode != "" {
			dis.Ext = dis.Ext.Set(untdid.ExtKeyAllowance, cbc.Code(strings.TrimSpace(d.ReasonCode)))
		}
		combo, err := p.vatCombo(d.VatCategoryCode, d.VatRatePercent)
		if err != nil {
			return nil, fmt.Errorf("discount %q: %w", dis.Reason, err)
		}
		if combo != nil {
			dis.Taxes = tax.Set{combo}
		}
		out = append(out, dis)
	}
	return out, nil
}

func (p *parser) charges() ([]*bill.Charge, error) {
	var out []*bill.Charge
	for _, c := range p.det.Charges {
		if c == nil {
			continue
		}
		ch := &bill.Charge{Reason: strings.TrimSpace(c.ReasonText)}
		var err error
		if ch.Percent, err = p.percent(c.Percent); err != nil {
			return nil, err
		}
		amount, err := p.amount(c.Amount)
		if err != nil {
			return nil, err
		}
		if amount != nil {
			ch.Amount = *amount
		}
		if ch.Base, err = p.amount(c.BaseAmount); err != nil {
			return nil, err
		}
		if c.ReasonCode != "" {
			ch.Ext = ch.Ext.Set(untdid.ExtKeyCharge, cbc.Code(strings.TrimSpace(c.ReasonCode)))
		}
		combo, err := p.vatCombo(c.VatCategoryCode, c.VatRatePercent)
		if err != nil {
			return nil, fmt.Errorf("charge %q: %w", ch.Reason, err)
		}
		if combo != nil {
			ch.Taxes = tax.Set{combo}
		}
		out = append(out, ch)
	}
	return out, nil
}

// vatCombo builds the VAT tax for a row, discount or charge, taking the
// category from the breakdown line at the same rate when the row names none.
func (p *parser) vatCombo(code, percent string) (*tax.Combo, error) {
	pct, err := p.percent(percent)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		if pct == nil {
			return nil, nil
		}
		if code, err = p.categoryForRate(*pct); err != nil {
			return nil, err
		}
	}
	combo := &tax.Combo{Category: tax.CategoryVAT}
	key, known := vatCategoryKeys[cbc.Code(code)]
	switch {
	case known:
		combo.Key = key
	case code != "":
		return nil, fmt.Errorf("unknown VAT category code %q", code)
	case pct.IsZero():
		combo.Key = tax.KeyZero
	default:
		combo.Key = tax.KeyStandard
	}
	if combo.Key == tax.KeyZero {
		zero := num.MakePercentage(0, 0)
		combo.Percent = &zero
	} else if pct != nil && !pct.IsZero() {
		combo.Percent = pct
	}
	if vatex := p.exemptionCode(code); vatex != "" {
		combo.Ext = combo.Ext.Set(cef.ExtKeyVATEX, cbc.Code(vatex))
	}
	return combo, nil
}

// categoryForRate finds the category the VAT breakdown gives for a rate,
// for rows that carry the rate alone. Two categories at one rate cannot be
// told apart, so that document is refused rather than guessed.
func (p *parser) categoryForRate(pct num.Percentage) (string, error) {
	var found []string
	for _, spec := range p.det.VatSpecifications {
		if spec == nil || strings.TrimSpace(spec.Code) == "" {
			continue
		}
		sp, err := p.percent(spec.RatePercent)
		if err != nil || sp == nil || !sp.Equals(pct) {
			continue
		}
		code := strings.TrimSpace(spec.Code)
		if !slices.Contains(found, code) {
			found = append(found, code)
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("rows at %s carry no VAT code and the breakdown lists %s at that rate", pct, strings.Join(found, " and "))
}

// exemptionCode finds the VATEX reason the breakdown gives for a category.
func (p *parser) exemptionCode(code string) string {
	for _, spec := range p.det.VatSpecifications {
		if spec != nil && strings.TrimSpace(spec.Code) == code && spec.ExemptionReasonCode != "" {
			return strings.TrimSpace(spec.ExemptionReasonCode)
		}
	}
	return ""
}

// taxNotes keeps the exemption reasons given in words: in the breakdown, or
// failing that on the rows of that category, which the guidelines allow.
func (p *parser) taxNotes(lines []*bill.Line) []*tax.Note {
	var notes []*tax.Note
	for _, spec := range p.det.VatSpecifications {
		if spec == nil {
			continue
		}
		code := cbc.Code(strings.TrimSpace(spec.Code))
		key, ok := vatCategoryKeys[code]
		if !ok || key.In(tax.KeyStandard, tax.KeyZero) {
			continue
		}
		text := strings.TrimSpace(strings.Join(spec.FreeText, " "))
		if text == "" && spec.ExemptionReasonCode == "" {
			text = rowText(lines, key)
		}
		if text == "" {
			continue
		}
		notes = append(notes, &tax.Note{
			Category: tax.CategoryVAT,
			Key:      key,
			Text:     text,
		})
	}
	return notes
}

// rowText is the first note on a line taxed with the given key.
func rowText(lines []*bill.Line, key cbc.Key) string {
	for _, line := range lines {
		vat := vatCombo(line.Taxes)
		if vat == nil || vat.Key != key {
			continue
		}
		for _, n := range line.Notes {
			if n != nil && n.Text != "" {
				return n.Text
			}
		}
	}
	return ""
}

func (p *parser) notes() []*org.Note {
	var notes []*org.Note
	for _, text := range p.det.FreeText {
		if text = strings.TrimSpace(text); text != "" {
			notes = append(notes, &org.Note{Key: org.NoteKeyGeneral, Text: text})
		}
	}
	return notes
}
