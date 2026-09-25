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

	// inboxLabel names the inbox an e-invoice address of an unknown shape
	// is kept on, so it stays visible without guessing a scheme for it.
	inboxLabel = "Finvoice"
)

var (
	// ovtAddress is 0037, the eight digits of a Y-tunnus and up to five
	// characters naming a unit.
	ovtAddress  = regexp.MustCompile(`^0037[0-9]{8}[0-9A-Za-z]{0,5}$`)
	ibanAddress = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
	// vatNumber is a country code followed by the identifier.
	vatNumber = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]+$`)
)

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
		country = l10n.ISOCountryCode(s.Address.CountryCode)
		party.Addresses = []*org.Address{parseAddress(s.Address.StreetName, s.Address.TownName,
			s.Address.PostCode, s.Address.Subdivision, s.Address.CountryCode, s.Address.PostOfficeBox)}
	}
	party.TaxID, party.Identities = parseIdentification(s.TaxCode, s.Identifier, country)
	setAddress(party, "", p.doc.SellerOrganisationUnitNumber)
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
		Name:  joinNames(b.Name),
		Alias: strings.TrimSpace(b.TradingName),
	}
	var country l10n.ISOCountryCode
	if b.Address != nil {
		country = l10n.ISOCountryCode(b.Address.CountryCode)
		party.Addresses = []*org.Address{parseAddress(b.Address.StreetName, b.Address.TownName,
			b.Address.PostCode, b.Address.Subdivision, b.Address.CountryCode, b.Address.PostOfficeBox)}
	}
	party.TaxID, party.Identities = parseIdentification(b.TaxCode, b.Identifier, country)
	setAddress(party, "", p.doc.BuyerOrganisationUnitNumber)
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
	setAddress(party, id.SchemeID, id.Value)
	if operator = strings.TrimSpace(operator); operator != "" {
		party.Ext = party.Ext.Set(finvoice.ExtKeyOperator, cbc.Code(operator))
	}
}

// setAddress records the party's e-invoice address: as an ISO 6523 endpoint
// when its scheme is given or can be told from its shape, else as an inbox
// code so an address agreed with an operator is not lost.
func setAddress(party *org.Party, scheme, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if scheme == "" {
		scheme = guessScheme(value)
	}
	if scheme == "" {
		party.Inboxes = []*org.Inbox{{Label: inboxLabel, Code: cbc.Code(value)}}
		return
	}
	party.Endpoints = []*org.Endpoint{{URI: cbc.URI(iso.ActorIDScheme + "::" + scheme + ":" + value)}}
	party.Inboxes = nil
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
// party identifier, with the ISO 6523 scheme it names, into a legal
// identity; a Finnish party's Y-tunnus is the identity its VAT number is
// built from, so the two are kept as one.
func parseIdentification(taxCode string, id *Identifier, country l10n.ISOCountryCode) (*tax.Identity, []*org.Identity) {
	var tid *tax.Identity
	taxCode = strings.ToUpper(strings.TrimSpace(taxCode))
	if vatNumber.MatchString(taxCode) {
		if parsed, err := tax.ParseIdentity(taxCode); err == nil {
			tid = parsed
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
	identity := &org.Identity{Scope: org.IdentityScopeLegal, Code: cbc.Code(code)}
	if id.SchemeID != "" {
		identity.Ext = identity.Ext.Set(iso.ExtKeySchemeID, cbc.Code(id.SchemeID))
	} else if country == l10n.FI.ISO() && businessID.MatchString(code) {
		identity.Ext = identity.Ext.Set(iso.ExtKeySchemeID, cbc.Code(schemeFinnishOrg))
	}
	return tid, []*org.Identity{identity}
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
