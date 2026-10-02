package finvoice

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
)

// icdOVT is the ISO 6523 scheme (ICD) of the Finnish OVT electronic address.
const icdOVT = "0216"

const (
	// identityMaxLength bounds the Seller, Buyer and Delivery PartyIdentifier.
	identityMaxLength = 35
	// einvoiceAddressMaxLength bounds an e-invoice address code, written as
	// the OrganisationUnitNumber and the From and To identifiers.
	einvoiceAddressMaxLength = 35
	// einvoiceSchemeMaxLength bounds the scheme written beside it.
	einvoiceSchemeMaxLength = 10
)

// legalIdentityLimit bounds a party's legal identity code, for the parties
// whose identifier Finvoice writes.
func legalIdentityLimit(id rules.Code, element string) rules.Def {
	return rules.Field("identities",
		rules.Each(
			rules.When(is.Func("legal scope", isLegalIdentity),
				rules.Field("code",
					rules.Assert(id, fmt.Sprintf("legal identity code must be at most %d characters (Finvoice %s)", identityMaxLength, element),
						is.RuneLength(0, identityMaxLength),
					),
				),
			),
		),
	)
}

// einvoiceAddressLimit bounds the e-invoice address a party resolves to,
// for the parties whose address Finvoice writes.
func einvoiceAddressLimit(id rules.Code, element string) rules.Def {
	return rules.Assert(id, fmt.Sprintf("e-invoice address code must be at most %d characters and its scheme at most %d (Finvoice %s)", einvoiceAddressMaxLength, einvoiceSchemeMaxLength, element),
		is.Func("e-invoice address fits", einvoiceAddressFits),
	)
}

func isLegalIdentity(val any) bool {
	id, ok := val.(*org.Identity)
	return ok && id != nil && id.Scope == org.IdentityScopeLegal
}

func einvoiceAddressFits(val any) bool {
	p, ok := val.(*org.Party)
	if !ok || p == nil {
		return true
	}
	scheme, code := EInvoiceAddress(p)
	return len(scheme) <= einvoiceSchemeMaxLength && utf8.RuneCountInString(code) <= einvoiceAddressMaxLength
}

// EInvoiceAddress is the party's e-invoice address, scheme and code, read
// from its ISO 6523 endpoint or, failing that, from a coded inbox, the older
// way of recording one; the scheme may then be empty, as Finvoice writes the
// code alone. A party with neither has none.
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
	return ok && icd == icdOVT && code != ""
}
