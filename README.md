# GOBL ⬅️➡️ Finnish Finvoice 3.0

Finnish Finvoice 3.0 support for [GOBL](https://github.com/invopop/gobl):
the `fi-finvoice-v3` addon plus converters to and from the XML format used on
the Finnish e-invoicing network.

Released under the Apache 2.0 [LICENSE](https://github.com/invopop/gobl.fi.finvoice/blob/main/LICENSE), Copyright 2026 [Invopop S.L.](https://invopop.com).

[![Lint](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/lint.yaml/badge.svg)](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/lint.yaml)
[![Test Go](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/test.yaml/badge.svg)](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/invopop/gobl.fi.finvoice)](https://goreportcard.com/report/github.com/invopop/gobl.fi.finvoice)
[![codecov](https://codecov.io/gh/invopop/gobl.fi.finvoice/graph/badge.svg)](https://codecov.io/gh/invopop/gobl.fi.finvoice)
[![GoDoc](https://godoc.org/github.com/invopop/gobl.fi.finvoice?status.svg)](https://godoc.org/github.com/invopop/gobl.fi.finvoice)
![Latest Tag](https://img.shields.io/github/v/tag/invopop/gobl.fi.finvoice)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/invopop/gobl.fi.finvoice)

This module is two things: a GOBL tax addon (`fi-finvoice-v3`) in the `addon`
package, with the Finvoice rules and the e-invoice operator extension, and a
converter in both directions in the root package:

- `Convert(env, opts...)` writes Finvoice 3.0 from a GOBL invoice or credit
  note, as `any` like the sibling converters; `ConvertInvoice` returns the
  `*Invoice` itself.
- `Parse(data)` reads Finvoice of any version into GOBL.

## Usage

Import the addon for its side effects to register it, then declare the
`fi-finvoice-v3` addon on a GOBL document:

```go
import _ "github.com/invopop/gobl.fi.finvoice/addon"
```

```yaml
$schema: "https://gobl.org/draft-0/bill/invoice"
$regime: "FI"
$addons:
  - "fi-finvoice-v3"
supplier:
  name: "Myyjä Oy"
  tax_id:
    country: "FI"
    code: "76543212"
  endpoints:
    - uri: "iso6523-actorid-upis::0216:003776543212"
customer:
  name: "Ostaja Oy"
  tax_id:
    country: "FI"
    code: "45678907"
  endpoints:
    - uri: "iso6523-actorid-upis::0216:003745678907"
  ext:
    fi-finvoice-operator: "003700020002"
payment:
  instructions:
    key: "credit-transfer+sepa"
    ref: "RF18539007547034"
    credit_transfer:
      - iban: "FI21 1234 5600 0007 85" # normalized to FI2112345600000785
        bic: "NDEAFIHH"
  terms:
    due_dates:
      - date: "2026-08-31"
        percent: "100%"
```

Convert and parse:

```go
import finvoice "github.com/invopop/gobl.fi.finvoice"

doc, err := finvoice.Convert(env,
	finvoice.WithSenderOperator("003700010001"),
	finvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
)
data, err := finvoice.Bytes(doc)

parsed, err := finvoice.Parse(data)
```

The documents under `examples/` are converted and validated against the
Finvoice 3.0 schema in `schemas/` by the test suite, and the Finvoice files
under `test/data/parse/` are read back into GOBL. Run `go test . -update` to
regenerate the golden files after a change.

## The addon

Finvoice conforms to EN 16931, so the addon `Requires` the EN 16931 addon and
only layers the Finvoice-specific tightening on top, principally the data
needed to build the `EpiDetails` payment block, which is mandatory on every
Finvoice invoice, credit notes included:

- **Customer**: must be present and named (`BuyerOrganisationName`).
- **E-invoice addresses**: supplier and customer each need one, an ISO 6523
  endpoint (`iso6523-actorid-upis::0216:003745678907` for an OVT code) or,
  failing that, a coded inbox. The receiver's routes the document and the
  sender's is where the operator delivers replies; a document missing either
  is accepted by the operator and then never delivered.
- **Operators**: `fi-finvoice-operator` takes two to thirty-five letters
  and digits, the shape of an OVT code or a BIC.
- **Identifiers**: values Finvoice cannot hold whole are refused, since a
  cut identifier points at nothing. The invoice number with its series and
  each preceding document number take up to 20 characters, ordering
  references and item references up to 70, a legal identity code and the
  payment reference up to 35, an e-invoice address code 2 to 35, and the
  address's scheme up to 10. Each kind of ordering reference takes one
  document, as each Finvoice element holds one identifier.
- **Currency**: at most two decimals, since the payment order's
  `EpiInstructedAmount` takes exactly two and would otherwise be rounded.
- **Payment details**: required unconditionally, not only when an amount is
  due as in EN 16931 (BR-CO-25).
- **Credit transfer**: payment instructions must use the `credit-transfer`
  key (extensions such as `credit-transfer+sepa` are accepted) and the first
  credit transfer entry must carry an IBAN (`EpiAccountID`), validated with
  the ISO 7064 mod 97-10 checksum.
- **Payment reference**: required (`EpiReference`).
- **Due date**: payment terms must include exactly one dated entry, for
  the whole amount (`EpiDateOptionDate`); the payment order takes one date,
  and this module does not write instalments.
- **Exemption reasons**: one code and one note per VAT category, across
  lines, discounts and charges, since the breakdown gives one
  `VatExemptionReasonCode` and one `VatFreeText` per category.
- **Attachments**: refused. Finvoice carries attachments as separate
  attachment messages through the forwarding service (guidelines §16),
  which this module does not produce; a sender's own link in the invoice
  may only point at generic material (§15.1).
- **Delivery period**: both dates are required, as `DeliveryPeriodDetails`
  takes both.
- **BIC** (`EpiBfiIdentifier`, `SellerBic`): recommended on the payment
  order's account but not required, matching the schema and current SEPA
  practice; required on any further account, which Finvoice lists with its
  BIC. A further account may be a BBAN.

IBANs and payment references are normalized to their machine form: grouping
spaces are removed and IBANs are upper-cased.

An invoice gives each party one electronic address, and on a Finnish invoice
that is the OVT (`iso6523-actorid-upis::0216:<OVT>`). When a supplier, customer
or other party carries an OVT endpoint, the addon keeps the first one and drops
the party's other `iso6523-actorid-upis` endpoints, so a party record that also
holds a Business ID or GLN address still builds (EN 16931 `ORG-PARTY-04`).
Endpoints with other schemes and inboxes are left alone, and parties without an
OVT are not changed.

The addon declares one extension, **`fi-finvoice-operator`**: the identifier
of the e-invoice operator (intermediator) that serves a party's e-invoice
address, quoted alongside that address. That is an operator's OVT code such
as `003700010001`, or a bank's BIC such as `NDEAFIHH`. A Finnish address may
be registered with several operators, and the operator decides where an
invoice is delivered, so set it on the party that receives the message: the
customer, or the supplier of a self-billed invoice.

The addon registers validation rules, normalizers and the extension into
GOBL's global registry, and lives in its own module so that only projects
handling Finnish Finvoice documents take on its weight.

## Writing Finvoice

`Convert` adds the addon to an invoice that does not declare it, validates the
invoice, rounds it to the currency, and writes a Finvoice 3.0 document.
E-invoice addresses come from the parties' ISO 6523 endpoints, or their coded
inboxes, as the addon requires; the Y-tunnus and VAT number from the tax
identity, and the payment order from the payment instructions the addon
requires.

- **Transmission details.** The supplier sends an invoice to the customer,
  and the customer sends a self-billed invoice to the supplier, as GOBL's
  own `FromEndpoint` and `ToEndpoint` read it. `MessageTransmissionDetails`
  is written when the receiving party has `fi-finvoice-operator`. The
  sender's endpoint becomes `FromIdentifier` and the receiver's
  `ToIdentifier`, and the receiver's operator `ToIntermediator`.
  `FromIntermediator` is the operator the message is sent through, given
  with `WithSenderOperator`; without it the conversion fails. Option values
  the format cannot hold, an operator that is not 2 to 35 letters and
  digits, a message identifier outside 2 to 48 characters or a URL over
  512, are refused before writing. A receiver with no operator gets no
  transmission details, and routing is left to the receiving operator's own
  address tables. Either way the e-invoice addresses are also written as
  `SellerOrganisationUnitNumber` and `BuyerOrganisationUnitNumber`, where
  operators put them on delivery.
- **Message identity.** `MessageIdentifier` is the invoice UUID and
  `MessageTimeStamp` the time of conversion; `WithMessageID` and
  `WithMessageTime` override them, and a resend of the same invoice should
  have a fresh identifier.
- **Payment reference.** `EpiRemittanceInfoIdentifier` is written with
  scheme `SPY` for a Finnish reference number and `ISO` for an RF creditor
  reference, only when its check digits hold; any other reference is in
  `EpiReference` alone.
- **Links.** `WithInvoiceURL(name, url)` adds an `InvoiceUrlNameText` and
  `InvoiceUrlText` pair, such as the reference to the invoice's PDF image an
  operator's intake expects alongside the XML.
- **Credit notes** are written as `INV02` with negative amounts, the sign
  convention of the format; the parser flips them back whenever the total
  is zero or negative, as the guidelines require of a credit note.
- **Invoice type.** `InvoiceTypeCode` is `INV01`, `INV02`, `INV06` or
  `INV07` from the GOBL type and tags, and `InvoiceTypeCodeUN` the UNTDID
  1001 code the EN 16931 addon records.
- **Texts are fitted, identifiers are not.** The addon refuses identifiers
  too long for their element before conversion. Names and notes are spread
  over as many elements as they need; a note longer than one element comes
  back as several. Streets, VAT notes and payment terms take the three,
  three and two lines the schema allows, and are cut beyond them. An email
  or web address too long for its element, and a postal address without a
  town and post code, are left out. So is a discount or charge percentage
  finer than the three decimals Finvoice takes, with its base; the amount
  beside it is written as usual, so the totals stay exact.
- **Prices.** A unit price finer than the five decimals Finvoice takes is
  written per a base quantity (`UnitPriceBaseQuantity`) of ten, a hundred
  or more, so it stays exact. A line with several discounts writes them all
  as `RowProgressiveDiscountDetails`, as the Finvoice correlation table does.
- **Row VAT amounts are not written.** GOBL works the tax out per rate, so
  per-row VAT amounts would not add up to the breakdown; the optional
  `RowVatAmount` and `RowAmount` are left to it.

## Reading Finvoice

`Parse` reads any Finvoice version, since the schema is backwards compatible
and operators deliver whatever the sender produced, often 1.3. The
forwarding service's SOAP frame in front of the document is read for its
routing when the document has no `MessageTransmissionDetails`, and the
charset the XML declaration names (UTF-8, ISO-8859-15, ISO-8859-1,
Windows-1252) is honoured. A file holding more than one Finvoice message is
refused.

`Parse` maps the document faithfully. It refuses only what it cannot map at
all: an unknown document type, currency or VAT code, a malformed value, or
an amount it reads in another currency. Where Finvoice leaves the truth to the
sender, it takes the sender's figure, the row total and the roundoff, and
records any difference as rounding. Everything else, including whether the
document agrees with itself, is left to GOBL's validation of the result. The
result declares `fi-finvoice-v3`:

- E-invoice addresses come from the transmission details or the SOAP frame
  when there are any, else from the organisation unit numbers. An address
  given without a scheme is read as an OVT code, an IBAN or a Y-tunnus by
  its shape, each only when its check digits hold, and any other
  shape is kept as an inbox code. Operators are set on
  `fi-finvoice-operator` of each party.
- VAT numbers are kept, spaced or hyphenated ones too, and validation
  reports a wrong one.
- Only invoices are read: `INV01`, `INV02`, `INV06` and `INV07`. Quotations,
  orders, reminders and the other messages of the code list, and copies and
  cancellations of an invoice, are refused with `ErrUnsupportedDocumentType`.
- The GOBL type comes from `InvoiceTypeCodeUN` when given, else from
  `InvoiceTypeCode`. Amounts are turned positive only for a credit note
  with a negative total, so `INV02` with `380` reads as an invoice with a
  negative total, as an EN 16931 invoice may have.
- A row made of sub-rows (`SubInvoiceRow`) is a subtotal for display, which
  the guidelines keep out of the totals, so it is left out. So is
  `SpecificationDetails`, the sector-specific itemisation shown with the
  invoice (§12). Consumer e-invoices and direct payment (§4, §5) are bank
  flows outside this module's scope; their documents still parse as
  invoices.
- Links (`InvoiceUrlText` and its name) are left out. In a delivery they
  name the PDF image that arrives beside the XML; otherwise they point at a
  sender's generic material or a bank's display service (§15). None of them
  is invoice data.
- A row that states its quantity in several units is read at the one in the
  unit price's unit, or the first when the price names none.
- The row total is the authority for a line, as the guidelines add the totals
  up from it: the unit price is read per its base quantity, and when the
  quantity times that price still misses the row total, the price the row
  total implies is used, with the fewest decimals that give it. A discount
  or charge percentage is kept only when it gives the amount stated beside
  it; otherwise the amount is kept alone.
- VAT categories come from `RowVatCode`, or from the VAT breakdown line with
  the same rate; a breakdown with two categories at one rate, or two
  exemption reason codes in one category, cannot be told apart and is
  refused, as is a code the breakdown gives that GOBL does not know. A zero
  rate that nothing names is read as zero-rated, and a positive one as
  standard. Exemption reason codes become `cef-vatex` codes and reason texts
  become tax notes, a text the breakdown repeats becoming one note. A row's
  free text stands in as the reason only when the breakdown gives neither a
  code nor a text, since row texts are free comments.
- The currency comes from the total's currency identifier and must be one
  GOBL knows. A GOBL invoice has one currency, so an amount labelled with
  another is refused.
- The stated total is kept: the roundoff amount is read, and a remaining
  difference with the calculated total of up to a subunit per row, plus one
  for the totals, is recorded as rounding. A larger difference means the rows
  were misread and is refused. GOBL works the VAT out per rate, so its VAT
  total can differ from the document's by that rounding.
- The due date is the payment order's. Payment terms that repeat with
  different dates are instalments, which the payment order has no room for,
  so they are refused.
- The payment means code sets the payment key; codes GOBL has no key for are
  read as the credit transfer a Finvoice payment order describes.
- Units are read from the UN/ECE code when GOBL knows it, else from the
  free-text unit when it is a GOBL unit key or a Finnish word for one
  (`kpl`, `h`, `pv`, `kk`); anything else becomes the generic unit, as does
  `t`, which is an hour or a tonne.
- Rows with nothing to price, such as headings, subtotal texts and rows with
  only a quantity, become notes on the invoice.

A document converted to Finvoice and parsed back yields the same GOBL
content, except for what the format cannot hold the way GOBL does. Series
and code become one invoice number, a street number part of the street, a
street continued on a second line a street extra, and a missing street the
town or PO box that stood in for it. An advance loses its description,
notes their key, VAT rates become their percentage, and charges and
discounts their UNTDID codes, not GOBL keys. Without transmission details,
addresses lose their scheme and are read back by shape. The sending party
gains the operator it was sent through, and the parsed document uses
currency rounding.

## Sources

- [Finvoice 3.0 XSD](https://file.finanssiala.fi/finvoice/css/270923/Finvoice3.0.xsd)
- [Finvoice 3.0 implementation guidelines](https://file.finanssiala.fi/finvoice/Finvoice_3_0_implementation_guidelines.pdf)
- [Finvoice standard](https://www.finanssiala.fi/en/topics/finvoice-standard/)
