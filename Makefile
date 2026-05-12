.PHONY: refresh-model-hints build test vet

## Refresh the external model capability snapshot from hermesguide.xyz.
## Commit the result (internal/llm/external_models.json) to ship updated hints.
refresh-model-hints:
	go run internal/llm/refresh_external.go

build:
	wails build

test:
	go test ./...

vet:
	go vet ./...
