package main

import (
	_ "embed"

	"prismconductor/internal/remoteworker"
)

//go:embed worker/dist/worker.js
var workerBundleBytes []byte

//go:embed worker/sandbox-deploy/dist/worker.js
var sandboxWorkerBundleBytes []byte

func init() {
	remoteworker.SetWorkerBundle(workerBundleBytes)
	remoteworker.SetSandboxWorkerBundle(sandboxWorkerBundleBytes)
}
