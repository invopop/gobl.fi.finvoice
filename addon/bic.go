package addon

import (
	"strconv"
	"strings"

	"github.com/invopop/gobl/cbc"
)

// finnishBICs maps a monetary institution code, the leading digits of a
// Finnish IBAN's account number, to the institution's BIC, as Finance
// Finland lists them ("Finnish Monetary Institution Codes and BICs",
// 16 March 2026). A code's length follows its first digits: one digit,
// except two for codes starting with 3, three for codes starting with 4,
// 71 or 79, and four for codes starting with 72 to 78.
var finnishBICs = map[string]cbc.Code{
	"1": "NDEAFIHH", // Nordea
	"2": "NDEAFIHH", // Nordea
	"5": "OKOYFIHH", // OP Group
	"6": "AABAFI22", // Bank of Åland
	"8": "DABAFIHH", // Danske Bank

	"33": "ESSEFIHX", // SEB
	"34": "DABAFIHH", // Danske Bank
	"36": "SBANFIHH", // S-Bank
	"37": "DNBAFIHX", // DNB Bank
	"39": "SBANFIHH", // S-Bank

	"405": "HELSFIHH", // Aktia Bank
	"497": "HELSFIHH", // Aktia Bank
	"713": "CITIFIHX", // Citibank
	"714": "EVSEFIHH", // Alisa Bank
	"715": "ITELFIHH", // Central Bank of Savings Banks Finland
	"717": "BIGKFIH1", // Bigbank
	"718": "UASNFIH2", // Saldo Bank
	"719": "NOLFFIH2", // Northmill Bank
	"792": "IVPYFIH2", // Ivy Pay
	"793": "TRYEFIH3", // TrueLayer
	"794": "PASXFIH2", // PaySaxas
	"795": "WAMOFIH2", // Wamo Solutions
	"796": "NARYFIH2", // Narvi Payments
	"797": "TRYEFIH2", // TrueLayer
	"799": "HOLVFIHH", // Holvi Payment Services
}

func init() {
	fill := func(from, to int, bic cbc.Code) {
		for code := from; code <= to; code++ {
			finnishBICs[strconv.Itoa(code)] = bic
		}
	}
	// Local co-operative banks (POP) and Bonum Bank.
	fill(470, 479, "POPFFI22")
	// Savings banks (Sp) and Oma Säästöpankki, with the gaps the list keeps.
	for _, r := range [][2]int{{400, 400}, {402, 403}, {406, 408}, {410, 412}, {414, 421}, {423, 432}, {435, 452}, {454, 464}, {483, 493}, {495, 496}} {
		fill(r[0], r[1], "ITELFIHH")
	}
}

// FinnishBIC returns the BIC of the bank holding a Finnish IBAN's account,
// read off the IBAN's monetary institution code, or an empty code when the
// IBAN isn't Finnish or its bank isn't on Finance Finland's list.
func FinnishBIC(iban cbc.Code) cbc.Code {
	s := strings.ToUpper(strings.TrimSpace(iban.String()))
	if len(s) < 8 || !strings.HasPrefix(s, "FI") {
		return cbc.CodeEmpty
	}
	bban := s[4:]
	n := 1
	switch {
	case bban[0] == '3':
		n = 2
	case bban[0] == '4', strings.HasPrefix(bban, "71"), strings.HasPrefix(bban, "79"):
		n = 3
	case bban[0] == '7':
		n = 4
	}
	if len(bban) < n {
		return cbc.CodeEmpty
	}
	return finnishBICs[bban[:n]]
}
