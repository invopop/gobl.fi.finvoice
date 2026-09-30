package finvoice_test

import (
	"testing"

	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/rules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ovt        = "iso6523-actorid-upis::0216:003710948874"
	ovtOther   = "iso6523-actorid-upis::0216:003722334455"
	businessID = "iso6523-actorid-upis::0037:2345678-0"
	gln        = "iso6523-actorid-upis::0088:7300010000001"
	mailto     = "mailto:billing@example.fi"
)

func endpoints(uris ...string) []*org.Endpoint {
	eps := make([]*org.Endpoint, len(uris))
	for i, u := range uris {
		eps[i] = &org.Endpoint{URI: cbc.URI(u)}
	}
	return eps
}

func endpointURIs(eps []*org.Endpoint) []string {
	uris := make([]string, len(eps))
	for i, e := range eps {
		uris[i] = e.URI.String()
	}
	return uris
}

func TestPartyEndpointNormalization(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "OVT with business ID keeps the OVT",
			in:   []string{ovt, businessID},
			want: []string{ovt},
		},
		{
			name: "keeps other schemes in order",
			in:   []string{ovt, gln, mailto},
			want: []string{ovt, mailto},
		},
		{
			name: "OVT after another address is still kept",
			in:   []string{businessID, ovt},
			want: []string{ovt},
		},
		{
			name: "two OVT endpoints keep the first",
			in:   []string{ovt, ovtOther},
			want: []string{ovt},
		},
		{
			name: "without OVT nothing changes",
			in:   []string{businessID, mailto},
			want: []string{businessID, mailto},
		},
		{
			name: "OVT without a code does not trim",
			in:   []string{"iso6523-actorid-upis::0216:", businessID},
			want: []string{"iso6523-actorid-upis::0216:", businessID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := testInvoiceStandard(t)
			inv.Supplier.Endpoints = endpoints(tt.in...)
			inv.Customer.Endpoints = endpoints(tt.in...)
			require.NoError(t, inv.Calculate())
			assert.Equal(t, tt.want, endpointURIs(inv.Supplier.Endpoints))
			assert.Equal(t, tt.want, endpointURIs(inv.Customer.Endpoints))
		})
	}

	t.Run("inboxes are untouched", func(t *testing.T) {
		inv := testInvoiceStandard(t)
		inv.Supplier.Endpoints = endpoints(ovt, businessID)
		inv.Supplier.Inboxes = []*org.Inbox{
			{Key: org.InboxKeyPeppol, Scheme: "0037", Code: "2345678-0"},
			{Key: org.InboxKeyPeppol, Scheme: "0088", Code: "7300010000001"},
		}
		require.NoError(t, inv.Calculate())
		assert.Equal(t, []string{ovt}, endpointURIs(inv.Supplier.Endpoints))
		require.Len(t, inv.Supplier.Inboxes, 2)
		assert.Equal(t, "0037", inv.Supplier.Inboxes[0].Scheme.String())
		assert.Equal(t, "0088", inv.Supplier.Inboxes[1].Scheme.String())
	})
}

func TestInvoiceWithOVTAndBusinessID(t *testing.T) {
	inv := testInvoiceStandard(t)
	inv.Supplier.Endpoints = endpoints(ovt, businessID)
	inv.Customer.Endpoints = endpoints(businessID, ovtOther)
	require.NoError(t, inv.Calculate())

	assert.NoError(t, rules.Validate(inv))
	assert.Equal(t, []string{ovt}, endpointURIs(inv.Supplier.Endpoints))
	assert.Equal(t, []string{ovtOther}, endpointURIs(inv.Customer.Endpoints))
}
