package fifinvoice

import (
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/tax"
)

// Lengths of the Finvoice elements the options are written to.
const (
	messageIDMinLength = 2
	messageIDMaxLength = 48
	urlMaxLength       = 512
)

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
// through, written as the sender's intermediator in the transmission
// details; the schema takes 2 to 35 characters.
func WithSenderOperator(id string) Option {
	return func(o *options) {
		o.senderOperator = id
	}
}

// WithMessageID sets the message identifier in the transmission details,
// which the invoice's UUID otherwise provides; the schema takes 2 to 48
// characters.
func WithMessageID(id string) Option {
	return func(o *options) {
		o.messageID = id
	}
}

// WithMessageTime sets the timestamp in the transmission details, which is
// otherwise the time of conversion.
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

// parseOptions applies the options and refuses values the schema cannot
// hold, since nothing else checks the caller's own input.
func parseOptions(opts ...Option) (*options, error) {
	o := new(options)
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	if o.messageTime.IsZero() {
		o.messageTime = time.Now()
	}
	// The sender's operator follows the same rule as the receiver's, which
	// the operator extension declares.
	if operator := tax.ExtensionForKey(finvoice.ExtKeyOperator); o.senderOperator != "" && !regexp.MustCompile(operator.Pattern).MatchString(o.senderOperator) {
		return nil, fmt.Errorf("sender operator must be 2 to 35 letters or digits")
	}
	if n := utf8.RuneCountInString(o.messageID); o.messageID != "" && (n < messageIDMinLength || n > messageIDMaxLength) {
		return nil, fmt.Errorf("message identifier must be %d to %d characters", messageIDMinLength, messageIDMaxLength)
	}
	for _, u := range o.urls {
		if utf8.RuneCountInString(u.name) > urlMaxLength {
			return nil, fmt.Errorf("invoice URL name must be at most %d characters", urlMaxLength)
		}
		if utf8.RuneCountInString(u.url) > urlMaxLength {
			return nil, fmt.Errorf("invoice URL must be at most %d characters", urlMaxLength)
		}
	}
	return o, nil
}
