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
	validXML(t, doc)
	return doc
}

// validXML renders the document and checks it against the Finvoice schema.
func validXML(t *testing.T, doc *fifinvoice.Document) []byte {
	t.Helper()
	data, err := doc.Bytes()
	require.NoError(t, err)
	schema := loadSchema(t)
	defer schema.Free()
	assertValidXML(t, schema, data)
	return data
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

	t.Run("a self-billed invoice goes from the customer to the supplier", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.SetTags(tax.TagSelfBilled)
		inv.Supplier.Ext = tax.ExtensionsOf(cbc.CodeMap{finvoice.ExtKeyOperator: "003700030003"})
		doc := convertAdjusted(t, env)
		require.NotNil(t, doc.Transmission)
		assert.Equal(t, "003745678907", doc.Transmission.Sender.Identifier.Value)
		assert.Equal(t, "003776543212", doc.Transmission.Receiver.Identifier.Value)
		assert.Equal(t, "003700030003", doc.Transmission.Receiver.Intermediator)

		inv.Supplier.Ext = inv.Supplier.Ext.Delete(finvoice.ExtKeyOperator)
		assert.Nil(t, convertAdjusted(t, env).Transmission, "the receiver names no operator")

		inv.Supplier.Endpoints = nil
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "supplier needs an e-invoice address")
	})
	t.Run("no receiver operator, no frame", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Ext = inv.Customer.Ext.Delete(finvoice.ExtKeyOperator)
		doc := convertAdjusted(t, env)
		assert.Nil(t, doc.Transmission)
		assert.Equal(t, "003776543212", doc.SellerOrganisationUnitNumber)
		assert.Equal(t, "003745678907", doc.BuyerOrganisationUnitNumber)
	})

	t.Run("message identifier", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		doc := convertAdjusted(t, env, fifinvoice.WithMessageID("MSG-1"))
		assert.Equal(t, "MSG-1", doc.Transmission.Message.Identifier)
	})
	t.Run("receiver operator without the sender's fails", func(t *testing.T) {
		env, _ := exampleEnvelope(t, "invoice")
		_, err := fifinvoice.Convert(env)
		require.ErrorIs(t, err, fifinvoice.ErrSenderOperatorRequired)
	})

	t.Run("a coded inbox serves as the e-invoice address", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Endpoints = nil
		inv.Customer.Inboxes = []*org.Inbox{{Scheme: "0216", Code: "003745678907"}}
		doc := convertAdjusted(t, env)
		assert.Equal(t, "003745678907", doc.BuyerOrganisationUnitNumber)
		assert.Equal(t, fifinvoice.Identifier{Value: "003745678907", SchemeID: "0216"}, doc.Transmission.Receiver.Identifier)
	})
	t.Run("a null list entry is pruned before writing", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Payment.Terms.DueDates = append(inv.Payment.Terms.DueDates, nil)
		inv.Lines = append(inv.Lines, nil)
		doc, err := fifinvoice.Convert(env, testOptions()...)
		require.NoError(t, err)
		assert.Len(t, doc.Rows, len(inv.Lines))
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
		name               string
		adjust             func(inv *bill.Invoice)
		code, codeUN, text string
	}{
		{"standard", func(*bill.Invoice) {}, "INV01", "380", "INVOICE"},
		{"proforma", func(inv *bill.Invoice) { inv.Type = bill.InvoiceTypeProforma }, "INV06", "325", "PRO FORMA INVOICE"},
		{"self-billed", func(inv *bill.Invoice) { inv.SetTags(tax.TagSelfBilled) }, "INV07", "389", "SELFBILLING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, inv := exampleEnvelope(t, "invoice")
			tt.adjust(inv)
			doc := convertAdjusted(t, env)
			assert.Equal(t, tt.code, doc.InvoiceDetails.TypeCode.Value)
			assert.Equal(t, tt.codeUN, doc.InvoiceDetails.TypeCodeUN)
			assert.Equal(t, tt.text, doc.InvoiceDetails.TypeText)
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
		{"Finnish reference with a wrong check digit", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "1001" }, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
			assert.Equal(t, "1001", doc.Epi.Identification.Reference)
		}},
		{"RF reference with a wrong checksum", func(inv *bill.Invoice) { inv.Payment.Instructions.Ref = "RF00539007547034" }, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Nil(t, doc.Epi.PaymentInstruction.RemittanceInfoIdentifier)
		}},
		{"payment means text", func(inv *bill.Invoice) { inv.Payment.Instructions.Detail = "Tilisiirto" }, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "Tilisiirto", doc.Epi.PaymentInstruction.PaymentMeansText)
		}},
		{"payee name cut at a space", func(inv *bill.Invoice) {
			inv.Payment.Payee = &org.Party{Name: "Helsingin Kaupungin Asuntotuotanto Oy Ab"}
		}, func(t *testing.T, doc *fifinvoice.Document) {
			assert.Equal(t, "Helsingin Kaupungin Asuntotuotanto", doc.Epi.Party.Beneficiary.NameAddress)
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

}

// TestConvertLimits checks long texts are cut or split to the schema's
// lengths in characters, and that identifiers never are.
func TestConvertLimits(t *testing.T) {
	t.Run("names and notes take as many elements as they need", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Name = strings.TrimSpace(strings.Repeat("Rakennuspalvelu ", 12))
		note := strings.TrimSpace(strings.Repeat("Toimitus sisältää asennuksen ja käyttöönoton. ", 150))
		inv.Notes = []*org.Note{{Key: org.NoteKeyGeneral, Text: note}}
		doc := convertAdjusted(t, env)
		assert.Len(t, doc.Seller.Name, 3)
		assert.Equal(t, inv.Supplier.Name, strings.Join(doc.Seller.Name, " "))
		assert.Equal(t, note, strings.Join(doc.InvoiceDetails.FreeText, " "))
	})
	t.Run("a price finer than five decimals is given per a base quantity", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Lines[0].Quantity = num.MakeAmount(1000000, 0)
		inv.Lines[0].Item.Price = num.NewAmount(12345, 7)
		inv.Lines[0].Discounts = nil
		doc := convertAdjusted(t, env)
		row := doc.Rows[0]
		assert.Equal(t, "0,12345", row.UnitPriceAmount.Value)
		require.NotNil(t, row.UnitPriceBaseQuantity)
		assert.Equal(t, "100", row.UnitPriceBaseQuantity.Value)
		assert.Equal(t, "1234,50", row.VatExcludedAmount.Value)
	})
	t.Run("several row discounts are all progressive", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		five := num.MakePercentage(5, 2)
		inv.Lines[0].Discounts = []*bill.LineDiscount{
			{Percent: &five, Reason: "Määräalennus"},
			{Amount: num.MakeAmount(1000, 2), Reason: "Kampanja"},
		}
		doc := convertAdjusted(t, env)
		row := doc.Rows[0]
		assert.Empty(t, row.DiscountPercent)
		assert.Nil(t, row.DiscountAmount)
		require.Len(t, row.ProgressiveDiscount, 2)
		assert.Equal(t, "Kampanja", row.ProgressiveDiscount[1].TypeText)
	})
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
	t.Run("a one-character last word stays with the word before it", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Delivery.Receiver.Name = "Asunto Oy Helsingin Mannerheimintie 5"
		inv.Customer.Addresses[0].Street = "Kuninkaankartanonkatu Pohjoinen"
		inv.Customer.Addresses[0].Number = "12 B"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Asunto Oy Helsingin", "Mannerheimintie 5"}, doc.DeliveryParty.Name)
		assert.Equal(t, []string{"Kuninkaankartanonkatu Pohjoinen", "12 B"}, doc.Buyer.Address.StreetName[:2])
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
	t.Run("an email and a web address of 70 are kept, of 71 left out", func(t *testing.T) {
		for _, n := range []int{70, 71} {
			env, inv := exampleEnvelope(t, "invoice")
			email := strings.Repeat("a", n-len("@myyja.fi")) + "@myyja.fi"
			website := "https://myyja.fi/" + strings.Repeat("a", n-len("https://myyja.fi/"))
			inv.Supplier.Emails[0].Address = email
			inv.Supplier.Websites[0].URL = website
			doc := convertAdjusted(t, env)
			if n == 70 {
				assert.Equal(t, email, doc.SellerCommunication.Email)
				assert.Equal(t, website, doc.SellerInformation.Website)
			} else {
				assert.Empty(t, doc.SellerCommunication.Email)
				assert.Empty(t, doc.SellerInformation.Website)
			}
		}
	})
	t.Run("display texts are cut to their elements", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "reverse-charge")
		inv.Lines[0].Discounts[0].Reason = strings.Repeat("alennus ", 6)
		inv.Tax.Notes[0].Text = strings.TrimSpace(strings.Repeat("Käännetty verovelvollisuus ", 10))
		doc := convertAdjusted(t, env)
		assert.Equal(t, 35, utf8.RuneCountInString(doc.Rows[0].ProgressiveDiscount[0].TypeText))
		assert.Len(t, doc.InvoiceDetails.VatSpecifications[0].FreeText, 3)
	})
	t.Run("an invoice number too long fails", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Series = "MYYNTI-2026-HELSINKI"
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "invoice number with its series must be at most 20 characters")
	})
}

// TestConvertMappings pins the fields the examples leave out.
func TestConvertMappings(t *testing.T) {
	env, inv := exampleEnvelope(t, "invoice")
	inv.Supplier.Alias = "Myyjä"
	inv.Supplier.Addresses[0].Region = "Uusimaa"
	inv.Payment.Instructions.CreditTransfer[0].Name = "Myyjä Oy tili"
	inv.Payment.Instructions.CreditTransfer = append(inv.Payment.Instructions.CreditTransfer,
		&pay.CreditTransfer{Number: "12345-678", BIC: "OKOYFIHH"})
	inv.Supplier.Emails = append(inv.Supplier.Emails, &org.Email{Address: "laskutus@myyja.fi"})
	inv.Ordering = &bill.Ordering{Contracts: []*org.DocumentRef{{Code: "SOP-9", IssueDate: cal.NewDate(2026, 1, 15)}}}
	doc := convertAdjusted(t, env, fifinvoice.WithInvoiceURL("LIITE", "file://liite.pdf"))
	assert.Equal(t, "20260115", doc.InvoiceDetails.AgreementDate.Value)
	assert.Equal(t, "Myyjä", doc.Seller.TradingName)
	assert.Equal(t, "Uusimaa", doc.Seller.Address.Subdivision)
	assert.Equal(t, "Myyjä Oy tili", doc.SellerInformation.Accounts[0].Name)
	assert.Equal(t, "Myyjä Oy tili", doc.Epi.Party.BFI.Name)
	require.Len(t, doc.SellerInformation.Accounts, 2)
	assert.Equal(t, fifinvoice.Account{Value: "12345-678", Scheme: "BBAN"}, doc.SellerInformation.Accounts[1].AccountID)
	assert.Equal(t, inv.Supplier.Emails[0].Address, doc.SellerCommunication.Email)
	assert.Equal(t, "laskutus@myyja.fi", doc.SellerInformation.Email, "the second email is the organisation's common address")
	assert.Equal(t, []string{"PDF", "LIITE"}, doc.InvoiceURLNames)
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
	t.Run("percentages finer than three decimals", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		third := num.MakePercentage(333333, 6)
		base := num.MakeAmount(90000, 2)
		inv.Lines[0].Discounts = []*bill.LineDiscount{{Percent: &third, Base: &base, Reason: "Kolmannes"}}
		inv.Charges = []*bill.Charge{{Percent: &third, Reason: "Rahti", Taxes: tax.Set{{Category: tax.CategoryVAT, Rate: tax.RateGeneral}}}}
		doc := convertAdjusted(t, env)
		assert.Empty(t, doc.Rows[0].DiscountPercent)
		assert.Nil(t, doc.Rows[0].DiscountBaseAmount)
		assert.Equal(t, "300,00", doc.Rows[0].DiscountAmount.Value, "the amount stands")
		assert.Empty(t, doc.InvoiceDetails.Charges[0].Percent)
	})
	t.Run("address with a PO box and no street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		inv.Customer.Addresses[0].PostOfficeBox = "PL 123"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"PL 123"}, doc.Buyer.Address.StreetName)
	})
	t.Run("address with a one-digit PO box and no street", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Customer.Addresses[0].Street = ""
		inv.Customer.Addresses[0].StreetExtra = ""
		inv.Customer.Addresses[0].PostOfficeBox = "5"
		doc := convertAdjusted(t, env)
		assert.Equal(t, []string{"Espoo"}, doc.Buyer.Address.StreetName)
		assert.Equal(t, "5", doc.Buyer.Address.PostOfficeBox)
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
// TestConvertOptions pins the bounds the schema puts on the caller's values.
func TestConvertOptions(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		name string
		opts []fifinvoice.Option
		want string
	}{
		{"operator of 35", []fifinvoice.Option{fifinvoice.WithSenderOperator(long(35))}, ""},
		{"message identifier of 48", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithMessageID(long(48))}, ""},
		{"URL and name of 512", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithInvoiceURL(long(512), "file://"+long(505))}, ""},
		{"operator of one character", []fifinvoice.Option{fifinvoice.WithSenderOperator("X")}, "sender operator must be 2 to 35 letters or digits"},
		{"operator of 36", []fifinvoice.Option{fifinvoice.WithSenderOperator(long(36))}, "sender operator must be 2 to 35 letters or digits"},
		{"operator of punctuation", []fifinvoice.Option{fifinvoice.WithSenderOperator("--")}, "sender operator must be 2 to 35 letters or digits"},
		{"message identifier of one character", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithMessageID("1")}, "message identifier must be 2 to 48 characters"},
		{"message identifier of 49", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithMessageID(long(49))}, "message identifier must be 2 to 48 characters"},
		{"URL name of 513", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithInvoiceURL(long(513), "file://a.pdf")}, "invoice URL name must be at most 512 characters"},
		{"URL of 513", []fifinvoice.Option{fifinvoice.WithSenderOperator(senderOperator), fifinvoice.WithInvoiceURL("PDF", "file://"+long(506))}, "invoice URL must be at most 512 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := exampleEnvelope(t, "invoice")
			doc, err := fifinvoice.Convert(env, tt.opts...)
			if tt.want == "" {
				require.NoError(t, err)
				validXML(t, doc)
				return
			}
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestConvertRejectsOtherDocuments(t *testing.T) {
	env := gobl.NewEnvelope()
	_, err := fifinvoice.Convert(env, testOptions()...)
	require.ErrorIs(t, err, fifinvoice.ErrUnsupportedDocumentType)

	t.Run("the addon is added to an invoice without it", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Addons = tax.WithAddons(en16931.V2017)
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.NoError(t, err)
		assert.True(t, finvoice.V3.In(inv.GetAddons()...))
	})
	t.Run("and its rules applied", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Addons = tax.WithAddons(en16931.V2017)
		inv.Payment.Instructions.Ref = ""
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "GOBL-FI-FINVOICE-BILL-INVOICE-06")
	})
	t.Run("a supplier without an e-invoice address when framed", func(t *testing.T) {
		env, inv := exampleEnvelope(t, "invoice")
		inv.Supplier.Endpoints = nil
		require.NoError(t, env.Calculate())
		_, err := fifinvoice.Convert(env, testOptions()...)
		require.ErrorContains(t, err, "supplier needs an e-invoice address")
	})
}
