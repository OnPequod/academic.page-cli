.PHONY: build test lint spec snapshot

build:
	go build -o bin/academic-page .

test:
	go test ./...

lint:
	golangci-lint run ./...

# Refresh the embedded API spec from production.
spec:
	curl -fsS -H 'Accept: application/json' https://academic.page/api/v1/openapi.json | jq . > internal/spec/openapi.json

snapshot:
	goreleaser release --snapshot --clean --skip=publish
