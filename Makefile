.PHONY: lint test build release-go

VERSION ?= dev

lint:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	python3 scripts/validate.py
	git diff --check

test:
	go test -race -timeout 120s ./...
	python3 -m unittest discover -s scripts -p 'test_*.py' -v

build:
	go build -trimpath -ldflags '-X main.version=$(VERSION)' -o bin/reah-mcp ./cmd/reah-mcp
	python3 scripts/package.py

release-go:
	python3 scripts/build_go.py --version '$(VERSION)'
