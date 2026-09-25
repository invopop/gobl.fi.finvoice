package fifinvoice

import (
	"strconv"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
)

const (
	// articleNameMaxLength bounds ArticleName.
	articleNameMaxLength = 100
	// articleIdentifierMaxLength bounds ArticleIdentifier.
	articleIdentifierMaxLength = 70
	// unitCodeMaxLength bounds the free-text unit.
	unitCodeMaxLength = 14
	// discountTextMaxLength bounds RowDiscountTypeText.
	discountTextMaxLength = 35
)

// InvoiceRow is one invoice line.
type InvoiceRow struct {
	ArticleIdentifier  string      `xml:"ArticleIdentifier,omitempty"`
	ArticleName        string      `xml:"ArticleName,omitempty"`
	ArticleDescription string      `xml:"ArticleDescription,omitempty"`
	EANCode            *Identifier `xml:"EanCode,omitempty"`

	DeliveredQuantity []*Quantity `xml:"DeliveredQuantity,omitempty"`
	OrderedQuantity   *Quantity   `xml:"OrderedQuantity,omitempty"`
	InvoicedQuantity  []*Quantity `xml:"InvoicedQuantity,omitempty"`
	StartDate         *Date       `xml:"StartDate,omitempty"`
	EndDate           *Date       `xml:"EndDate,omitempty"`

	UnitPriceAmount       *UnitAmount `xml:"UnitPriceAmount,omitempty"`
	UnitPriceNetAmount    *UnitAmount `xml:"UnitPriceNetAmount,omitempty"`
	UnitPriceBaseQuantity *Quantity   `xml:"UnitPriceBaseQuantity,omitempty"`

	RowPositionIdentifier string `xml:"RowPositionIdentifier,omitempty"`
	OriginalInvoiceNumber string `xml:"OriginalInvoiceNumber,omitempty"`

	FreeText []string `xml:"RowFreeText,omitempty"`

	DiscountPercent     string                `xml:"RowDiscountPercent,omitempty"`
	DiscountAmount      *Amount               `xml:"RowDiscountAmount,omitempty"`
	DiscountBaseAmount  *Amount               `xml:"RowDiscountBaseAmount,omitempty"`
	DiscountTypeCode    string                `xml:"RowDiscountTypeCode,omitempty"`
	DiscountTypeText    string                `xml:"RowDiscountTypeText,omitempty"`
	ProgressiveDiscount []*RowDiscountDetails `xml:"RowProgressiveDiscountDetails,omitempty"`
	Charges             []*RowChargeDetails   `xml:"RowChargeDetails,omitempty"`

	VatRatePercent    string  `xml:"RowVatRatePercent,omitempty"`
	VatCode           string  `xml:"RowVatCode,omitempty"`
	VatAmount         *Amount `xml:"RowVatAmount,omitempty"`
	VatExcludedAmount *Amount `xml:"RowVatExcludedAmount,omitempty"`
	Amount            *Amount `xml:"RowAmount,omitempty"`
}

// RowDiscountDetails is a second or later discount on a line.
type RowDiscountDetails struct {
	Percent    string  `xml:"RowDiscountPercent,omitempty"`
	Amount     *Amount `xml:"RowDiscountAmount,omitempty"`
	BaseAmount *Amount `xml:"RowDiscountBaseAmount,omitempty"`
	TypeCode   string  `xml:"RowDiscountTypeCode,omitempty"`
	TypeText   string  `xml:"RowDiscountTypeText,omitempty"`
}

// RowChargeDetails is a charge on a line.
type RowChargeDetails struct {
	ReasonText string  `xml:"ReasonText,omitempty"`
	ReasonCode string  `xml:"ReasonCode,omitempty"`
	Percent    string  `xml:"Percent,omitempty"`
	Amount     *Amount `xml:"Amount,omitempty"`
	BaseAmount *Amount `xml:"BaseAmount,omitempty"`
}

func (c *converter) newRows() []*InvoiceRow {
	var rows []*InvoiceRow
	for _, line := range c.inv.Lines {
		if line == nil || line.Item == nil {
			continue
		}
		rows = append(rows, c.newRow(line))
	}
	return rows
}

// newRow writes a line. The row's VAT amount is left to the breakdown: GOBL
// works the tax out per rate, so per-row amounts would not add up to it.
func (c *converter) newRow(line *bill.Line) *InvoiceRow {
	item := line.Item
	row := &InvoiceRow{
		ArticleIdentifier:     cut(item.Ref.String(), articleIdentifierMaxLength),
		ArticleName:           cut(item.Name, articleNameMaxLength),
		ArticleDescription:    cut(item.Description, freeTextMaxLength),
		EANCode:               itemEAN(item),
		InvoicedQuantity:      []*Quantity{c.newQuantity(line.Quantity, item)},
		RowPositionIdentifier: strconv.Itoa(line.Index),
		StartDate:             nil,
	}
	if item.Price != nil {
		// GOBL's price is the net unit price (BT-146); with no item-level
		// discount it is the gross one (BT-148) too.
		price := &UnitAmount{
			Value:      formatAmount(*item.Price),
			Currency:   c.cur.String(),
			UnitCode:   cut(item.Unit.String(), unitCodeMaxLength),
			UnitCodeUN: unitCodeUN(item),
		}
		row.UnitPriceAmount = price
		row.UnitPriceNetAmount = price
	}
	if line.Period != nil {
		row.StartDate = newDatePtr(line.Period.Start)
		row.EndDate = newDatePtr(line.Period.End)
	}
	for _, n := range line.Notes {
		if n != nil {
			row.FreeText = append(row.FreeText, split(n.Text, freeTextMaxLength, freeTextLines)...)
		}
	}
	c.applyRowDiscounts(row, line)
	c.applyRowCharges(row, line)
	if line.Total != nil {
		row.VatExcludedAmount = c.amount(*line.Total)
	}
	if vat := vatCombo(line.Taxes); vat != nil {
		row.VatCode = vatCategory(vat.Ext)
		row.VatRatePercent = vatRatePercent(vat.Ext, vat.Percent)
	}
	return row
}

// newQuantity writes the quantity with the GOBL unit as free text and its
// UN/ECE code; negated on a credit note.
func (c *converter) newQuantity(q num.Amount, item *org.Item) *Quantity {
	return &Quantity{
		Value:      formatQuantity(c.signed(q)),
		UnitCode:   cut(item.Unit.String(), unitCodeMaxLength),
		UnitCodeUN: unitCodeUN(item),
	}
}

// unitCodeUN is the UN/ECE code for the item's unit: the one its GOBL key
// maps to, or the one the document carries in the unit extension.
func unitCodeUN(item *org.Item) string {
	if code := untdid.UnitCode(item.Unit); code != "" {
		return code.String()
	}
	return item.Ext.Get(untdid.ExtKeyUnit).String()
}

func itemEAN(item *org.Item) *Identifier {
	for _, id := range item.Identities {
		if id != nil && id.Key.In(org.IdentityKeyEAN, org.IdentityKeyGTIN) {
			return &Identifier{Value: id.Code.String()}
		}
	}
	return nil
}

// applyRowDiscounts writes the first discount in the row's own fields and
// the rest as progressive discounts.
func (c *converter) applyRowDiscounts(row *InvoiceRow, line *bill.Line) {
	for i, dis := range line.Discounts {
		if dis == nil {
			continue
		}
		d := &RowDiscountDetails{
			Amount:   c.amount(dis.Amount),
			TypeCode: dis.Ext.Get(untdid.ExtKeyAllowance).String(),
			TypeText: cut(dis.Reason, discountTextMaxLength),
		}
		if dis.Percent != nil {
			d.Percent = formatPercent(*dis.Percent)
		}
		if dis.Base != nil {
			d.BaseAmount = c.amount(*dis.Base)
		}
		if i == 0 {
			row.DiscountPercent = d.Percent
			row.DiscountAmount = d.Amount
			row.DiscountBaseAmount = d.BaseAmount
			row.DiscountTypeCode = d.TypeCode
			row.DiscountTypeText = d.TypeText
			continue
		}
		row.ProgressiveDiscount = append(row.ProgressiveDiscount, d)
	}
}

func (c *converter) applyRowCharges(row *InvoiceRow, line *bill.Line) {
	for _, ch := range line.Charges {
		if ch == nil {
			continue
		}
		d := &RowChargeDetails{
			ReasonText: cut(ch.Reason, termsTextMaxLength),
			ReasonCode: ch.Ext.Get(untdid.ExtKeyCharge).String(),
			Amount:     c.amount(ch.Amount),
		}
		if ch.Percent != nil {
			d.Percent = formatPercent(*ch.Percent)
		}
		if ch.Base != nil {
			d.BaseAmount = c.amount(*ch.Base)
		}
		row.Charges = append(row.Charges, d)
	}
}
