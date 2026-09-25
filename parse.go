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

// Parse reads a Finvoice document of any version into a GOBL envelope
// declaring the fi-finvoice-v3 addon. A transport frame in front of the
// document is skipped, and the charset the declaration names is honoured.
func Parse(data []byte) (*gobl.Envelope, error) {
	dec := xml.NewDecoder(bytes.NewReader(documentBody(data)))
	dec.CharsetReader = charsetReader
	doc := new(Document)
	if err := dec.Decode(doc); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
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

// documentBody returns the document from its root on, keeping the XML
// declaration in front of it so the charset is known. Whatever precedes
// the declaration, such as a SOAP frame, is dropped.
func documentBody(data []byte) []byte {
	loc := finvoiceStart.FindIndex(data)
	if loc == nil {
		return data
	}
	decls := xmlDeclaration.FindAll(data[:loc[0]], -1)
	if len(decls) == 0 {
		return data[loc[0]:]
	}
	body := make([]byte, 0, len(data)-loc[0]+len(decls[len(decls)-1])+1)
	body = append(body, decls[len(decls)-1]...)
	body = append(body, '\n')
	return append(body, data[loc[0]:]...)
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
}

func (d *Document) goblInvoice() (*bill.Invoice, error) {
	det := d.InvoiceDetails
	if det == nil {
		return nil, errMissingInvoiceDetails
	}
	inv := &bill.Invoice{
		Addons: tax.WithAddons(finvoice.V3),
		Tax: &bill.Tax{
			// Finvoice totals are sums of the rounded row amounts.
			Rounding: tax.RoundingRuleCurrency,
		},
		Code: cbc.Code(strings.TrimSpace(det.InvoiceNumber)),
	}
	inv.Type = det.invoiceType()
	if tags := invoiceTypeTags[det.TypeCodeUN]; len(tags) > 0 {
		inv.SetTags(tags...)
	} else if det.TypeCode == TypeCodeSelfBilling {
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
	if det.TotalVatIncludedAmount != nil {
		if det.TotalVatIncludedAmount.Currency != "" {
			p.cur = currency.Code(det.TotalVatIncludedAmount.Currency)
		}
		if total, err := parseAmount(det.TotalVatIncludedAmount.Value); err == nil {
			p.negate = inv.Type.In(bill.InvoiceTypeCreditNote) && total.IsNegative()
		}
	}
	inv.Currency = p.cur

	inv.Supplier = p.supplier()
	inv.Customer = p.buyer()
	p.applyFrame(inv)
	if inv.Supplier.TaxID == nil {
		inv.Regime = tax.WithRegime("FI")
	}

	inv.Preceding = p.preceding()
	inv.Ordering = p.ordering()
	inv.Delivery = p.delivery()
	lines, err := p.lines()
	if err != nil {
		return nil, err
	}
	inv.Lines = lines
	inv.Discounts, err = p.discounts()
	if err != nil {
		return nil, err
	}
	inv.Charges, err = p.charges()
	if err != nil {
		return nil, err
	}
	inv.Tax.Notes = p.taxNotes()
	inv.Payment, err = p.payment()
	if err != nil {
		return nil, err
	}
	inv.Notes = p.notes()
	return inv, nil
}

// invoiceType reads the GOBL type: the UNTDID code when given, else the
// Finvoice code.
func (det *InvoiceDetails) invoiceType() cbc.Key {
	if t, ok := invoiceTypes[det.TypeCodeUN]; ok {
		return t
	}
	switch det.TypeCode {
	case TypeCodeCreditNote:
		return bill.InvoiceTypeCreditNote
	case TypeCodeProforma:
		return bill.InvoiceTypeProforma
	}
	return bill.InvoiceTypeStandard
}

// reconcileTotals keeps the total the sender stated: when the calculated
// total differs, the difference is recorded as rounding so the payable
// amount matches the document.
func (d *Document) reconcileTotals(env *gobl.Envelope, inv *bill.Invoice) error {
	stated := d.InvoiceDetails.TotalVatIncludedAmount
	if stated == nil || inv.Totals == nil {
		return nil
	}
	amount, err := parseAmount(stated.Value)
	if err != nil {
		return fmt.Errorf("stated total %q: %w", stated.Value, err)
	}
	if inv.Type.In(bill.InvoiceTypeCreditNote) && amount.IsNegative() {
		amount = amount.Negate()
	}
	computed := inv.Totals.TotalWithTax
	if amount.Equals(computed) {
		return nil
	}
	rounding := amount.Subtract(computed)
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
