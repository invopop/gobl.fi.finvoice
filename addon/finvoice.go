// Package addon provides extensions and validations for the Finnish
// Finvoice 3.0 format.
package addon

import (
	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/norm"
	"github.com/invopop/gobl/pkg/here"
	"github.com/invopop/gobl/rules"
	"github.com/invopop/gobl/rules/is"
	"github.com/invopop/gobl/tax"
)

const (
	// Key identifies the Finvoice addon family. Individual versions append a
	// suffix; the family key is used as the fault-code namespace so that
	// rules that carry across versions keep stable codes.
	Key cbc.Key = "fi-finvoice"

	// V3 is the key for the Finvoice version 3.0
	V3 cbc.Key = Key + "-v3"
)

func init() {
	tax.RegisterAddonDef(newAddon())
	rules.RegisterWithGuard(
		Key.String(),
		rules.GOBL.Add("FI-FINVOICE"),
		is.InContext(tax.AddonIn(V3)),
		billInvoiceRules(),
	)
	norm.RegisterWithGuard(
		is.InContext(tax.AddonIn(V3)),
		norm.For(normalizePayInstructions),
		norm.For(normalizePayCreditTransfer),
		norm.For(normalizeOrgParty),
	)
}

func newAddon() *tax.AddonDef {
	return &tax.AddonDef{
		Key: V3,
		Name: i18n.String{
			i18n.EN: "Finland Finvoice 3.0",
		},
		Requires: []cbc.Key{
			en16931.V2017,
		},
		Extensions: extensions,
		Description: i18n.String{
			i18n.EN: here.Doc(`
				Support for Finvoice 3.0, the Finnish electronic invoice format that
				Finance Finland maintains and Finnish operators and banks exchange.
				Finvoice conforms to the European Norm (EN) 16931, so this addon adds only
				what the format needs on top of the EN 16931 rules: the e-invoice
				addresses and operators that route the message, values that fit their
				Finvoice elements whole, and the payment data for the EpiDetails payment
				order, which every Finvoice invoice carries.

				For more information on Finvoice, visit
				[www.finanssiala.fi](https://www.finanssiala.fi/en/topics/finvoice-standard/).
			`),
		},
	}
}
