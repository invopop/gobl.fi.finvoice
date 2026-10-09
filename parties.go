package finvoice

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
)

// Lengths of the party elements, and how many street lines an address takes.
const (
	nameMaxLength        = 70 // each organisation name element, which may repeat
	addressMaxLength     = 35 // each street line, the town and the post code
	streetLines          = 3
	emailMaxLength       = 70
	websiteMaxLength     = 70
	contactNameMaxLength = 70
	phoneMaxLength       = 35
)

// businessID is a Finnish Y-tunnus: seven digits, a hyphen and a check digit.
var businessID = regexp.MustCompile(`^([0-9]{7})-?([0-9])$`)

// schemeFinnishOrg is the ISO 6523 scheme of a Y-tunnus.
const schemeFinnishOrg = "0212"

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
		Name:        split(p.Name, nameMaxLength, unbounded),
		TradingName: fit(p.Alias, nameMaxLength),
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
		Name:        split(p.Name, nameMaxLength, unbounded),
		TradingName: fit(p.Alias, nameMaxLength),
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

// applySellerDetails writes the seller elements that sit outside
// SellerPartyDetails: the e-invoice address, contact, and bank accounts.
func (c *converter) applySellerDetails(doc *Invoice) {
	p := c.inv.Supplier
	_, doc.SellerOrganisationUnitNumber = addon.EInvoiceAddress(p)
	doc.SellerContactPersonName = contactName(p)
	if phone, email := contactDetails(p); phone != "" || email != "" {
		doc.SellerCommunication = &SellerCommunicationDetails{Phone: phone, Email: email}
	}
	info := &SellerInformationDetails{}
	if len(p.Websites) > 0 && p.Websites[0] != nil && !tooLong(p.Websites[0].URL, websiteMaxLength) {
		info.Website = p.Websites[0].URL
	}
	// The first email is the contact's; a second one is the organisation's.
	if len(p.Emails) > 1 && p.Emails[1] != nil && !tooLong(p.Emails[1].Address, emailMaxLength) {
		info.Email = p.Emails[1].Address
	}
	info.Accounts = c.newSellerAccounts()
	if info.Website != "" || info.Email != "" || len(info.Accounts) > 0 {
		doc.SellerInformation = info
	}
}

// applyBuyerDetails writes the buyer elements outside BuyerPartyDetails.
func (c *converter) applyBuyerDetails(doc *Invoice) {
	p := c.inv.Customer
	_, doc.BuyerOrganisationUnitNumber = addon.EInvoiceAddress(p)
	doc.BuyerContactPersonName = contactName(p)
	if phone, email := contactDetails(p); phone != "" || email != "" {
		doc.BuyerCommunication = &BuyerCommunicationDetails{Phone: phone, Email: email}
	}
}

// newPartyIdentifier writes the party's legal registration: its first
// legal-scope identity with the ISO 6523 scheme the EN 16931 addon records on
// it, else, for a Finnish party, the Y-tunnus its VAT number is built from.
func newPartyIdentifier(p *org.Party) *Identifier {
	for _, id := range p.Identities {
		if id != nil && id.Scope == org.IdentityScopeLegal {
			return &Identifier{
				Value:    id.Code.String(),
				SchemeID: id.Ext.Get(iso.ExtKeySchemeID).String(),
			}
		}
	}
	if p.TaxID != nil && p.TaxID.Country == l10n.FI.Tax() {
		if m := businessID.FindStringSubmatch(p.TaxID.Code.String()); m != nil {
			return &Identifier{Value: m[1] + "-" + m[2], SchemeID: schemeFinnishOrg}
		}
	}
	return nil
}

// partyTaxCode is the VAT number: country code plus the tax identity code.
func partyTaxCode(p *org.Party) string {
	if p.TaxID == nil || p.TaxID.Code == cbc.CodeEmpty {
		return ""
	}
	return p.TaxID.String()
}

// newPostalAddress writes the first address, left out when its town or post
// code is too short for the elements Finvoice requires.
func newPostalAddress(p *org.Party) *postalAddress {
	if len(p.Addresses) == 0 || p.Addresses[0] == nil {
		return nil
	}
	a := p.Addresses[0]
	if tooShort(a.Locality) || tooShort(a.Code.String()) {
		return nil
	}
	out := &postalAddress{
		townName:      cut(a.Locality, addressMaxLength),
		postCode:      cut(a.Code.String(), addressMaxLength),
		subdivision:   fit(a.Region, addressMaxLength),
		countryCode:   a.Country.String(),
		postOfficeBox: cut(a.PostOfficeBox, addressMaxLength),
	}
	street := strings.TrimSpace(a.Street + " " + a.Number)
	out.streetName = split(street, addressMaxLength, streetLines)
	if extra := split(a.StreetExtra, addressMaxLength, streetLines-len(out.streetName)); len(extra) > 0 {
		out.streetName = append(out.streetName, extra...)
	}
	if len(out.streetName) == 0 {
		// A street line of at least two characters is required; the PO box or
		// the town stands in.
		street := out.townName
		if utf8.RuneCountInString(out.postOfficeBox) > 1 {
			street = out.postOfficeBox
		}
		out.streetName = []string{street}
	}
	return out
}

func contactName(p *org.Party) string {
	if len(p.People) == 0 || p.People[0] == nil {
		return ""
	}
	n := p.People[0].Name
	var parts []string
	for _, part := range []string{n.Prefix, n.Given, n.Middle, n.Surname} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return cut(strings.Join(parts, " "), contactNameMaxLength)
}

// contactDetails returns the first phone and email; an email too long for
// its element is left out, since a cut address reaches nobody.
func contactDetails(p *org.Party) (phone, email string) {
	if len(p.Telephones) > 0 && p.Telephones[0] != nil {
		phone = cut(p.Telephones[0].Number, phoneMaxLength)
	}
	if len(p.Emails) > 0 && p.Emails[0] != nil && !tooLong(p.Emails[0].Address, emailMaxLength) {
		email = p.Emails[0].Address
	}
	return phone, email
}
