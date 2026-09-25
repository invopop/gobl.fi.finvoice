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

// priceExp is the precision a unit price derived from a row total is kept at.
const priceExp = 4

// unitKeys maps the free-text units Finnish senders write to GOBL units,
// for rows without a UN/ECE code.
var unitKeys = map[string]cbc.Key{
	"kpl":   org.UnitPiece,
	"pcs":   org.UnitPiece,
	"st":    org.UnitPiece,
	"h":     org.UnitHour,
	"t":     org.UnitHour,
	"tunti": org.UnitHour,
	"pv":    org.UnitDay,
	"kk":    org.UnitMonth,
}

// lines reads the rows. Rows with nothing to price, such as headings and
// text rows, become notes on the invoice so their text is kept.
func (p *parser) lines() ([]*bill.Line, []*org.Note, error) {
	var lines []*bill.Line
	var notes []*org.Note
	for i, row := range p.doc.Rows {
		if row == nil {
			continue
		}
		if row.textOnly() {
			for _, text := range row.texts() {
				notes = append(notes, &org.Note{Key: org.NoteKeyGeneral, Text: text})
			}
			continue
		}
		line, err := p.line(row, i+1)
		if err != nil {
			return nil, nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		lines = append(lines, line)
	}
	return lines, notes, nil
}

// textOnly reports a row that prices nothing: no quantity, price or amount.
func (r *InvoiceRow) textOnly() bool {
	return r.quantity() == nil && r.price() == nil && r.VatExcludedAmount == nil
}

// texts are the row's name and free texts, for a row kept as notes.
func (r *InvoiceRow) texts() []string {
	var out []string
	for _, s := range append([]string{r.ArticleName}, r.FreeText...) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// quantity is the invoiced quantity, or the closest thing the row gives.
func (r *InvoiceRow) quantity() *Quantity {
	candidates := append(append(append([]*Quantity{}, r.InvoicedQuantity...), r.DeliveredQuantity...), r.OrderedQuantity)
	for _, q := range candidates {
		if q != nil && strings.TrimSpace(q.Value) != "" {
			return q
		}
	}
	return nil
}

// price is the net unit price (BT-146), or the gross one when only that is
// given.
func (r *InvoiceRow) price() *UnitAmount {
	for _, a := range []*UnitAmount{r.UnitPriceNetAmount, r.UnitPriceAmount} {
		if a != nil && strings.TrimSpace(a.Value) != "" {
			return a
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

	var err error
	if line.Period, err = parsePeriod(row.StartDate, row.EndDate); err != nil {
		return nil, err
	}
	if line.Discounts, err = p.lineDiscounts(row); err != nil {
		return nil, err
	}
	if line.Charges, err = p.lineCharges(row); err != nil {
		return nil, err
	}
	if err := p.applyPrice(line, row); err != nil {
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

// applyPrice sets the unit price. The row total is the authority (the
// guidelines add the totals up from it): the price is read per its base
// quantity, and when the quantity times that price still misses the row
// total, the price the row total implies is used instead.
func (p *parser) applyPrice(line *bill.Line, row *InvoiceRow) error {
	stated, err := p.amount(row.VatExcludedAmount)
	if err != nil {
		return err
	}
	if a := row.price(); a != nil {
		price, err := parseAmount(a.Value)
		if err != nil {
			return fmt.Errorf("unit price %q: %w", a.Value, err)
		}
		if bq := row.UnitPriceBaseQuantity; bq != nil && strings.TrimSpace(bq.Value) != "" {
			base, err := parseAmount(bq.Value)
			if err != nil {
				return fmt.Errorf("unit price base quantity %q: %w", bq.Value, err)
			}
			if !base.IsZero() {
				price = price.RescaleUp(priceExp).Divide(base)
			}
		}
		line.Item.Price = &price
	}
	if stated == nil || line.Quantity.IsZero() {
		return nil
	}
	if line.Item.Price != nil && p.lineTotal(line).Equals(stated.Rescale(p.lineTotal(line).Exp())) {
		return nil
	}
	// Discounts and charges are given as amounts, so the row total says
	// what the goods themselves came to.
	goods := *stated
	for _, d := range line.Discounts {
		goods = goods.Add(d.Amount)
	}
	for _, c := range line.Charges {
		goods = goods.Subtract(c.Amount)
	}
	price := goods.RescaleUp(priceExp).Divide(line.Quantity)
	line.Item.Price = &price
	return nil
}

// lineTotal is what GOBL will calculate for the line: quantity times price,
// less discounts, plus charges, at the currency's precision.
func (p *parser) lineTotal(line *bill.Line) num.Amount {
	// The price keeps the precision: a quantity has none to spare.
	sum := line.Item.Price.Multiply(line.Quantity)
	total := sum
	for _, d := range line.Discounts {
		if d.Percent != nil {
			total = total.Subtract(d.Percent.Of(sum))
		} else {
			total = total.Subtract(d.Amount)
		}
	}
	for _, c := range line.Charges {
		if c.Percent != nil {
			total = total.Add(c.Percent.Of(sum))
		} else {
			total = total.Add(c.Amount)
		}
	}
	return total.Rescale(p.cur.Def().Subunits)
}

// applyUnit sets the item's unit from the UN/ECE code when GOBL knows it,
// else from the free-text unit when it is a GOBL unit key or a Finnish word
// for one; any other UN/ECE code is kept in the unit extension.
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
	code = strings.ToLower(strings.TrimSpace(code))
	if key, ok := unitKeys[code]; ok {
		item.Unit = key
		return
	}
	if key := cbc.Key(code); key != "" && org.HasValidUnitKey.Check(key) {
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
