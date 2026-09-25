package fifinvoice

import (
	"regexp"
	"strings"

	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/catalogues/iso"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/l10n"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/tax"
)

// ISO 6523 scheme codes guessed for an e-invoice address given without one.
const (
	// schemeOVT is the Finnish OVT code, which starts with 0037.
	schemeOVT = "0216"
	// schemeFinnishOrg is the Finnish organisation number (Y-tunnus).
	schemeFinnishOrg = "0212"
	// schemeIBANAddress is an IBAN used as an e-invoice address.
	schemeIBANAddress = "9918"
)

var (
	ovtAddress  = regexp.MustCompile(`^0037[0-9]{8,}$`)
	ibanAddress = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
)

func (p *parser) supplier() *org.Party {
	s := p.doc.Seller
	if s == nil {
		return &org.Party{}
	}
	party := &org.Party{
		Name:  strings.TrimSpace(s.Name),
		Alias: strings.TrimSpace(s.TradingName),
	}
	var country l10n.ISOCountryCode
	if s.Address != nil {
		country = l10n.ISOCountryCode(s.Address.CountryCode)
		party.Addresses = []*org.Address{parseAddress(s.Address.StreetName, s.Address.TownName,
			s.Address.PostCode, s.Address.Subdivision, s.Address.CountryCode, s.Address.PostOfficeBox)}
	}
	party.TaxID, party.Identities = parseIdentification(s.TaxCode, s.Identifier, country)
	if ep := newEndpoint("", p.doc.SellerOrganisationUnitNumber); ep != nil {
		party.Endpoints = []*org.Endpoint{ep}
	}
	addContact(party, p.doc.SellerContactPersonName)
	if c := p.doc.SellerCommunication; c != nil {
		addCommunication(party, c.Phone, c.Email)
	}
	if info := p.doc.SellerInformation; info != nil {
		if info.Email != "" && len(party.Emails) == 0 {
			party.Emails = []*org.Email{{Address: strings.TrimSpace(info.Email)}}
		}
		if info.Website != "" {
			party.Websites = []*org.Website{{URL: strings.TrimSpace(info.Website)}}
		}
	}
	return party
}

func (p *parser) buyer() *org.Party {
	b := p.doc.Buyer
	if b == nil {
		return nil
	}
	party := &org.Party{
		Name:  strings.TrimSpace(b.Name),
		Alias: strings.TrimSpace(b.TradingName),
	}
	var country l10n.ISOCountryCode
	if b.Address != nil {
		country = l10n.ISOCountryCode(b.Address.CountryCode)
		party.Addresses = []*org.Address{parseAddress(b.Address.StreetName, b.Address.TownName,
			b.Address.PostCode, b.Address.Subdivision, b.Address.CountryCode, b.Address.PostOfficeBox)}
	}
	party.TaxID, party.Identities = parseIdentification(b.TaxCode, b.Identifier, country)
	if ep := newEndpoint("", p.doc.BuyerOrganisationUnitNumber); ep != nil {
		party.Endpoints = []*org.Endpoint{ep}
	}
	addContact(party, p.doc.BuyerContactPersonName)
	if c := p.doc.BuyerCommunication; c != nil {
		addCommunication(party, c.Phone, c.Email)
	}
	return party
}

// applyFrame takes the e-invoice addresses and operators from the message
// frame, which wins over the organisation unit numbers when both are given.
func (p *parser) applyFrame(inv *bill.Invoice) {
	frame := p.doc.Transmission
	if frame == nil {
		return
	}
	applyRoute(inv.Supplier, frame.Sender.Identifier, frame.Sender.Intermediator)
	applyRoute(inv.Customer, frame.Receiver.Identifier, frame.Receiver.Intermediator)
}

func applyRoute(party *org.Party, id Identifier, operator string) {
	if party == nil {
		return
	}
	if ep := newEndpoint(id.SchemeID, id.Value); ep != nil {
		party.Endpoints = []*org.Endpoint{ep}
	}
	if operator = strings.TrimSpace(operator); operator != "" {
		party.Ext = party.Ext.Set(finvoice.ExtKeyOperator, cbc.Code(operator))
	}
}

// newEndpoint builds the ISO 6523 endpoint for an e-invoice address, guessing
// the scheme from the address's shape when the document names none.
func newEndpoint(scheme, value string) *org.Endpoint {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if scheme == "" {
		scheme = guessScheme(value)
	}
	if scheme == "" {
		return nil
	}
	return &org.Endpoint{URI: cbc.URI(iso.ActorIDScheme + "::" + scheme + ":" + value)}
}

func guessScheme(value string) string {
	switch {
	case ovtAddress.MatchString(value):
		return schemeOVT
	case ibanAddress.MatchString(value):
		return schemeIBANAddress
	case businessID.MatchString(value):
		return schemeFinnishOrg
	}
	return ""
}

// parseIdentification reads the VAT number into the tax identity and the
// party identifier into either the same identity, when it is the Finnish
// Y-tunnus the VAT number is built from, or a legal identity.
func parseIdentification(taxCode string, id *Identifier, country l10n.ISOCountryCode) (*tax.Identity, []*org.Identity) {
	var tid *tax.Identity
	if taxCode = strings.TrimSpace(taxCode); taxCode != "" {
		if parsed, err := tax.ParseIdentity(taxCode); err == nil {
			tid = parsed
		}
	}
	if id == nil || strings.TrimSpace(id.Value) == "" {
		return tid, nil
	}
	code := strings.TrimSpace(id.Value)
	if m := businessID.FindStringSubmatch(code); m != nil && (country == l10n.FI.ISO() || tid != nil && tid.Country == l10n.FI.Tax()) {
		if tid == nil {
			tid = &tax.Identity{Country: l10n.FI.Tax(), Code: cbc.Code(m[1] + m[2])}
		}
		if tid.Code.String() == m[1]+m[2] {
			return tid, nil
		}
	}
	return tid, []*org.Identity{{Scope: org.IdentityScopeLegal, Code: cbc.Code(code)}}
}

func parseAddress(streets []string, town, postCode, subdivision, countryCode, poBox string) *org.Address {
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
