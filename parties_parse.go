package finvoice

import (
	"regexp"
	"slices"
	"strings"

	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// vatPrefix is a VAT number starting with its two-letter country code.
var vatPrefix = regexp.MustCompile(`^[A-Z]{2}.*[A-Z0-9]`)

func (p *parser) supplier() *org.Party {
	s := p.doc.Seller
	if s == nil {
		return &org.Party{}
	}
	party := &org.Party{
		Name:  joinNames(s.Name),
		Alias: strings.TrimSpace(s.TradingName),
	}
	var country l10n.ISOCountryCode
	if s.Address != nil {
		address := goblAddress(s.Address.StreetName, s.Address.TownName,
			s.Address.PostCode, s.Address.Subdivision, s.Address.CountryCode, s.Address.PostOfficeBox)
		party.Addresses = []*org.Address{address}
		country = address.Country
	}
	party.TaxID, party.Identities = goblIdentification(s.TaxCode, s.Identifier, country)
	setEInvoiceAddress(party, "", p.doc.SellerOrganisationUnitNumber)
	addContact(party, p.doc.SellerContactPersonName)
	if c := p.doc.SellerCommunication; c != nil {
		addCommunication(party, c.Phone, c.Email)
	}
	if info := p.doc.SellerInformation; info != nil {
		if email := strings.TrimSpace(info.Email); email != "" && !slices.ContainsFunc(party.Emails, func(e *org.Email) bool { return e.Address == email }) {
			party.Emails = append(party.Emails, &org.Email{Address: email})
		}
		if info.Website != "" {
			party.Websites = []*org.Website{{URL: strings.TrimSpace(info.Website)}}
		}
	}
	return party
}

func (p *parser) customer() *org.Party {
	b := p.doc.Buyer
	if b == nil {
		return nil
	}
	party := &org.Party{
		Name:  joinNames(b.Name),
		Alias: strings.TrimSpace(b.TradingName),
	}
	var country l10n.ISOCountryCode
	if b.Address != nil {
		address := goblAddress(b.Address.StreetName, b.Address.TownName,
			b.Address.PostCode, b.Address.Subdivision, b.Address.CountryCode, b.Address.PostOfficeBox)
		party.Addresses = []*org.Address{address}
		country = address.Country
	}
	party.TaxID, party.Identities = goblIdentification(b.TaxCode, b.Identifier, country)
	setEInvoiceAddress(party, "", p.doc.BuyerOrganisationUnitNumber)
	addContact(party, p.doc.BuyerContactPersonName)
	if c := p.doc.BuyerCommunication; c != nil {
		addCommunication(party, c.Phone, c.Email)
	}
	return party
}

// joinNames reads an organisation name given over several elements.
func joinNames(names []string) string {
	var parts []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " ")
}

// goblIdentification reads the VAT number and the party identifier, which
// is left out when it is the Finnish Y-tunnus the VAT number is built from.
func goblIdentification(taxCode string, id *Identifier, country l10n.ISOCountryCode) (*tax.Identity, []*org.Identity) {
	var tid *tax.Identity
	taxCode = strings.ToUpper(strings.TrimSpace(taxCode))
	if vatPrefix.MatchString(taxCode) {
		// Kept as written so validation, not the parser, judges the number.
		tid = &tax.Identity{
			Country: l10n.TaxCountryCode(taxCode[:2]),
			Code:    cbc.NormalizeAlphanumericalCode(cbc.Code(taxCode[2:])),
		}
	}
	if id == nil || strings.TrimSpace(id.Value) == "" {
		return tid, nil
	}
	code := strings.TrimSpace(id.Value)
	if tid != nil && tid.Country == l10n.FI.Tax() {
		if m := businessID.FindStringSubmatch(code); m != nil && tid.Code.String() == m[1]+m[2] {
			return tid, nil
		}
	}
	// The address places the party, or the VAT number when the address names no country.
	abroad := country != "" && country != l10n.FI.ISO()
	if country == "" && tid != nil {
		abroad = tid.Country != l10n.FI.Tax()
	}
	identity := &org.Identity{Scope: org.IdentityScopeLegal, Code: cbc.Code(code)}
	if scheme := strings.TrimSpace(id.SchemeID); scheme != "" {
		identity.Ext = identity.Ext.Set(iso.ExtKeySchemeID, cbc.Code(scheme))
	} else if !abroad && isBusinessID(code) {
		identity.Ext = identity.Ext.Set(iso.ExtKeySchemeID, cbc.Code(schemeFinnishOrg))
	}
	return tid, []*org.Identity{identity}
}

func goblAddress(streets []string, town, postCode, subdivision, countryCode, poBox string) *org.Address {
	a := &org.Address{
		Locality:      strings.TrimSpace(town),
		Code:          cbc.Code(strings.TrimSpace(postCode)),
		Region:        strings.TrimSpace(subdivision),
		Country:       l10n.ISOCountryCode(strings.TrimSpace(countryCode)),
		PostOfficeBox: strings.TrimSpace(poBox),
	}
	var lines []string
	for _, s := range streets {
		if s = strings.TrimSpace(s); s != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) > 0 {
		a.Street = lines[0]
	}
	if len(lines) > 1 {
		a.StreetExtra = strings.Join(lines[1:], ", ")
	}
	return a
}

// addContact records the contact person, whose name Finvoice gives as one
// string.
func addContact(party *org.Party, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	n := &org.Name{Given: name}
	if given, surname, ok := strings.Cut(name, " "); ok {
		n.Given, n.Surname = given, surname
	}
	party.People = []*org.Person{{Name: n}}
}

func addCommunication(party *org.Party, phone, email string) {
	if phone = strings.TrimSpace(phone); phone != "" {
		party.Telephones = []*org.Telephone{{Number: phone}}
	}
	if email = strings.TrimSpace(email); email != "" {
		party.Emails = []*org.Email{{Address: email}}
	}
}
