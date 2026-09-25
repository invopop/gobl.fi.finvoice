// Package fifinvoice converts GOBL invoices and credit notes to Finvoice 3.0
// and back. The fi-finvoice-v3 addon in the addon subpackage carries the
// validation rules and the e-invoice operator extension the converter reads.
package fifinvoice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/invopop/gobl"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/currency"
)

// Version is the Finvoice version written.
const Version = "3.0"

var (
	// ErrUnsupportedDocumentType is returned when the envelope holds anything
	// other than an invoice, or a Finvoice message that is not one.
	ErrUnsupportedDocumentType = errors.New("unsupported document type")
	// ErrSenderOperatorRequired is returned when the customer names its
	// operator but the conversion was given no sender operator to route from.
	ErrSenderOperatorRequired = errors.New("sender operator is required to route to the customer's operator")
)

// Document is the Finvoice XML document.
type Document struct {
	XMLName xml.Name `xml:"Finvoice"`
	Version string   `xml:"Version,attr"`

	Transmission *MessageTransmissionDetails `xml:"MessageTransmissionDetails,omitempty"`

	Seller                       *SellerPartyDetails         `xml:"SellerPartyDetails"`
	SellerOrganisationUnitNumber string                      `xml:"SellerOrganisationUnitNumber,omitempty"`
	SellerContactPersonName      string                      `xml:"SellerContactPersonName,omitempty"`
	SellerCommunication          *SellerCommunicationDetails `xml:"SellerCommunicationDetails,omitempty"`
	SellerInformation            *SellerInformationDetails   `xml:"SellerInformationDetails,omitempty"`

	Buyer                       *BuyerPartyDetails         `xml:"BuyerPartyDetails"`
	BuyerOrganisationUnitNumber string                     `xml:"BuyerOrganisationUnitNumber,omitempty"`
	BuyerContactPersonName      string                     `xml:"BuyerContactPersonName,omitempty"`
	BuyerCommunication          *BuyerCommunicationDetails `xml:"BuyerCommunicationDetails,omitempty"`

	DeliveryParty   *DeliveryPartyDetails `xml:"DeliveryPartyDetails,omitempty"`
	DeliveryDetails *DeliveryDetails      `xml:"DeliveryDetails,omitempty"`

	InvoiceDetails *InvoiceDetails `xml:"InvoiceDetails"`
	Rows           []*InvoiceRow   `xml:"InvoiceRow"`
	Epi            *EpiDetails     `xml:"EpiDetails"`

	InvoiceURLNames []string `xml:"InvoiceUrlNameText,omitempty"`
	InvoiceURLs     []string `xml:"InvoiceUrlText,omitempty"`
}

// converter holds what every part of the document needs from the invoice.
type converter struct {
	inv  *bill.Invoice
	cur  currency.Code
	opts *options
	// negate flips every amount: a Finvoice credit note is a zero or negative
	// document.
	negate bool
}

// Convert turns a GOBL envelope holding an invoice or credit note into a
// Finvoice document. The fi-finvoice-v3 addon is added when the invoice does
// not declare it, and the invoice must validate under it.
func Convert(env *gobl.Envelope, opts ...Option) (*Document, error) {
	inv, ok := env.Extract().(*bill.Invoice)
	if !ok || inv == nil {
		return nil, ErrUnsupportedDocumentType
	}
	if err := ensureAddon(env, inv); err != nil {
		return nil, err
	}
	if err := inv.RemoveIncludedTaxes(); err != nil {
		return nil, err
	}
	// Finvoice amounts carry the currency's decimals, and the format has
	// InvoiceTotalRoundoffAmount for the difference rounding makes.
	if err := inv.RoundToCurrency(); err != nil {
		return nil, err
	}

	c := &converter{
		inv:    inv,
		cur:    inv.Currency,
		opts:   parseOptions(opts...),
		negate: inv.Type.In(bill.InvoiceTypeCreditNote),
	}
	if c.opts.messageID == "" {
		c.opts.messageID = inv.UUID.String()
	}

	frame, err := c.newTransmission()
	if err != nil {
		return nil, err
	}
	details, err := c.newInvoiceDetails()
	if err != nil {
		return nil, err
	}
	epi, err := c.newEpiDetails()
	if err != nil {
		return nil, err
	}
	seller, err := c.newSeller()
	if err != nil {
		return nil, err
	}
	buyer, err := c.newBuyer()
	if err != nil {
		return nil, err
	}
	delivery, err := c.newDeliveryParty()
	if err != nil {
		return nil, err
	}
	rows, err := c.newRows()
	if err != nil {
		return nil, err
	}

	doc := &Document{
		Version:        Version,
		Transmission:   frame,
		Seller:         seller,
		Buyer:          buyer,
		DeliveryParty:  delivery,
		InvoiceDetails: details,
		Rows:           rows,
		Epi:            epi,
	}
	c.applySellerDetails(doc)
	if err := c.applyBuyerDetails(doc); err != nil {
		return nil, err
	}
	c.applyDelivery(doc)
	for _, u := range c.opts.urls {
		doc.InvoiceURLNames = append(doc.InvoiceURLNames, u.name)
		doc.InvoiceURLs = append(doc.InvoiceURLs, u.url)
	}
	return doc, nil
}

// ensureAddon declares the addon on an invoice that lacks it and validates
// the envelope, so that a document the addon rejects never reaches the
// mapping.
func ensureAddon(env *gobl.Envelope, inv *bill.Invoice) error {
	if !finvoice.V3.In(inv.GetAddons()...) {
		inv.SetAddons(append(inv.GetAddons(), finvoice.V3)...)
		if err := env.Calculate(); err != nil {
			return err
		}
	}
	return env.Validate()
}

// Bytes renders the document as indented UTF-8 XML.
func (d *Document) Bytes() ([]byte, error) {
	buf := bytes.NewBufferString(xml.Header)
	data, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}
	buf.Write(data)
	buf.WriteString("\n")
	return buf.Bytes(), nil
}
