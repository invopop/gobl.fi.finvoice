package fifinvoice

import (
	"fmt"
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

func (p *parser) lines() ([]*bill.Line, error) {
	var lines []*bill.Line
	for i, row := range p.doc.Rows {
		if row == nil || row.empty() {
			continue
		}
		line, err := p.line(row, i+1)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// empty reports a row with nothing to invoice, such as a subtotal row.
func (r *InvoiceRow) empty() bool {
	return r.ArticleName == "" && r.ArticleIdentifier == "" && r.quantity() == nil && r.VatExcludedAmount == nil
}

// quantity is the invoiced quantity, or the closest thing the row gives.
func (r *InvoiceRow) quantity() *Quantity {
	for _, q := range []*Quantity{r.InvoicedQuantity, r.DeliveredQuantity, r.OrderedQuantity} {
		if q != nil && strings.TrimSpace(q.Value) != "" {
			return q
		}
	}
	return nil
}

func (p *parser) line(row *InvoiceRow, position int) (*bill.Line, error) {
	item := &org.Item{
		Name:        strings.TrimSpace(row.ArticleName),
		Ref:         cbc.Code(strings.TrimSpace(row.ArticleIdentifier)),
		Description: strings.TrimSpace(row.ArticleDescription),
	}
	if item.Name == "" {
		item.Name = firstNonEmpty(item.Ref.String(), fmt.Sprintf("Row %d", position))
	}
	if row.EANCode != nil && strings.TrimSpace(row.EANCode.Value) != "" {
		item.Identities = []*org.Identity{{Key: org.IdentityKeyGTIN, Code: cbc.Code(strings.TrimSpace(row.EANCode.Value))}}
	}

	qty := num.MakeAmount(1, 0)
	if q := row.quantity(); q != nil {
		v, err := parseAmount(q.Value)
		if err != nil {
			return nil, fmt.Errorf("quantity %q: %w", q.Value, err)
		}
		qty = v
		applyUnit(item, q.UnitCode, q.UnitCodeUN)
	}
	if p.negate {
		qty = qty.Negate()
	}
	line := &bill.Line{Quantity: qty, Item: item}

	if row.UnitPriceAmount != nil && strings.TrimSpace(row.UnitPriceAmount.Value) != "" {
		price, err := parseAmount(row.UnitPriceAmount.Value)
		if err != nil {
			return nil, fmt.Errorf("unit price %q: %w", row.UnitPriceAmount.Value, err)
		}
		item.Price = &price
	} else if excluded, err := p.amount(row.VatExcludedAmount); err != nil {
		return nil, err
	} else if excluded != nil && !qty.IsZero() {
		// No unit price: the row total divided by the quantity stands in.
		price := excluded.RescaleUp(4).Divide(qty)
		item.Price = &price
	}

	var err error
	if line.Period = parsePeriod(row.StartDate, row.EndDate); line.Period != nil && line.Period.Start == nil {
		line.Period = nil
	}
	if line.Discounts, err = p.lineDiscounts(row); err != nil {
		return nil, err
	}
	if line.Charges, err = p.lineCharges(row); err != nil {
		return nil, err
	}
	combo, err := p.vatCombo(row.VatCode, row.VatRatePercent)
	if err != nil {
		return nil, err
	}
	if combo != nil {
		line.Taxes = tax.Set{combo}
	}
	for _, text := range row.FreeText {
		if text = strings.TrimSpace(text); text != "" {
			line.Notes = append(line.Notes, &org.Note{Text: text})
		}
	}
	return line, nil
}

// applyUnit sets the item's unit from the UN/ECE code when GOBL knows it,
// else from the free-text unit when that is a GOBL unit key; any other UN/ECE
// code is kept in the unit extension.
func applyUnit(item *org.Item, code, codeUN string) {
	codeUN = strings.TrimSpace(codeUN)
	if codeUN != "" {
		if key := untdid.UnitKey(cbc.Code(codeUN)); key != "" {
			item.Unit = key
			return
		}
		item.Ext = item.Ext.Set(untdid.ExtKeyUnit, cbc.Code(codeUN))
		return
	}
	if key := cbc.Key(strings.TrimSpace(code)); key != "" && org.HasValidUnitKey.Check(key) {
		item.Unit = key
	}
}

func (p *parser) lineDiscounts(row *InvoiceRow) ([]*bill.LineDiscount, error) {
	all := []*RowDiscountDetails{{
		Percent:    row.DiscountPercent,
		Amount:     row.DiscountAmount,
		BaseAmount: row.DiscountBaseAmount,
		TypeCode:   row.DiscountTypeCode,
		TypeText:   row.DiscountTypeText,
	}}
	all = append(all, row.ProgressiveDiscount...)
	var out []*bill.LineDiscount
	for _, d := range all {
		if d == nil || (d.Percent == "" && d.Amount == nil) {
			continue
		}
		dis := &bill.LineDiscount{Reason: strings.TrimSpace(d.TypeText)}
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
		if d.TypeCode != "" {
			dis.Ext = dis.Ext.Set(untdid.ExtKeyAllowance, cbc.Code(strings.TrimSpace(d.TypeCode)))
		}
		out = append(out, dis)
	}
	return out, nil
}

func (p *parser) lineCharges(row *InvoiceRow) ([]*bill.LineCharge, error) {
	var out []*bill.LineCharge
	for _, c := range row.Charges {
		if c == nil || (c.Percent == "" && c.Amount == nil) {
			continue
		}
		ch := &bill.LineCharge{Reason: strings.TrimSpace(c.ReasonText)}
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
		out = append(out, ch)
	}
	return out, nil
}
