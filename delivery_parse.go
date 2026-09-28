package finvoice

import (
	"fmt"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
)

func (p *parser) delivery() (*bill.DeliveryDetails, error) {
	d := &bill.DeliveryDetails{}
	if dd := p.doc.DeliveryDetails; dd != nil {
		var err error
		if d.Date, err = parseDatePtr(dd.Date); err != nil {
			return nil, fmt.Errorf("delivery: %w", err)
		}
		if per := dd.Period; per != nil {
			if d.Period, err = parsePeriod(per.StartDate, per.EndDate); err != nil {
				return nil, fmt.Errorf("delivery period: %w", err)
			}
		}
	}
	if dp := p.doc.DeliveryParty; dp != nil && joinNames(dp.Name) != "" {
		receiver := &org.Party{Name: joinNames(dp.Name)}
		var country l10n.ISOCountryCode
		if dp.Address != nil {
			address := goblAddress(dp.Address.StreetName, dp.Address.TownName,
				dp.Address.PostCode, dp.Address.Subdivision, dp.Address.CountryCode, dp.Address.PostOfficeBox)
			receiver.Addresses = []*org.Address{address}
			country = address.Country
		}
		receiver.TaxID, receiver.Identities = goblIdentification(dp.TaxCode, dp.Identifier, country)
		d.Receiver = receiver
	}
	if d.Date == nil && d.Period == nil && d.Receiver == nil {
		return nil, nil
	}
	return d, nil
}
