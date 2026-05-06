// Command silenterr is a go vet vettool that runs the silenterr analyzer.
// Build it with: go build -o /tmp/silenterr ./internal/lint/silenterr/cmd/silenterr
// Use it with:   go vet -vettool=/tmp/silenterr ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"prismconductor/internal/lint/silenterr"
)

func main() {
	multichecker.Main(silenterr.Analyzer)
}
