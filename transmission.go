package finvoice

import (
	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// specificationEN16931 is the EN 16931 specification identifier (BT-24).
const specificationEN16931 = "EN16931"

// MessageTransmissionDetails routes the message: who sends it, who receives
// it, and through which operators.
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

// newTransmission writes the transmission details when the receiver names
// its operator; without them, routing is left to the operator's own address
// tables.
func (c *converter) newTransmission() (*MessageTransmissionDetails, error) {
	sender, receiver := c.sender(), c.receiver()
	receiverOperator := receiver.Ext.Get(addon.ExtKeyOperator).String()
	if receiverOperator == "" {
		return nil, nil
	}
	if c.opts.senderOperator == "" {
		return nil, ErrSenderOperatorRequired
	}
	return &MessageTransmissionDetails{
		Sender: MessageSenderDetails{
			Identifier:    newEInvoiceIdentifier(sender),
			Intermediator: c.opts.senderOperator,
		},
		Receiver: MessageReceiverDetails{
			Identifier:    newEInvoiceIdentifier(receiver),
			Intermediator: receiverOperator,
		},
		Message: MessageDetails{
			Identifier:              c.opts.messageID,
			Timestamp:               formatTime(c.opts.messageTime),
			SpecificationIdentifier: specificationEN16931,
		},
	}, nil
}

// sender is the party that sends the document: the customer of a
// self-billed invoice, else the supplier, as GOBL's FromEndpoint reads it.
func (c *converter) sender() *org.Party {
	if c.inv.HasTags(tax.TagSelfBilled) {
		return c.inv.Customer
	}
	return c.inv.Supplier
}

// receiver is the party the document is routed to.
func (c *converter) receiver() *org.Party {
	if c.inv.HasTags(tax.TagSelfBilled) {
		return c.inv.Supplier
	}
	return c.inv.Customer
}

// newEInvoiceIdentifier writes the party's e-invoice address with its scheme.
func newEInvoiceIdentifier(p *org.Party) Identifier {
	scheme, code := addon.EInvoiceAddress(p)
	return Identifier{Value: code, SchemeID: scheme}
}
