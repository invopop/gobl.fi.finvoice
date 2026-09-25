package finvoice

import (
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/i18n"
	"github.com/invopop/gobl/pkg/here"
)

// ExtKeyOperator identifies the e-invoice operator ("intermediator") a party
// receives Finvoice documents through.
const ExtKeyOperator cbc.Key = "fi-finvoice-operator"

var extensions = []*cbc.Definition{
	{
		Key:     ExtKeyOperator,
		Pattern: `^[A-Za-z0-9]{2,35}$`,
		Name: i18n.String{
			i18n.EN: "Finnish e-invoice operator",
			i18n.FI: "Verkkolaskuoperaattori",
		},
		Desc: i18n.String{
			i18n.EN: here.Doc(`
				Identifier of the e-invoice operator (intermediator) the party receives
				e-invoices through, quoted alongside the party's e-invoice address: an
				operator's own OVT code such as ~003700010001~, or a bank's BIC such
				as ~NDEAFIHH~.

				A Finnish e-invoice address may be registered with several operators, and
				the operator decides where an invoice is delivered. Set it on the customer
				to have the Finvoice message routed to that operator:

				~~~js
				"customer": {
					"name": "Ostaja Oy",
					"endpoints": [
						{ "uri": "iso6523-actorid-upis::0216:003745678907" }
					],
					"ext": {
						"fi-finvoice-operator": "003700010001"
					}
				}
				~~~
			`),
		},
	},
}
