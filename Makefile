.PHONY: build install test integration package
PREFIX ?= $(HOME)/.local
VERSION ?= dev
COMMIT := $(shell git rev-parse HEAD)
build:
	go build -trimpath -ldflags '-X main.version=$(VERSION) -X main.commit=$(COMMIT)' -o bin/harness ./cmd/harness
install: build
	install -d "$(PREFIX)/bin"
	install -m 755 bin/harness "$(PREFIX)/bin/harness"
test:
	go test -race ./...
integration:
	HARNESS_DOCKER_TEST=1 go test -count=1 ./internal/sandbox ./internal/runner
package:
	sh scripts/package.sh $(VERSION) $(shell go env GOOS) $(shell go env GOARCH)
