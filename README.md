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
package, carrying the Finvoice rules and the e-invoice operator extension, and
a converter in both directions in the root package:

- `Convert(env, opts...)` writes Finvoice 3.0 from a GOBL invoice or credit note.
- `Parse(data)` reads Finvoice of any version into GOBL.

## The addon

Finvoice conforms to EN 16931, so the addon `Requires` the EN 16931 addon and
only layers the Finvoice-specific tightening on top, principally the data
needed to build the `EpiDetails` payment block, which is mandatory on every
Finvoice invoice, credit notes included:

- **Customer**: must be present and named (`BuyerOrganisationName`).
- **Names**: supplier, customer and payee names need at least two
  characters, the schema's minimum for a name.
- **Operators**: `fi-finvoice-operator` takes two to thirty-five letters
  and digits, the shape of an OVT code or a BIC.
- **Payment details**: required unconditionally, not only when an amount is
  due as in EN 16931 (BR-CO-25).
- **Credit transfer**: payment instructions must use the `credit-transfer`
  key (extensions such as `credit-transfer+sepa` are accepted) and the first
  credit transfer entry must carry an IBAN (`EpiAccountID`), validated with
  the ISO 7064 mod 97-10 checksum.
- **Payment reference**: required (`EpiReference`).
- **Due date**: payment terms must include at least one dated entry
  (`EpiDateOptionDate`).
- **BIC** (`EpiBfiIdentifier`): recommended alongside the IBAN but not
  required, matching the schema and current SEPA practice.

IBANs and payment references are normalized to their machine form: grouping
spaces are removed and IBANs are upper-cased.

The addon declares one extension, **`fi-finvoice-operator`**: the identifier
of the e-invoice operator (intermediator) a party receives e-invoices through,
quoted alongside its e-invoice address. That is an operator's OVT code such
as `003700010001`, or a bank's BIC such as `NDEAFIHH`. A Finnish address may
be registered with several operators, and the operator decides where an
invoice is delivered, so set it on the customer to have the message routed
there.

The addon registers validation rules, normalizers and the extension into
GOBL's global registry, and lives in its own module so that only projects
handling Finnish Finvoice documents take on its weight.

## Writing Finvoice

`Convert` adds the addon to an invoice that does not declare it, validates the
invoice, rounds it to the currency, and writes a Finvoice 3.0 document.
Addresses come from the parties' ISO 6523 endpoints
(`iso6523-actorid-upis::0216:003745678907` for an OVT code), the Y-tunnus and
VAT number from the tax identity, and the payment order from the payment
instructions the addon requires.

- **Routing frame.** `MessageTransmissionDetails` is written when the
  customer carries `fi-finvoice-operator`. The supplier's endpoint becomes
  `FromIdentifier` and the customer's `ToIdentifier`. The customer's
  operator becomes `ToIntermediator`. `FromIntermediator` is the operator the
  message is sent through, given with `WithSenderOperator`; without it the
  conversion fails. A customer with no operator gets no frame, and routing is
  left to the receiving operator's own address tables. Either way the
  e-invoice addresses are also written as `SellerOrganisationUnitNumber` and
  `BuyerOrganisationUnitNumber`, where operators put them on delivery, and a
  customer without an endpoint is refused.
- **Message identity.** `MessageIdentifier` is the invoice UUID and
  `MessageTimeStamp` the time of conversion; `WithMessageID` and
  `WithMessageTime` override them, and a resend of the same invoice should
  carry a fresh identifier.
- **Links.** `WithInvoiceURL(name, url)` adds an `InvoiceUrlNameText` and
  `InvoiceUrlText` pair, such as the reference to the invoice's PDF image an
  operator's intake expects alongside the XML.
- **Credit notes** are written as `INV02` with negative amounts, the sign
  convention of the format; the parser flips them back.
- **Invoice type.** `InvoiceTypeCode` is `INV01`, `INV02`, `INV06` or
  `INV07` from the GOBL type and tags, and `InvoiceTypeCodeUN` the UNTDID
  1001 code the EN 16931 addon records.
- **Identifiers the format cannot hold are refused, not cut.** An invoice
  number, payment reference, ordering reference, legal identity, article
  identifier, quantity, sender operator or message identifier longer than
  its element, or payment terms with more than one due date, return an
  error. Long names and texts are spread over the repeats the schema allows
  and cut beyond them. An email or web address too long for its element, an
  alias or region shorter than the two characters the schema requires, and a
  postal address without a town and post code are left out.
- **Row VAT amounts are not written.** GOBL works the tax out per rate, so
  per-row VAT amounts would not add up to the breakdown; the optional
  `RowVatAmount` and `RowAmount` are left to it.

## Reading Finvoice

`Parse` reads any Finvoice version, since the schema is backwards compatible
and operators deliver whatever the sender produced, often 1.3. The
forwarding service's SOAP frame in front of the document is read for its
routing when the document has no `MessageTransmissionDetails`, and the
charset the XML declaration names (UTF-8, ISO-8859-15, ISO-8859-1,
Windows-1252) is honoured. The result declares `fi-finvoice-v3`:

- E-invoice addresses come from the frame when there is one, else from the
  organisation unit numbers; an address given without a scheme is read as an
  OVT code, an IBAN or a Y-tunnus by its shape, and any other shape is kept as
  an inbox code. Operators land on `fi-finvoice-operator` of each party.
- Only invoices are read: `INV01`, `INV02`, `INV06` and `INV07`. Quotations,
  orders, reminders and the other messages of the code list, and copies of an
  invoice, are refused with `ErrUnsupportedDocumentType`.
- The row total is the authority for a line, as the guidelines add the totals
  up from it: the unit price is read per its base quantity, and when the
  quantity times that price still misses the row total, the price the row
  total implies is used.
- VAT categories come from `RowVatCode`, or from the VAT breakdown line with
  the same rate; a breakdown with two categories at one rate cannot be told
  apart and is refused. Exemption reason codes become `cef-vatex` codes, and
  reason texts, on the breakdown or on the rows, become tax notes.
- The currency comes from the total's currency identifier and must be one
  GOBL knows; any other code is refused.
- The stated total is kept: the roundoff amount is read, and a remaining
  difference with the calculated total of up to a subunit per row is recorded
  as rounding. A larger difference means the rows were misread and is refused.
- Units are read from the UN/ECE code when GOBL knows it, else from the
  free-text unit when it is a GOBL unit key or a Finnish word for one
  (`kpl`, `h`, `pv`, `kk`); anything else becomes the generic unit.
- Rows with nothing to price, such as headings and subtotal texts, become
  notes on the invoice.

A document converted to Finvoice and parsed back yields the same GOBL
content, with a few things the format cannot carry: series and code travel
as one invoice number, a street number as part of the street, an advance
without its description, notes without their key, VAT rates as their
percentage, and charges and discounts as their UNTDID codes rather than GOBL
keys. The supplier gains the sender operator it was sent through, and the
parsed document uses currency rounding.

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
import fifinvoice "github.com/invopop/gobl.fi.finvoice"

doc, err := fifinvoice.Convert(env,
	fifinvoice.WithSenderOperator("003700010001"),
	fifinvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
)
data, err := doc.Bytes()

env, err := fifinvoice.Parse(data)
```

The documents under `examples/` are converted and validated against the
Finvoice 3.0 schema in `schemas/` by the test suite, and the Finvoice files
under `test/data/parse/` are read back into GOBL. Run `go test . -update` to
regenerate the golden files after a change.

## Sources

- [Finvoice 3.0 XSD](https://file.finanssiala.fi/finvoice/css/270923/Finvoice3.0.xsd)
- [Finvoice 3.0 implementation guidelines](https://file.finanssiala.fi/finvoice/Finvoice_3_0_implementation_guidelines.pdf)
- [Finvoice standard](https://www.finanssiala.fi/en/topics/finvoice-standard/)
