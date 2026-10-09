package finvoice

// deliveryNameMaxLength bounds each DeliveryOrganisationName.
const deliveryNameMaxLength = 35

// DeliveryPartyDetails identifies who receives the goods.
type DeliveryPartyDetails struct {
	Identifier *Identifier                   `xml:"DeliveryPartyIdentifier,omitempty"`
	Name       []string                      `xml:"DeliveryOrganisationName"`
	TaxCode    string                        `xml:"DeliveryOrganisationTaxCode,omitempty"`
	Address    *DeliveryPostalAddressDetails `xml:"DeliveryPostalAddressDetails"`
}

// DeliveryPostalAddressDetails is the delivery address.
type DeliveryPostalAddressDetails struct {
	StreetName    []string `xml:"DeliveryStreetName"`
	TownName      string   `xml:"DeliveryTownName"`
	PostCode      string   `xml:"DeliveryPostCodeIdentifier"`
	Subdivision   string   `xml:"DeliveryCountrySubdivision,omitempty"`
	CountryCode   string   `xml:"CountryCode,omitempty"`
	PostOfficeBox string   `xml:"DeliveryPostofficeBoxIdentifier,omitempty"`
}

// DeliveryDetails holds when the goods were delivered.
type DeliveryDetails struct {
	Date   *Date                  `xml:"DeliveryDate,omitempty"`
	Period *DeliveryPeriodDetails `xml:"DeliveryPeriodDetails,omitempty"`
}

// DeliveryPeriodDetails is the delivery period.
type DeliveryPeriodDetails struct {
	StartDate *Date `xml:"StartDate"`
	EndDate   *Date `xml:"EndDate"`
}

// newDeliveryParty writes the delivery receiver, which Finvoice only takes
// with a name and an address.
func (c *converter) newDeliveryParty() *DeliveryPartyDetails {
	d := c.inv.Delivery
	if d == nil || d.Receiver == nil {
		return nil
	}
	a := newPostalAddress(d.Receiver)
	name := split(d.Receiver.Name, deliveryNameMaxLength, unbounded)
	if a == nil || len(name) == 0 {
		return nil
	}
	return &DeliveryPartyDetails{
		Identifier: newPartyIdentifier(d.Receiver),
		Name:       name,
		TaxCode:    partyTaxCode(d.Receiver),
		Address: &DeliveryPostalAddressDetails{
			StreetName:    a.streetName,
			TownName:      a.townName,
			PostCode:      a.postCode,
			Subdivision:   a.subdivision,
			CountryCode:   a.countryCode,
			PostOfficeBox: a.postOfficeBox,
		},
	}
}

func (c *converter) applyDelivery(doc *Invoice) {
	d := c.inv.Delivery
	if d == nil || (d.Date == nil && d.Period == nil) {
		return
	}
	doc.DeliveryDetails = &DeliveryDetails{Date: newDatePtr(d.Date)}
	// The addon requires both dates on a delivery period.
	if d.Period != nil {
		doc.DeliveryDetails.Period = &DeliveryPeriodDetails{
			StartDate: newDate(*d.Period.Start),
			EndDate:   newDate(*d.Period.End),
		}
	}
}
