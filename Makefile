.PHONY: lint test build schema

lint:
	@test -z "$$(gofmt -l cmd internal scripts/schema)" || (gofmt -l cmd internal scripts/schema; exit 1)
	go vet ./...
	python3 scripts/validate.py

test:
	go test -race -shuffle=on ./...

build:
	go build -trimpath -o bin/reah-mcp ./cmd/reah-mcp

schema:
	go run ./scripts/schema
