package fifinvoice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/invopop/gobl"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/tax"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

var (
	// finvoiceStart finds the document root, which a transport frame may
	// precede.
	finvoiceStart = regexp.MustCompile(`<Finvoice[\s>]`)
	// xmlDeclaration finds the declaration that names the document's charset.
	xmlDeclaration = regexp.MustCompile(`<\?xml\s[^>]*\?>`)

	errMissingInvoiceDetails = errors.New("document has no InvoiceDetails")
)

// invoiceTypes maps UNTDID 1001 document type codes to GOBL invoice types.
var invoiceTypes = map[string]cbc.Key{
	"325": bill.InvoiceTypeProforma,
	"380": bill.InvoiceTypeStandard,
	"381": bill.InvoiceTypeCreditNote,
	"383": bill.InvoiceTypeDebitNote,
	"384": bill.InvoiceTypeCorrective,
	"389": bill.InvoiceTypeStandard,
	"326": bill.InvoiceTypeStandard,
}

// invoiceTypeTags maps UNTDID 1001 document type codes to GOBL tags.
var invoiceTypeTags = map[string][]cbc.Key{
	"389": {tax.TagSelfBilled},
	"326": {tax.TagPartial},
}

// finvoiceTypes maps the Finvoice invoice type codes this module reads to
// GOBL invoice types. Quotations, orders, reminders and the other message
// types of the SPY list are not invoices.
var finvoiceTypes = map[string]cbc.Key{
	typeCodeInvoice:     bill.InvoiceTypeStandard,
	typeCodeCreditNote:  bill.InvoiceTypeCreditNote,
	typeCodeProforma:    bill.InvoiceTypeProforma,
	typeCodeSelfBilling: bill.InvoiceTypeStandard,
}

// Parse reads a Finvoice document of any version into a GOBL envelope
// declaring the fi-finvoice-v3 addon. A transport frame in front of the
// document is read for its routing and the charset the declaration names is
// honoured.
func Parse(data []byte) (*gobl.Envelope, error) {
	frame, body := splitFrame(data)
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = charsetReader
	doc := new(Document)
	if err := dec.Decode(doc); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}
	if doc.Transmission == nil && len(frame) > 0 {
		routing, err := parseFrame(frame)
		if err != nil {
			return nil, err
		}
		doc.Transmission = routing
	}

	inv, err := doc.goblInvoice()
	if err != nil {
		return nil, err
	}
	env := gobl.NewEnvelope()
	if err := env.Insert(inv); err != nil {
		return nil, err
	}
	if err := doc.reconcileTotals(env, inv); err != nil {
		return nil, err
	}
	return env, nil
}

// splitFrame separates a transport frame from the document. The document
// starts at the last XML declaration before its root, so its charset stays
// known; the frame is whatever precedes that.
func splitFrame(data []byte) (frame, body []byte) {
	loc := finvoiceStart.FindIndex(data)
	if loc == nil {
		return nil, data
	}
	decls := xmlDeclaration.FindAllIndex(data[:loc[0]], -1)
	if len(decls) == 0 {
		return data[:loc[0]], data[loc[0]:]
	}
	last := decls[len(decls)-1]
	body = make([]byte, 0, len(data)-loc[0]+last[1]-last[0]+1)
	body = append(body, data[last[0]:last[1]]...)
	body = append(body, '\n')
	return data[:last[0]], append(body, data[loc[0]:]...)
}

// charsetReader decodes the single-byte charsets Finvoice documents declare.
func charsetReader(label string, r io.Reader) (io.Reader, error) {
	var enc encoding.Encoding
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "", "utf-8", "utf8":
		return r, nil
	case "iso-8859-15", "iso8859-15", "latin-9", "latin9":
		enc = charmap.ISO8859_15
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1":
		enc = charmap.ISO8859_1
	case "windows-1252", "cp1252":
		enc = charmap.Windows1252
	default:
		return nil, fmt.Errorf("unsupported encoding %q", label)
	}
	return enc.NewDecoder().Reader(r), nil
}

// parser holds what every part of the conversion needs from the document.
type parser struct {
	doc *Document
	det *InvoiceDetails
	cur currency.Code
	// negate flips every amount back to positive: a Finvoice credit note is
	// a zero or negative document, a GOBL one is not.
	negate bool
	// stated is the total the document declares, VAT included.
	stated num.Amount
}

func (d *Document) goblInvoice() (*bill.Invoice, error) {
	det := d.InvoiceDetails
	if det == nil {
		return nil, errMissingInvoiceDetails
	}
	typ, err := det.invoiceType()
	if err != nil {
		return nil, err
	}
	inv := &bill.Invoice{
		Addons: tax.WithAddons(finvoice.V3),
		Tax: &bill.Tax{
			// Finvoice amounts carry the currency's decimals and its totals
			// add them up, which is what currency rounding reproduces.
			Rounding: tax.RoundingRuleCurrency,
		},
		Type: typ,
		Code: cbc.Code(strings.TrimSpace(det.InvoiceNumber)),
	}
	if tags := invoiceTypeTags[det.TypeCodeUN]; len(tags) > 0 {
		inv.SetTags(tags...)
	} else if det.TypeCode.Value == typeCodeSelfBilling {
		inv.SetTags(tax.TagSelfBilled)
	}

	if det.InvoiceDate != nil {
		date, err := parseDate(det.InvoiceDate.Value)
		if err != nil {
			return nil, fmt.Errorf("invoice date: %w", err)
		}
		inv.IssueDate = date
	}

	p := &parser{doc: d, det: det, cur: currency.EUR}
	if det.TotalVatIncludedAmount == nil {
		return nil, errors.New("document has no InvoiceTotalVatIncludedAmount")
	}
	if det.TotalVatIncludedAmount.Currency != "" {
		p.cur = currency.Code(det.TotalVatIncludedAmount.Currency)
	}
	stated, err := parseAmount(det.TotalVatIncludedAmount.Value)
	if err != nil {
		return nil, fmt.Errorf("stated total %q: %w", det.TotalVatIncludedAmount.Value, err)
	}
	p.negate = inv.Type.In(bill.InvoiceTypeCreditNote) && stated.IsNegative()
	if p.negate {
		stated = stated.Negate()
	}
	p.stated = stated
	inv.Currency = p.cur

	inv.Supplier = p.supplier()
	inv.Customer = p.buyer()
	p.applyFrame(inv)
	if inv.Supplier.TaxID == nil {
		inv.SetRegime(l10n.FI.Tax())
	}

	if inv.Preceding, err = p.preceding(); err != nil {
		return nil, err
	}
	if inv.Ordering, err = p.ordering(); err != nil {
		return nil, err
	}
	if inv.Delivery, err = p.delivery(); err != nil {
		return nil, err
	}
	if inv.Lines, inv.Notes, err = p.lines(); err != nil {
		return nil, err
	}
	if inv.Discounts, err = p.discounts(); err != nil {
		return nil, err
	}
	if inv.Charges, err = p.charges(); err != nil {
		return nil, err
	}
	inv.Tax.Notes = p.taxNotes(inv.Lines)
	if inv.Payment, err = p.payment(); err != nil {
		return nil, err
	}
	inv.Notes = append(inv.Notes, p.notes()...)
	if inv.Totals, err = p.rounding(); err != nil {
		return nil, err
	}
	return inv, nil
}

// invoiceType reads the GOBL type: the UNTDID code when given, else the
// Finvoice code, refusing messages that are not invoices and copies of one.
func (det *InvoiceDetails) invoiceType() (cbc.Key, error) {
	code := strings.TrimSpace(det.TypeCode.Value)
	typ, ok := finvoiceTypes[code]
	if !ok {
		return "", fmt.Errorf("%w: Finvoice %s", ErrUnsupportedDocumentType, firstNonEmpty(code, "message without a type code"))
	}
	if t, ok := invoiceTypes[strings.TrimSpace(det.TypeCodeUN)]; ok {
		typ = t
	}
	if strings.TrimSpace(det.OriginCode) == originCopy {
		return "", fmt.Errorf("%w: a copy of %s", ErrUnsupportedDocumentType, strings.TrimSpace(det.InvoiceNumber))
	}
	return typ, nil
}

// rounding keeps the rounding the document states (BT-114) so the payable
// amount matches it after calculation.
func (p *parser) rounding() (*bill.Totals, error) {
	roundoff, err := p.amount(p.det.TotalRoundoffAmount)
	if err != nil {
		return nil, err
	}
	if roundoff == nil || roundoff.IsZero() {
		return nil, nil
	}
	return &bill.Totals{Rounding: roundoff}, nil
}

// reconcileTolerance is how far the calculated total may sit from the
// stated one and still be read as rounding: a subunit per row, and one
// more for the totals.
func (p *parser) reconcileTolerance(rows int) num.Amount {
	return num.MakeAmount(int64(rows+1), p.cur.Def().Subunits)
}

// reconcileTotals keeps the total the sender stated: a difference within a
// subunit per row is recorded as rounding, anything larger means the rows
// were misread and is refused.
func (d *Document) reconcileTotals(env *gobl.Envelope, inv *bill.Invoice) error {
	p := &parser{doc: d, det: d.InvoiceDetails, cur: inv.Currency}
	stated, err := parseAmount(d.InvoiceDetails.TotalVatIncludedAmount.Value)
	if err != nil {
		return err
	}
	if inv.Type.In(bill.InvoiceTypeCreditNote) && stated.IsNegative() {
		stated = stated.Negate()
	}
	diff := stated.Subtract(inv.Totals.TotalWithTax)
	if diff.IsZero() {
		return nil
	}
	if diff.Abs().Compare(p.reconcileTolerance(len(inv.Lines))) > 0 {
		return fmt.Errorf("stated total %s does not match the rows, which add up to %s", stated, inv.Totals.TotalWithTax)
	}
	rounding := diff
	if inv.Totals.Rounding != nil {
		rounding = rounding.Add(*inv.Totals.Rounding)
	}
	inv.Totals.Rounding = &rounding
	return env.Calculate()
}

// amount reads a monetary amount, flipping the sign on a negative credit
// note.
func (p *parser) amount(a *Amount) (*num.Amount, error) {
	if a == nil || strings.TrimSpace(a.Value) == "" {
		return nil, nil
	}
	v, err := parseAmount(a.Value)
	if err != nil {
		return nil, fmt.Errorf("amount %q: %w", a.Value, err)
	}
	if p.negate {
		v = v.Negate()
	}
	return &v, nil
}

func (p *parser) percent(s string) (*num.Percentage, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	v, err := parsePercent(s)
	if err != nil {
		return nil, fmt.Errorf("percentage %q: %w", s, err)
	}
	return &v, nil
}

func parseDatePtr(d *Date) (*cal.Date, error) {
	if d == nil || strings.TrimSpace(d.Value) == "" {
		return nil, nil
	}
	v, err := parseDate(d.Value)
	if err != nil {
		return nil, fmt.Errorf("date %q: %w", d.Value, err)
	}
	return &v, nil
}
