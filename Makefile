.PHONY: build test lint spec snapshot

build:
	go build -o bin/academic-page .

test:
	go test ./...

lint:
	golangci-lint run ./...

# Refresh the embedded API spec. SPEC_URL defaults to production; point it at
# a local app to pick up unreleased API changes, for example:
#   make spec SPEC_URL=http://academic-turbo-raven.test/api/v1/openapi.json
# The servers entry is reset to production either way, so the embedded
# document never names a local host.
SPEC_URL ?= https://academic.page/api/v1/openapi.json
PRODUCTION_API := https://academic.page/api/v1

spec:
	curl -fsS -H 'Accept: application/json' '$(SPEC_URL)' | jq --arg url '$(PRODUCTION_API)' '.servers = [{"url": $$url}]' > internal/spec/openapi.json.tmp
	mv internal/spec/openapi.json.tmp internal/spec/openapi.json

snapshot:
	goreleaser release --snapshot --clean --skip=publish
