package fifinvoice

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
)

const (
	schemeIBAN = "IBAN"
	schemeBBAN = "BBAN"
	schemeBIC  = "BIC"

	// nameMaxLength bounds each organisation name element, which may repeat.
	nameMaxLength = 70
	// nameLines is how many name elements the parties' names may take.
	nameLines = 2
	// addressMaxLength bounds each street line, the town and the post code.
	addressMaxLength = 35
	// streetLines is how many street lines an address may carry.
	streetLines = 3
	// emailMaxLength bounds an email address, which is left out when longer.
	emailMaxLength = 70
)

// businessID is a Finnish Y-tunnus: seven digits, a hyphen and a check digit.
var businessID = regexp.MustCompile(`^([0-9]{7})-?([0-9])$`)

// SellerPartyDetails identifies the seller.
type SellerPartyDetails struct {
	Identifier  *Identifier                 `xml:"SellerPartyIdentifier,omitempty"`
	Name        []string                    `xml:"SellerOrganisationName"`
	TradingName string                      `xml:"SellerOrganisationTradingName,omitempty"`
	TaxCode     string                      `xml:"SellerOrganisationTaxCode,omitempty"`
	Address     *SellerPostalAddressDetails `xml:"SellerPostalAddressDetails,omitempty"`
}

// SellerPostalAddressDetails is the seller's postal address.
type SellerPostalAddressDetails struct {
	StreetName    []string `xml:"SellerStreetName"`
	TownName      string   `xml:"SellerTownName"`
	PostCode      string   `xml:"SellerPostCodeIdentifier"`
	Subdivision   string   `xml:"SellerCountrySubdivision,omitempty"`
	CountryCode   string   `xml:"CountryCode,omitempty"`
	PostOfficeBox string   `xml:"SellerPostOfficeBoxIdentifier,omitempty"`
}

// SellerCommunicationDetails holds the seller contact's phone and email.
type SellerCommunicationDetails struct {
	Phone string `xml:"SellerPhoneNumberIdentifier,omitempty"`
	Email string `xml:"SellerEmailaddressIdentifier,omitempty"`
}

// SellerInformationDetails holds the seller's general details and bank
// accounts.
type SellerInformationDetails struct {
	Email    string                  `xml:"SellerCommonEmailaddressIdentifier,omitempty"`
	Website  string                  `xml:"SellerWebaddressIdentifier,omitempty"`
	Accounts []*SellerAccountDetails `xml:"SellerAccountDetails,omitempty"`
}

// SellerAccountDetails is one of the seller's bank accounts.
type SellerAccountDetails struct {
	AccountID Account `xml:"SellerAccountID"`
	BIC       Account `xml:"SellerBic"`
	Name      string  `xml:"SellerAccountName,omitempty"`
}

// BuyerPartyDetails identifies the buyer.
type BuyerPartyDetails struct {
	Identifier  *Identifier                `xml:"BuyerPartyIdentifier,omitempty"`
	Name        []string                   `xml:"BuyerOrganisationName"`
	TradingName string                     `xml:"BuyerOrganisationTradingName,omitempty"`
	TaxCode     string                     `xml:"BuyerOrganisationTaxCode,omitempty"`
	Address     *BuyerPostalAddressDetails `xml:"BuyerPostalAddressDetails,omitempty"`
}

// BuyerPostalAddressDetails is the buyer's postal address.
type BuyerPostalAddressDetails struct {
	StreetName    []string `xml:"BuyerStreetName"`
	TownName      string   `xml:"BuyerTownName"`
	PostCode      string   `xml:"BuyerPostCodeIdentifier"`
	Subdivision   string   `xml:"BuyerCountrySubdivision,omitempty"`
	CountryCode   string   `xml:"CountryCode,omitempty"`
	PostOfficeBox string   `xml:"BuyerPostOfficeBoxIdentifier,omitempty"`
}

// BuyerCommunicationDetails holds the buyer contact's phone and email.
type BuyerCommunicationDetails struct {
	Phone string `xml:"BuyerPhoneNumberIdentifier,omitempty"`
	Email string `xml:"BuyerEmailaddressIdentifier,omitempty"`
}

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

// postalAddress is the role-independent view of an address, before it is
// written under the seller, buyer or delivery element names.
type postalAddress struct {
	streetName    []string
	townName      string
	postCode      string
	subdivision   string
	countryCode   string
	postOfficeBox string
}

func (c *converter) newSeller() *SellerPartyDetails {
	p := c.inv.Supplier
	s := &SellerPartyDetails{
		Identifier:  newPartyIdentifier(p),
		Name:        split(p.Name, nameMaxLength, nameLines),
		TradingName: cut(p.Alias, nameMaxLength),
		TaxCode:     partyTaxCode(p),
	}
	if a := newPostalAddress(p); a != nil {
		s.Address = &SellerPostalAddressDetails{
			StreetName:    a.streetName,
			TownName:      a.townName,
			PostCode:      a.postCode,
			Subdivision:   a.subdivision,
			CountryCode:   a.countryCode,
			PostOfficeBox: a.postOfficeBox,
		}
	}
	return s
}

func (c *converter) newBuyer() *BuyerPartyDetails {
	p := c.inv.Customer
	b := &BuyerPartyDetails{
		Identifier:  newPartyIdentifier(p),
		Name:        split(p.Name, nameMaxLength, nameLines),
		TradingName: cut(p.Alias, nameMaxLength),
		TaxCode:     partyTaxCode(p),
	}
	if a := newPostalAddress(p); a != nil {
		b.Address = &BuyerPostalAddressDetails{
			StreetName:    a.streetName,
			TownName:      a.townName,
			PostCode:      a.postCode,
			Subdivision:   a.subdivision,
			CountryCode:   a.countryCode,
			PostOfficeBox: a.postOfficeBox,
		}
	}
	return b
}

// newDeliveryParty writes the delivery receiver, which Finvoice only takes
// with an address.
func (c *converter) newDeliveryParty() *DeliveryPartyDetails {
	d := c.inv.Delivery
	if d == nil || d.Receiver == nil {
		return nil
	}
	a := newPostalAddress(d.Receiver)
	if a == nil {
		return nil
	}
	return &DeliveryPartyDetails{
		Identifier: newPartyIdentifier(d.Receiver),
		Name:       split(d.Receiver.Name, addressMaxLength, nameLines),
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

// applySellerDetails writes the seller elements that sit outside
// SellerPartyDetails: the e-invoice address, contact, and bank accounts.
func (c *converter) applySellerDetails(doc *Document) {
	p := c.inv.Supplier
	_, doc.SellerOrganisationUnitNumber = partyAddress(p)
	doc.SellerContactPersonName = contactName(p)
	if phone, email := contactDetails(p); phone != "" || email != "" {
		doc.SellerCommunication = &SellerCommunicationDetails{Phone: phone, Email: email}
	}
	info := &SellerInformationDetails{}
	if len(p.Websites) > 0 && p.Websites[0] != nil {
		info.Website = p.Websites[0].URL
	}
	info.Accounts = c.newSellerAccounts()
	if info.Website != "" || len(info.Accounts) > 0 {
		doc.SellerInformation = info
	}
}

// newSellerAccounts lists the accounts a transfer may be paid to. Finvoice
// requires a BIC on each, so accounts without one are left to the payment
// order alone.
func (c *converter) newSellerAccounts() []*SellerAccountDetails {
	var out []*SellerAccountDetails
	for _, ct := range c.inv.Payment.Instructions.CreditTransfer {
		if ct == nil || ct.BIC == "" {
			continue
		}
		id := newAccountID(ct)
		if id == nil {
			continue
		}
		out = append(out, &SellerAccountDetails{
			AccountID: *id,
			BIC:       Account{Value: ct.BIC.String(), Scheme: schemeBIC},
			Name:      cut(ct.Name, nameMaxLength),
		})
	}
	return out
}

// applyBuyerDetails writes the buyer elements outside BuyerPartyDetails. The
// e-invoice address is what the document is routed by, so a customer
// without one is refused.
func (c *converter) applyBuyerDetails(doc *Document) error {
	p := c.inv.Customer
	_, doc.BuyerOrganisationUnitNumber = partyAddress(p)
	if doc.BuyerOrganisationUnitNumber == "" {
		return fmt.Errorf("customer needs an e-invoice address as an endpoint, such as %s::0216:003701120389", iso.ActorIDScheme)
	}
	doc.BuyerContactPersonName = contactName(p)
	if phone, email := contactDetails(p); phone != "" || email != "" {
		doc.BuyerCommunication = &BuyerCommunicationDetails{Phone: phone, Email: email}
	}
	return nil
}

func (c *converter) applyDelivery(doc *Document) {
	d := c.inv.Delivery
	if d == nil || (d.Date == nil && d.Period == nil) {
		return
	}
	doc.DeliveryDetails = &DeliveryDetails{Date: newDatePtr(d.Date)}
	if d.Period != nil && d.Period.Start != nil && d.Period.End != nil {
		doc.DeliveryDetails.Period = &DeliveryPeriodDetails{
			StartDate: newDate(*d.Period.Start),
			EndDate:   newDate(*d.Period.End),
		}
	}
}

// newPartyIdentifier writes the party's legal registration: the Y-tunnus
// for a Finnish party, else its first legal-scope identity with the ISO 6523
// scheme the EN 16931 addon records on it.
func newPartyIdentifier(p *org.Party) *Identifier {
	if p == nil {
		return nil
	}
	if p.TaxID != nil && p.TaxID.Country == l10n.FI.Tax() {
		if m := businessID.FindStringSubmatch(p.TaxID.Code.String()); m != nil {
			return &Identifier{Value: m[1] + "-" + m[2]}
		}
	}
	for _, id := range p.Identities {
		if id != nil && id.Scope == org.IdentityScopeLegal && id.Code != "" {
			return &Identifier{
				Value:    id.Code.String(),
				SchemeID: id.Ext.Get(iso.ExtKeySchemeID).String(),
			}
		}
	}
	return nil
}

// partyTaxCode is the VAT number: country code plus the tax identity code.
func partyTaxCode(p *org.Party) string {
	if p == nil || p.TaxID == nil || p.TaxID.Code == "" {
		return ""
	}
	return p.TaxID.String()
}

// newPostalAddress writes the first address, which Finvoice only takes with
// a street, a town and a post code; an address missing one of them is left
// out rather than filled in.
func newPostalAddress(p *org.Party) *postalAddress {
	if p == nil || len(p.Addresses) == 0 || p.Addresses[0] == nil {
		return nil
	}
	a := p.Addresses[0]
	if a.Locality == "" || a.Code == "" {
		return nil
	}
	out := &postalAddress{
		townName:      cut(a.Locality, addressMaxLength),
		postCode:      cut(a.Code.String(), addressMaxLength),
		subdivision:   cut(a.Region, addressMaxLength),
		countryCode:   a.Country.String(),
		postOfficeBox: cut(a.PostOfficeBox, addressMaxLength),
	}
	street := strings.TrimSpace(a.Street + " " + a.Number)
	out.streetName = split(street, addressMaxLength, streetLines)
	if extra := split(a.StreetExtra, addressMaxLength, streetLines-len(out.streetName)); len(extra) > 0 {
		out.streetName = append(out.streetName, extra...)
	}
	if len(out.streetName) == 0 {
		// A street line is required; the PO box or town stands in.
		out.streetName = []string{cut(firstNonEmpty(a.PostOfficeBox, a.Locality), addressMaxLength)}
	}
	return out
}

func contactName(p *org.Party) string {
	if p == nil || len(p.People) == 0 || p.People[0] == nil || p.People[0].Name == nil {
		return ""
	}
	n := p.People[0].Name
	var parts []string
	for _, part := range []string{n.Prefix, n.Given, n.Middle, n.Surname} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return cut(strings.Join(parts, " "), nameMaxLength)
}

// contactDetails returns the first phone and email; an email too long for
// its element is left out, since a cut address reaches nobody.
func contactDetails(p *org.Party) (phone, email string) {
	if p == nil {
		return "", ""
	}
	if len(p.Telephones) > 0 && p.Telephones[0] != nil {
		phone = cut(p.Telephones[0].Number, addressMaxLength)
	}
	if len(p.Emails) > 0 && p.Emails[0] != nil && !tooLong(p.Emails[0].Address, emailMaxLength) {
		email = p.Emails[0].Address
	}
	return phone, email
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
