package addon_test

import (
	"testing"

	"github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/cbc"
	"github.com/stretchr/testify/assert"
)

func TestFinnishBIC(t *testing.T) {
	tests := []struct {
		name string
		iban cbc.Code
		want cbc.Code
	}{
		{"Nordea, one-digit code", "FI2112345600000785", "NDEAFIHH"},
		{"OP, one-digit code", "FI4250001510000023", "OKOYFIHH"},
		{"Danske, one-digit code 8", "FI0080001234567890", "DABAFIHH"},
		{"Bank of Åland, one-digit code 6", "FI0060001234567890", "AABAFI22"},
		{"SEB, two-digit code", "FI0033001234567890", "ESSEFIHX"},
		{"Danske, two-digit code 34", "FI0034001234567890", "DABAFIHH"},
		{"S-Bank, two-digit code 39", "FI0039001234567890", "SBANFIHH"},
		{"Aktia, three-digit code", "FI0040512345678901", "HELSFIHH"},
		{"POP, three-digit code in a range", "FI0047312345678901", "POPFFI22"},
		{"savings bank, three-digit code in a range", "FI0044012345678901", "ITELFIHH"},
		{"savings bank central, 715", "FI0071512345678901", "ITELFIHH"},
		{"Citibank, 713", "FI0071312345678901", "CITIFIHX"},
		{"Holvi, 799", "FI0079912345678901", "HOLVFIHH"},
		{"TrueLayer, 793 and 797 differ", "FI0079312345678901", "TRYEFIH3"},
		{"TrueLayer, 797", "FI0079712345678901", "TRYEFIH2"},
		{"Handelsbanken left Finland: 31 is unlisted", "FI0031001234567890", ""},
		{"a code in the four-digit 72-78 range is unlisted", "FI0072001234567890", ""},
		{"savings bank gap: 401 is unlisted", "FI0040112345678901", ""},
		{"a foreign IBAN", "DE89370400440532013000", ""},
		{"too short to hold a bank code", "FI21", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, addon.FinnishBIC(tt.iban))
		})
	}
}
