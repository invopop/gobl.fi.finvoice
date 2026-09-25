package fifinvoice

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// The ebXML message header of the forwarding service's SOAP frame
// (guidelines §17), whose parties carry the routing when the document has no
// MessageTransmissionDetails of its own.
const (
	roleSender        = "Sender"
	roleReceiver      = "Receiver"
	roleIntermediator = "Intermediator"
)

// soapFrame is the part of the frame the routing lives in.
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

// parseFrame reads the SOAP frame's sender, receiver and their
// intermediators into the routing the document would otherwise carry itself.
func parseFrame(data []byte) (*MessageTransmissionDetails, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = charsetReader
	frame := new(soapFrame)
	if err := dec.Decode(frame); err != nil {
		if err == io.EOF {
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
