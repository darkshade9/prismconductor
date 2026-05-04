package main

import (
	_ "embed"

	"prismconductor/internal/remoteworker"
)

//go:embed worker/dist/worker.js
var workerBundleBytes []byte

func init() {
	remoteworker.SetWorkerBundle(workerBundleBytes)
}
