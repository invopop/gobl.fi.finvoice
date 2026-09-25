package fifinvoice_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/invopop/gobl"
	fifinvoice "github.com/invopop/gobl.fi.finvoice"
	finvoice "github.com/invopop/gobl.fi.finvoice/addon"
	"github.com/invopop/gobl/addons/eu/en16931"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/cal"
	"github.com/invopop/gobl/cbc"
	"github.com/invopop/gobl/num"
	"github.com/invopop/gobl/org"
	"github.com/invopop/gobl/pay"
	"github.com/invopop/gobl/pkg/examples"
	"github.com/invopop/gobl/tax"
	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/xsd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Made-up operator codes: the sender's, and the one the example customer
// names.
const (
	senderOperator   = "003700010001"
	receiverOperator = "003700020002"
)

// testOptions keep the message frame deterministic for golden comparison.
func testOptions() []fifinvoice.Option {
	return []fifinvoice.Option{
		fifinvoice.WithSenderOperator(senderOperator),
		fifinvoice.WithMessageTime(time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)),
		fifinvoice.WithInvoiceURL("PDF", "file://invoice.pdf"),
	}
}

// convertCase is an example document and the Finvoice XML it should produce.
type convertCase struct {
	name, src, golden string
}

// convertCases lists the example sources; each converts from a freshly
// calculated envelope so the goldens never lag the examples.
func convertCases(t *testing.T) []convertCase {
	t.Helper()
	sources, err := examples.Sources("examples")
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	var cases []convertCase
	for _, src := range sources {
		name := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		cases = append(cases, convertCase{
			name:   name,
			src:    src,
			golden: filepath.Join("examples", "out", name+".xml"),
		})
	}
	return cases
}

// exampleEnvelope calculates an example document into an envelope.
func exampleEnvelope(t *testing.T, name string) (*gobl.Envelope, *bill.Invoice) {
	t.Helper()
	src := filepath.Join("examples", name+".yaml")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	out, err := examples.Convert(data, examples.IsEnvelope(src))
	require.NoError(t, err)
	env := new(gobl.Envelope)
	require.NoError(t, json.Unmarshal(out, env))
	return env, env.Extract().(*bill.Invoice)
}

func loadSchema(t *testing.T) *xsd.Schema {
	t.Helper()
	schema, err := xsd.ParseFromFile(filepath.Join("schemas", "Finvoice3.0.xsd"))
	require.NoError(t, err)
	return schema
}

func assertValidXML(t *testing.T, schema *xsd.Schema, data []byte) {
	t.Helper()
	doc, err := libxml2.Parse(data)
	require.NoError(t, err)
	defer doc.Free()
	if err := schema.Validate(doc); err != nil {
		for _, e := range err.(xsd.SchemaValidationError).Errors() {
			t.Error(e)
		}
	}
}

// convertAdjusted recalculates an adjusted example, converts it and checks
// the XML against the schema.
func convertAdjusted(t *testing.T, env *gobl.Envelope, opts ...fifinvoice.Option) *fifinvoice.Document {
	t.Helper()
	require.NoError(t, env.Calculate())
	doc, err := fifinvoice.Convert(env, append(testOptions(), opts...)...)
	require.NoError(t, err)
	data, err := doc.Bytes()
	require.NoError(t, err)
	schema := loadSchema(t)
	defer schema.Free()
	assertValidXML(t, schema, data)
	return doc
}

// TestConvert checks every example against its golden XML and the Finvoice
// 3.0 schema. Run with -update to regenerate the goldens.
func TestConvert(t *testing.T) {
	schema := loadSchema(t)
	defer schema.Free()

	for _, tc := range convertCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := exampleEnvelope(t, tc.name)
			doc, err := fifinvoice.Convert(env, testOptions()...)
			require.NoError(t, err)
			data, err := doc.Bytes()
			require.NoError(t, err)

			assertValidXML(t, schema, data)

			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(tc.golden), 0o755))
				require.NoError(t, os.WriteFile(tc.golden, data, 0o644))
				return
			}
			expected, err := os.ReadFile(tc.golden)
			require.NoError(t, err, "golden %s missing, run with -update", tc.golden)
			assert.Equal(t, string(expected), string(data), "run with -update to refresh %s", tc.golden)
		})
	}
}

// TestConvertFrame covers the routing frame: written only for a customer
// that names an operator, and then only with the sender's operator to hand.
func TestConvertFrame(t *testing.T) {
	t.Run("both operators route the message", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc, err := fifinvoice.Convert(env, testOptions()...)
		require.NoError(t, err)
		require.NotNil(t, doc.Transmission)
		assert.Equal(t, "003776543212", doc.Transmission.Sender.Identifier.Value)
		assert.Equal(t, "0216", doc.Transmission.Sender.Identifier.SchemeID)
		assert.Equal(t, senderOperator, doc.Transmission.Sender.Intermediator)
		assert.Equal(t, "003745678907", doc.Transmission.Receiver.Identifier.Value)
		assert.Equal(t, receiverOperator, doc.Transmission.Receiver.Intermediator)
		assert.Equal(t, "3a4b1f4e-7b1c-4a1a-9c2e-0d5f6a7b8c9d", doc.Transmission.Message.Identifier)
		assert.Equal(t, "2026-09-01T08:30:00Z", doc.Transmission.Message.Timestamp)
	})

	t.Run("no receiver operator, no frame", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Ext = inv.Customer.Ext.Delete(finvoice.ExtKeyOperator)
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.Transmission)
		assert.Equal(t, "003776543212", doc.SellerOrganisationUnitNumber)
		assert.Equal(t, "003745678907", doc.BuyerOrganisationUnitNumber)
	})

	t.Run("receiver operator without the sender's fails", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		_, err := fifinvoice.Convert(env)
		require.ErrorIs(t, err, fifinvoice.ErrSenderOperatorRequired)
	})

	t.Run("customer without an address fails", func(t *testing.T) {
		for _, withOperator := range []bool{true, false} {
			env, inv := exampleEnvelope(t, "invoice")
			inv.Customer.Endpoints = nil
			if !withOperator {
				inv.Customer.Ext = inv.Customer.Ext.Delete(finvoice.ExtKeyOperator)
			}
			require.NoError(t, env.Calculate())
			_, err := fifinvoice.Convert(env, testOptions()...)
			require.ErrorContains(t, err, "customer needs an e-invoice address")
		}
	})

	t.Run("message identifier", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc := convertAdjusted(t, env, fifinvoice.WithMessageID("MSG-1"))
		assert.Equal(t, "MSG-1", doc.Transmission.Message.Identifier)
	})
}

// TestConvertCreditNote checks the sign convention: a credit note is a zero
// or negative document in Finvoice.
func TestConvertCreditNote(t *testing.T) {
	env, _ := exampleEnvelope(t, "credit-note")
	doc, err := fifinvoice.Convert(env, testOptions()...)
	require.NoError(t, err)
	assert.Equal(t, "INV02", doc.InvoiceDetails.TypeCode.Value)
	assert.Equal(t, "SPY", doc.InvoiceDetails.TypeCode.CodeList)
	assert.Equal(t, "CREDIT NOTE", doc.InvoiceDetails.TypeText)
	assert.Equal(t, "381", doc.InvoiceDetails.TypeCodeUN)
	assert.Equal(t, "-56,75", doc.InvoiceDetails.TotalVatIncludedAmount.Value)
	assert.Equal(t, "-56,75", doc.Epi.PaymentInstruction.InstructedAmount.Value)
	assert.Equal(t, "-2", doc.Rows[0].InvoicedQuantity[0].Value)
	assert.Equal(t, "25,00", doc.Rows[0].UnitPriceAmount.Value)
	assert.Equal(t, "-50,00", doc.Rows[0].VatExcludedAmount.Value)
	assert.Equal(t, "2026-1001", doc.InvoiceDetails.OriginalInvoiceNumber)
}

func TestConvertTypes(t *testing.T) {
	tests := []struct {
		name         string
		adjust       func(inv *bill.Invoice)
		code, codeUN string
	}{
		{"standard", func(*bill.Invoice) {}, "INV01", "380"},
		{"proforma", func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma }, "INV06", "325"},
		{"self-billed", func(inv *bill.Invoice) { inv.SetTags(tax.TagSelfBilled) }, "INV07", "389"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, "invoice")
			tt.adjust(inv)
			doc := convertAdjusted(t, env)
			assert.Equal(t, tt.code, doc.InvoiceDetails.TypeCode.Value)
			assert.Equal(t, tt.codeUN, doc.InvoiceDetails.TypeCodeUN)
		})
	}
}

func TestConvertPaymentOrder(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(inv *bill.Invoice)
		check  func(t *testing.T, doc *fifinvoice.Document)
	}{
		{"RF creditor reference", func(*bill.Invoice) {}, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "ISO", doc.Epi.PaymentInstruction.RemittanceInfoIdentifier.Scheme)
			assert.Equal(t, "SLEV", doc.Epi.PaymentInstruction.Charge.Option)
		}},
		{"Finnish reference", func(inv *bill.Invoice) {
			inv.Payment.Instructions.Ref = "1232"
			inv.Payment.Instructions.Key = pay.MeansKeyCreditTransfer
		}, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "SPY", doc.Epi.PaymentInstruction.RemittanceInfoIdentifier.Scheme)
			assert.Equal(t, "SHA", doc.Epi.PaymentInstruction.Charge.Option)
			assert.Equal(t, "30", doc.Epi.PaymentInstruction.PaymentMeansCode)
		}},
		{"other reference", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "LASKU1001" }, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
			assert.Equal(t, "LASKU1001", doc.Epi.Identification.Reference)
		}},
		{"no BIC", func(inv *bill.Invoice) { inv.Payment.Instructions.CreditTransfer[0].BIC = "" }, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Nil(t, doc.Epi.Party.BFI.Identifier)
			assert.Nil(t, doc.SellerInformation.Accounts)
		}},
		{"payee", func(inv *bill.Invoice) {
			inv.Payment.Payee = &org.Party{Name: "Perintä Oy"}
		}, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "Perintä Oy", doc.Epi.Party.Beneficiary.NameAddress)
		}},
		{"advance paid", func(inv *bill.Invoice) {
			inv.Payment.Advances = []*pay.Record{{Amount: num.MakeAmount(8836, 2), Description: "Ennakko"}}
		}, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "88,36", doc.InvoiceDetails.PaidAmount.Value)
			assert.Equal(t, "1000,00", doc.Epi.PaymentInstruction.InstructedAmount.Value)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, "invoice")
			tt.adjust(inv)
			tt.check(t, convertAdjusted(t, env))
		})
	}

	t.Run("two due dates fail", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		half := num.MakePercentage(50, 2)
		inv.Payment.Terms.DueDates = []*pay.DueDate{
			{Date: cal.NewDate(2026, 9, 15), Percent: &half},
			{Date: cal.NewDate(2026, 10, 15), Percent: &half},
		}
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "2 due dates")
	})
	t.Run("long reference fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Instructions.Ref = cbc.Code("RF18" + strings.Repeat("5390075470", 4))
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "payment reference")
	})
}

// TestConvertLimits checks long texts are cut or split to the schema's
// lengths in characters, and that identifiers never are.
func TestConvertLimits(t *testing.T) {
	t.Run("names and streets", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Name = strings.Repeat("ä", 80) + " " + strings.Repeat("ö", 30)
		inv.Customer.Addresses[0].Street = strings.Repeat("ö", 40)
		inv.Lines[0].Item.Name = strings.Repeat("å", 120)
		doc := convertAdjusted(t, env)
		require.Len(t, doc.Seller.Name, 2)
		assert.Equal(t, 70, utf8.RuneCountInString(doc.Seller.Name[0]))
		assert.Equal(t, strings.Repeat("ä", 10)+" "+strings.Repeat("ö", 30), doc.Seller.Name[1])
		assert.Equal(t, 35, utf8.RuneCountInString(doc.Buyer.Address.StreetName[0]))
		assert.Equal(t, 100, utf8.RuneCountInString(doc.Rows[0].ArticleName))
	})
	t.Run("one-character tail joins the previous line", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Name = strings.Repeat("ä", 69) + " b"
		inv.Customer.Name = strings.Repeat("ö", 60) + " " + strings.Repeat("ö", 8) + " c"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{strings.Repeat("ä", 68), "ä b"}, doc.Seller.Name)
		assert.Equal(t, []string{strings.Repeat("ö", 60), strings.Repeat("ö", 8) + " c"}, doc.Buyer.Name)
	})
	t.Run("one-character optional fields left out", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Alias = "X"
		inv.Supplier.Addresses[0].Region = "Y"
		inv.Supplier.Addresses[0].PostOfficeBox = "Z"
		inv.Customer.Addresses[0].Locality = "W"
		doc := convertAdjusted(t, env)
		assert.Empty(t, doc.Seller.TradingName)
		assert.Empty(t, doc.Seller.Address.Subdivision)
		assert.Empty(t, doc.Seller.Address.PostOfficeBox)
		assert.Nil(t, doc.Buyer.Address)
	})
	t.Run("texts split at words", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Terms.Notes = "Maksuehto 14 päivää netto, viivästyskorko korkolain mukaan ja perintäkulut peritään erikseen"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{
			"Maksuehto 14 päivää netto, viivästyskorko korkolain mukaan ja",
			"perintäkulut peritään erikseen",
		}, doc.InvoiceDetails.PaymentTerms[0].FreeText)
	})
	t.Run("invoice number too long fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Series = "MYYNTI-2026-HELSINKI"
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "invoice number")
	})
	t.Run("long email left out", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Emails[0].Address = strings.Repeat("a", 70) + "@myyja.example"
		doc := convertAdjusted(t, env)
		assert.Empty(t, doc.SellerCommunication.Email)
	})
	t.Run("long website left out", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Websites[0].URL = "https://myyja.example/" + strings.Repeat("a", 60)
		doc := convertAdjusted(t, env)
		assert.Empty(t, doc.SellerInformation.Website)
	})
	t.Run("long quantity fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Lines[0].Quantity = num.MakeAmount(1234567890123456, 5)
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, `quantity "12345678901,23456"`)
	})
	t.Run("identifiers too long fail", func(t *testing.T) {
		long := strings.Repeat("9", 80)
		tests := []struct {
			name, want string
			adjust     func(inv *bill.Invoice)
		}{
			{"legal identity", `legal identity "9999`, func(inv *bill.Invoice) {
				inv.Customer.TaxID = nil
				inv.Customer.Identities = []*org.Identity{{Scope: org.IdentityScopeLegal, Code: cbc.Code(long)}}
			}},
			{"buyer reference", `buyer reference "9999`, func(inv *bill.Invoice) { inv.Ordering.Code = cbc.Code(long) }},
			{"order reference", `order reference "9999`, func(inv *bill.Invoice) {
				inv.Ordering.Purchases = []*org.DocumentRef{{Code: cbc.Code(long)}}
			}},
			{"article identifier", `article identifier "9999`, func(inv *bill.Invoice) { inv.Lines[0].Item.Ref = cbc.Code(long) }},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				env, inv := exampleEnvelope(t, "invoice")
				tt.adjust(inv)
				require.NoError(t, env.Calculate())
				_, err := fifinvoice.Convert(env, testOptions()...)
				require.ErrorContains(t, err, tt.want)
			})
		}
	})
	t.Run("caller identifiers out of range fail", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, fifinvoice.WithSenderOperator("X"))
		require.ErrorContains(t, err, `sender operator "X"`)
		_, err = fifinvoice.Convert(env, fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithMessageID(strings.Repeat("m", 49)))
		require.ErrorContains(t, err, "message identifier")
	})
}

// TestConvertOmissions pins what Finvoice has no room for.
func TestConvertOmissions(t *testing.T) {
	t.Run("delivery receiver without an address", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Delivery.Receiver.Addresses = nil
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.DeliveryParty)
		assert.NotNil(t, doc.DeliveryDetails)
	})
	t.Run("address without a post code", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Customer.Addresses[0].Code = ""
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.Buyer.Address)
	})
	t.Run("address without a street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Espoo"}, doc.Buyer.Address.StreetName)
	})
}

// TestConvertTotalsAddUp checks the identities the totals must satisfy: row
// amounts at the currency's precision (BR-DEC-23), rows adding up to the
// rows total (BR-CO-10) and the VAT-inclusive total being the sum of the
// exclusive total and the VAT (BR-CO-15).
func TestConvertTotalsAddUp(t *testing.T) {
	tests := []struct {
		name, example string
		adjust        func(inv *bill.Invoice)
	}{
		{"invoice", "invoice", func(*bill.Invoice) {}},
		{"credit note", "credit-note", func(*bill.Invoice) {}},
		{"reverse charge", "reverse-charge", func(*bill.Invoice) {}},
		{"line total with more decimals than the currency", "invoice", func(inv *bill.Invoice) {
			inv.Lines[1].Quantity = num.MakeAmount(3, 0)
			inv.Lines[1].Item.Price = num.NewAmount(10555, 3)
		}},
		{"prices include VAT", "invoice", func(inv *bill.Invoice) { inv.Tax.PricesInclude = tax.CategoryVAT }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, tt.example)
			tt.adjust(inv)
			doc := convertAdjusted(t, env)
			det := doc.InvoiceDetails

			rows := num.MakeAmount(0, 2)
			for _, r := range doc.Rows {
				_, decimals, _ := strings.Cut(r.VatExcludedAmount.Value, ",")
				assert.LessOrEqual(t, len(decimals), 2, "BR-DEC-23: %s", r.VatExcludedAmount.Value)
				rows = rows.Add(amountOf(t, r.VatExcludedAmount))
			}
			assert.Equal(t, amountOf(t, det.RowsTotalVatExcludedAmount).String(), rows.String(), "BR-CO-10")
			inclusive := amountOf(t, det.TotalVatExcludedAmount).Add(amountOf(t, det.TotalVatAmount))
			assert.Equal(t, amountOf(t, det.TotalVatIncludedAmount).String(), inclusive.String(), "BR-CO-15")
		})
	}
}

func amountOf(t *testing.T, a *fifinvoice.Amount) num.Amount {
	t.Helper()
	require.NotNil(t, a)
	v, err := num.AmountFromString(strings.Replace(a.Value, ",", ".", 1))
	require.NoError(t, err)
	return v
}

// TestConvertRejectsOtherDocuments guards the entry point.
func TestConvertRejectsOtherDocuments(t *testing.T) {
	env := gobl.NewEnvelope()
	_, err := fifinvoice.Convert(env, testOptions()...)
	require.ErrorIs(t, err, fifinvoice.ErrUnsupportedDocumentType)

	t.Run("addon added and its rules applied", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Addons = tax.WithAddons(en16931.V2017)
		inv.Customer = nil
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "customer")
	})
}
