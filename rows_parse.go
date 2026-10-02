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

// priceMaxExp bounds the precision of a unit price derived from a row total.
const priceMaxExp = 9

// unitKeys maps the free-text units Finnish senders write to GOBL units,
// for rows without a UN/ECE code.
var unitKeys = map[string]cbc.Key{
	"kpl":   org.UnitPiece,
	"pcs":   org.UnitPiece,
	"st":    org.UnitPiece,
	"h":     org.UnitHour,
	"tunti": org.UnitHour,
	// t is an hour (tunti) or a tonne, so it stays generic.
	"t":  cbc.KeyEmpty,
	"pv": org.UnitDay,
	"kk": org.UnitMonth,
}

// lines reads the rows. Rows with nothing to price, such as headings and
// text rows, become notes on the invoice so their text is kept.
func (p *parser) lines() ([]*bill.Line, []*org.Note, error) {
	var lines []*bill.Line
	var notes []*org.Note
	for i, row := range p.doc.Rows {
		// A row of sub-rows is a subtotal for display, which the guidelines
		// keep out of the invoice totals.
		if len(row.SubRows) > 0 {
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

// textOnly reports a row that prices nothing: no price and no row total,
// whatever quantity it names.
func (r *InvoiceRow) textOnly() bool {
	return r.price() == nil && r.VatExcludedAmount == nil
}

// texts are the row's name, description and free texts, for a row kept as
// notes.
func (r *InvoiceRow) texts() []string {
	var out []string
	for _, s := range append([]string{r.ArticleName, r.ArticleDescription}, r.FreeText...) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// quantity is the invoiced quantity, or the closest thing the row gives. A
// row may state its quantity in several units; the one in the unit price's
// unit is the one the price applies to.
func (r *InvoiceRow) quantity() *Quantity {
	var given []*Quantity
	for _, q := range append(append(append([]*Quantity{}, r.InvoicedQuantity...), r.DeliveredQuantity...), r.OrderedQuantity) {
		if q != nil && strings.TrimSpace(q.Value) != "" {
			given = append(given, q)
		}
	}
	if len(given) == 0 {
		return nil
	}
	if price := r.price(); price != nil {
		if q := quantityIn(given, price.UnitCodeUN, func(q *Quantity) string { return q.UnitCodeUN }); q != nil {
			return q
		}
		if q := quantityIn(given, price.UnitCode, func(q *Quantity) string { return q.UnitCode }); q != nil {
			return q
		}
	}
	return given[0]
}

// quantityIn is the first quantity whose unit, read by get, is the one given.
func quantityIn(given []*Quantity, unit string, get func(*Quantity) string) *Quantity {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return nil
	}
	for _, q := range given {
		if strings.EqualFold(strings.TrimSpace(get(q)), unit) {
			return q
		}
	}
	return nil
}

// price is the net unit price (BT-146), or the gross one when only that is
// given.
func (r *InvoiceRow) price() *UnitAmount {
	for _, a := range []*UnitAmount{r.UnitPriceNetAmount, r.UnitPriceAmount} {
		if a != nil {
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
	var unit, unitUN string
	if q := row.quantity(); q != nil {
		v, err := parseAmount(q.Value)
		if err != nil {
			return nil, fmt.Errorf("quantity %q: %w", q.Value, err)
		}
		qty = v
		unit, unitUN = q.UnitCode, q.UnitCodeUN
	}
	// A quantity that names no unit is in the price's.
	if price := row.price(); price != nil && strings.TrimSpace(unit) == "" && strings.TrimSpace(unitUN) == "" {
		unit, unitUN = price.UnitCode, price.UnitCodeUN
	}
	applyUnit(item, unit, unitUN)
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
	combo, err := p.newVatCombo(row.VatCode, row.VatRatePercent)
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

// applyPrice sets the unit price, taking the one the row total implies when
// the stated price does not reproduce it, since the guidelines add the
// totals up from the row total.
func (p *parser) applyPrice(line *bill.Line, row *InvoiceRow) error {
	stated, err := p.amount(row.VatExcludedAmount)
	if err != nil {
		return err
	}
	if a := row.price(); a != nil {
		price, err := p.parseAmount(a.Value, a.Currency)
		if err != nil {
			return fmt.Errorf("unit price: %w", err)
		}
		if bq := row.UnitPriceBaseQuantity; bq != nil && strings.TrimSpace(bq.Value) != "" {
			base, err := parseAmount(bq.Value)
			if err != nil {
				return fmt.Errorf("unit price base quantity %q: %w", bq.Value, err)
			}
			if !base.IsZero() {
				price = perUnit(price, base)
			}
		}
		line.Item.Price = &price
	}
	var sum *num.Amount
	if line.Item.Price != nil {
		s := p.lineSum(line)
		sum = &s
	}
	for _, d := range line.Discounts {
		if !p.percentHolds(d.Percent, d.Base, sum, d.Amount) {
			d.Percent, d.Base = nil, nil
		}
	}
	for _, c := range line.Charges {
		if !p.percentHolds(c.Percent, c.Base, sum, c.Amount) {
			c.Percent, c.Base = nil, nil
		}
	}
	if stated == nil || line.Quantity.IsZero() {
		// A row with nothing to divide by still needs a price for GOBL to
		// calculate; a non-zero row total then shows in the totals check.
		if line.Item.Price == nil {
			price := num.AmountZero.RescaleUp(p.cur.Def().Subunits)
			line.Item.Price = &price
		}
		return nil
	}
	if line.Item.Price != nil && p.reproduces(line, *stated) {
		return nil
	}
	// The row total is the sum adjusted by the discounts and charges, so the
	// sum is what the adjustments are undone from.
	fixed, fraction := adjustments(line)
	factor := fraction.Add(num.MakeAmount(1, 0))
	if factor.IsZero() {
		// A row given away whole prices at nothing; one that still costs
		// something cannot be undone.
		if !stated.IsZero() {
			return fmt.Errorf("row total %s gives no unit price when its discounts take all of it", stated)
		}
		price := num.AmountZero.RescaleUp(p.cur.Def().Subunits)
		line.Item.Price = &price
		return nil
	}
	// The fewest decimals that reproduce the total, from the currency's up;
	// a large quantity at a small total needs more.
	for exp := p.cur.Def().Subunits; exp <= priceMaxExp; exp++ {
		goods := stated.Subtract(fixed).RescaleUp(exp).Divide(factor)
		price := goods.Divide(line.Quantity)
		line.Item.Price = &price
		if p.reproduces(line, *stated) {
			break
		}
	}
	return nil
}

// perUnit divides a price given per a base quantity, with the fewest
// decimals that multiply back to it, since dividing keeps the price's own.
func perUnit(price, base num.Amount) num.Amount {
	for exp := price.Exp(); exp < priceMaxExp; exp++ {
		unit := price.RescaleUp(exp).Divide(base)
		if unit.Multiply(base).Equals(price.RescaleUp(exp)) {
			return unit
		}
	}
	return price.RescaleUp(priceMaxExp).Divide(base)
}

// reproduces reports whether the line's price gives the stated row total.
func (p *parser) reproduces(line *bill.Line, stated num.Amount) bool {
	total := p.lineTotal(line)
	return total.Equals(stated.Rescale(total.Exp()))
}

// lineTotal is what GOBL will calculate for the line with currency
// rounding: the sum rounded first, then each discount and charge rounded on
// its own.
func (p *parser) lineTotal(line *bill.Line) num.Amount {
	sum := p.lineSum(line)
	total := sum
	for _, d := range line.Discounts {
		total = total.Subtract(p.adjustmentAmount(d.Percent, d.Base, d.Amount, sum))
	}
	for _, c := range line.Charges {
		total = total.Add(p.adjustmentAmount(c.Percent, c.Base, c.Amount, sum))
	}
	return total
}

// lineSum is quantity times price at the currency's precision; multiplying
// from the price keeps its decimals until then.
func (p *parser) lineSum(line *bill.Line) num.Amount {
	return line.Item.Price.Multiply(line.Quantity).Rescale(p.cur.Def().Subunits)
}

// adjustmentAmount is a discount or charge as GOBL calculates it: a
// percentage of its base, or of the sum without one, else the amount.
func (p *parser) adjustmentAmount(percent *num.Percentage, base *num.Amount, amount, sum num.Amount) num.Amount {
	subunits := p.cur.Def().Subunits
	if percent == nil {
		return amount.Rescale(subunits)
	}
	if base != nil {
		sum = base.Rescale(subunits)
	}
	return percent.Of(sum).Rescale(subunits)
}

// percentHolds reports whether a percentage gives the amount stated beside
// it, on its base or else the sum. Finvoice rounds percentages to three
// decimals and GOBL recalculates the amount from any percentage kept.
func (p *parser) percentHolds(percent *num.Percentage, base, sum *num.Amount, amount num.Amount) bool {
	if percent == nil || amount.IsZero() {
		return true
	}
	if base == nil {
		base = sum
	}
	if base == nil {
		return false
	}
	return p.adjustmentAmount(percent, base, amount, *base).Equals(amount.Rescale(p.cur.Def().Subunits))
}

// adjustments sums the line's discounts and charges the way GOBL applies
// them: fixed amounts, plus a fraction of the line sum for each percentage
// given without a base.
func adjustments(line *bill.Line) (fixed, fraction num.Amount) {
	// Amounts keep the receiver's precision, so each total first takes on
	// the precision of what it adds.
	for _, d := range line.Discounts {
		a, f := adjustment(d.Percent, d.Base, d.Amount)
		fixed = fixed.RescaleUp(a.Exp()).Subtract(a)
		fraction = fraction.RescaleUp(f.Exp()).Subtract(f)
	}
	for _, c := range line.Charges {
		a, f := adjustment(c.Percent, c.Base, c.Amount)
		fixed = fixed.RescaleUp(a.Exp()).Add(a)
		fraction = fraction.RescaleUp(f.Exp()).Add(f)
	}
	return fixed, fraction
}

func adjustment(percent *num.Percentage, base *num.Amount, amount num.Amount) (fixed, fraction num.Amount) {
	switch {
	case percent == nil:
		return amount, num.AmountZero
	case base != nil:
		return percent.Of(*base), num.AmountZero
	default:
		return num.AmountZero, percent.Base()
	}
}

// applyUnit sets the item's unit from the UN/ECE code, else from the
// free-text unit, keeping a UN/ECE code GOBL has no key for in the unit
// extension.
func applyUnit(item *org.Item, code, codeUN string) {
	codeUN = strings.TrimSpace(codeUN)
	if key := untdid.UnitKey(cbc.Code(codeUN)); codeUN != "" && key != cbc.KeyEmpty {
		item.Unit = key
		return
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if key, ok := unitKeys[code]; ok {
		item.Unit = key
		return
	}
	if key := cbc.Key(code); key != cbc.KeyEmpty && org.HasValidUnitKey.Check(key) {
		item.Unit = key
		return
	}
	if codeUN != "" {
		item.Ext = item.Ext.Set(untdid.ExtKeyUnit, cbc.Code(codeUN))
	}
}

func (p *parser) lineDiscounts(row *InvoiceRow) ([]*bill.LineDiscount, error) {
	all := []*RowProgressiveDiscountDetails{{
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
