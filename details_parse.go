package finvoice

import (
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/bill"
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
		ref, err := goblDocumentRef(det.OriginalInvoiceNumber, det.OriginalInvoiceDate)
		if err != nil {
			return nil, fmt.Errorf("original invoice: %w", err)
		}
		refs = append(refs, ref)
	}
	for _, r := range det.OriginalInvoiceReference {
		if strings.TrimSpace(r.InvoiceNumber) == "" {
			continue
		}
		ref, err := goblDocumentRef(r.InvoiceNumber, r.InvoiceDate)
		if err != nil {
			return nil, fmt.Errorf("original invoice reference: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func goblDocumentRef(code string, date *Date) (*org.DocumentRef, error) {
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
		ref, err := goblDocumentRef(det.OrderIdentifier, det.OrderDate)
		if err != nil {
			return nil, fmt.Errorf("order: %w", err)
		}
		o.Purchases = []*org.DocumentRef{ref}
	}
	if det.SellerReference != "" {
		o.Sales = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.SellerReference))}}
	}
	if det.AgreementIdentifier != "" {
		ref := &org.DocumentRef{Code: cbc.Code(strings.TrimSpace(det.AgreementIdentifier))}
		if ref.IssueDate, err = parseDatePtr(det.AgreementDate); err != nil {
			return nil, fmt.Errorf("agreement: %w", err)
		}
		o.Contracts = []*org.DocumentRef{ref}
	}
	if det.ProjectReference != "" {
		o.Projects = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.ProjectReference))}}
	}
	if det.TenderReference != "" {
		o.Tender = []*org.DocumentRef{{Code: cbc.Code(strings.TrimSpace(det.TenderReference))}}
	}
	if o.Code == cbc.CodeEmpty && o.Period == nil && o.Purchases == nil && o.Sales == nil && o.Contracts == nil && o.Projects == nil && o.Tender == nil {
		return nil, nil
	}
	return o, nil
}

func (p *parser) discounts(sum num.Amount) ([]*bill.Discount, error) {
	var out []*bill.Discount
	for _, d := range p.det.Discounts {
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
		if !p.percentHolds(dis.Percent, dis.Base, &sum, dis.Amount) {
			dis.Percent, dis.Base = nil, nil
		}
		if d.ReasonCode != "" {
			dis.Ext = dis.Ext.Set(untdid.ExtKeyAllowance, cbc.Code(strings.TrimSpace(d.ReasonCode)))
		}
		combo, err := p.newVatCombo(d.VatCategoryCode, d.VatRatePercent)
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

func (p *parser) charges(sum num.Amount) ([]*bill.Charge, error) {
	var out []*bill.Charge
	for _, c := range p.det.Charges {
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
		if !p.percentHolds(ch.Percent, ch.Base, &sum, ch.Amount) {
			ch.Percent, ch.Base = nil, nil
		}
		if c.ReasonCode != "" {
			ch.Ext = ch.Ext.Set(untdid.ExtKeyCharge, cbc.Code(strings.TrimSpace(c.ReasonCode)))
		}
		combo, err := p.newVatCombo(c.VatCategoryCode, c.VatRatePercent)
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

// newVatCombo builds the VAT tax for a row, discount or charge, taking the
// category from the breakdown line at the same rate when the row names none.
func (p *parser) newVatCombo(code, percent string) (*tax.Combo, error) {
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
		zero := num.PercentageZero
		combo.Percent = &zero
	} else if pct != nil && !pct.IsZero() {
		combo.Percent = pct
	}
	vatex, err := p.exemptionCode(code)
	if err != nil {
		return nil, err
	}
	if vatex != "" {
		combo.Ext = combo.Ext.Set(cef.ExtKeyVATEX, cbc.Code(vatex))
	}
	return combo, nil
}

// categoryForRate finds the category the VAT breakdown gives for a rate,
// for rows that give only the rate. Two categories at one rate cannot be
// told apart, so that document is refused.
func (p *parser) categoryForRate(pct num.Percentage) (string, error) {
	var found []string
	for _, spec := range p.det.VatSpecifications {
		if strings.TrimSpace(spec.Code) == "" {
			continue
		}
		sp, err := p.percent(spec.RatePercent)
		if err != nil {
			return "", fmt.Errorf("VAT breakdown: %w", err)
		}
		if sp == nil || !sp.Equals(pct) {
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
// Rows name only their category, so two reasons in one category cannot be
// told apart.
func (p *parser) exemptionCode(code string) (string, error) {
	var found []string
	for _, spec := range p.det.VatSpecifications {
		if strings.TrimSpace(spec.Code) != code || spec.ExemptionReasonCode == "" {
			continue
		}
		if vatex := strings.TrimSpace(spec.ExemptionReasonCode); !slices.Contains(found, vatex) {
			found = append(found, vatex)
		}
	}
	if len(found) > 1 {
		return "", fmt.Errorf("VAT breakdown gives %s two exemption reasons, %s", code, strings.Join(found, " and "))
	}
	return strings.Join(found, ""), nil
}

// taxNotes keeps the exemption reasons given in words: in the breakdown, or
// failing that on the rows of that category, which the guidelines allow.
func (p *parser) taxNotes(lines []*bill.Line) ([]*tax.Note, error) {
	var notes []*tax.Note
	for _, spec := range p.det.VatSpecifications {
		code := cbc.Code(strings.TrimSpace(spec.Code))
		key, ok := vatCategoryKeys[code]
		if !ok && code != cbc.CodeEmpty {
			return nil, fmt.Errorf("VAT breakdown: unknown VAT category code %q", code)
		}
		if !ok || key.In(tax.KeyStandard, tax.KeyZero) {
			continue
		}
		text := strings.TrimSpace(strings.Join(spec.FreeText, " "))
		if text == "" && spec.ExemptionReasonCode == "" {
			text = rowText(lines, key)
		}
		if text == "" || slices.ContainsFunc(notes, func(n *tax.Note) bool { return n.Key == key && n.Text == text }) {
			continue
		}
		notes = append(notes, &tax.Note{
			Category: tax.CategoryVAT,
			Key:      key,
			Text:     text,
		})
	}
	return notes, nil
}

// rowText is the first note on a line taxed with the given key.
func rowText(lines []*bill.Line, key cbc.Key) string {
	for _, line := range lines {
		vat := line.Taxes.Get(tax.CategoryVAT)
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

// finvoiceTypes maps the Finvoice type codes that are invoices to GOBL
// invoice types; quotations, orders, reminders and the rest of the SPY list
// are not.
var finvoiceTypes = map[string]cbc.Key{
	typeCodeInvoice:     bill.InvoiceTypeStandard,
	typeCodeCreditNote:  bill.InvoiceTypeCreditNote,
	typeCodeProforma:    bill.InvoiceTypeProforma,
	typeCodeSelfBilling: bill.InvoiceTypeStandard,
}

// untdidInvoiceTypes maps UNTDID 1001 document type codes to GOBL invoice types.
var untdidInvoiceTypes = map[string]cbc.Key{
	"325": bill.InvoiceTypeProforma,
	"380": bill.InvoiceTypeStandard,
	"381": bill.InvoiceTypeCreditNote,
	"383": bill.InvoiceTypeDebitNote,
	"384": bill.InvoiceTypeCorrective,
	"389": bill.InvoiceTypeStandard,
	"326": bill.InvoiceTypeStandard,
	"261": bill.InvoiceTypeCreditNote,
	"386": bill.InvoiceTypeStandard,
	"393": bill.InvoiceTypeStandard,
	"396": bill.InvoiceTypeCreditNote,
}

// untdidInvoiceTags maps UNTDID 1001 document type codes to GOBL tags.
var untdidInvoiceTags = map[string][]cbc.Key{
	"389": {tax.TagSelfBilled},
	"326": {tax.TagPartial},
	"261": {tax.TagSelfBilled},
	"386": {tax.TagPrepayment},
	"393": {tax.TagFactoring},
	"396": {tax.TagFactoring},
}

// invoiceType reads the GOBL type: the UNTDID code when given, else the
// Finvoice code, refusing messages that are not invoices and copies of one.
func (det *InvoiceDetails) invoiceType() (cbc.Key, error) {
	code := strings.TrimSpace(det.TypeCode.Value)
	typ, ok := finvoiceTypes[code]
	if !ok {
		return "", fmt.Errorf("%w: Finvoice %s", ErrUnsupportedDocumentType, code)
	}
	if t, ok := untdidInvoiceTypes[strings.TrimSpace(det.TypeCodeUN)]; ok {
		typ = t
	}
	switch strings.TrimSpace(det.OriginCode) {
	case originCopy:
		return "", fmt.Errorf("%w: a copy of %s", ErrUnsupportedDocumentType, strings.TrimSpace(det.InvoiceNumber))
	case originCancel:
		return "", fmt.Errorf("%w: a cancellation of %s", ErrUnsupportedDocumentType, strings.TrimSpace(det.InvoiceNumber))
	}
	return typ, nil
}
