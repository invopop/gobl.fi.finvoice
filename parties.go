package fifinvoice

import (
	"regexp"
	"strings"

	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

const (
	schemeIBAN = "IBAN"
	schemeBBAN = "BBAN"
	schemeBIC  = "BIC"

	// nameMaxLength bounds organisation names.
	nameMaxLength = 70
	// streetMaxLength bounds each street line, town and post code.
	streetMaxLength = 35
	// streetLines is how many street lines an address may carry.
	streetLines = 3
)

// businessID is a Finnish Y-tunnus: seven digits, a hyphen and a check digit.
var businessID = regexp.MustCompile(`^([0-9]{7})-?([0-9])$`)

// SellerPartyDetails identifies the seller.
type SellerPartyDetails struct {
	Identifier  *Identifier                 `xml:"SellerPartyIdentifier,omitempty"`
	Name        string                      `xml:"SellerOrganisationName"`
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
	Name        string                     `xml:"BuyerOrganisationName"`
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
	Name       string                        `xml:"DeliveryOrganisationName"`
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
		Name:        cut(p.Name, nameMaxLength),
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
		Name:        cut(p.Name, nameMaxLength),
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
		Name:       cut(d.Receiver.Name, streetMaxLength),
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
	if c.inv.Payment == nil || c.inv.Payment.Instructions == nil {
		return nil
	}
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

func (c *converter) applyBuyerDetails(doc *Document) {
	p := c.inv.Customer
	_, doc.BuyerOrganisationUnitNumber = partyAddress(p)
	doc.BuyerContactPersonName = contactName(p)
	if phone, email := contactDetails(p); phone != "" || email != "" {
		doc.BuyerCommunication = &BuyerCommunicationDetails{Phone: phone, Email: email}
	}
}

func (c *converter) applyDelivery(doc *Document) {
	d := c.inv.Delivery
	if d == nil || d.Date == nil {
		return
	}
	doc.DeliveryDetails = &DeliveryDetails{Date: newDate(*d.Date)}
}

// newPartyIdentifier writes the party's legal registration: the Y-tunnus
// for a Finnish party, else its first legal-scope identity.
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
			return &Identifier{Value: id.Code.String()}
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

func newPostalAddress(p *org.Party) *postalAddress {
	if p == nil || len(p.Addresses) == 0 || p.Addresses[0] == nil {
		return nil
	}
	a := p.Addresses[0]
	out := &postalAddress{
		townName:      cut(a.Locality, streetMaxLength),
		postCode:      cut(a.Code.String(), streetMaxLength),
		subdivision:   cut(a.Region, streetMaxLength),
		countryCode:   a.Country.String(),
		postOfficeBox: cut(a.PostOfficeBox, streetMaxLength),
	}
	street := strings.TrimSpace(a.Street + " " + a.Number)
	for _, line := range []string{street, a.StreetExtra} {
		if line != "" && len(out.streetName) < streetLines {
			out.streetName = append(out.streetName, cut(line, streetMaxLength))
		}
	}
	if len(out.streetName) == 0 {
		// A street line is required; the PO box or town stands in.
		out.streetName = []string{cut(firstNonEmpty(a.PostOfficeBox, a.Locality), streetMaxLength)}
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

func contactDetails(p *org.Party) (phone, email string) {
	if p == nil {
		return "", ""
	}
	if len(p.Telephones) > 0 && p.Telephones[0] != nil {
		phone = cut(p.Telephones[0].Number, streetMaxLength)
	}
	if len(p.Emails) > 0 && p.Emails[0] != nil {
		email = cut(p.Emails[0].Address, nameMaxLength)
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

// vatCategory reads the UNTDID 5305 category the EN 16931 addon records on
// a tax combo or rate total.
func vatCategory(ext tax.Extensions) string {
	return ext.Get(untdidTaxCategory).String()
}

// vatCombo is the VAT tax on a line, charge or discount, if any.
func vatCombo(set tax.Set) *tax.Combo {
	if set == nil {
		return nil
	}
	return set.Get(tax.CategoryVAT)
}

// invoiceNumber is the series and code together, the document's one number.
func invoiceNumber(inv *bill.Invoice) string {
	return inv.Series.Join(inv.Code).String()
}
