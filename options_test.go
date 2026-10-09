package finvoice_test

import (
	"strings"
	"testing"

	finvoice "github.com/invopop/gobl.fi.finvoice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertOptions pins the bounds the schema puts on the caller's values.
func TestConvertOptions(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		name string
		opts []finvoice.Option
		want string
	}{
		{"operator of 35", []finvoice.Option{finvoice.WithSenderOperator(long(35))}, ""},
		{"message identifier of 48", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithMessageID(long(48))}, ""},
		{"URL and name of 512", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithInvoiceURL(long(512), "file://"+long(505))}, ""},
		{"operator of one character", []finvoice.Option{finvoice.WithSenderOperator("X")}, "sender operator must be 2 to 35 letters or digits"},
		{"operator of 36", []finvoice.Option{finvoice.WithSenderOperator(long(36))}, "sender operator must be 2 to 35 letters or digits"},
		{"operator of punctuation", []finvoice.Option{finvoice.WithSenderOperator("--")}, "sender operator must be 2 to 35 letters or digits"},
		{"message identifier of one character", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithMessageID("1")}, "message identifier must be 2 to 48 characters"},
		{"message identifier of 49", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithMessageID(long(49))}, "message identifier must be 2 to 48 characters"},
		{"URL name of 513", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithInvoiceURL(long(513), "file://a.pdf")}, "invoice URL name must be at most 512 characters"},
		{"URL of 513", []finvoice.Option{finvoice.WithSenderOperator(senderOperator), finvoice.WithInvoiceURL("PDF", "file://"+long(506))}, "invoice URL must be at most 512 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := exampleEnvelope(t, "invoice")
			doc, err := finvoice.ConvertInvoice(env, tt.opts...)
			if tt.want == "" {
				require.NoError(t, err)
				validXML(t, doc)
				return
			}
			require.ErrorContains(t, err, tt.want)
		})
	}
	t.Run("links keep their order", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc := convertAdjusted(t, env, finvoice.WithInvoiceURL("LIITE", "file://liite.pdf"))
		assert.Equal(t, []string{"PDF", "LIITE"}, doc.InvoiceURLNames)
	})
}
