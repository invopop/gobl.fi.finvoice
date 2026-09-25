package fifinvoice_test

import (
	"flag"
	"testing"

	// Register the addon so example documents declaring fi-finvoice-v3
	// normalize and validate.
	_ "github.com/invopop/gobl.fi.finvoice/addon"

	"github.com/invopop/gobl/pkg/examples"
)

var update = flag.Bool("update", false, "update the golden files")

// TestExamples turns every document under examples/ into a calculated,
// validated JSON envelope and compares it with its golden output.
func TestExamples(t *testing.T) {
	examples.Run(t, "examples", *update)
}
