package finvoice

import (
	"strings"

	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/org"
)

// icdOVT is the ISO 6523 scheme (ICD) of the Finnish OVT electronic address.
const icdOVT = "0216"

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
