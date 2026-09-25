package fifinvoice

import (
	"fmt"
	"strings"

	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/org"
)

// specificationEN16931 is the EN 16931 specification identifier (BT-24).
const specificationEN16931 = "EN16931"

// MessageTransmissionDetails is the routing frame: who sends the message,
// who receives it, and through which operators.
type MessageTransmissionDetails struct {
	Sender   MessageSenderDetails   `xml:"MessageSenderDetails"`
	Receiver MessageReceiverDetails `xml:"MessageReceiverDetails"`
	Message  MessageDetails         `xml:"MessageDetails"`
}

// MessageSenderDetails holds the sender's e-invoice address and operator.
type MessageSenderDetails struct {
	Identifier    Identifier `xml:"FromIdentifier"`
	Intermediator string     `xml:"FromIntermediator"`
}

// MessageReceiverDetails holds the receiver's e-invoice address and operator.
type MessageReceiverDetails struct {
	Identifier    Identifier `xml:"ToIdentifier"`
	Intermediator string     `xml:"ToIntermediator"`
}

// MessageDetails identifies the message itself.
type MessageDetails struct {
	Identifier              string `xml:"MessageIdentifier"`
	Timestamp               string `xml:"MessageTimeStamp"`
	RefToMessageIdentifier  string `xml:"RefToMessageIdentifier,omitempty"`
	SpecificationIdentifier string `xml:"SpecificationIdentifier,omitempty"`
}

// newTransmission builds the frame when the customer names its operator;
// otherwise the document goes out frameless and routing is left to the
// operator's own address tables.
func (c *converter) newTransmission() (*MessageTransmissionDetails, error) {
	customer := c.inv.Customer
	receiverOperator := customer.Ext.Get(finvoice.ExtKeyOperator).String()
	if receiverOperator == "" {
		return nil, nil
	}
	if c.opts.senderOperator == "" {
		return nil, ErrSenderOperatorRequired
	}
	from := newAddressIdentifier(c.inv.Supplier)
	if from == nil {
		return nil, fmt.Errorf("supplier needs an e-invoice address as an endpoint, such as %s::0216:003776543212", iso.ActorIDScheme)
	}
	to := newAddressIdentifier(customer)
	if to == nil {
		return nil, fmt.Errorf("customer needs an e-invoice address as an endpoint, such as %s::0216:003745678907", iso.ActorIDScheme)
	}
	return &MessageTransmissionDetails{
		Sender: MessageSenderDetails{
			Identifier:    *from,
			Intermediator: c.opts.senderOperator,
		},
		Receiver: MessageReceiverDetails{
			Identifier:    *to,
			Intermediator: receiverOperator,
		},
		Message: MessageDetails{
			Identifier:              c.opts.messageID,
			Timestamp:               formatTime(c.opts.messageTime),
			SpecificationIdentifier: specificationEN16931,
		},
	}, nil
}

// newAddressIdentifier reads the party's e-invoice address off its ISO 6523
// endpoint, whose URI reads "iso6523-actorid-upis::<scheme>:<code>".
func newAddressIdentifier(p *org.Party) *Identifier {
	scheme, code := partyAddress(p)
	if code == "" {
		return nil
	}
	return &Identifier{Value: code, SchemeID: scheme}
}

func partyAddress(p *org.Party) (scheme, code string) {
	if p == nil {
		return "", ""
	}
	e := p.Endpoint(iso.ActorIDScheme)
	if e == nil {
		return "", ""
	}
	scheme, code, ok := strings.Cut(strings.TrimPrefix(e.URI.Opaque(), ":"), ":")
	if !ok || scheme == "" || code == "" {
		return "", ""
	}
	return scheme, code
}
