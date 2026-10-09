package finvoice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/tax"
)

// Roles in the ebXML message header of the forwarding service's SOAP frame
// (guidelines §17).
const (
	roleSender        = "Sender"
	roleReceiver      = "Receiver"
	roleIntermediator = "Intermediator"
)

// ISO 6523 schemes guessed for an e-invoice address given without one; an
// address of no known shape is kept as an inbox under inboxLabel.
const (
	schemeFinnishOrg  = "0212"
	schemeIBANAddress = "9918"
	inboxLabel        = "Finvoice"
)

var (
	// finvoiceStart finds the document root, which a transport frame may
	// precede, and the comments, CDATA and processing instructions that may
	// name it as text.
	finvoiceStart = regexp.MustCompile(`(?s)<!--.*?-->|<!\[CDATA\[.*?\]\]>|<\?.*?\?>|<Finvoice[\s>]`)
	// xmlDeclaration finds the declaration that names a charset.
	xmlDeclaration = regexp.MustCompile(`<\?xml\s[^>]*\?>`)
	// ovtAddress is 0037, a Y-tunnus and up to five characters naming a unit.
	ovtAddress = regexp.MustCompile(`^0037[0-9]{8}[0-9A-Za-z]{0,5}$`)
)

// soapFrame is the part of the frame that holds the routing.
type soapFrame struct {
	XMLName xml.Name
	Header  struct {
		MessageHeader struct {
			From []soapParty `xml:"From"`
			To   []soapParty `xml:"To"`
		} `xml:"MessageHeader"`
	} `xml:"Header"`
}

// soapParty is one eb:From or eb:To entry: an identifier and its role.
type soapParty struct {
	PartyID string `xml:"PartyId"`
	Role    string `xml:"Role"`
}

// splitFrame separates a transport frame from the document, giving the
// document the declaration that names its charset.
func splitFrame(data []byte) (frame, body []byte) {
	start := -1
	for off := 0; start < 0; {
		loc := finvoiceStart.FindIndex(data[off:])
		if loc == nil {
			return nil, data
		}
		if bytes.HasPrefix(data[off+loc[0]:], []byte("<Finvoice")) {
			start = off + loc[0]
		}
		off += loc[1]
	}
	decls := xmlDeclaration.FindAllIndex(data[:start], -1)
	if len(decls) == 0 {
		return data[:start], data[start:]
	}
	last := decls[len(decls)-1]
	body = append(body, data[last[0]:last[1]]...)
	body = append(body, '\n')
	body = append(body, data[start:]...)
	// A file with one declaration, at its very start, is in one charset, so
	// that declaration serves the frame as well.
	if len(decls) == 1 && len(bytes.TrimSpace(data[:last[0]])) == 0 {
		return data[:start], body
	}
	frame = append(frame, data[:last[0]]...)
	frame = append(frame, data[last[1]:start]...)
	return frame, body
}

// parseFrame reads the SOAP frame's sender, receiver and their
// intermediators into transmission details.
func parseFrame(data []byte) (*MessageTransmissionDetails, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader
	frame := new(soapFrame)
	if err := dec.Decode(frame); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("unmarshal transport frame: %w", err)
	}
	if frame.XMLName.Local != "Envelope" {
		return nil, nil
	}
	sender, senderOp := routeParties(frame.Header.MessageHeader.From, roleSender)
	receiver, receiverOp := routeParties(frame.Header.MessageHeader.To, roleReceiver)
	if sender == "" && receiver == "" {
		return nil, nil
	}
	return &MessageTransmissionDetails{
		Sender: MessageSenderDetails{
			Identifier:    Identifier{Value: sender},
			Intermediator: senderOp,
		},
		Receiver: MessageReceiverDetails{
			Identifier:    Identifier{Value: receiver},
			Intermediator: receiverOp,
		},
	}, nil
}

// routeParties picks the party in the given role and its intermediator out
// of a From or To list.
func routeParties(parties []soapParty, role string) (party, intermediator string) {
	for _, p := range parties {
		switch strings.TrimSpace(p.Role) {
		case role:
			party = strings.TrimSpace(p.PartyID)
		case roleIntermediator:
			intermediator = strings.TrimSpace(p.PartyID)
		}
	}
	return party, intermediator
}

// applyTransmission takes the e-invoice addresses and operators from the
// transmission details, which win over the organisation unit numbers.
func (p *parser) applyTransmission(inv *bill.Invoice) {
	td := p.doc.Transmission
	if td == nil {
		return
	}
	// The customer sends a self-billed invoice.
	sender, receiver := inv.Supplier, inv.Customer
	if inv.HasTags(tax.TagSelfBilled) {
		sender, receiver = receiver, sender
	}
	applyRoute(sender, td.Sender.Identifier, td.Sender.Intermediator)
	applyRoute(receiver, td.Receiver.Identifier, td.Receiver.Intermediator)
}

func applyRoute(party *org.Party, id Identifier, operator string) {
	if party == nil {
		return
	}
	setEInvoiceAddress(party, id.SchemeID, id.Value)
	if operator = strings.TrimSpace(operator); operator != "" {
		party.Ext = party.Ext.Set(addon.ExtKeyOperator, cbc.Code(operator))
	}
}

// setEInvoiceAddress records the party's e-invoice address as an ISO 6523
// endpoint when its scheme is known, else as an inbox code.
func setEInvoiceAddress(party *org.Party, scheme, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if scheme = strings.TrimSpace(scheme); scheme == "" {
		scheme = guessScheme(value)
	}
	if scheme == "" {
		party.Inboxes = []*org.Inbox{{Label: inboxLabel, Code: cbc.Code(value)}}
		party.Endpoints = nil
		return
	}
	party.Endpoints = []*org.Endpoint{{URI: cbc.URI(iso.ActorIDScheme + "::" + scheme + ":" + value)}}
	party.Inboxes = nil
}

// guessScheme names the scheme of an address whose shape and check digits
// identify it.
func guessScheme(value string) string {
	switch {
	case ovtAddress.MatchString(value) && isBusinessID(value[4:12]):
		return addon.SchemeOVT
	case pay.IsIBAN.Check(value):
		return schemeIBANAddress
	case isBusinessID(value):
		return schemeFinnishOrg
	}
	return ""
}

// isBusinessID reports a Y-tunnus whose check digit holds: weights 7, 9,
// 10, 5, 8, 4, 2 on the first seven digits, modulo 11.
func isBusinessID(value string) bool {
	m := businessID.FindStringSubmatch(value)
	if m == nil {
		return false
	}
	sum := 0
	for i, w := range []int{7, 9, 10, 5, 8, 4, 2} {
		sum += int(m[1][i]-'0') * w
	}
	check := (11 - sum%11) % 11
	return check != 10 && check == int(m[2][0]-'0')
}
