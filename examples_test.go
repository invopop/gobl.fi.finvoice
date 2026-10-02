package fifinvoice_test

import (
	"flag"
	"testing"

	"github.com/invopop/gobl/pkg/examples"
)

var update = flag.Bool("update", false, "update the golden files")

// TestExamples turns every document under examples/ into a calculated,
// validated JSON envelope and compares it with its golden output.
func TestExamples(t *testing.T) {
	examples.Run(t, "examples", *update)
}
