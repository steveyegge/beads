package lint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzerFiresOnSprintfWithPgx(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "bad")
}
