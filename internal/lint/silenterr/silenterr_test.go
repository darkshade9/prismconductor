package silenterr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"prismconductor/internal/lint/silenterr"
)

func TestSilenterr(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), silenterr.Analyzer, "a")
}
