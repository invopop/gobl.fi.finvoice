package fifinvoice

import (
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// Finvoice invoice type codes (SPY code list, guidelines §10.5).
const (
	TypeCodeInvoice     = "INV01"
	TypeCodeCreditNote  = "INV02"
	TypeCodeProforma    = "INV06"
	TypeCodeSelfBilling = "INV07"

	TypeTextInvoice     = "INVOICE"
	TypeTextCreditNote  = "CREDIT NOTE"
	TypeTextProforma    = "PRO FORMA INVOICE"
	TypeTextSelfBilling = "SELFBILLING"

	OriginOriginal = "Original"
)

const (
	untdidTaxCategory = untdid.ExtKeyTaxCategory
	untdidVATEX       = cef.ExtKeyVATEX

	// freeTextMaxLength bounds InvoiceFreeText.
	freeTextMaxLength = 512
	// termsTextMaxLength bounds each PaymentTermsFreeText, of which there
	// may be two.
	termsTextMaxLength = 70
	termsTextLines     = 2
	// vatFreeTextMaxLength bounds each VatFreeText, of which there may be three.
	vatFreeTextMaxLength = 70
	vatFreeTextLines     = 3
	// referenceMaxLength bounds the order, agreement and buyer references.
	referenceMaxLength = 70
	// invoiceNumberMaxLength bounds InvoiceNumber.
	invoiceNumberMaxLength = 20
)

// InvoiceDetails holds the document header: type, numbers, dates,
// references, totals, VAT breakdown, terms, and document-level discounts and
// charges.
type InvoiceDetails struct {
	TypeCode   string `xml:"InvoiceTypeCode"`
	TypeCodeUN string `xml:"InvoiceTypeCodeUN,omitempty"`
	TypeText   string `xml:"InvoiceTypeText"`
	OriginCode string `xml:"OriginCode"`

	InvoiceNumber            string                      `xml:"InvoiceNumber"`
	InvoiceDate              *Date                       `xml:"InvoiceDate"`
	OriginalInvoiceNumber    string                      `xml:"OriginalInvoiceNumber,omitempty"`
	OriginalInvoiceDate      *Date                       `xml:"OriginalInvoiceDate,omitempty"`
	OriginalInvoiceReference []*OriginalInvoiceReference `xml:"OriginalInvoiceReference,omitempty"`
	PeriodStartDate          *Date                       `xml:"InvoicingPeriodStartDate,omitempty"`
	PeriodEndDate            *Date                       `xml:"InvoicingPeriodEndDate,omitempty"`

	SellerReference    string `xml:"SellerReferenceIdentifier,omitempty"`
	OrderIdentifier    string `xml:"OrderIdentifier,omitempty"`
	OrderDate          *Date  `xml:"OrderDate,omitempty"`
	AgreementIdentifier string `xml:"AgreementIdentifier,omitempty"`
	BuyerReference     string `xml:"BuyerReferenceIdentifier,omitempty"`
	ProjectReference   string `xml:"ProjectReferenceIdentifier,omitempty"`

	RowsTotalVatExcludedAmount      *Amount `xml:"RowsTotalVatExcludedAmount,omitempty"`
	DiscountsTotalVatExcludedAmount *Amount `xml:"DiscountsTotalVatExcludedAmount,omitempty"`
	ChargesTotalVatExcludedAmount   *Amount `xml:"ChargesTotalVatExcludedAmount,omitempty"`
	TotalVatExcludedAmount          *Amount `xml:"InvoiceTotalVatExcludedAmount,omitempty"`
	TotalVatAmount                  *Amount `xml:"InvoiceTotalVatAmount,omitempty"`
	TotalVatIncludedAmount          *Amount `xml:"InvoiceTotalVatIncludedAmount"`
	TotalRoundoffAmount             *Amount `xml:"InvoiceTotalRoundoffAmount,omitempty"`
	PaidAmount                      *Amount `xml:"InvoicePaidAmount,omitempty"`

	VatSpecifications []*VatSpecificationDetails `xml:"VatSpecificationDetails,omitempty"`
	FreeText          []string                   `xml:"InvoiceFreeText,omitempty"`
	PaymentTerms      []*PaymentTermsDetails     `xml:"PaymentTermsDetails,omitempty"`
	Discounts         []*DiscountDetails         `xml:"DiscountDetails,omitempty"`
	Charges           []*ChargeDetails           `xml:"ChargeDetails,omitempty"`
	TenderReference   string                     `xml:"TenderReference,omitempty"`
}

// OriginalInvoiceReference points at a further preceding document.
type OriginalInvoiceReference struct {
	InvoiceNumber string `xml:"InvoiceNumber,omitempty"`
	InvoiceDate   *Date  `xml:"InvoiceDate,omitempty"`
}

// VatSpecificationDetails is one line of the VAT breakdown.
type VatSpecificationDetails struct {
	BaseAmount          *Amount  `xml:"VatBaseAmount,omitempty"`
	RatePercent         string   `xml:"VatRatePercent,omitempty"`
	Code                string   `xml:"VatCode,omitempty"`
	RateAmount          *Amount  `xml:"VatRateAmount,omitempty"`
	FreeText            []string `xml:"VatFreeText,omitempty"`
	ExemptionReasonCode string   `xml:"VatExemptionReasonCode,omitempty"`
}

// PaymentTermsDetails describes when the invoice is due.
type PaymentTermsDetails struct {
	FreeText []string `xml:"PaymentTermsFreeText,omitempty"`
	DueDate  *Date    `xml:"InvoiceDueDate,omitempty"`
}

// DiscountDetails is a document-level allowance.
type DiscountDetails struct {
	FreeText        string  `xml:"FreeText,omitempty"`
	ReasonCode      string  `xml:"ReasonCode,omitempty"`
	Percent         string  `xml:"Percent,omitempty"`
	Amount          *Amount `xml:"Amount,omitempty"`
	BaseAmount      *Amount `xml:"BaseAmount,omitempty"`
	VatCategoryCode string  `xml:"VatCategoryCode,omitempty"`
	VatRatePercent  string  `xml:"VatRatePercent,omitempty"`
}

// ChargeDetails is a document-level charge.
type ChargeDetails struct {
	ReasonText      string  `xml:"ReasonText,omitempty"`
	ReasonCode      string  `xml:"ReasonCode,omitempty"`
	Percent         string  `xml:"Percent,omitempty"`
	Amount          *Amount `xml:"Amount,omitempty"`
	BaseAmount      *Amount `xml:"BaseAmount,omitempty"`
	VatCategoryCode string  `xml:"VatCategoryCode,omitempty"`
	VatRatePercent  string  `xml:"VatRatePercent,omitempty"`
}

func (c *converter) newInvoiceDetails() *InvoiceDetails {
	inv := c.inv
	code, text := invoiceType(inv)
	d := &InvoiceDetails{
		TypeCode:      code,
		TypeText:      text,
		OriginCode:    OriginOriginal,
		InvoiceNumber: cut(invoiceNumber(inv), invoiceNumberMaxLength),
		InvoiceDate:   newDate(inv.IssueDate),
	}
	if inv.Tax != nil {
		d.TypeCodeUN = inv.Tax.Ext.Get(untdid.ExtKeyDocumentType).String()
	}
	c.applyPreceding(d)
	c.applyOrdering(d)
	c.applyTotals(d)
	d.VatSpecifications = c.newVatSpecifications()
	for _, n := range inv.Notes {
		if n != nil && n.Text != "" {
			d.FreeText = append(d.FreeText, cut(n.Text, freeTextMaxLength))
		}
	}
	d.PaymentTerms = c.newPaymentTerms()
	d.Discounts = c.newDiscounts()
	d.Charges = c.newCharges()
	return d
}

// invoiceType maps the GOBL type to the Finvoice code and its English text.
func invoiceType(inv *bill.Invoice) (code, text string) {
	switch {
	case inv.Type.In(bill.InvoiceTypeCreditNote):
		return TypeCodeCreditNote, TypeTextCreditNote
	case inv.Type.In(bill.InvoiceTypeProforma):
		return TypeCodeProforma, TypeTextProforma
	case inv.HasTags(tax.TagSelfBilled):
		return TypeCodeSelfBilling, TypeTextSelfBilling
	default:
		return TypeCodeInvoice, TypeTextInvoice
	}
}

func (c *converter) applyPreceding(d *InvoiceDetails) {
	for i, ref := range c.inv.Preceding {
		if ref == nil {
			continue
		}
		number := cut(ref.Series.Join(ref.Code).String(), invoiceNumberMaxLength)
		var date *Date
		if ref.IssueDate != nil {
			date = newDate(*ref.IssueDate)
		}
		if i == 0 {
			d.OriginalInvoiceNumber = number
			d.OriginalInvoiceDate = date
			continue
		}
		d.OriginalInvoiceReference = append(d.OriginalInvoiceReference, &OriginalInvoiceReference{
			InvoiceNumber: number,
			InvoiceDate:   date,
		})
	}
}

func (c *converter) applyOrdering(d *InvoiceDetails) {
	if del := c.inv.Delivery; del != nil && del.Period != nil {
		d.PeriodStartDate = newDatePtr(del.Period.Start)
		d.PeriodEndDate = newDatePtr(del.Period.End)
	}
	o := c.inv.Ordering
	if o == nil {
		return
	}
	d.BuyerReference = cut(o.Code.String(), referenceMaxLength)
	if ref := firstDocumentRef(o.Sales); ref != nil {
		d.SellerReference = cut(ref.Series.Join(ref.Code).String(), referenceMaxLength)
	}
	if ref := firstDocumentRef(o.Purchases); ref != nil {
		d.OrderIdentifier = cut(ref.Series.Join(ref.Code).String(), referenceMaxLength)
		if ref.IssueDate != nil {
			d.OrderDate = newDate(*ref.IssueDate)
		}
	}
	if ref := firstDocumentRef(o.Contracts); ref != nil {
		d.AgreementIdentifier = cut(ref.Series.Join(ref.Code).String(), referenceMaxLength)
	}
	if ref := firstDocumentRef(o.Projects); ref != nil {
		d.ProjectReference = cut(ref.Series.Join(ref.Code).String(), referenceMaxLength)
	}
	if ref := firstDocumentRef(o.Tender); ref != nil {
		d.TenderReference = cut(ref.Series.Join(ref.Code).String(), referenceMaxLength)
	}
}

func firstDocumentRef(refs []*org.DocumentRef) *org.DocumentRef {
	for _, ref := range refs {
		if ref != nil {
			return ref
		}
	}
	return nil
}

func (c *converter) applyTotals(d *InvoiceDetails) {
	t := c.inv.Totals
	if t == nil {
		return
	}
	d.RowsTotalVatExcludedAmount = c.amount(t.Sum)
	if t.Discount != nil {
		d.DiscountsTotalVatExcludedAmount = c.amount(*t.Discount)
	}
	if t.Charge != nil {
		d.ChargesTotalVatExcludedAmount = c.amount(*t.Charge)
	}
	d.TotalVatExcludedAmount = c.amount(t.Total)
	d.TotalVatAmount = c.amount(t.Tax)
	d.TotalVatIncludedAmount = c.amount(t.TotalWithTax)
	if t.Rounding != nil {
		d.TotalRoundoffAmount = c.amount(*t.Rounding)
	}
	if t.Advances != nil {
		d.PaidAmount = c.amount(*t.Advances)
	}
}

// newVatSpecifications writes one breakdown line per VAT rate total.
func (c *converter) newVatSpecifications() []*VatSpecificationDetails {
	t := c.inv.Totals
	if t == nil || t.Taxes == nil {
		return nil
	}
	cat := t.Taxes.Category(tax.CategoryVAT)
	if cat == nil {
		return nil
	}
	var out []*VatSpecificationDetails
	for _, rate := range cat.Rates {
		if rate == nil {
			continue
		}
		spec := &VatSpecificationDetails{
			BaseAmount:          c.amount(rate.Base),
			Code:                vatCategory(rate.Ext),
			RateAmount:          c.amount(rate.Amount),
			ExemptionReasonCode: rate.Ext.Get(untdidVATEX).String(),
		}
		if rate.Percent != nil {
			spec.RatePercent = formatPercent(*rate.Percent)
		}
		if note := c.vatNote(spec.Code); note != "" {
			spec.FreeText = chunks(note, vatFreeTextMaxLength, vatFreeTextLines)
		}
		out = append(out, spec)
	}
	return out
}

// vatNote is the exemption reason recorded for a VAT category, if any.
func (c *converter) vatNote(category string) string {
	if c.inv.Tax == nil || category == "" {
		return ""
	}
	for _, n := range c.inv.Tax.Notes {
		if n != nil && n.Category == tax.CategoryVAT && vatCategory(n.Ext) == category {
			return n.Text
		}
	}
	return ""
}

func (c *converter) newPaymentTerms() []*PaymentTermsDetails {
	p := c.inv.Payment
	if p == nil || p.Terms == nil {
		return nil
	}
	terms := &PaymentTermsDetails{
		FreeText: chunks(p.Terms.Notes, termsTextMaxLength, termsTextLines),
	}
	if due := firstDueDate(p.Terms); due != nil {
		terms.DueDate = newDate(*due)
	}
	if len(terms.FreeText) == 0 && terms.DueDate == nil {
		return nil
	}
	return []*PaymentTermsDetails{terms}
}

func (c *converter) newDiscounts() []*DiscountDetails {
	var out []*DiscountDetails
	for _, dis := range c.inv.Discounts {
		if dis == nil {
			continue
		}
		d := &DiscountDetails{
			FreeText:   cut(dis.Reason, termsTextMaxLength),
			ReasonCode: dis.Ext.Get(untdid.ExtKeyAllowance).String(),
			Amount:     c.amount(dis.Amount),
		}
		if dis.Percent != nil {
			d.Percent = formatPercent(*dis.Percent)
		}
		if dis.Base != nil {
			d.BaseAmount = c.amount(*dis.Base)
		}
		d.VatCategoryCode, d.VatRatePercent = vatCategoryAndRate(dis.Taxes)
		out = append(out, d)
	}
	return out
}

func (c *converter) newCharges() []*ChargeDetails {
	var out []*ChargeDetails
	for _, ch := range c.inv.Charges {
		if ch == nil {
			continue
		}
		d := &ChargeDetails{
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
		d.VatCategoryCode, d.VatRatePercent = vatCategoryAndRate(ch.Taxes)
		out = append(out, d)
	}
	return out
}

// vatCategoryAndRate reads the VAT category code and percentage off a tax set.
func vatCategoryAndRate(set tax.Set) (code, percent string) {
	vat := vatCombo(set)
	if vat == nil {
		return "", ""
	}
	if vat.Percent != nil {
		percent = formatPercent(*vat.Percent)
	}
	return vatCategory(vat.Ext), percent
}

// amount writes a monetary amount in the document currency, negated on a
// credit note.
func (c *converter) amount(a num.Amount) *Amount {
	return newAmount(c.signed(a), c.cur)
}

func (c *converter) signed(a num.Amount) num.Amount {
	if c.negate {
		return a.Negate()
	}
	return a
}

// vatCategoryKeys maps the UNTDID 5305 codes EN 16931 uses to GOBL tax keys,
// for reading a Finvoice back.
var vatCategoryKeys = map[cbc.Code]cbc.Key{
	"S":  tax.KeyStandard,
	"Z":  tax.KeyZero,
	"E":  tax.KeyExempt,
	"AE": tax.KeyReverseCharge,
	"K":  tax.KeyIntraCommunity,
	"G":  tax.KeyExport,
	"O":  tax.KeyOutsideScope,
}
