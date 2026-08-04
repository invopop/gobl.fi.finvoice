# GOBL ➡️ Finnish Finvoice 3.0

Finnish Finvoice 3.0 addon for [GOBL](https://github.com/invopop/gobl), used
for invoices delivered over the Finnish banking network.

Released under the Apache 2.0 [LICENSE](https://github.com/invopop/gobl.fi.finvoice/blob/main/LICENSE), Copyright 2026 [Invopop S.L.](https://invopop.com).

[![Lint](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/lint.yaml/badge.svg)](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/lint.yaml)
[![Test Go](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/test.yaml/badge.svg)](https://github.com/invopop/gobl.fi.finvoice/actions/workflows/test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/invopop/gobl.fi.finvoice)](https://goreportcard.com/report/github.com/invopop/gobl.fi.finvoice)
[![codecov](https://codecov.io/gh/invopop/gobl.fi.finvoice/graph/badge.svg)](https://codecov.io/gh/invopop/gobl.fi.finvoice)
[![GoDoc](https://godoc.org/github.com/invopop/gobl.fi.finvoice?status.svg)](https://godoc.org/github.com/invopop/gobl.fi.finvoice)
![Latest Tag](https://img.shields.io/github/v/tag/invopop/gobl.fi.finvoice)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/invopop/gobl.fi.finvoice)

This module implements the Finvoice 3.0 requirements as a GOBL tax addon
(`fi-finvoice-v3`). Finvoice conforms to EN 16931, so the addon `Requires` the
EN 16931 addon and only layers the Finvoice-specific tightening on top —
principally the data needed to build the `EpiDetails` payment block, which is
mandatory on every Finvoice invoice, credit notes included:

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

Unlike the format converters in the GOBL ecosystem, this is a true **addon**:
it registers validation rules and normalizers into GOBL's global registry. It
lives in its own module so that only projects handling Finnish Finvoice
documents take on its weight.

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
payment:
  instructions:
    key: "credit-transfer+sepa"
    ref: "RF18539007547034"
    credit_transfer:
      - iban: "FI21 1234 5600 0007 85" # normalized to FI2112345600000785
  terms:
    due_dates:
      - date: "2026-08-31"
        percent: "100%"
```

## Sources

- [Finvoice 3.0 XSD](https://file.finanssiala.fi/finvoice/css/270923/Finvoice3.0.xsd)
- [Finvoice implementation guidelines](https://www.finanssiala.fi/en/topics/finvoice-implementation-guidelines/)
