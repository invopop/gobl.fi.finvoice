package fifinvoice

import (
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

func (p *parser) preceding() []*org.DocumentRef {
	det := p.det
	var refs []*org.DocumentRef
	if det.OriginalInvoiceNumber != "" {
		ref := &org.DocumentRef{Code: cbc.Code(strings.TrimSpace(det.OriginalInvoiceNumber))}
		ref.IssueDate, _ = parseDatePtr(det.OriginalInvoiceDate)
		refs = append(refs, ref)
	}
	for _, r := range det.OriginalInvoiceReference {
		if r == nil || strings.TrimSpace(r.InvoiceNumber) == "" {
			continue
		}
		ref := &org.DocumentRef{Code: cbc.Code(strings.TrimSpace(r.InvoiceNumber))}
		ref.IssueDate, _ = parseDatePtr(r.InvoiceDate)
		refs = append(refs, ref)
	}
	return refs
}

func (p *parser) ordering() *bill.Ordering {
	det := p.det
	o := &bill.Ordering{Code: cbc.Code(strings.TrimSpace(det.BuyerReference))}
	if det.OrderIdentifier != "" {
		ref := &org.DocumentRef{Code: cbc.Code(strings.TrimSpace(det.OrderIdentifier))}
		ref.IssueDate, _ = parseDatePtr(det.OrderDate)
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
	if o.Code == "" && o.Purchases == nil && o.Sales == nil && o.Contracts == nil && o.Projects == nil && o.Tender == nil {
		return nil
	}
	return o
}

func (p *parser) delivery() *bill.DeliveryDetails {
	d := &bill.DeliveryDetails{}
	if p.doc.DeliveryDetails != nil {
		d.Date, _ = parseDatePtr(p.doc.DeliveryDetails.Date)
		if per := p.doc.DeliveryDetails.Period; per != nil {
			d.Period = parsePeriod(per.StartDate, per.EndDate)
		}
	}
	if d.Period == nil {
		d.Period = parsePeriod(p.det.PeriodStartDate, p.det.PeriodEndDate)
	}
	if dp := p.doc.DeliveryParty; dp != nil && dp.Name != "" {
		receiver := &org.Party{Name: strings.TrimSpace(dp.Name)}
		if dp.Address != nil {
			receiver.Addresses = []*org.Address{parseAddress(dp.Address.StreetName, dp.Address.TownName,
				dp.Address.PostCode, dp.Address.Subdivision, dp.Address.CountryCode, dp.Address.PostOfficeBox)}
		}
		receiver.TaxID, receiver.Identities = parseIdentification(dp.TaxCode, dp.Identifier, "")
		d.Receiver = receiver
	}
	if d.Date == nil && d.Period == nil && d.Receiver == nil {
		return nil
	}
	return d
}

func parsePeriod(start, end *Date) *cal.Period {
	s, err := parseDatePtr(start)
	if err != nil || s == nil {
		return nil
	}
	e, err := parseDatePtr(end)
	if err != nil || e == nil {
		return nil
	}
	return &cal.Period{Start: s, End: e}
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
		if combo, err := p.vatCombo(d.VatCategoryCode, d.VatRatePercent); err != nil {
			return nil, err
		} else if combo != nil {
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
		if combo, err := p.vatCombo(c.VatCategoryCode, c.VatRatePercent); err != nil {
			return nil, err
		} else if combo != nil {
			ch.Taxes = tax.Set{combo}
		}
		out = append(out, ch)
	}
	return out, nil
}

// vatCombo builds the VAT tax for a row, discount or charge from its
// category code and rate. Without a code, the rate decides: a positive one is
// standard-rated, zero is zero-rated, unless the VAT breakdown names the
// category for that rate.
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
		code = p.categoryForRate(*pct)
	}
	combo := &tax.Combo{Category: tax.CategoryVAT}
	key, known := vatCategoryKeys[cbc.Code(code)]
	switch {
	case known:
		combo.Key = key
	case pct == nil || pct.IsZero():
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
		combo.Ext = combo.Ext.Set(untdidVATEX, cbc.Code(vatex))
	}
	return combo, nil
}

// categoryForRate finds the category the VAT breakdown gives for a rate,
// for rows that carry the rate alone.
func (p *parser) categoryForRate(pct num.Percentage) string {
	for _, spec := range p.det.VatSpecifications {
		if spec == nil || spec.Code == "" {
			continue
		}
		sp, err := p.percent(spec.RatePercent)
		if err != nil || sp == nil {
			continue
		}
		if sp.Equals(pct) {
			return strings.TrimSpace(spec.Code)
		}
	}
	return ""
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

// taxNotes keeps the exemption reasons the breakdown gives in words.
func (p *parser) taxNotes() []*tax.Note {
	var notes []*tax.Note
	for _, spec := range p.det.VatSpecifications {
		if spec == nil || len(spec.FreeText) == 0 {
			continue
		}
		key, ok := vatCategoryKeys[cbc.Code(strings.TrimSpace(spec.Code))]
		if !ok || key == tax.KeyStandard || key == tax.KeyZero {
			continue
		}
		notes = append(notes, &tax.Note{
			Category: tax.CategoryVAT,
			Key:      key,
			Text:     strings.TrimSpace(strings.Join(spec.FreeText, " ")),
		})
	}
	return notes
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
