package fifinvoice

import "time"

// Option configures a conversion.
type Option func(*options)

type options struct {
	senderOperator string
	messageID      string
	messageTime    time.Time
	urls           []invoiceURL
}

type invoiceURL struct {
	name string
	url  string
}

// WithSenderOperator names the e-invoice operator the message is sent
// through, written as the sender's intermediator in the message frame; the
// schema takes 2 to 35 characters.
func WithSenderOperator(id string) Option {
	return func(o *options) {
		o.senderOperator = id
	}
}

// WithMessageID sets the message frame's identifier, which the invoice's
// UUID otherwise provides; the schema takes 2 to 48 characters.
func WithMessageID(id string) Option {
	return func(o *options) {
		o.messageID = id
	}
}

// WithMessageTime sets the message frame's timestamp, which is otherwise the
// time of conversion.
func WithMessageTime(t time.Time) Option {
	return func(o *options) {
		o.messageTime = t
	}
}

// WithInvoiceURL adds a named link to the document, such as the reference to
// the invoice's PDF image that an operator's intake expects.
func WithInvoiceURL(name, url string) Option {
	return func(o *options) {
		o.urls = append(o.urls, invoiceURL{name: name, url: url})
	}
}

func parseOptions(opts ...Option) *options {
	o := new(options)
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	if o.messageTime.IsZero() {
		o.messageTime = time.Now()
	}
	return o
}
