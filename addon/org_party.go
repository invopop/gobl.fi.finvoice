package finvoice

import (
	"fmt"

	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
)

// identityMaxLength bounds the Seller, Buyer and Delivery PartyIdentifier.
const identityMaxLength = 35

// orgPartyRules covers the parties a Finvoice document names.
func orgPartyRules() *rules.Set {
	return rules.For(new(org.Party),
		rules.Field("identities",
			rules.Assert("01", fmt.Sprintf("legal identity code must be at most %d characters (Finvoice SellerPartyIdentifier, BuyerPartyIdentifier)", identityMaxLength),
				is.Func("legal identity fits", legalIdentityFits),
			),
		),
	)
}

func legalIdentityFits(val any) bool {
	ids, ok := val.([]*org.Identity)
	if !ok {
		return true
	}
	for _, id := range ids {
		if id != nil && id.Scope == org.IdentityScopeLegal && !fits(id.Code, identityMaxLength) {
			return false
		}
	}
	return true
}
