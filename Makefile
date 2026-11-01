.PHONY: lint test build release-go

VERSION ?= dev

lint:
	test -z "$$(gofmt -l cmd internal/client)"
	go vet ./...
	python3 scripts/check_go_descriptor.py
	npm run lint
	python3 scripts/validate.py
	git diff --check

test:
	go test -race -timeout 120s ./...
	npm test
	python3 -m unittest discover -s scripts -p 'test_*.py' -v

build:
	go build -trimpath -ldflags '-X main.version=$(VERSION)' -o bin/reah-mcp ./cmd/reah-mcp
	npm run build
	python3 scripts/package.py

release-go:
	python3 scripts/build_go.py --version '$(VERSION)'
