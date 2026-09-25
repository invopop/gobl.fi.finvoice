package fifinvoice

import (
	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/cef"
	"github.com/invopop/gobl/catalogues/untdid"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// Finvoice invoice type codes from the SPY code list (guidelines §10.5) and
// their English texts.
const (
	typeCodeInvoice     = "INV01"
	typeCodeCreditNote  = "INV02"
	typeCodeProforma    = "INV06"
	typeCodeSelfBilling = "INV07"

	typeTextInvoice     = "INVOICE"
	typeTextCreditNote  = "CREDIT NOTE"
	typeTextProforma    = "PRO FORMA INVOICE"
	typeTextSelfBilling = "SELFBILLING"

	typeCodeList = "SPY"

	originOriginal = "Original"
	originCopy     = "Copy"
)

const (
	// freeTextMaxLength bounds each free text element, which may repeat.
	freeTextMaxLength = 512
	freeTextLines     = 10
	// termsTextMaxLength bounds each PaymentTermsFreeText, of which there
	// may be two.
	termsTextMaxLength = 70
	termsTextLines     = 2
	// vatFreeTextMaxLength bounds each VatFreeText, of which there may be three.
	vatFreeTextMaxLength = 70
	vatFreeTextLines     = 3
)

// InvoiceDetails holds the document header: type, numbers, dates,
// references, totals, VAT breakdown, terms, and document-level discounts and
// charges.
type InvoiceDetails struct {
	TypeCode   InvoiceTypeCode `xml:"InvoiceTypeCode"`
	TypeCodeUN string          `xml:"InvoiceTypeCodeUN,omitempty"`
	TypeText   string          `xml:"InvoiceTypeText"`
	OriginCode string          `xml:"OriginCode"`

	InvoiceNumber            string                      `xml:"InvoiceNumber"`
	InvoiceDate              *Date                       `xml:"InvoiceDate"`
	OriginalInvoiceNumber    string                      `xml:"OriginalInvoiceNumber,omitempty"`
	OriginalInvoiceDate      *Date                       `xml:"OriginalInvoiceDate,omitempty"`
	OriginalInvoiceReference []*OriginalInvoiceReference `xml:"OriginalInvoiceReference,omitempty"`
	PeriodStartDate          *Date                       `xml:"InvoicingPeriodStartDate,omitempty"`
	PeriodEndDate            *Date                       `xml:"InvoicingPeriodEndDate,omitempty"`

	SellerReference     string `xml:"SellerReferenceIdentifier,omitempty"`
	OrderIdentifier     string `xml:"OrderIdentifier,omitempty"`
	OrderDate           *Date  `xml:"OrderDate,omitempty"`
	AgreementIdentifier string `xml:"AgreementIdentifier,omitempty"`
	BuyerReference      string `xml:"BuyerReferenceIdentifier,omitempty"`
	ProjectReference    string `xml:"ProjectReferenceIdentifier,omitempty"`

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

// InvoiceTypeCode is the Finvoice invoice type code with its code list.
type InvoiceTypeCode struct {
	Value    string `xml:",chardata"`
	CodeList string `xml:"CodeListAgencyIdentifier,attr,omitempty"`
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
		TypeCode:      InvoiceTypeCode{Value: code, CodeList: typeCodeList},
		TypeCodeUN:    inv.Tax.Ext.Get(untdid.ExtKeyDocumentType).String(),
		TypeText:      text,
		OriginCode:    originOriginal,
		InvoiceNumber: inv.Series.Join(inv.Code).String(),
		InvoiceDate:   newDate(inv.IssueDate),
	}
	c.applyPreceding(d)
	c.applyOrdering(d)
	c.applyTotals(d)
	d.VatSpecifications = c.newVatSpecifications()
	for _, n := range inv.Notes {
		if n != nil {
			d.FreeText = append(d.FreeText, split(n.Text, freeTextMaxLength, freeTextLines)...)
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
		return typeCodeCreditNote, typeTextCreditNote
	case inv.Type.In(bill.InvoiceTypeProforma):
		return typeCodeProforma, typeTextProforma
	case inv.HasTags(tax.TagSelfBilled):
		return typeCodeSelfBilling, typeTextSelfBilling
	default:
		return typeCodeInvoice, typeTextInvoice
	}
}

func (c *converter) applyPreceding(d *InvoiceDetails) {
	for i, ref := range c.inv.Preceding {
		if ref == nil {
			continue
		}
		number := ref.Series.Join(ref.Code).String()
		if i == 0 {
			d.OriginalInvoiceNumber = number
			d.OriginalInvoiceDate = newDatePtr(ref.IssueDate)
			continue
		}
		d.OriginalInvoiceReference = append(d.OriginalInvoiceReference, &OriginalInvoiceReference{
			InvoiceNumber: number,
			InvoiceDate:   newDatePtr(ref.IssueDate),
		})
	}
}

func (c *converter) applyOrdering(d *InvoiceDetails) {
	o := c.inv.Ordering
	if o == nil {
		return
	}
	if o.Period != nil {
		d.PeriodStartDate = newDatePtr(o.Period.Start)
		d.PeriodEndDate = newDatePtr(o.Period.End)
	}
	if ref := firstDocumentRef(o.Purchases); ref != nil {
		d.OrderDate = newDatePtr(ref.IssueDate)
	}
	d.BuyerReference = o.Code.String()
	d.SellerReference = documentRefCode(o.Sales).String()
	d.OrderIdentifier = documentRefCode(o.Purchases).String()
	d.AgreementIdentifier = documentRefCode(o.Contracts).String()
	d.ProjectReference = documentRefCode(o.Projects).String()
	d.TenderReference = documentRefCode(o.Tender).String()
}

// documentRefCode is the full number of the first reference, if any.
func documentRefCode(refs []*org.DocumentRef) cbc.Code {
	if ref := firstDocumentRef(refs); ref != nil {
		return ref.Series.Join(ref.Code)
	}
	return cbc.CodeEmpty
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
	if t.Taxes == nil {
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
			RatePercent:         vatRatePercent(rate.Ext, rate.Percent),
			Code:                vatCategory(rate.Ext),
			RateAmount:          c.amount(rate.Amount),
			ExemptionReasonCode: rate.Ext.Get(cef.ExtKeyVATEX).String(),
		}
		if note := c.vatNote(spec.Code); note != "" {
			spec.FreeText = split(note, vatFreeTextMaxLength, vatFreeTextLines)
		}
		out = append(out, spec)
	}
	return out
}

// vatNote is the exemption reason recorded for a VAT category, if any.
func (c *converter) vatNote(category string) string {
	if category == "" {
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
	terms := c.inv.Payment.Terms
	out := &PaymentTermsDetails{
		FreeText: split(terms.Notes, termsTextMaxLength, termsTextLines),
		DueDate:  newDatePtr(firstDueDate(terms)),
	}
	return []*PaymentTermsDetails{out}
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
	return vatCategory(vat.Ext), vatRatePercent(vat.Ext, vat.Percent)
}

// vatCategory reads the UNTDID 5305 category the EN 16931 addon records on
// a tax combo or rate total.
func vatCategory(ext tax.Extensions) string {
	return ext.Get(untdid.ExtKeyTaxCategory).String()
}

// vatRatePercent writes the rate, which EN 16931 expects as zero on the
// exempt, reverse charge, intra-community and export categories (BR-E-05,
// BR-AE-05, BR-IC-05, BR-G-05) and absent only for out of scope.
func vatRatePercent(ext tax.Extensions, percent *num.Percentage) string {
	if percent != nil {
		return formatPercent(*percent)
	}
	switch ext.Get(untdid.ExtKeyTaxCategory) {
	case en16931.TaxCategoryExempt, en16931.TaxCategoryReverseCharge,
		en16931.TaxCategoryIntraCommunity, en16931.TaxCategoryExport:
		return formatPercent(num.MakePercentage(0, 0))
	}
	return ""
}

// vatCombo is the VAT tax on a line, charge or discount, if any.
func vatCombo(set tax.Set) *tax.Combo {
	if set == nil {
		return nil
	}
	return set.Get(tax.CategoryVAT)
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
