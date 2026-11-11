.PHONY: lint test build release-go prepare-enclave
.DEFAULT_GOAL := lint

VERSION ?= dev
ifeq ($(shell uname -s),Darwin)
GO_TAGS = -tags secureenclave
prepare-enclave:
	python3 scripts/build_enclave.py
else
prepare-enclave:
endif

lint: prepare-enclave
	test -z "$$(gofmt -l cmd internal)"
	go vet $(GO_TAGS) ./...
	python3 scripts/validate.py
	git diff --check

test: prepare-enclave
	go test $(GO_TAGS) -race -timeout 120s ./...
	python3 -m unittest discover -s scripts -p 'test_*.py' -v

build: prepare-enclave
	go build $(GO_TAGS) -trimpath -ldflags '-X main.version=$(VERSION)' -o bin/reah-mcp ./cmd/reah-mcp
	python3 scripts/package.py

release-go:
	python3 scripts/build_go.py --version '$(VERSION)'
