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

- `Convert(env, opts...)` — GOBL invoice or credit note → Finvoice 3.0
- `Parse(data)` — Finvoice (any version) → GOBL

## The addon

Finvoice conforms to EN 16931, so the addon `Requires` the EN 16931 addon and
only layers the Finvoice-specific tightening on top — principally the data
needed to build the `EpiDetails` payment block, which is mandatory on every
Finvoice invoice, credit notes included:

- **Customer** — must be present and named (`BuyerOrganisationName`).
- **Payment details** — required unconditionally, not only when an amount is
  due as in EN 16931 (BR-CO-25).
- **Credit transfer** — payment instructions must use the `credit-transfer`
  key (extensions such as `credit-transfer+sepa` are accepted) and the first
  credit transfer entry must carry an IBAN (`EpiAccountID`), validated with
  the ISO 7064 mod 97-10 checksum.
- **Payment reference** — required (`EpiReference`).
- **Due date** — payment terms must include at least one dated entry
  (`EpiDateOptionDate`).
- **BIC** (`EpiBfiIdentifier`) — recommended alongside the IBAN but not
  required, matching the schema and current SEPA practice.

IBANs and payment references are normalized to their machine form: grouping
spaces are removed and IBANs are upper-cased.

The addon declares one extension, **`fi-finvoice-operator`**: the identifier
of the e-invoice operator (intermediator) a party receives e-invoices through,
quoted alongside its e-invoice address — an operator's OVT code such as
`003723327487`, or a bank's BIC such as `NDEAFIHH`. A Finnish address may be
registered with several operators, and the operator decides where an invoice
is delivered, so set it on the customer to have the message routed there.

The addon registers validation rules, normalizers and the extension into
GOBL's global registry, and lives in its own module so that only projects
handling Finnish Finvoice documents take on its weight.

## The converter

`Convert` writes a Finvoice 3.0 document from an invoice that declares
`fi-finvoice-v3`. Addresses come from the parties' ISO 6523 endpoints
(`iso6523-actorid-upis::0216:003701120389` for an OVT code), the Y-tunnus
and VAT number from the tax identity, and the payment order from the payment
instructions the addon requires.

- **Routing frame.** `MessageTransmissionDetails` is written when the
  customer carries `fi-finvoice-operator`: the parties' endpoints become
  `FromIdentifier` and `ToIdentifier`, the customer's operator
  `ToIntermediator`, and the operator the message is sent through, given
  with `WithSenderOperator`, `FromIntermediator` — without it the conversion
  fails. A customer with no operator gets no frame, and routing is left to
  the receiving operator's own address tables. Either way the e-invoice
  addresses are also written as `SellerOrganisationUnitNumber` and
  `BuyerOrganisationUnitNumber`, where operators put them on delivery.
- **Message identity.** `MessageIdentifier` is the invoice UUID and
  `MessageTimeStamp` the time of conversion; `WithMessageID` and
  `WithMessageTime` override them, and a resend of the same invoice should
  carry a fresh identifier.
- **Links.** `WithInvoiceURL(name, url)` adds an `InvoiceUrlNameText` /
  `InvoiceUrlText` pair, such as the reference to the invoice's PDF image an
  operator's intake expects alongside the XML.
- **Credit notes** are written as `INV02` with negative amounts, the sign
  convention of the format; the parser flips them back.
- **Invoice type.** `InvoiceTypeCode` is `INV01`, `INV02`, `INV06` or
  `INV07` from the GOBL type and tags, and `InvoiceTypeCodeUN` the UNTDID
  1001 code the EN 16931 addon records.

`Parse` reads any Finvoice version, since the schema is backwards compatible
and operators deliver whatever the sender produced — often 1.3. A transport
frame (the SOAP envelope of the forwarding service) in front of the document
is skipped, and the charset the XML declaration names (UTF-8, ISO-8859-15,
ISO-8859-1, Windows-1252) is honoured. The result declares `fi-finvoice-v3`
and validates under it:

- E-invoice addresses come from the frame when there is one, else from the
  organisation unit numbers; an address given without a scheme is read as an
  OVT code, an IBAN or a Y-tunnus by its shape. Operators in the frame land
  on `fi-finvoice-operator` of each party.
- VAT categories come from `RowVatCode`, or from the VAT breakdown line with
  the same rate; exemption reason codes and texts become `cef-vatex` codes and
  tax notes.
- The stated total is kept: a difference with the calculated total is
  recorded as rounding.
- Units are read from the UN/ECE code when GOBL knows it, else from the
  free-text unit when it is a GOBL unit key; anything else becomes the
  generic unit.

A document converted to Finvoice and parsed back yields the same GOBL
content, with a few things the format cannot carry: series and code travel
as one invoice number, a street number as part of the street, an advance
without its description, notes without their key, VAT rates as their
percentage, and charges and discounts as their UNTDID codes rather than GOBL
keys. The supplier gains the sender operator it was sent through.

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
    code: "23456780"
  endpoints:
    - uri: "iso6523-actorid-upis::0216:003723456780"
customer:
  name: "Ostaja Oy"
  tax_id:
    country: "FI"
    code: "01120389"
  endpoints:
    - uri: "iso6523-actorid-upis::0216:003701120389"
  ext:
    fi-finvoice-operator: "003723327487"
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
	fifinvoice.WithSenderOperator("003723327487"),
	fifinvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
)
data, err := doc.Bytes()

env, err := fifinvoice.Parse(data)
```

The documents under `examples/` are converted and validated against the
Finvoice 3.0 schema in `schemas/` by the test suite; run `go test ./...
-update` to regenerate the golden files after a change.

## Sources

- [Finvoice 3.0 XSD](https://file.finanssiala.fi/finvoice/css/270923/Finvoice3.0.xsd)
- [Finvoice implementation guidelines](https://www.finanssiala.fi/en/topics/finvoice-implementation-guidelines/)
