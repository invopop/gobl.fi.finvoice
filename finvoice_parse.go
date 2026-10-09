package finvoice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/invopop/gobl"
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/currency"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/tax"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// Parse reads a Finvoice document of any version into a GOBL envelope
// declaring the fi-finvoice-v3 addon. A transport frame in front of the
// document is read for its routing and the charset the declaration names is
// honoured.
func Parse(data []byte) (*gobl.Envelope, error) {
	frame, body := splitFrame(data)
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = charsetReader
	doc := new(Invoice)
	if err := dec.Decode(doc); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}
	if err := endOfDocument(dec); err != nil {
		return nil, err
	}
	if doc.Transmission == nil && len(frame) > 0 {
		routing, err := parseFrame(frame)
		if err != nil {
			return nil, err
		}
		doc.Transmission = routing
	}

	p, err := newParser(doc)
	if err != nil {
		return nil, err
	}
	inv, err := p.invoice()
	if err != nil {
		return nil, err
	}
	env := gobl.NewEnvelope()
	if err := env.Insert(inv); err != nil {
		return nil, err
	}
	if err := p.reconcileTotals(env, inv); err != nil {
		return nil, err
	}
	return env, nil
}

// endOfDocument refuses anything after the document, since a file sent to
// an operator may hold several Finvoice messages and one envelope holds one.
func endOfDocument(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("after the document: %w", err)
		}
		if _, ok := tok.(xml.StartElement); ok {
			return errors.New("the file holds more than one Finvoice message")
		}
	}
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
	doc *Invoice
	det *InvoiceDetails
	cur currency.Code
	// negate flips every amount back to positive: a Finvoice credit note is
	// a zero or negative document, a GOBL one is not.
	negate bool
	typ    cbc.Key
	// stated is the total the document declares, VAT included.
	stated num.Amount
}

// newParser reads the document's type, currency and stated total, which the
// rest of the reading needs.
func newParser(d *Invoice) (*parser, error) {
	det := d.InvoiceDetails
	if det == nil {
		return nil, errors.New("document has no InvoiceDetails")
	}
	if strings.TrimSpace(det.InvoiceNumber) == "" {
		return nil, errors.New("document has no InvoiceNumber")
	}
	if det.InvoiceDate == nil || strings.TrimSpace(det.InvoiceDate.Value) == "" {
		return nil, errors.New("document has no InvoiceDate")
	}
	typ, err := det.invoiceType()
	if err != nil {
		return nil, err
	}
	if det.TotalVatIncludedAmount == nil {
		return nil, errors.New("document has no InvoiceTotalVatIncludedAmount")
	}
	cur := strings.TrimSpace(det.TotalVatIncludedAmount.Currency)
	p := &parser{doc: d, det: det, typ: typ, cur: currency.Code(cur)}
	if p.cur.Def() == nil {
		return nil, fmt.Errorf("unknown currency %q", cur)
	}
	stated, err := parseAmount(det.TotalVatIncludedAmount.Value)
	if err != nil {
		return nil, fmt.Errorf("stated total %q: %w", det.TotalVatIncludedAmount.Value, err)
	}
	p.negate = typ.In(bill.InvoiceTypeCreditNote) && !stated.IsPositive()
	if p.negate {
		stated = stated.Negate()
	}
	p.stated = stated
	return p, nil
}

func (p *parser) invoice() (*bill.Invoice, error) {
	det := p.det
	inv := &bill.Invoice{
		Addons: tax.WithAddons(addon.V3),
		Tax: &bill.Tax{
			// Finvoice amounts have the currency's decimals and its totals
			// add them up, which is what currency rounding reproduces.
			Rounding: tax.RoundingRuleCurrency,
		},
		Type:     p.typ,
		Code:     cbc.Code(strings.TrimSpace(det.InvoiceNumber)),
		Currency: p.cur,
	}
	tags := slices.Clone(untdidInvoiceTags[strings.TrimSpace(det.TypeCodeUN)])
	if strings.TrimSpace(det.TypeCode.Value) == typeCodeSelfBilling && !slices.Contains(tags, tax.TagSelfBilled) {
		tags = append(tags, tax.TagSelfBilled)
	}
	inv.SetTags(tags...)

	var err error
	if inv.IssueDate, err = parseDate(det.InvoiceDate.Value); err != nil {
		return nil, fmt.Errorf("invoice date: %w", err)
	}

	inv.Supplier = p.supplier()
	inv.Customer = p.customer()
	p.applyTransmission(inv)
	if inv.Supplier == nil || inv.Supplier.TaxID == nil {
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
	// Document percentages apply to the sum of the lines.
	sum := num.AmountZero
	for _, line := range inv.Lines {
		sum = sum.RescaleUp(p.cur.Def().Subunits).Add(p.lineTotal(line))
	}
	if inv.Discounts, err = p.discounts(sum); err != nil {
		return nil, err
	}
	if inv.Charges, err = p.charges(sum); err != nil {
		return nil, err
	}
	if inv.Tax.Notes, err = p.taxNotes(inv.Lines); err != nil {
		return nil, err
	}
	if inv.Payment, err = p.payment(); err != nil {
		return nil, err
	}
	inv.Notes = append(inv.Notes, p.notes()...)
	if inv.Totals, err = p.rounding(); err != nil {
		return nil, err
	}
	return inv, nil
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

// reconcileTotals keeps the total the sender stated: a difference within the
// tolerance is recorded as rounding, anything larger means the rows were
// misread and is refused.
func (p *parser) reconcileTotals(env *gobl.Envelope, inv *bill.Invoice) error {
	calculated := num.AmountZero
	if inv.Totals != nil {
		calculated = inv.Totals.TotalWithTax
	}
	diff := p.stated.Subtract(calculated)
	if diff.IsZero() {
		return nil
	}
	if inv.Totals == nil || diff.Abs().Compare(p.reconcileTolerance(len(inv.Lines))) > 0 {
		// The message uses the document's own signs.
		stated, rows := p.stated, calculated
		if p.negate {
			stated, rows = stated.Negate(), rows.Negate()
		}
		return fmt.Errorf("stated total %s does not match the rows, which add up to %s", stated, rows)
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
	if a == nil {
		return nil, nil
	}
	v, err := p.parseAmount(a.Value, a.Currency)
	if err != nil {
		return nil, err
	}
	if p.negate {
		v = v.Negate()
	}
	return &v, nil
}

// parseAmount reads an amount that must be in the document's currency, as a
// GOBL invoice has one.
func (p *parser) parseAmount(value, cur string) (num.Amount, error) {
	v, err := parseAmount(value)
	if err != nil {
		return v, fmt.Errorf("amount %q: %w", value, err)
	}
	if cur = strings.TrimSpace(cur); cur != p.cur.String() {
		return v, fmt.Errorf("amount %s is in %s, the document in %s", value, cur, p.cur)
	}
	return v, nil
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
