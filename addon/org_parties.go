package addon

import (
	"strings"
	"unicode/utf8"

	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
)

// SchemeOVT is the ISO 6523 scheme (ICD) of the Finnish OVT e-invoice address.
const SchemeOVT = "0216"

// Lengths of a party's identifier and of its e-invoice address code and
// scheme, as Finvoice writes them.
const (
	identityMaxLength        = 35
	einvoiceAddressMinLength = 2
	einvoiceAddressMaxLength = 35
	einvoiceSchemeMaxLength  = 10
)

func isLegalIdentity(val any) bool {
	id, ok := val.(*org.Identity)
	return ok && id != nil && id.Scope == org.IdentityScopeLegal
}

// hasEInvoiceAddress checks the party can be addressed; the operator never
// delivers a document whose seller or buyer lacks one.
func hasEInvoiceAddress(val any) bool {
	p, ok := val.(*org.Party)
	if !ok || p == nil {
		return true
	}
	_, code := EInvoiceAddress(p)
	return code != ""
}

func einvoiceAddressFits(val any) bool {
	p, ok := val.(*org.Party)
	if !ok || p == nil {
		return true
	}
	scheme, code := EInvoiceAddress(p)
	if code == "" {
		return true
	}
	n := utf8.RuneCountInString(code)
	return len(scheme) <= einvoiceSchemeMaxLength && n >= einvoiceAddressMinLength && n <= einvoiceAddressMaxLength
}

// EInvoiceAddress reads the party's e-invoice address from its ISO 6523
// endpoint, else from its first coded inbox, whose scheme may be empty.
func EInvoiceAddress(p *org.Party) (scheme, code string) {
	if e := p.Endpoint(iso.ActorIDScheme); e != nil {
		scheme, code, _ = strings.Cut(strings.TrimPrefix(e.URI.Opaque(), ":"), ":")
		return scheme, code
	}
	for _, ib := range p.Inboxes {
		if ib != nil && ib.Code != cbc.CodeEmpty {
			return ib.Scheme.String(), ib.Code.String()
		}
	}
	return "", ""
}

// normalizeOrgParty keeps only the OVT among a party's ISO 6523 endpoints,
// because an invoice gives each party one electronic address and on a Finnish
// invoice that is the OVT.
func normalizeOrgParty(p *org.Party) {
	if p == nil || !hasOVTEndpoint(p.Endpoints) {
		return
	}
	kept := make([]*org.Endpoint, 0, len(p.Endpoints))
	foundOVT := false
	for _, e := range p.Endpoints {
		switch {
		case e == nil || e.URI.Scheme() != iso.ActorIDScheme:
			kept = append(kept, e)
		case !foundOVT && isOVT(e):
			foundOVT = true
			kept = append(kept, e)
		}
	}
	p.Endpoints = kept
}

func hasOVTEndpoint(endpoints []*org.Endpoint) bool {
	for _, e := range endpoints {
		if e != nil && isOVT(e) {
			return true
		}
	}
	return false
}

// isOVT reports whether the endpoint is an ISO 6523 address with the OVT
// scheme and a code, from its opaque part ":<icd>:<code>". Only the canonical
// empty-authority form counts, the one GOBL's EN 16931 rules accept.
func isOVT(e *org.Endpoint) bool {
	if e.URI.Scheme() != iso.ActorIDScheme {
		return false
	}
	rest, ok := strings.CutPrefix(e.URI.Opaque(), ":")
	if !ok {
		return false
	}
	icd, code, ok := strings.Cut(rest, ":")
	return ok && icd == SchemeOVT && code != ""
}
